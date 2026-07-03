package dashboardapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSPAFallbackServesIndexForUnknownPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("INDEX"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("JS"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := NewRouter(&fakeStore{}, dir)

	// A real asset is served as-is.
	rec := do(t, h, "/app.js")
	if rec.Code != http.StatusOK || rec.Body.String() != "JS" {
		t.Fatalf("asset not served: %d %q", rec.Code, rec.Body.String())
	}

	// An unknown client route falls back to index.html.
	rec = do(t, h, "/events/abc")
	if rec.Code != http.StatusOK || rec.Body.String() != "INDEX" {
		t.Fatalf("SPA fallback failed: %d %q", rec.Code, rec.Body.String())
	}

	// The API still wins over static.
	req := httptest.NewRequest(http.MethodGet, "/api/facets", nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK || rec2.Body.String() == "INDEX" {
		t.Fatalf("api route shadowed by static: %d %q", rec2.Code, rec2.Body.String())
	}
}
