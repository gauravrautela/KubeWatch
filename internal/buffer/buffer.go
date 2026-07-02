// Package buffer is the agent's bounded, in-memory queue of change events.
package buffer

import (
	"sync"
	"sync/atomic"

	"github.com/gauravrautela/kubewatch/internal/event"
)

// Buffer is a bounded FIFO of change events. When full, Add drops the oldest
// event and increments Dropped — an honest, metered gap, never silent.
type Buffer struct {
	mu      sync.Mutex
	events  []event.ChangeEvent
	max     int
	dropped atomic.Int64
}

// New returns a Buffer that holds at most max events.
func New(max int) *Buffer {
	return &Buffer{max: max}
}

// Add appends an event, dropping the oldest if the buffer is full. Its
// signature matches webhook.Sink.
func (b *Buffer) Add(e event.ChangeEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) >= b.max {
		b.events = b.events[1:]
		b.dropped.Add(1)
	}
	b.events = append(b.events, e)
}

// Drain returns all buffered events and clears the buffer.
func (b *Buffer) Drain() []event.ChangeEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) == 0 {
		return nil
	}
	out := b.events
	b.events = nil
	return out
}

// Dropped returns the number of events dropped due to overflow.
func (b *Buffer) Dropped() int64 { return b.dropped.Load() }
