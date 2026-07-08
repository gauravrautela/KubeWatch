package dashboardapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gauravrautela/kubewatch/internal/storage"
)

// incidentFakeStore records the incident query and serves canned rows.
type incidentFakeStore struct {
	fakeStore
	events    []storage.IncidentRow
	stats     map[storage.ResourceKey]storage.ResourceStats
	gotFrom   time.Time
	gotTo     time.Time
	gotWindow time.Time
	eventsErr error
}

func (f *incidentFakeStore) IncidentEvents(_ context.Context, cluster string, from, to time.Time) ([]storage.IncidentRow, error) {
	f.gotFrom, f.gotTo = from, to
	return f.events, f.eventsErr
}

func (f *incidentFakeStore) ResourceStats(_ context.Context, cluster string, windowStart time.Time) (map[storage.ResourceKey]storage.ResourceStats, error) {
	f.gotWindow = windowStart
	return f.stats, nil
}

func getIncident(t *testing.T, store ReadStore, target string) *httptest.ResponseRecorder {
	t.Helper()
	h := NewRouter(store, "")
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestIncidentRequiresCluster(t *testing.T) {
	rec := getIncident(t, &incidentFakeStore{}, "/api/incident")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestIncidentRejectsBadParams(t *testing.T) {
	for _, target := range []string{
		"/api/incident?cluster=c1&at=yesterday",
		"/api/incident?cluster=c1&lookback=nope",
		"/api/incident?cluster=c1&lookback=25h",
		"/api/incident?cluster=c1&lookback=-1h",
		"/api/incident?cluster=c1&limit=0",
		"/api/incident?cluster=c1&limit=abc",
	} {
		if rec := getIncident(t, &incidentFakeStore{}, target); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", target, rec.Code)
		}
	}
}

func TestIncidentWindowAndResponse(t *testing.T) {
	at := time.Date(2026, 7, 8, 14, 0, 0, 0, time.UTC)
	fake := &incidentFakeStore{
		events: []storage.IncidentRow{{
			EventID: "11111111-1111-1111-1111-111111111111", EventTime: at.Add(-2 * time.Minute),
			Operation: "UPDATE", Kind: "Deployment", Namespace: "payments", Name: "checkout",
			UserName: "alice", ActorType: "human", Classes: []string{"image"},
		}},
		stats: map[storage.ResourceKey]storage.ResourceStats{},
	}
	rec := getIncident(t, fake, "/api/incident?cluster=c1&at=2026-07-08T14:00:00Z&lookback=1h")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if want := at.Add(-time.Hour); !fake.gotFrom.Equal(want) {
		t.Fatalf("from = %v, want %v", fake.gotFrom, want)
	}
	if want := at.Add(10 * time.Minute); !fake.gotTo.Equal(want) {
		t.Fatalf("to = %v, want %v (skew allowance)", fake.gotTo, want)
	}
	if want := at.Add(-time.Hour); !fake.gotWindow.Equal(want) {
		t.Fatalf("stats windowStart = %v, want %v", fake.gotWindow, want)
	}

	var resp struct {
		Incident struct {
			Cluster  string `json:"cluster"`
			Lookback string `json:"lookback"`
		} `json:"incident"`
		Suspects []struct {
			Name    string   `json:"name"`
			Score   int      `json:"score"`
			Reasons []string `json:"reasons"`
			Events  []struct {
				EventID string `json:"event_id"`
			} `json:"events"`
		} `json:"suspects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Incident.Cluster != "c1" || resp.Incident.Lookback != "1h0m0s" {
		t.Fatalf("incident meta = %+v", resp.Incident)
	}
	if len(resp.Suspects) != 1 || resp.Suspects[0].Name != "checkout" {
		t.Fatalf("suspects = %+v", resp.Suspects)
	}
	if resp.Suspects[0].Score != 100 { // fresh human image change caps at 100
		t.Fatalf("score = %d, want 100", resp.Suspects[0].Score)
	}
	if len(resp.Suspects[0].Events) != 1 || resp.Suspects[0].Events[0].EventID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("events = %+v", resp.Suspects[0].Events)
	}
}

func TestIncidentQueryFailure(t *testing.T) {
	fake := &incidentFakeStore{eventsErr: context.DeadlineExceeded}
	rec := getIncident(t, fake, "/api/incident?cluster=c1")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", rec.Code)
	}
}
