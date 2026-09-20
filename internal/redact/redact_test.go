package redact

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/gauravrautela/kubewatch/internal/classify"
	"github.com/gauravrautela/kubewatch/internal/diff"
)

// secretWith builds a Secret body with the given data values.
func secretWith(t *testing.T, data map[string]string) []byte {
	t.Helper()
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"type":       "Opaque",
		"metadata":   map[string]any{"name": "creds", "namespace": "default"},
		"data":       data,
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRedactSecret(t *testing.T) {
	t.Run("data values", func(t *testing.T) {
		old := secretWith(t, map[string]string{"user": "YWRtaW4=", "password": "czNjcjN0"})
		new := secretWith(t, map[string]string{"user": "YWRtaW4=", "password": "bjN3cDQ1cw=="})

		gotOld, gotNew, err := Secret(old, new)
		if err != nil {
			t.Fatal(err)
		}
		for _, body := range [][]byte{gotOld, gotNew} {
			for _, value := range []string{"YWRtaW4=", "czNjcjN0", "bjN3cDQ1cw=="} {
				if bytes.Contains(body, []byte(value)) {
					t.Fatalf("value %q survived redaction: %s", value, body)
				}
			}
			var obj map[string]any
			if err := json.Unmarshal(body, &obj); err != nil {
				t.Fatal(err)
			}
			data := obj["data"].(map[string]any)
			if len(data) != 2 {
				t.Fatalf("both keys must survive: %v", data)
			}
			for k, v := range data {
				if s, ok := v.(string); !ok || !strings.HasPrefix(s, "<redacted") {
					t.Fatalf("key %q must hold a marker, got %v", k, v)
				}
			}
			if obj["type"] != "Opaque" {
				t.Fatalf("the rest of the Secret must be untouched: %s", body)
			}
		}
	})

	t.Run("stringData values", func(t *testing.T) {
		raw := []byte(`{"kind":"Secret","stringData":{"token":"plaintext-token"}}`)
		_, got, err := Secret(nil, raw)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(got, []byte("plaintext-token")) {
			t.Fatalf("stringData value survived: %s", got)
		}
		if !bytes.Contains(got, []byte("token")) {
			t.Fatalf("stringData key must survive: %s", got)
		}
	})

	t.Run("last-applied annotation", func(t *testing.T) {
		applied := `{"apiVersion":"v1","kind":"Secret","data":{"password":"czNjcjN0"},"metadata":{"name":"creds"}}`
		raw, err := json.Marshal(map[string]any{
			"kind": "Secret",
			"metadata": map[string]any{
				"name":        "creds",
				"annotations": map[string]any{LastAppliedAnnotation: applied},
			},
			"data": map[string]any{"password": "czNjcjN0"},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, got, err := Secret(nil, raw)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(got, []byte("czNjcjN0")) {
			t.Fatalf("the annotation's copy of the value survived: %s", got)
		}
		var obj map[string]any
		if err := json.Unmarshal(got, &obj); err != nil {
			t.Fatal(err)
		}
		ann := obj["metadata"].(map[string]any)["annotations"].(map[string]any)
		inner, ok := ann[LastAppliedAnnotation].(string)
		if !ok || !strings.Contains(inner, `"password"`) {
			t.Fatalf("the annotation must keep its keys: %v", ann)
		}
	})

	t.Run("unparseable annotation is replaced", func(t *testing.T) {
		raw := []byte(`{"kind":"Secret","metadata":{"annotations":{"` + LastAppliedAnnotation + `":"{not json czNjcjN0"}},"data":{"password":"czNjcjN0"}}`)
		_, got, err := Secret(nil, raw)
		if err != nil {
			t.Fatalf("the body itself parsed, so it must be kept: %v", err)
		}
		if bytes.Contains(got, []byte("czNjcjN0")) {
			t.Fatalf("value survived in the unparseable annotation: %s", got)
		}
		var obj map[string]any
		if err := json.Unmarshal(got, &obj); err != nil {
			t.Fatal(err)
		}
		ann := obj["metadata"].(map[string]any)["annotations"].(map[string]any)
		if ann[LastAppliedAnnotation] != Marker {
			t.Fatalf("want the whole annotation replaced, got %v", ann[LastAppliedAnnotation])
		}
		if data := obj["data"].(map[string]any); data["password"] != Marker {
			t.Fatalf("the body's own keys must still be there: %v", data)
		}
	})

	t.Run("no data field", func(t *testing.T) {
		raw := []byte(`{"kind":"Secret","metadata":{"name":"creds"},"type":"Opaque"}`)
		_, got, err := Secret(nil, raw)
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]any
		if err := json.Unmarshal(got, &obj); err != nil {
			t.Fatal(err)
		}
		if _, ok := obj["data"]; ok {
			t.Fatalf("an absent data field must stay absent: %s", got)
		}
		if obj["type"] != "Opaque" {
			t.Fatalf("the body must be otherwise unchanged: %s", got)
		}
	})

	t.Run("empty data field", func(t *testing.T) {
		raw := []byte(`{"kind":"Secret","data":{}}`)
		_, got, err := Secret(nil, raw)
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]any
		if err := json.Unmarshal(got, &obj); err != nil {
			t.Fatal(err)
		}
		if data, ok := obj["data"].(map[string]any); !ok || len(data) != 0 {
			t.Fatalf("an empty data field must stay an empty object: %s", got)
		}
	})

	t.Run("empty bodies", func(t *testing.T) {
		gotOld, gotNew, err := Secret(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(gotOld) != 0 || len(gotNew) != 0 {
			t.Fatalf("empty in, empty out: %q %q", gotOld, gotNew)
		}
	})

	t.Run("numbers keep their form", func(t *testing.T) {
		raw := []byte(`{"kind":"Secret","metadata":{"generation":12345678901234567890},"data":{"k":"dg=="}}`)
		_, got, err := Secret(nil, raw)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(got, []byte("12345678901234567890")) {
			t.Fatalf("a number the change never touched was reformatted: %s", got)
		}
	})
}

func TestRedactUnreadableBody(t *testing.T) {
	for name, raw := range map[string]string{
		"data is a list":     `{"kind":"Secret","data":["a","b"]}`,
		"data is a string":   `{"kind":"Secret","data":"YWRtaW4="}`,
		"value is a number":  `{"kind":"Secret","data":{"password":1234}}`,
		"value is an object": `{"kind":"Secret","data":{"password":{"v":"czNjcjN0"}}}`,
		"body is not JSON":   `not json at all`,
		"body is a list":     `[{"kind":"Secret"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Secret(nil, []byte(raw)); !errors.Is(err, ErrUnreadable) {
				t.Fatalf("want ErrUnreadable, got %v", err)
			}
		})
	}
}

// dataOf returns one redacted body's data map.
func dataOf(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	data, _ := obj["data"].(map[string]any)
	return data
}

func TestMarkers(t *testing.T) {
	t.Run("add, change, remove in one update", func(t *testing.T) {
		old := secretWith(t, map[string]string{"user": "YWRtaW4=", "password": "czNjcjN0"})
		new := secretWith(t, map[string]string{"password": "bjN3cDQ1cw==", "token": "dG9r"})

		gotOld, gotNew, err := Secret(old, new)
		if err != nil {
			t.Fatal(err)
		}
		oldData, newData := dataOf(t, gotOld), dataOf(t, gotNew)

		// changed: present on both sides, different markers, so the hub's
		// diff records one replace for the key.
		if oldData["password"] != MarkerBefore || newData["password"] != MarkerAfter {
			t.Fatalf("changed key must differ across sides: %v -> %v", oldData["password"], newData["password"])
		}
		// removed and added: one side only, plain marker.
		if oldData["user"] != Marker {
			t.Fatalf("removed key: want %q, got %v", Marker, oldData["user"])
		}
		if _, ok := newData["user"]; ok {
			t.Fatal("a removed key must not appear on the new side")
		}
		if newData["token"] != Marker {
			t.Fatalf("added key: want %q, got %v", Marker, newData["token"])
		}
		if _, ok := oldData["token"]; ok {
			t.Fatal("an added key must not appear on the old side")
		}
	})

	t.Run("an unchanged key reads the same on both sides", func(t *testing.T) {
		old := secretWith(t, map[string]string{"user": "YWRtaW4=", "password": "czNjcjN0"})
		new := secretWith(t, map[string]string{"user": "YWRtaW4=", "password": "bjN3cDQ1cw=="})

		gotOld, gotNew, err := Secret(old, new)
		if err != nil {
			t.Fatal(err)
		}
		if dataOf(t, gotOld)["user"] != Marker || dataOf(t, gotNew)["user"] != Marker {
			t.Fatal("an unchanged key must read the same on both sides, so the diff ignores it")
		}
	})

	t.Run("X to Y then Y to X byte-identical", func(t *testing.T) {
		x := secretWith(t, map[string]string{"password": "eA=="})
		y := secretWith(t, map[string]string{"password": "eQ=="})

		forwardOld, forwardNew, err := Secret(x, y)
		if err != nil {
			t.Fatal(err)
		}
		backOld, backNew, err := Secret(y, x)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(forwardOld, backOld) || !bytes.Equal(forwardNew, backNew) {
			t.Fatalf("the two directions must be indistinguishable:\n X->Y %s | %s\n Y->X %s | %s",
				forwardOld, forwardNew, backOld, backNew)
		}
	})
}

// TestClassifyAfterRedaction is the guard: a change to a value alone, once
// redacted, must still reach the hub as a config-data change, which is what
// the incident page ranks on.
func TestClassifyAfterRedaction(t *testing.T) {
	old := secretWith(t, map[string]string{"password": "czNjcjN0"})
	new := secretWith(t, map[string]string{"password": "bjN3cDQ1cw=="})

	gotOld, gotNew, err := Secret(old, new)
	if err != nil {
		t.Fatal(err)
	}
	d, err := diff.Compute(gotOld, gotNew)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(d, []byte(`"path":"data.password"`)) {
		t.Fatalf("the diff must still name the key: %s", d)
	}
	classes := classify.Classify("Secret", "", "UPDATE", string(d))
	if !slices.Contains(classes, classify.ClassConfigData) {
		t.Fatalf("want config-data, got %v", classes)
	}
}
