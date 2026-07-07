package storage

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Filter holds the structured, indexable search dimensions for the dashboard.
type Filter struct {
	Cluster   string
	Namespace string
	Kind      string
	Name      string // substring, case-insensitive
	User      string // substring, case-insensitive
	Q         string // substring, case-insensitive; matches name OR user_name
	Operation string
	From      time.Time // zero => unbounded
	To        time.Time // zero => unbounded

	ExcludeKinds      []string // exact kinds to exclude (ignore filter)
	ExcludeNamespaces []string // exact namespaces to exclude (ignore filter)
}

// Cursor is a keyset position: the (event_time, event_id) of a row.
type Cursor struct {
	Time time.Time
	ID   uuid.UUID
}

// EncodeCursor renders a cursor as an opaque base64 token.
func EncodeCursor(c Cursor) string {
	raw := fmt.Sprintf("%d:%s", c.Time.UnixNano(), c.ID.String())
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses an opaque cursor token.
func DecodeCursor(s string) (Cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("cursor: %w", err)
	}
	parts := strings.SplitN(string(b), ":", 2)
	if len(parts) != 2 {
		return Cursor{}, fmt.Errorf("cursor: malformed")
	}
	ns, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return Cursor{}, fmt.Errorf("cursor: bad time: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return Cursor{}, fmt.Errorf("cursor: bad id: %w", err)
	}
	return Cursor{Time: time.Unix(0, ns).UTC(), ID: id}, nil
}

// Row is a lightweight change-event row for the feed/search (no full objects).
type Row struct {
	EventID     string    `json:"event_id"`
	EventTime   time.Time `json:"event_time"`
	IngestedAt  time.Time `json:"ingested_at"`
	Cluster     string    `json:"cluster"`
	Source      string    `json:"source"`
	Operation   string    `json:"operation"`
	APIGroup    string    `json:"api_group"`
	APIVersion  string    `json:"api_version"`
	Kind        string    `json:"kind"`
	Namespace   string    `json:"namespace"`
	Name        string    `json:"name"`
	ResourceUID string    `json:"resource_uid"`
	SubResource string    `json:"sub_resource"`
	UserName    string    `json:"user_name"`
	UserGroups  []string  `json:"user_groups"`
	DryRun      bool      `json:"dry_run"`
	Diff        string    `json:"diff"`
}

// Detail is a full change event including the before/after objects.
type Detail struct {
	Row
	OldObject string `json:"old_object"`
	NewObject string `json:"new_object"`
	UserUID   string `json:"user_uid"`
	UserAgent string `json:"user_agent"`
}

// Page is one keyset page of rows plus the cursor for the next (older) page.
type Page struct {
	Rows       []Row  `json:"events"`
	NextCursor string `json:"next_cursor"`
}

// Bucket is one time bucket of the activity histogram.
type Bucket struct {
	BucketStart time.Time `json:"bucket_start"`
	Count       uint64    `json:"count"`
}

// Facets are distinct low-cardinality filter values for the UI dropdowns.
type Facets struct {
	Clusters   []string `json:"clusters"`
	Namespaces []string `json:"namespaces"`
	Kinds      []string `json:"kinds"`
	Operations []string `json:"operations"`
}

// ListParams are the inputs to ListEvents.
type ListParams struct {
	Filter Filter
	Cursor string // opaque; "" => from newest
	Since  string // opaque; "" => not a since-query
	Limit  int
}

const listColumns = `event_id, event_time, ingested_at, cluster, source, operation, ` +
	`api_group, api_version, kind, namespace, name, resource_uid, sub_resource, ` +
	`user_name, user_groups, dry_run, diff`

const detailColumns = listColumns + `, old_object, new_object, user_uid, user_agent`

const getEventQuery = "SELECT " + detailColumns + " FROM change_events WHERE event_id = ? LIMIT 1"

