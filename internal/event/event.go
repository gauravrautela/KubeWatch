// Package event defines the normalized change-event model shared by the agent
// (which produces it) and the hub (which stores it).
package event

import "time"

// Operation is the Kubernetes verb that produced a change.
type Operation string

const (
	OpCreate Operation = "CREATE"
	OpUpdate Operation = "UPDATE"
	OpDelete Operation = "DELETE"
)

// ChangeEvent is the normalized representation of a single Kubernetes change.
// Produced by the agent from an AdmissionReview, forwarded to the hub, and
// stored as one ClickHouse row.
type ChangeEvent struct {
	EventID     string    `json:"event_id"`
	EventTime   time.Time `json:"event_time"`
	Cluster     string    `json:"cluster"` // set by the hub from the agent token
	Source      string    `json:"source"`  // "webhook" | "reconcile"
	Operation   Operation `json:"operation"`
	APIGroup    string    `json:"api_group"`
	APIVersion  string    `json:"api_version"`
	Kind        string    `json:"kind"`
	Namespace   string    `json:"namespace"`
	Name        string    `json:"name"`
	ResourceUID string    `json:"resource_uid"`
	SubResource string    `json:"sub_resource"`
	UserName    string    `json:"user_name"`
	UserGroups  []string  `json:"user_groups"`
	UserUID     string    `json:"user_uid"`
	UserAgent   string    `json:"user_agent"` // reserved; webhook source leaves empty
	DryRun      bool      `json:"dry_run"`
	OldObject   string    `json:"old_object"`   // raw JSON, before
	NewObject   string    `json:"new_object"`   // raw JSON, after
	Diff        string    `json:"diff"`         // structured JSON diff, computed at hub
	ChangeClass []string  `json:"change_class"` // semantic classes, computed at hub
	ActorType   string    `json:"actor_type"`   // "unknown"|"human"|"serviceaccount"|"system", computed at hub
}

// Batch is the payload agents POST to the hub ingest API.
type Batch struct {
	Events []ChangeEvent `json:"events"`
}
