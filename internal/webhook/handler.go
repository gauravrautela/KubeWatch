package webhook

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/gauravrautela/kubewatch/internal/event"
)

// Sink receives parsed change events. It must not block for long.
type Sink func(event.ChangeEvent)

// Handler serves the validating webhook. It records the change and always
// admits the request (never blocks cluster operations).
type Handler struct {
	sink     Sink
	received atomic.Int64
}

// NewHandler constructs a webhook Handler that feeds parsed events to sink.
func NewHandler(sink Sink) *Handler {
	return &Handler{sink: sink}
}

// Received returns the count of admission requests that carried a non-nil
// Request (i.e. a real admission call arrived).
func (h *Handler) Received() int64 { return h.received.Load() }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var review admissionv1.AdmissionReview
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Warn("webhook: failed to read request body", "err", err)
	} else if uerr := json.Unmarshal(body, &review); uerr != nil {
		// best-effort: a malformed body still yields an allow response
		slog.Warn("webhook: failed to decode AdmissionReview", "err", uerr)
	}

	if review.Request != nil {
		h.received.Add(1)
	}

	if ev, ok, perr := Parse(&review); perr != nil {
		slog.Warn("webhook: failed to parse admission request", "err", perr)
	} else if ok {
		slog.Debug("webhook: change captured", "op", string(ev.Operation), "kind", ev.Kind, "namespace", ev.Namespace, "name", ev.Name, "user", ev.UserName)
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
