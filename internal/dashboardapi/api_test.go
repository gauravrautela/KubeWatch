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

type fakeStore struct {
	page    storage.Page
	detail  storage.Detail
	found   bool
	lastP   storage.ListParams
	listErr error
}

func (f *fakeStore) ListEvents(_ context.Context, p storage.ListParams) (storage.Page, error) {
	f.lastP = p
	return f.page, f.listErr
}
func (f *fakeStore) GetEvent(_ context.Context, id string) (storage.Detail, bool, error) {
	return f.detail, f.found, nil
}
func (f *fakeStore) Activity(_ context.Context, _ storage.Filter, _ string) ([]storage.Bucket, error) {
	return []storage.Bucket{{Count: 5}}, nil
}
func (f *fakeStore) Facets(_ context.Context) (storage.Facets, error) {
	return storage.Facets{Clusters: []string{"c1"}, Namespaces: []string{"default", "kube-system"}}, nil
}
func (f *fakeStore) IncidentEvents(_ context.Context, _ storage.IncidentFilter) ([]storage.IncidentRow, error) {
	return nil, nil
}

func (f *fakeStore) ResourceStats(_ context.Context, _ string, _ time.Time) (map[storage.ResourceKey]storage.ResourceStats, error) {
	return nil, nil
}

func do(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestListEventsParsesFiltersAndClampsLimit(t *testing.T) {
	fs := &fakeStore{page: storage.Page{Rows: []storage.Row{{EventID: "1"}}, NextCursor: "cur"}}
	h := NewRouter(fs, "")
	rec := do(t, h, "/api/events?cluster=c1&name=web&limit=9999")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if fs.lastP.Filter.Cluster != "c1" || fs.lastP.Filter.Name != "web" {
		t.Fatalf("filters not parsed: %+v", fs.lastP.Filter)
	}
	if fs.lastP.Limit != 200 {
		t.Fatalf("limit not clamped to 200, got %d", fs.lastP.Limit)
	}
	var page storage.Page
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.NextCursor != "cur" || len(page.Rows) != 1 {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestListEventsRejectsBadTime(t *testing.T) {
	h := NewRouter(&fakeStore{}, "")
	rec := do(t, h, "/api/events?from=not-a-time")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestListEventsRejectsBadCursor(t *testing.T) {
	h := NewRouter(&fakeStore{}, "")
	rec := do(t, h, "/api/events?cursor=!!bad!!")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestGetEventNotFound(t *testing.T) {
	h := NewRouter(&fakeStore{found: false}, "")
	rec := do(t, h, "/api/events/11111111-1111-1111-1111-111111111111")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

func TestActivityRejectsBadBucket(t *testing.T) {
	h := NewRouter(&fakeStore{}, "")
	rec := do(t, h, "/api/activity?bucket=century")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestFacetsOK(t *testing.T) {
	h := NewRouter(&fakeStore{}, "")
	rec := do(t, h, "/api/facets")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var fc storage.Facets
	if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
		t.Fatal(err)
	}
	if len(fc.Clusters) != 1 || fc.Clusters[0] != "c1" {
		t.Fatalf("unexpected facets: %+v", fc)
	}
}

func TestHealthz(t *testing.T) {
	rec := do(t, NewRouter(&fakeStore{}, ""), "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestListEventsParsesSearchAndExcludes(t *testing.T) {
	fs := &fakeStore{}
	h := NewRouter(fs, "")
	rec := do(t, h, "/api/events?q=ali&exclude_kinds=Lease,Endpoints&exclude_namespaces=kube-system")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	f := fs.lastP.Filter
	if f.Q != "ali" {
		t.Errorf("q not parsed: %+v", f)
	}
	if len(f.ExcludeKinds) != 2 || f.ExcludeKinds[0] != "Lease" || f.ExcludeKinds[1] != "Endpoints" {
		t.Errorf("exclude_kinds not parsed: %+v", f.ExcludeKinds)
	}
	if len(f.ExcludeNamespaces) != 1 || f.ExcludeNamespaces[0] != "kube-system" {
		t.Errorf("exclude_namespaces not parsed: %+v", f.ExcludeNamespaces)
	}
}

func TestListEventsTrimsAndDropsEmptyExcludes(t *testing.T) {
	fs := &fakeStore{}
	h := NewRouter(fs, "")
	rec := do(t, h, "/api/events?exclude_kinds=Lease,%20Endpoints,&exclude_namespaces=,kube-system%20")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	f := fs.lastP.Filter
	if len(f.ExcludeKinds) != 2 || f.ExcludeKinds[0] != "Lease" || f.ExcludeKinds[1] != "Endpoints" {
		t.Errorf("exclude_kinds not trimmed/cleaned: %+v", f.ExcludeKinds)
	}
	if len(f.ExcludeNamespaces) != 1 || f.ExcludeNamespaces[0] != "kube-system" {
		t.Errorf("exclude_namespaces not trimmed/cleaned: %+v", f.ExcludeNamespaces)
	}
}

func TestFacetsIncludesNamespaces(t *testing.T) {
	rec := do(t, NewRouter(&fakeStore{}, ""), "/api/facets")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var fc storage.Facets
	if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
		t.Fatal(err)
	}
	if len(fc.Namespaces) != 2 || fc.Namespaces[0] != "default" {
		t.Fatalf("namespaces missing from facets: %+v", fc)
	}
}

func TestListEventsParsesNewExcludes(t *testing.T) {
	fs := &fakeStore{}
	h := NewRouter(fs, "")
	rec := do(t, h, "/api/events?exclude_users=bot,%20alice,&exclude_clusters=staging&exclude_names=web&exclude_operations=UPDATE")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	f := fs.lastP.Filter
	if len(f.ExcludeUsers) != 2 || f.ExcludeUsers[0] != "bot" || f.ExcludeUsers[1] != "alice" {
		t.Errorf("exclude_users not parsed/trimmed: %+v", f.ExcludeUsers)
	}
	if len(f.ExcludeClusters) != 1 || f.ExcludeClusters[0] != "staging" {
		t.Errorf("exclude_clusters not parsed: %+v", f.ExcludeClusters)
	}
	if len(f.ExcludeNames) != 1 || f.ExcludeNames[0] != "web" {
		t.Errorf("exclude_names not parsed: %+v", f.ExcludeNames)
	}
	if len(f.ExcludeOperations) != 1 || f.ExcludeOperations[0] != "UPDATE" {
		t.Errorf("exclude_operations not parsed: %+v", f.ExcludeOperations)
	}
}
