// Package dashboardapi serves the read-only KubeWatch dashboard JSON API.
package dashboardapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gauravrautela/kubewatch/internal/storage"
)

const (
	defaultLimit = 50
	maxLimit     = 200
	queryTimeout = 30 * time.Second
)

// ReadStore is the read surface the API needs; satisfied by *storage.Store.
type ReadStore interface {
	ListEvents(ctx context.Context, p storage.ListParams) (storage.Page, error)
	GetEvent(ctx context.Context, id string) (storage.Detail, bool, error)
	Activity(ctx context.Context, f storage.Filter, bucket string) ([]storage.Bucket, error)
	Facets(ctx context.Context) (storage.Facets, error)
}

// Handler holds the API dependencies.
type Handler struct {
	store ReadStore
}

// NewRouter builds the dashboard HTTP handler. If spaDir is non-empty, static
// SPA assets are served from it with index.html fallback for client routes.
func NewRouter(store ReadStore, spaDir string) http.Handler {
	h := &Handler{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", h.listEvents)
	mux.HandleFunc("GET /api/events/{id}", h.getEvent)
	mux.HandleFunc("GET /api/activity", h.activity)
	mux.HandleFunc("GET /api/facets", h.facets)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	if spaDir != "" {
		mux.Handle("GET /", spaFileServer(spaDir))
	}
	return withTimeout(mux)
}

// withTimeout bounds every request (and thus every ClickHouse query issued via
// r.Context()) so a slow or huge query cannot hang the server.
func withTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f, err := parseFilter(q)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cursor := q.Get("cursor")
	since := q.Get("since")
	if cursor != "" {
		if _, err := storage.DecodeCursor(cursor); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid cursor")
			return
		}
	}
	if since != "" {
		if _, err := storage.DecodeCursor(since); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid since cursor")
			return
		}
	}
	page, err := h.store.ListEvents(r.Context(), storage.ListParams{
		Filter: f, Cursor: cursor, Since: since, Limit: parseLimit(q),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *Handler) getEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid event id")
		return
	}
	d, found, err := h.store.GetEvent(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	if !found {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (h *Handler) activity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f, err := parseFilter(q)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	bucket := q.Get("bucket")
	if bucket == "" {
		bucket = "hour"
	}
	if !storage.IsValidBucket(bucket) {
		writeErr(w, http.StatusBadRequest, "invalid bucket")
		return
	}
	buckets, err := h.store.Activity(r.Context(), f, bucket)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, buckets)
}

func (h *Handler) facets(w http.ResponseWriter, r *http.Request) {
	fc, err := h.store.Facets(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, fc)
}

func parseFilter(q url.Values) (storage.Filter, error) {
	f := storage.Filter{
		Cluster:   q.Get("cluster"),
		Namespace: q.Get("namespace"),
		Kind:      q.Get("kind"),
		Name:      q.Get("name"),
		User:      q.Get("user"),
		Operation: q.Get("operation"),
	}
	f.Q = q.Get("q")
	if v := q.Get("exclude_kinds"); v != "" {
		f.ExcludeKinds = strings.Split(v, ",")
	}
	if v := q.Get("exclude_namespaces"); v != "" {
		f.ExcludeNamespaces = strings.Split(v, ",")
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return storage.Filter{}, err
		}
		f.From = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return storage.Filter{}, err
		}
		f.To = t
	}
	return f, nil
}

func parseLimit(q url.Values) int {
	v := q.Get("limit")
	if v == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
