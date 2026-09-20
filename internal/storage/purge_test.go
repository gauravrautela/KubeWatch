package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gauravrautela/kubewatch/internal/event"
	"github.com/gauravrautela/kubewatch/internal/redact"
)

func TestPurgeStatement(t *testing.T) {
	got := purgeStatement(2)
	for _, want := range []string{
		"ALTER TABLE change_events UPDATE ",
		"old_object = multiIf(event_id = ?, ?, event_id = ?, ?, old_object)",
		"new_object = multiIf(event_id = ?, ?, event_id = ?, ?, new_object)",
		"diff = multiIf(event_id = ?, ?, event_id = ?, ?, diff)",
		"WHERE event_id IN (?, ?)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("statement missing %q:\n%s", want, got)
		}
	}
	// A row the chunk does not name keeps its own value: that is what the
	// trailing column name in each multiIf is for.
	if strings.Count(got, "multiIf(") != 3 {
		t.Fatalf("want one multiIf per value column:\n%s", got)
	}
}

func TestPurgeChunkIsTwoHundred(t *testing.T) {
	if PurgeChunk != 200 {
		t.Fatalf("the purge rewrites 200 rows per mutation, got %d", PurgeChunk)
	}
}

func TestPurgedRow(t *testing.T) {
	stored := storedSecret{
		id:        uuid.New(),
		oldObject: `{"kind":"Secret","data":{"password":"czNjcjN0","user":"YWRtaW4="}}`,
		newObject: `{"kind":"Secret","data":{"password":"bjN3cDQ1cw==","user":"YWRtaW4="}}`,
		diffJSON:  `[{"path":"data.password","op":"replace","old":"czNjcjN0","new":"bjN3cDQ1cw=="}]`,
	}

	got, changed := purged(stored)
	if !changed {
		t.Fatal("a row holding values must be rewritten")
	}
	whole := got.oldObject + got.newObject + got.diffJSON
	for _, value := range []string{"czNjcjN0", "bjN3cDQ1cw==", "YWRtaW4="} {
		if strings.Contains(whole, value) {
			t.Fatalf("value %q survived the purge: %s", value, whole)
		}
	}
	for _, want := range []string{"password", "user", `"path":"data.password"`} {
		if !strings.Contains(whole, want) {
			t.Fatalf("the purge must keep %q: %s", want, whole)
		}
	}

	// Running it again changes nothing: the row is already empty of values.
	again, changedAgain := purged(got)
	if changedAgain {
		t.Fatalf("a purged row must be left alone, got %+v", again)
	}
}

func TestPurgedRowFailsClosed(t *testing.T) {
	got, changed := purged(storedSecret{
		id:        uuid.New(),
		oldObject: `{"kind":"Secret","data":["czNjcjN0"]}`,
		newObject: `{"kind":"Secret","data":["czNjcjN0"]}`,
		diffJSON:  `[]`,
	})
	if !changed {
		t.Fatal("a row whose body cannot be emptied must still be rewritten")
	}
	if got.oldObject != "" || got.newObject != "" {
		t.Fatalf("both bodies must go: %q %q", got.oldObject, got.newObject)
	}
}

// TestPurgeRewritesStoredSecrets is AC-010: a hub that starts against a store
// holding Secret events captured before this change leaves no value behind,
// and each event keeps its keys, user, time and operation.
func TestPurgeRewritesStoredSecrets(t *testing.T) {
	s := testStore(t)
	defer s.Close()
	ctx := context.Background()

	id := uuid.NewString()
	captured := time.Now().UTC().Add(-time.Hour)
	before := event.ChangeEvent{
		EventID:    id,
		EventTime:  captured,
		Cluster:    "test-cluster",
		Source:     "webhook",
		Operation:  event.OpUpdate,
		APIVersion: "v1",
		Kind:       "Secret",
		Namespace:  "default",
		Name:       "creds-" + id[:8],
		UserName:   "alice",
		UserGroups: []string{"system:authenticated"},
		OldObject:  `{"kind":"Secret","data":{"password":"czNjcjN0","user":"YWRtaW4="}}`,
		NewObject:  `{"kind":"Secret","data":{"password":"bjN3cDQ1cw==","user":"YWRtaW4="}}`,
		Diff:       `[{"path":"data.password","op":"replace","old":"czNjcjN0","new":"bjN3cDQ1cw=="}]`,
		ChangeClass: []string{
			"config-data",
		},
		ActorType: "human",
	}
	if err := s.InsertBatch(ctx, []event.ChangeEvent{before}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	rewritten, err := s.PurgeSecretValues(ctx)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if rewritten == 0 {
		t.Fatal("the purge rewrote nothing")
	}

	after, found, err := s.GetEvent(ctx, id)
	if err != nil || !found {
		t.Fatalf("read back: found=%v err=%v", found, err)
	}
	whole := after.OldObject + after.NewObject + after.Diff
	for _, value := range []string{"czNjcjN0", "bjN3cDQ1cw==", "YWRtaW4="} {
		if strings.Contains(whole, value) {
			t.Fatalf("value %q is still stored: %s", value, whole)
		}
	}
	if !strings.Contains(whole, redact.MarkerPrefix) {
		t.Fatalf("the stored bodies must carry markers: %s", whole)
	}
	for _, want := range []string{"password", "user"} {
		if !strings.Contains(whole, want) {
			t.Fatalf("key %q must survive the purge: %s", want, whole)
		}
	}
	if after.UserName != "alice" || after.Operation != "UPDATE" || after.Name != before.Name {
		t.Fatalf("who, when and what must survive: %+v", after)
	}
	if !after.EventTime.Truncate(time.Second).Equal(captured.Truncate(time.Second)) {
		t.Fatalf("event_time changed: %v != %v", after.EventTime, captured)
	}

	// A second run finds nothing left to do.
	if again, err := s.PurgeSecretValues(ctx); err != nil || again != 0 {
		t.Fatalf("re-running the purge must be a no-op: rewrote=%d err=%v", again, err)
	}
}
