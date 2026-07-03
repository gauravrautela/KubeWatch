package storage

import (
	"testing"

	"github.com/google/uuid"

	"github.com/gauravrautela/kubewatch/internal/event"
)

func TestValidForInsert(t *testing.T) {
	validID := uuid.NewString()

	tests := []struct {
		name string
		ev   event.ChangeEvent
		ok   bool
	}{
		{
			name: "valid webhook update event",
			ev: event.ChangeEvent{
				EventID:   validID,
				Source:    "webhook",
				Operation: event.OpUpdate,
			},
			ok: true,
		},
		{
			name: "empty event id",
			ev: event.ChangeEvent{
				EventID:   "",
				Source:    "webhook",
				Operation: event.OpUpdate,
			},
			ok: false,
		},
		{
			name: "non-uuid event id",
			ev: event.ChangeEvent{
				EventID:   "1",
				Source:    "webhook",
				Operation: event.OpUpdate,
			},
			ok: false,
		},
		{
			name: "valid uuid but empty source",
			ev: event.ChangeEvent{
				EventID:   validID,
				Source:    "",
				Operation: event.OpUpdate,
			},
			ok: false,
		},
		{
			name: "valid uuid, webhook source, empty operation",
			ev: event.ChangeEvent{
				EventID:   validID,
				Source:    "webhook",
				Operation: "",
			},
			ok: false,
		},
		{
			name: "valid uuid, reconcile source, delete operation",
			ev: event.ChangeEvent{
				EventID:   validID,
				Source:    "reconcile",
				Operation: event.OpDelete,
			},
			ok: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := validForInsert(tt.ev)
			if ok != tt.ok {
				t.Fatalf("validForInsert(%+v) ok = %v, want %v", tt.ev, ok, tt.ok)
			}
			if ok && id.String() != tt.ev.EventID {
				t.Fatalf("validForInsert(%+v) id = %v, want %v", tt.ev, id, tt.ev.EventID)
			}
		})
	}
}
