package webhook

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"

	"github.com/gauravrautela/kubewatch/internal/event"
)

// withCapturedLogs installs a debug-level JSON slog logger writing to buf as
// the default logger, returning a restore func to put the previous default
// back (call via defer).
func withCapturedLogs(buf *bytes.Buffer) func() {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return func() { slog.SetDefault(prev) }
}

func TestHandlerLogsCapturedChangeAndCountsReceived(t *testing.T) {
	var buf bytes.Buffer
	defer withCapturedLogs(&buf)()

	h := NewHandler(func(event.ChangeEvent) {})

	body, _ := json.Marshal(sampleReview())
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if got := h.Received(); got != 1 {
		t.Fatalf("want Received()==1, got %d", got)
	}
	out := buf.String()
	if !strings.Contains(out, "change captured") {
		t.Fatalf("expected a 'change captured' debug log line, got: %s", out)
	}
}

func TestHandlerLogsParseFailureButStillAllows(t *testing.T) {
	var buf bytes.Buffer
	defer withCapturedLogs(&buf)()

	h := NewHandler(func(event.ChangeEvent) {})

	// The outer AdmissionReview JSON (and the Object.Raw bytes themselves) are
	// syntactically valid JSON, so the body decodes fine. But the object's
	// "metadata" field has the wrong shape (a string instead of an object),
	// which makes the nested objectMeta unmarshal inside Parse fail.
	r := sampleReview()
	r.Request.Object.Raw = []byte(`{"metadata":"not-an-object"}`)
	body, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("failed to build request fixture: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp admissionv1.AdmissionReview
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response must be a valid AdmissionReview even on parse failure: %v", err)
	}
	if resp.Response == nil || !resp.Response.Allowed {
		t.Fatal("webhook must always allow, even when Parse fails")
	}

	out := buf.String()
	if !strings.Contains(out, "failed to parse admission request") {
		t.Fatalf("expected a 'failed to parse admission request' warn log line (previously swallowed), got: %s", out)
	}
}
