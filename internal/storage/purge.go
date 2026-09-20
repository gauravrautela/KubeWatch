package storage

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gauravrautela/kubewatch/internal/diff"
	"github.com/gauravrautela/kubewatch/internal/redact"
)

// PurgeChunk is how many rows one mutation rewrites.
const PurgeChunk = 200

// secretRowsQuery reads one page of Secret events that still hold a body no
// marker has been through — the rows captured before agents began redacting.
// Paging is by (event_time, event_id) so each row is visited once per run,
// including rows the rewrite turns out not to change.
const secretRowsQuery = `
SELECT event_id, event_time, old_object, new_object, diff
FROM change_events
WHERE kind = 'Secret'
  AND (
       (old_object != '' AND position(old_object, ?) = 0)
    OR (new_object != '' AND position(new_object, ?) = 0)
  )
  AND (event_time, event_id) > (?, ?)
ORDER BY event_time, event_id
LIMIT ?
`

// storedSecret is one row the purge may rewrite.
type storedSecret struct {
	id        uuid.UUID
	eventTime time.Time
	oldObject string
	newObject string
	diffJSON  string
}

// PurgeSecretValues empties the Secret values in events captured before the
// agents began redacting, keeping each event's keys, user, time and operation.
// It returns how many rows it rewrote. Running it again is safe: a row whose
// bodies already carry a marker is never selected, and a row the rewrite would
// not change is left alone, so two hubs starting together cannot undo each
// other's work.
func (s *Store) PurgeSecretValues(ctx context.Context) (int, error) {
	var cursorTime time.Time
	var cursorID uuid.UUID
	rewritten := 0

	for {
		rows, err := s.secretRowsToPurge(ctx, cursorTime, cursorID)
		if err != nil {
			return rewritten, fmt.Errorf("purge: read: %w", err)
		}
		if len(rows) == 0 {
			return rewritten, nil
		}
		last := rows[len(rows)-1]
		cursorTime, cursorID = last.eventTime, last.id

		updates := make([]storedSecret, 0, len(rows))
		for _, r := range rows {
			if u, ok := purged(r); ok {
				updates = append(updates, u)
			}
		}
		if len(updates) > 0 {
			if err := s.applyPurge(ctx, updates); err != nil {
				return rewritten, fmt.Errorf("purge: rewrite: %w", err)
			}
			rewritten += len(updates)
		}
	}
}

// purged returns the row as it should be stored, and whether anything changed.
// A body the redaction cannot prove it has emptied costs both bodies, the same
// rule the agent follows.
func purged(r storedSecret) (storedSecret, bool) {
	oldOut, newOut, err := redact.Secret([]byte(r.oldObject), []byte(r.newObject))
	if err != nil {
		oldOut, newOut = nil, nil
	}
	d, derr := diff.Compute(oldOut, newOut)
	if derr != nil {
		d = []byte("[]")
	}
	out := storedSecret{
		id:        r.id,
		eventTime: r.eventTime,
		oldObject: string(oldOut),
		newObject: string(newOut),
		diffJSON:  string(d),
	}
	if out.oldObject == r.oldObject && out.newObject == r.newObject && out.diffJSON == r.diffJSON {
		return storedSecret{}, false
	}
	return out, true
}

// secretRowsToPurge reads the page after the cursor.
func (s *Store) secretRowsToPurge(ctx context.Context, cursorTime time.Time, cursorID uuid.UUID) ([]storedSecret, error) {
	rows, err := s.conn.Query(ctx, secretRowsQuery,
		redact.MarkerPrefix, redact.MarkerPrefix, cursorTime, cursorID, PurgeChunk)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []storedSecret{}
	for rows.Next() {
		var r storedSecret
		if err := rows.Scan(&r.id, &r.eventTime, &r.oldObject, &r.newObject, &r.diffJSON); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// purgeStatement builds the mutation that rewrites n rows in one go: each
// column takes its new value per event_id, and any row not listed is left as
// it is.
func purgeStatement(n int) string {
	var b strings.Builder
	b.WriteString("ALTER TABLE change_events UPDATE ")
	for i, col := range []string{"old_object", "new_object", "diff"} {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(col)
		b.WriteString(" = multiIf(")
		for j := 0; j < n; j++ {
			b.WriteString("event_id = ?, ?, ")
		}
		b.WriteString(col)
		b.WriteString(")")
	}
	b.WriteString(" WHERE event_id IN (")
	b.WriteString(strings.TrimSuffix(strings.Repeat("?, ", n), ", "))
	b.WriteString(")")
	return b.String()
}

// applyPurge rewrites one chunk of rows with a single mutation.
func (s *Store) applyPurge(ctx context.Context, updates []storedSecret) error {
	args := make([]any, 0, len(updates)*7)
	for _, col := range []func(storedSecret) string{
		func(u storedSecret) string { return u.oldObject },
		func(u storedSecret) string { return u.newObject },
		func(u storedSecret) string { return u.diffJSON },
	} {
		for _, u := range updates {
			args = append(args, u.id, col(u))
		}
	}
	for _, u := range updates {
		args = append(args, u.id)
	}
	return s.conn.Exec(ctx, purgeStatement(len(updates)), args...)
}

// PurgeSecretValuesLogged runs the purge and reports what it did, so an
// operator can see it in the hub's startup logs rather than only in the store.
func (s *Store) PurgeSecretValuesLogged(ctx context.Context) {
	start := time.Now()
	rewritten, err := s.PurgeSecretValues(ctx)
	switch {
	case err != nil:
		log.Printf("storage: purge of stored Secret values failed after %d rows: %v", rewritten, err)
	case rewritten > 0:
		log.Printf("storage: emptied the Secret values in %d stored events in %s", rewritten, time.Since(start).Round(time.Millisecond))
	default:
		log.Printf("storage: no stored Secret event held a value")
	}
}
