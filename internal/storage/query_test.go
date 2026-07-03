package storage

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	c := Cursor{Time: time.Unix(0, 1234567890123).UTC(), ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")}
	got, err := DecodeCursor(EncodeCursor(c))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Time.Equal(c.Time) || got.ID != c.ID {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, c)
	}
}

func TestDecodeCursorRejectsGarbage(t *testing.T) {
	if _, err := DecodeCursor("not-base64!!"); err == nil {
		t.Fatal("expected error for bad base64")
	}
}

func TestBuildListQueryFiltersAndKeyset(t *testing.T) {
	f := Filter{Cluster: "c1", Name: "web", From: time.Unix(100, 0).UTC()}
	older := &Cursor{Time: time.Unix(200, 0).UTC(), ID: uuid.MustParse("22222222-2222-2222-2222-222222222222")}
	q, args := buildListQuery(f, older, nil, 50)

	for _, want := range []string{
		"FROM change_events",
		"cluster = ?",
		"name ILIKE ?",
		"event_time >= ?",
		"(event_time, event_id) < (?, ?)",
		"ORDER BY event_time DESC, event_id DESC LIMIT ?",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q\n%s", want, q)
		}
	}
	// args order: cluster, name-pattern, from, older.Time, older.ID, limit
	if len(args) != 6 {
		t.Fatalf("want 6 args, got %d: %v", len(args), args)
	}
	if args[0] != "c1" || args[1] != "%web%" || args[5] != 50 {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestBuildListQueryNoFilters(t *testing.T) {
	q, args := buildListQuery(Filter{}, nil, nil, 10)
	if strings.Contains(q, "WHERE") {
		t.Errorf("expected no WHERE clause: %s", q)
	}
	if len(args) != 1 || args[0] != 10 {
		t.Fatalf("want just the limit arg, got %v", args)
	}
}

func TestBuildListQueryOrdersSinceAscending(t *testing.T) {
	newer := &Cursor{Time: time.Unix(200, 0).UTC(), ID: uuid.MustParse("33333333-3333-3333-3333-333333333333")}
	q, _ := buildListQuery(Filter{}, nil, newer, 50)
	if !strings.Contains(q, "ORDER BY event_time ASC, event_id ASC") {
		t.Errorf("since query must order ascending:\n%s", q)
	}
	if !strings.Contains(q, "(event_time, event_id) > (?, ?)") {
		t.Errorf("since query must use the newer (>) comparison:\n%s", q)
	}

	q2, _ := buildListQuery(Filter{}, nil, nil, 50)
	if !strings.Contains(q2, "ORDER BY event_time DESC, event_id DESC") {
		t.Errorf("normal query must order descending:\n%s", q2)
	}
}

func TestBuildActivityQueryBucketAllowlist(t *testing.T) {
	if _, _, err := buildActivityQuery(Filter{}, "century"); err == nil {
		t.Fatal("expected error for invalid bucket")
	}
	q, _, err := buildActivityQuery(Filter{Kind: "Deployment"}, "hour")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"toStartOfHour(event_time)", "count()", "GROUP BY bucket_start", "kind = ?"} {
		if !strings.Contains(q, want) {
			t.Errorf("activity query missing %q\n%s", want, q)
		}
	}
}
