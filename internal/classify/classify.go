// Package classify derives semantic change classes and actor types from
// Kubernetes change events, at hub ingest time.
package classify

import "strings"

// Change classes stored in change_events.change_class.
const (
	ClassRBAC         = "rbac"
	ClassNetwork      = "network"
	ClassConfigData   = "config-data"
	ClassImage        = "image"
	ClassEnv          = "env"
	ClassResources    = "resources"
	ClassScale        = "scale"
	ClassMetadataOnly = "metadata-only"
	ClassStatusOnly   = "status-only"
	ClassOther        = "other"
)

// Actor types stored in change_events.actor_type.
const (
	ActorUnknown        = "unknown"
	ActorHuman          = "human"
	ActorServiceAccount = "serviceaccount"
	ActorSystem         = "system"
)

// ActorType derives the actor category from a request username.
func ActorType(userName string) string {
	switch {
	case userName == "":
		return ActorUnknown
	case strings.HasPrefix(userName, "system:serviceaccount:"):
		return ActorServiceAccount
	case strings.HasPrefix(userName, "system:"):
		return ActorSystem
	default:
		return ActorHuman
	}
}
