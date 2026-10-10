package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// ListEvents returns one keyset page of lightweight rows matching p.Filter,
// newest first. Cursor pages older; Since fetches rows newer than a cursor.
func (s *Store) ListEvents(ctx context.Context, p ListParams) (Page, error) {
	var older, newer *Cursor
	if p.Cursor != "" {
		c, err := DecodeCursor(p.Cursor)
		if err != nil {
			return Page{}, err
		}
		older = &c
	}
	if p.Since != "" {
		c, err := DecodeCursor(p.Since)
		if err != nil {
			return Page{}, err
		}
		newer = &c
	}
	q, args := buildListQuery(p.Filter, older, newer, p.Limit)
	rows, err := s.conn.Query(ctx, q, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()

	page := Page{Rows: []Row{}}
	for rows.Next() {
		var r Row
		var dry uint8
		if err := rows.Scan(
			&r.EventID, &r.EventTime, &r.IngestedAt, &r.Cluster, &r.Source, &r.Operation,
			&r.APIGroup, &r.APIVersion, &r.Kind, &r.Namespace, &r.Name, &r.ResourceUID, &r.SubResource,
			&r.UserName, &r.UserGroups, &dry, &r.Diff,
		); err != nil {
			return Page{}, err
		}
		r.DryRun = dry != 0
		page.Rows = append(page.Rows, r)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	if len(page.Rows) > 0 {
		last := page.Rows[len(page.Rows)-1]
		if id, err := uuid.Parse(last.EventID); err == nil {
			switch {
			case p.Since != "":
				// since-poll (ASC): last row is the newest; advance `since` to it
				// whenever any rows were returned, so the next poll continues forward.
				page.NextCursor = EncodeCursor(Cursor{Time: last.EventTime, ID: id})
			case p.Limit > 0 && len(page.Rows) == p.Limit:
				// older-pagination (DESC): last row is the oldest; cursor the next page.
				page.NextCursor = EncodeCursor(Cursor{Time: last.EventTime, ID: id})
			}
		}
	}
	return page, nil
}

// GetEvent returns the full detail of one change. found is false if no row exists.
func (s *Store) GetEvent(ctx context.Context, id string) (Detail, bool, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return Detail{}, false, fmt.Errorf("bad event id: %w", err)
	}
	var d Detail
	var dry uint8
	err = s.conn.QueryRow(ctx, getEventQuery, uid).Scan(
		&d.EventID, &d.EventTime, &d.IngestedAt, &d.Cluster, &d.Source, &d.Operation,
		&d.APIGroup, &d.APIVersion, &d.Kind, &d.Namespace, &d.Name, &d.ResourceUID, &d.SubResource,
		&d.UserName, &d.UserGroups, &dry, &d.Diff,
		&d.OldObject, &d.NewObject, &d.UserUID, &d.UserAgent,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Detail{}, false, nil
	}
	if err != nil {
		return Detail{}, false, err
	}
	d.DryRun = dry != 0
	return d, true, nil
}

// Activity returns time-bucketed change counts matching f. bucket is one of
// "minute", "hour", "day".
func (s *Store) Activity(ctx context.Context, f Filter, bucket string) ([]Bucket, error) {
	q, args, err := buildActivityQuery(f, bucket)
	if err != nil {
		return nil, err
	}
	rows, err := s.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Bucket{}
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.BucketStart, &b.Count); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Facets returns distinct low-cardinality filter values for the UI.
func (s *Store) Facets(ctx context.Context) (Facets, error) {
	clusters, err := s.distinct(ctx, facetClustersQuery)
	if err != nil {
		return Facets{}, err
	}
	namespaces, err := s.distinct(ctx, facetNamespacesQuery)
	if err != nil {
		return Facets{}, err
	}
	kinds, err := s.distinct(ctx, facetKindsQuery)
	if err != nil {
		return Facets{}, err
	}
	ops, err := s.distinct(ctx, facetOperationsQuery)
	if err != nil {
		return Facets{}, err
	}
	return Facets{Clusters: clusters, Namespaces: namespaces, Kinds: kinds, Operations: ops}, nil
}

func (s *Store) distinct(ctx context.Context, q string) ([]string, error) {
	rows, err := s.conn.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// clusterOpRow is one (cluster, operation) aggregate from clustersQuery.
type clusterOpRow struct {
	Cluster, Operation string
	Last               time.Time
	Count              uint64
}

// foldClusters merges per-operation rows into per-cluster summaries, newest
// last event first (ties broken by cluster name). Never returns nil.
func foldClusters(rows []clusterOpRow) []ClusterSummary {
	byCluster := map[string]*ClusterSummary{}
	for _, r := range rows {
		c, ok := byCluster[r.Cluster]
		if !ok {
			c = &ClusterSummary{Cluster: r.Cluster, ByOperation: map[string]uint64{}}
			byCluster[r.Cluster] = c
		}
		if r.Last.After(c.LastEventTime) {
			c.LastEventTime = r.Last
		}
		if r.Count > 0 {
			c.ByOperation[r.Operation] += r.Count
			c.Total += r.Count
		}
	}
	out := make([]ClusterSummary, 0, len(byCluster))
	for _, c := range byCluster {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastEventTime.Equal(out[j].LastEventTime) {
			return out[i].LastEventTime.After(out[j].LastEventTime)
		}
		return out[i].Cluster < out[j].Cluster
	})
	return out
}

// Clusters returns every cluster seen in change_events with its last event
// time and non-dry-run change counts (total and per operation) since `since`,
// newest last event first.
func (s *Store) Clusters(ctx context.Context, since time.Time) ([]ClusterSummary, error) {
	rows, err := s.conn.Query(ctx, clustersQuery, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agg []clusterOpRow
	for rows.Next() {
		var r clusterOpRow
		if err := rows.Scan(&r.Cluster, &r.Operation, &r.Last, &r.Count); err != nil {
			return nil, err
		}
		agg = append(agg, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return foldClusters(agg), nil
}
