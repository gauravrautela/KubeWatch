package buffer

import (
	"testing"

	"github.com/gauravrautela/kubewatch/internal/event"
)

func TestDrainReturnsAndClears(t *testing.T) {
	b := New(10)
	b.Add(event.ChangeEvent{EventID: "1"})
	b.Add(event.ChangeEvent{EventID: "2"})
	got := b.Drain()
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	if len(b.Drain()) != 0 {
		t.Fatal("buffer should be empty after drain")
	}
}

func TestOverflowDropsOldestAndMeters(t *testing.T) {
	b := New(2)
	b.Add(event.ChangeEvent{EventID: "1"})
	b.Add(event.ChangeEvent{EventID: "2"})
	b.Add(event.ChangeEvent{EventID: "3"}) // drops "1"
	if b.Dropped() != 1 {
		t.Fatalf("want 1 dropped, got %d", b.Dropped())
	}
	got := b.Drain()
	if len(got) != 2 || got[0].EventID != "2" || got[1].EventID != "3" {
		t.Fatalf("unexpected retained events: %+v", got)
	}
}
