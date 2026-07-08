package storage

import (
	"context"
	"time"
)

// IncidentRow is a lightweight change event for incident scoring (no object
// bodies, no diff).
type IncidentRow struct {
	EventID   string    `json:"event_id"`
	EventTime time.Time `json:"event_time"`
	Operation string    `json:"operation"`
	Kind      string    `json:"kind"`
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	UserName  string    `json:"user_name"`
	ActorType string    `json:"actor_type"`
	Classes   []string  `json:"classes"`
}

// ResourceKey identifies one resource within a cluster.
type ResourceKey struct {
	Namespace string
	Kind      string
	Name      string
}

// ResourceStats summarizes a resource's 30-day change history for rarity and
// churn scoring.
type ResourceStats struct {
	PerDay       float64   // avg changes per active day over the 30-day window
	LastPriorDay time.Time // last day with changes strictly before the window; zero if none
	PriorTotal   uint64    // total changes before the window start day
}

// incidentEventsLimit caps the window scan so a pathological window cannot
// pull unbounded rows into memory.
const incidentEventsLimit = 20000

const incidentEventsQuery = `SELECT event_id, event_time, operation, kind, namespace, name, user_name, actor_type, change_class
FROM change_events
WHERE cluster = ? AND event_time >= ? AND event_time <= ? AND dry_run = 0
ORDER BY event_time DESC
LIMIT ?`

// IncidentEvents returns the cluster's non-dry-run events in [from, to],
// newest first.
func (s *Store) IncidentEvents(ctx context.Context, cluster string, from, to time.Time) ([]IncidentRow, error) {
	rows, err := s.conn.Query(ctx, incidentEventsQuery, cluster, from, to, incidentEventsLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IncidentRow{}
	for rows.Next() {
		var r IncidentRow
		if err := rows.Scan(
			&r.EventID, &r.EventTime, &r.Operation, &r.Kind, &r.Namespace,
			&r.Name, &r.UserName, &r.ActorType, &r.Classes,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// statsEpoch is the sentinel max(if(...)) yields when a resource has no
// activity before the incident window.
var statsEpoch = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)

const resourceStatsQuery = `SELECT namespace, kind, name,
    sum(change_count) AS total,
    uniqExact(day) AS days_active,
    max(if(day < ?, day, toDate('1970-01-01'))) AS last_prior_day,
    sum(if(day < ?, change_count, 0)) AS prior_total
FROM resource_change_stats
WHERE cluster = ? AND day >= ?
GROUP BY namespace, kind, name`

// statsWindow returns the UTC day the incident window starts on and the
// 30-day floor for the stats query.
func statsWindow(windowStart time.Time) (windowDay, floor time.Time) {
	windowDay = windowStart.UTC().Truncate(24 * time.Hour)
	return windowDay, windowDay.AddDate(0, 0, -30)
}

// ResourceStats returns 30-day change stats for every resource in the
// cluster, keyed by resource identity.
func (s *Store) ResourceStats(ctx context.Context, cluster string, windowStart time.Time) (map[ResourceKey]ResourceStats, error) {
	windowDay, floor := statsWindow(windowStart)
	rows, err := s.conn.Query(ctx, resourceStatsQuery, windowDay, windowDay, cluster, floor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[ResourceKey]ResourceStats{}
	for rows.Next() {
		var k ResourceKey
		var total, priorTotal, daysActive uint64
		var lastPrior time.Time
		if err := rows.Scan(&k.Namespace, &k.Kind, &k.Name, &total, &daysActive, &lastPrior, &priorTotal); err != nil {
			return nil, err
		}
		st := ResourceStats{PriorTotal: priorTotal}
		if daysActive > 0 {
			st.PerDay = float64(total) / float64(daysActive)
		}
		if lastPrior.After(statsEpoch) {
			st.LastPriorDay = lastPrior
		}
		out[k] = st
	}
	return out, rows.Err()
}