const (
	facetClustersQuery   = "SELECT DISTINCT cluster FROM change_events ORDER BY cluster"
	facetNamespacesQuery = "SELECT DISTINCT namespace FROM change_events WHERE namespace != '' ORDER BY namespace"
	facetKindsQuery      = "SELECT DISTINCT kind FROM change_events ORDER BY kind"
	facetOperationsQuery = "SELECT DISTINCT operation FROM change_events ORDER BY operation"
)

var bucketExprs = map[string]string{
	"minute": "toStartOfMinute(event_time)",
	"hour":   "toStartOfHour(event_time)",
	"day":    "toStartOfDay(event_time)",
}

// IsValidBucket reports whether bucket is an accepted activity bucket unit.
// Handlers use it to return 400 for a bad unit before hitting the database.
func IsValidBucket(bucket string) bool {
	_, ok := bucketExprs[bucket]
	return ok
}

// condBuilder accumulates WHERE conditions and their bound args in lockstep so
// the arg order always matches the ? placeholder order.
type condBuilder struct {
	conds []string
	args  []any
}

func (b *condBuilder) add(cond string, arg any) {
	b.conds = append(b.conds, cond)
	b.args = append(b.args, arg)
}

func (b *condBuilder) where() string {
	if len(b.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(b.conds, " AND ")
}

func filterConds(f Filter) *condBuilder {
	b := &condBuilder{}
	if f.Cluster != "" {
		b.add("cluster = ?", f.Cluster)
	}
	if f.Namespace != "" {
		b.add("namespace = ?", f.Namespace)
	}
	if f.Kind != "" {
		b.add("kind = ?", f.Kind)
	}
	if f.Operation != "" {
		b.add("operation = ?", f.Operation)
	}
	if f.Name != "" {
		b.add("name ILIKE ?", "%"+f.Name+"%")
	}
	if f.User != "" {
		b.add("user_name ILIKE ?", "%"+f.User+"%")
	}
	if f.Q != "" {
		p := "%" + f.Q + "%"
		b.conds = append(b.conds, "(name ILIKE ? OR user_name ILIKE ?)")
		b.args = append(b.args, p, p)
	}
	if len(f.ExcludeKinds) > 0 {
		b.add("kind NOT IN (?)", f.ExcludeKinds)
	}
	if len(f.ExcludeNamespaces) > 0 {
		b.add("namespace NOT IN (?)", f.ExcludeNamespaces)
	}
	if !f.From.IsZero() {
		b.add("event_time >= ?", f.From)
	}
	if !f.To.IsZero() {
		b.add("event_time <= ?", f.To)
	}
	return b
}

func buildListQuery(f Filter, older, newer *Cursor, limit int) (string, []any) {
	b := filterConds(f)
	if older != nil {
		b.add("(event_time, event_id) < (?, ?)", older.Time)
		b.args = append(b.args, older.ID) // second placeholder of the tuple
	}
	if newer != nil {
		b.add("(event_time, event_id) > (?, ?)", newer.Time)
		b.args = append(b.args, newer.ID)
	}
	order := "event_time DESC, event_id DESC"
	if newer != nil {
		// since-polling: ascend so the page is contiguous forward from the cursor
		// (no rows skipped between polls under burst)
		order = "event_time ASC, event_id ASC"
	}
	q := "SELECT " + listColumns + " FROM change_events" + b.where() +
		" ORDER BY " + order + " LIMIT ?"
	return q, append(b.args, limit)
}

func buildActivityQuery(f Filter, bucket string) (string, []any, error) {
	expr, ok := bucketExprs[bucket]
	if !ok {
		return "", nil, fmt.Errorf("invalid bucket %q", bucket)
	}
	b := filterConds(f)
	q := "SELECT " + expr + " AS bucket_start, count() AS c FROM change_events" +
		b.where() + " GROUP BY bucket_start ORDER BY bucket_start ASC"
	return q, b.args, nil
}
