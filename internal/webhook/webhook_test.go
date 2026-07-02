package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/gauravrautela/kubewatch/internal/event"
)

func sampleReview() *admissionv1.AdmissionReview {
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"},
		Request: &admissionv1.AdmissionRequest{
			UID:       types.UID("abc"),
			Operation: admissionv1.Update,
			Kind:      metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
			Namespace: "default",
			Name:      "web",
			UserInfo:  authenticationv1.UserInfo{Username: "alice", UID: "u1", Groups: []string{"g1"}},
			OldObject: runtime.RawExtension{Raw: []byte(`{"metadata":{"uid":"xyz"},"spec":{"replicas":2}}`)},
			Object:    runtime.RawExtension{Raw: []byte(`{"metadata":{"uid":"xyz"},"spec":{"replicas":3}}`)},
		},
	}
}

func TestParseUpdate(t *testing.T) {
	ev, ok, err := Parse(sampleReview())
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ev.Operation != event.OpUpdate || ev.Kind != "Deployment" || ev.Name != "web" {
		t.Fatalf("bad event: %+v", ev)
	}
	if ev.UserName != "alice" || ev.ResourceUID != "xyz" || ev.Source != "webhook" {
		t.Fatalf("bad attribution: %+v", ev)
	}
	if ev.EventID == "" {
		t.Fatal("event id must be set")
	}
}

func TestParseSkipsConnect(t *testing.T) {
	r := sampleReview()
	r.Request.Operation = admissionv1.Connect
	_, ok, err := Parse(r)
	if err != nil || ok {
		t.Fatalf("connect should be skipped: ok=%v err=%v", ok, err)
	}
}

func TestHandlerAlwaysAllows(t *testing.T) {
	var got event.ChangeEvent
	h := NewHandler(func(e event.ChangeEvent) { got = e })

	body, _ := json.Marshal(sampleReview())
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp admissionv1.AdmissionReview
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Response == nil || !resp.Response.Allowed {
		t.Fatal("webhook must always allow")
	}
	if got.Name != "web" {
		t.Fatalf("sink did not receive event: %+v", got)
	}
}
