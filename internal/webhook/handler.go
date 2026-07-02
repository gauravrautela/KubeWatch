package webhook

import (
	"encoding/json"
	"io"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"

	"github.com/gauravrautela/kubewatch/internal/event"
)

// Sink receives parsed change events. It must not block for long.
type Sink func(event.ChangeEvent)

// Handler serves the validating webhook. It records the change and always
// admits the request (never blocks cluster operations).
type Handler struct {
	sink Sink
}

// NewHandler constructs a webhook Handler that feeds parsed events to sink.
func NewHandler(sink Sink) *Handler {
	return &Handler{sink: sink}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var review admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &review); err != nil {
		http.Error(w, "decode", http.StatusBadRequest)
		return
	}

	if ev, ok, perr := Parse(&review); perr == nil && ok {
		h.sink(ev)
	}

	resp := admissionv1.AdmissionReview{TypeMeta: review.TypeMeta}
	if review.Request != nil {
		resp.Response = &admissionv1.AdmissionResponse{
			UID:     review.Request.UID,
			Allowed: true,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
