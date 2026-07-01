package diff

import (
	"encoding/json"
	"testing"
)

func computeChanges(t *testing.T, oldJSON, newJSON string) []Change {
	t.Helper()
	raw, err := Compute([]byte(oldJSON), []byte(newJSON))
	if err != nil {
		t.Fatal(err)
	}
	var got []Change
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestComputeReplace(t *testing.T) {
	got := computeChanges(t,
		`{"spec":{"replicas":2}}`,
		`{"spec":{"replicas":3}}`)
	if len(got) != 1 {
		t.Fatalf("want 1 change, got %d: %+v", len(got), got)
	}
	c := got[0]
	if c.Path != "spec.replicas" || c.Op != "replace" {
		t.Fatalf("unexpected change: %+v", c)
	}
	if c.Old.(float64) != 2 || c.New.(float64) != 3 {
		t.Fatalf("unexpected old/new: %+v", c)
	}
}

func TestComputeAddAndRemove(t *testing.T) {
	got := computeChanges(t,
		`{"a":1}`,
		`{"b":2}`)
	ops := map[string]string{}
	for _, c := range got {
		ops[c.Path] = c.Op
	}
	if ops["a"] != "remove" || ops["b"] != "add" {
		t.Fatalf("unexpected ops: %+v", ops)
	}
}

func TestComputeCreate(t *testing.T) {
	got := computeChanges(t, ``, `{"a":1}`)
	if len(got) != 1 || got[0].Op != "add" || got[0].Path != "a" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestComputeNoChange(t *testing.T) {
	got := computeChanges(t, `{"a":1}`, `{"a":1}`)
	if len(got) != 0 {
		t.Fatalf("want no changes, got %+v", got)
	}
}

func TestComputeAddedExplicitNull(t *testing.T) {
	got := computeChanges(t, ``, `{"a":null}`)
	if len(got) != 1 {
		t.Fatalf("want 1 change, got %d: %+v", len(got), got)
	}
	c := got[0]
	if c.Op != "add" || c.Path != "a" {
		t.Fatalf("unexpected change: %+v", c)
	}
}

func TestComputeValueToNull(t *testing.T) {
	got := computeChanges(t, `{"a":1}`, `{"a":null}`)
	if len(got) != 1 {
		t.Fatalf("want 1 change, got %d: %+v", len(got), got)
	}
	c := got[0]
	if c.Op != "replace" || c.Path != "a" {
		t.Fatalf("unexpected change: %+v", c)
	}
	if c.Old.(float64) != 1 {
		t.Fatalf("unexpected old: %+v", c)
	}
}

func TestComputeNullToValue(t *testing.T) {
	got := computeChanges(t, `{"a":null}`, `{"a":1}`)
	if len(got) != 1 {
		t.Fatalf("want 1 change, got %d: %+v", len(got), got)
	}
	c := got[0]
	if c.Op != "replace" || c.Path != "a" {
		t.Fatalf("unexpected change: %+v", c)
	}
	if c.New.(float64) != 1 {
		t.Fatalf("unexpected new: %+v", c)
	}
}

func TestComputeKeyRemoved(t *testing.T) {
	got := computeChanges(t, `{"a":1}`, `{}`)
	if len(got) != 1 {
		t.Fatalf("want 1 change, got %d: %+v", len(got), got)
	}
	c := got[0]
	if c.Op != "remove" || c.Path != "a" {
		t.Fatalf("unexpected change: %+v", c)
	}
	if c.Old.(float64) != 1 {
		t.Fatalf("unexpected old: %+v", c)
	}
}
