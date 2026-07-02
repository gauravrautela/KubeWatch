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

func TestHandlerAllowsOnMalformedBody(t *testing.T) {
	called := false
	h := NewHandler(func(e event.ChangeEvent) { called = true })

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader([]byte("{not json")))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp admissionv1.AdmissionReview
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response must be a valid AdmissionReview even on malformed input: %v", err)
	}
	if resp.Response == nil || !resp.Response.Allowed {
		t.Fatal("webhook must always allow, even on malformed body")
	}
	if called {
		t.Fatal("sink should not be called for a malformed body")
	}
}

func TestParseCreateFromObjectBody(t *testing.T) {
	r := sampleReview()
	r.Request.Operation = admissionv1.Create
	r.Request.Name = ""
	r.Request.Namespace = ""
	r.Request.Object = runtime.RawExtension{Raw: []byte(`{"metadata":{"name":"created-thing","namespace":"prod","uid":"new-uid"}}`)}
	r.Request.OldObject = runtime.RawExtension{}

	ev, ok, err := Parse(r)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ev.Operation != event.OpCreate {
		t.Fatalf("want OpCreate, got %v", ev.Operation)
	}
	if ev.Name != "created-thing" || ev.Namespace != "prod" || ev.ResourceUID != "new-uid" {
		t.Fatalf("identity not pulled from object body: %+v", ev)
	}
}

func TestParseDeleteFromOldObject(t *testing.T) {
	r := sampleReview()
	r.Request.Operation = admissionv1.Delete
	r.Request.Name = ""
	r.Request.Namespace = ""
	r.Request.Object = runtime.RawExtension{}
	r.Request.OldObject = runtime.RawExtension{Raw: []byte(`{"metadata":{"name":"deleted-thing","namespace":"prod","uid":"old-uid"}}`)}

	ev, ok, err := Parse(r)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ev.Operation != event.OpDelete {
		t.Fatalf("want OpDelete, got %v", ev.Operation)
	}
	if ev.Name != "deleted-thing" || ev.Namespace != "prod" || ev.ResourceUID != "old-uid" {
		t.Fatalf("identity not pulled from old object: %+v", ev)
	}
}

func TestParseMalformedObjectBody(t *testing.T) {
	r := sampleReview()
	r.Request.Object = runtime.RawExtension{Raw: []byte("{bad")}

	ev, ok, err := Parse(r)
	if ok {
		t.Fatalf("want ok=false for malformed object body, got ev=%+v", ev)
	}
	if err == nil {
		t.Fatal("want non-nil error for malformed object body")
	}
}
