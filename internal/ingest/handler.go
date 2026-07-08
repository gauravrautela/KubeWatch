package ingest

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gauravrautela/kubewatch/internal/classify"
	"github.com/gauravrautela/kubewatch/internal/diff"
	"github.com/gauravrautela/kubewatch/internal/event"
)

// Handler accepts batched events from agents, authenticates the cluster, tags
// and diffs each event, and forwards them to the batcher.
type Handler struct {
	auth    Authenticator
	batcher *Batcher
}

// NewHandler constructs the ingest HTTP handler.
func NewHandler(auth Authenticator, batcher *Batcher) *Handler {
	return &Handler{auth: auth, batcher: batcher}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return after
	}
	return ""
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cluster, ok := h.auth.ClusterFor(bearerToken(r))
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var batch event.Batch
	if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	for i := range batch.Events {
		e := batch.Events[i]
		e.Cluster = cluster
		if d, err := diff.Compute([]byte(e.OldObject), []byte(e.NewObject)); err == nil {
			e.Diff = string(d)
		}
		e.ChangeClass = classify.Classify(e.Kind, e.SubResource, string(e.Operation), e.Diff)
		e.ActorType = classify.ActorType(e.UserName)
		h.batcher.Add(e)
	}
	w.WriteHeader(http.StatusAccepted)
}
