// Package classify derives semantic change classes and actor types from
// Kubernetes change events, at hub ingest time.
package classify

import (
	"encoding/json"
	"sort"
	"strings"
)

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

// diffChange mirrors internal/diff.Change with raw values, so container
// arrays can be re-parsed without a package dependency.
type diffChange struct {
	Path string          `json:"path"`
	Op   string          `json:"op"`
	Old  json.RawMessage `json:"old,omitempty"`
	New  json.RawMessage `json:"new,omitempty"`
}

var rbacKinds = map[string]bool{
	"Role": true, "ClusterRole": true, "RoleBinding": true,
	"ClusterRoleBinding": true, "ServiceAccount": true,
}

var networkKinds = map[string]bool{"NetworkPolicy": true, "Ingress": true}

var configKinds = map[string]bool{"ConfigMap": true, "Secret": true}

// containerPaths are diff paths where a whole container array is replaced
// (the diff compares arrays as whole values).
var containerPaths = []string{
	"spec.containers",
	"spec.template.spec.containers",
	"spec.jobTemplate.spec.template.spec.containers",
}

var noisePaths = []string{
	"status", "metadata.resourceVersion", "metadata.generation", "metadata.managedFields",
}

// Classify derives the semantic change classes for one event. It always
// returns a sorted, non-empty list.
func Classify(kind, subResource, operation, diffJSON string) []string {
	if operation != "UPDATE" {
		return createDeleteClasses(kind)
	}
	var changes []diffChange
	if err := json.Unmarshal([]byte(diffJSON), &changes); err != nil {
		return []string{ClassOther}
	}
	set := map[string]bool{}
	substantive := false
	metaOnly := false
	for _, c := range changes {
		switch {
		case isNoisePath(c.Path):
			continue
		case isMetadataPath(c.Path):
			metaOnly = true
		default:
			substantive = true
			classifyPath(kind, c, set)
		}
	}
	if subResource == "scale" {
		set[ClassScale] = true
		substantive = true
	}
	if !substantive {
		if metaOnly {
			return []string{ClassMetadataOnly}
		}
		return []string{ClassStatusOnly}
	}
	if rbacKinds[kind] {
		set[ClassRBAC] = true
	}
	if networkKinds[kind] {
		set[ClassNetwork] = true
	}
	if len(set) == 0 {
		set[ClassOther] = true
	}
	return sortedKeys(set)
}

// classifyPath maps one substantive diff path to zero or more classes.
func classifyPath(kind string, c diffChange, set map[string]bool) {
	switch {
	case configKinds[kind] && (pathUnder(c.Path, "data") || pathUnder(c.Path, "stringData") || pathUnder(c.Path, "binaryData")):
		set[ClassConfigData] = true
	case kind == "Service" && pathUnder(c.Path, "spec"):
		set[ClassNetwork] = true
	case c.Path == "spec.replicas":
		set[ClassScale] = true
	case isContainersPath(c.Path):
		for _, cl := range containerClasses(c.Old, c.New) {
			set[cl] = true
		}
	}
}

// createDeleteClasses classifies CREATE/DELETE by kind only; such events are
// never noise.
func createDeleteClasses(kind string) []string {
	set := map[string]bool{}
	if rbacKinds[kind] {
		set[ClassRBAC] = true
	}
	if networkKinds[kind] {
		set[ClassNetwork] = true
	}
	if configKinds[kind] {
		set[ClassConfigData] = true
	}
	if len(set) == 0 {
		set[ClassOther] = true
	}
	return sortedKeys(set)
}

// containerClasses is completed in the container-classification task.
func containerClasses(oldRaw, newRaw json.RawMessage) []string {
	return []string{ClassOther}
}

func pathUnder(p, prefix string) bool {
	return p == prefix || strings.HasPrefix(p, prefix+".")
}

func isNoisePath(p string) bool {
	for _, n := range noisePaths {
		if pathUnder(p, n) {
			return true
		}
	}
	return false
}

func isMetadataPath(p string) bool {
	return pathUnder(p, "metadata.labels") || pathUnder(p, "metadata.annotations")
}

func isContainersPath(p string) bool {
	for _, cp := range containerPaths {
		if pathUnder(p, cp) {
			return true
		}
	}
	return false
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
