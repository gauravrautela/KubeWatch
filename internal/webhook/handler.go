package webhook

import (
	"encoding/json"
	"io"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	var review admissionv1.AdmissionReview
	if body, err := io.ReadAll(r.Body); err == nil {
		_ = json.Unmarshal(body, &review) // best-effort: a malformed body still yields an allow response
	}

	if ev, ok, perr := Parse(&review); perr == nil && ok {
		h.sink(ev)
	}

	resp := admissionv1.AdmissionReview{TypeMeta: review.TypeMeta}
	if resp.TypeMeta.APIVersion == "" {
		resp.TypeMeta = metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"}
	}
	resp.Response = &admissionv1.AdmissionResponse{Allowed: true}
	if review.Request != nil {
		resp.Response.UID = review.Request.UID
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
