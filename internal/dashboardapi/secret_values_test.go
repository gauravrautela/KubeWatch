package dashboardapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/gauravrautela/kubewatch/internal/dashboardapi"
	"github.com/gauravrautela/kubewatch/internal/event"
	"github.com/gauravrautela/kubewatch/internal/ingest"
	"github.com/gauravrautela/kubewatch/internal/storage"
	"github.com/gauravrautela/kubewatch/internal/webhook"
)

// The values a tester puts in the Secret. None of them may appear anywhere in
// what the dashboard's API serves.
const (
	valueUserBefore     = "YWRtaW4="
	valuePasswordBefore = "czNjcjN0"
	valuePasswordAfter  = "bjN3cDQ1cw=="
	valueTokenAfter     = "dG9rZW4="
)

// capturingInserter stands in for ClickHouse, keeping what the hub would store.
type capturingInserter struct {
	mu   sync.Mutex
	rows []event.ChangeEvent
}

func (c *capturingInserter) InsertBatch(_ context.Context, events []event.ChangeEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rows = append(c.rows, events...)
	return nil
}

func (c *capturingInserter) stored() []event.ChangeEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]event.ChangeEvent(nil), c.rows...)
}

// storedStore serves what the hub stored through the dashboard's read surface.
type storedStore struct {
	row    storage.Row
	detail storage.Detail
}

func (s *storedStore) ListEvents(context.Context, storage.ListParams) (storage.Page, error) {
	return storage.Page{Rows: []storage.Row{s.row}}, nil
}

func (s *storedStore) GetEvent(context.Context, string) (storage.Detail, bool, error) {
	return s.detail, true, nil
}

func (s *storedStore) Activity(context.Context, storage.Filter, string) ([]storage.Bucket, error) {
	return nil, nil
}

func (s *storedStore) Facets(context.Context) (storage.Facets, error) {
	return storage.Facets{}, nil
}

func (s *storedStore) IncidentEvents(context.Context, storage.IncidentFilter) ([]storage.IncidentRow, error) {
	return nil, nil
}

func (s *storedStore) ResourceStats(context.Context, string, time.Time) (map[storage.ResourceKey]storage.ResourceStats, error) {
	return nil, nil
}

// secretUpdate is the AC-004 change: one update that adds token, changes
// password and removes user.
func secretUpdate() *admissionv1.AdmissionReview {
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"},
		Request: &admissionv1.AdmissionRequest{
			UID:       types.UID("abc"),
			Operation: admissionv1.Update,
			Kind:      metav1.GroupVersionKind{Version: "v1", Kind: "Secret"},
			Namespace: "default",
			Name:      "creds",
			UserInfo:  authenticationv1.UserInfo{Username: "alice", UID: "u1"},
			OldObject: runtime.RawExtension{Raw: []byte(
				`{"kind":"Secret","metadata":{"uid":"xyz","name":"creds"},"data":{"user":"` + valueUserBefore + `","password":"` + valuePasswordBefore + `"}}`)},
			Object: runtime.RawExtension{Raw: []byte(
				`{"kind":"Secret","metadata":{"uid":"xyz","name":"creds"},"data":{"password":"` + valuePasswordAfter + `","token":"` + valueTokenAfter + `"}}`)},
		},
	}
}

// TestSecretValuesNeverReachAPI drives one Secret change the whole way an
// operator's would go — the agent's webhook, the hub's ingest, the store, and
// the dashboard's list and detail endpoints — and asserts no value appears
// anywhere at the end, while the keys and their operations do.
func TestSecretValuesNeverReachAPI(t *testing.T) {
	// The agent captures the change.
	var captured event.ChangeEvent
	body, err := json.Marshal(secretUpdate())
	if err != nil {
		t.Fatal(err)
	}
	agent := webhook.NewHandler(func(e event.ChangeEvent) { captured = e })
	agent.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body)))
	if captured.Kind != "Secret" {
		t.Fatal("the agent did not capture the Secret change")
	}

	// The hub receives the batch, diffs and classifies it, and stores it.
	inserter := &capturingInserter{}
	batcher := ingest.NewBatcher(inserter, 10, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { batcher.Run(ctx); close(done) }()

	batch, err := json.Marshal(event.Batch{Events: []event.ChangeEvent{captured}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader(batch))
	req.Header.Set("Authorization", "Bearer devtoken")
	rec := httptest.NewRecorder()
	ingest.NewHandler(ingest.StaticAuth{"devtoken": "local"}, batcher).ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("ingest refused the batch: %d", rec.Code)
	}
	cancel()
	<-done

	stored := inserter.stored()
	if len(stored) != 1 {
		t.Fatalf("want one stored event, got %d", len(stored))
	}
	row := stored[0]

	// The dashboard serves what was stored.
	store := &storedStore{
		row: storage.Row{
			EventID: "11111111-1111-1111-1111-111111111111", Kind: row.Kind,
			Namespace: row.Namespace, Name: row.Name, Operation: string(row.Operation),
			UserName: row.UserName, Diff: row.Diff,
		},
		detail: storage.Detail{
			Row: storage.Row{
				EventID: "11111111-1111-1111-1111-111111111111", Kind: row.Kind,
				Namespace: row.Namespace, Name: row.Name, Operation: string(row.Operation),
				UserName: row.UserName, Diff: row.Diff,
			},
			OldObject: row.OldObject, NewObject: row.NewObject,
		},
	}
	api := dashboardapi.NewRouter(store, "")

	for _, path := range []string{
		"/api/events",
		"/api/events/11111111-1111-1111-1111-111111111111",
	} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
			}
			served := rec.Body.String()
			for _, value := range []string{valueUserBefore, valuePasswordBefore, valuePasswordAfter, valueTokenAfter} {
				if strings.Contains(served, value) {
					t.Fatalf("value %q reached the dashboard API: %s", value, served)
				}
			}
			// What an auditor needs is still there: the keys, their
			// operations, and who made the change.
			for _, want := range []string{"data.password", "data.token", "data.user", "alice"} {
				if !strings.Contains(served, want) {
					t.Fatalf("%q missing from the response: %s", want, served)
				}
			}
		})
	}

	// The detail endpoint is what the diff view and both copy buttons read.
	if strings.Contains(store.detail.OldObject+store.detail.NewObject, valuePasswordBefore) {
		t.Fatal("a value survived in the bodies the copy buttons serve")
	}
}
