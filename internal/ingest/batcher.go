package ingest

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/gauravrautela/kubewatch/internal/event"
)

// Inserter persists a batch of events. Satisfied by *storage.Store.
type Inserter interface {
	InsertBatch(ctx context.Context, events []event.ChangeEvent) error
}

// Batcher accumulates events and flushes them to the Inserter on a size or time
// threshold. This is the hub-side buffer that keeps ClickHouse inserts batched.
type Batcher struct {
	inserter Inserter
	in       chan event.ChangeEvent
	maxSize  int
	maxWait  time.Duration
	dropped  atomic.Int64
}

// NewBatcher constructs a Batcher. Call Run in a goroutine to start flushing.
func NewBatcher(inserter Inserter, maxSize int, maxWait time.Duration) *Batcher {
	return &Batcher{
		inserter: inserter,
		in:       make(chan event.ChangeEvent, maxSize*2),
		maxSize:  maxSize,
		maxWait:  maxWait,
	}
}

// Add enqueues an event without blocking. Returns false (and meters) when the
// internal channel is full — an honest, visible drop, never silent.
func (b *Batcher) Add(e event.ChangeEvent) bool {
	select {
	case b.in <- e:
		return true
	default:
		b.dropped.Add(1)
		return false
	}
}

// Dropped returns how many events were dropped due to a full channel.
func (b *Batcher) Dropped() int64 { return b.dropped.Load() }

// Run flushes batches until ctx is cancelled, then flushes once more.
func (b *Batcher) Run(ctx context.Context) {
	ticker := time.NewTicker(b.maxWait)
	defer ticker.Stop()
	pending := make([]event.ChangeEvent, 0, b.maxSize)

	flush := func() {
		if len(pending) == 0 {
			return
		}
		if err := b.inserter.InsertBatch(ctx, pending); err != nil {
			log.Printf("ingest: insert batch of %d failed: %v", len(pending), err)
		}
		pending = pending[:0]
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case e := <-b.in:
			pending = append(pending, e)
			if len(pending) >= b.maxSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
