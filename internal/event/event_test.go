package event

import (
	"encoding/json"
	"testing"
	"time"
)

func TestChangeEventJSONRoundTrip(t *testing.T) {
	in := ChangeEvent{
		EventID:    "11111111-1111-1111-1111-111111111111",
		EventTime:  time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
		Source:     "webhook",
		Operation:  OpUpdate,
		Kind:       "Deployment",
		Namespace:  "default",
		Name:       "web",
		UserName:   "alice",
		UserGroups: []string{"system:authenticated"},
		NewObject:  `{"spec":{"replicas":3}}`,
	}
	raw, err := json.Marshal(Batch{Events: []ChangeEvent{in}})
	if err != nil {
		t.Fatal(err)
	}
	var out Batch
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(out.Events))
	}
	if !eventsEqual(out.Events[0], in) {
		t.Fatalf("round trip mismatch:\ngot:      %+v\nexpected: %+v", out.Events[0], in)
	}
}

func eventsEqual(a, b ChangeEvent) bool {
	if a.EventID != b.EventID ||
		!a.EventTime.Equal(b.EventTime) ||
		a.Cluster != b.Cluster ||
		a.Source != b.Source ||
		a.Operation != b.Operation ||
		a.APIGroup != b.APIGroup ||
		a.APIVersion != b.APIVersion ||
		a.Kind != b.Kind ||
		a.Namespace != b.Namespace ||
		a.Name != b.Name ||
		a.ResourceUID != b.ResourceUID ||
		a.SubResource != b.SubResource ||
		a.UserName != b.UserName ||
		a.UserUID != b.UserUID ||
		a.UserAgent != b.UserAgent ||
		a.DryRun != b.DryRun ||
		a.OldObject != b.OldObject ||
		a.NewObject != b.NewObject ||
		a.Diff != b.Diff {
		return false
	}
	if len(a.UserGroups) != len(b.UserGroups) {
		return false
	}
	for i := range a.UserGroups {
		if a.UserGroups[i] != b.UserGroups[i] {
			return false
		}
	}
	return true
}
