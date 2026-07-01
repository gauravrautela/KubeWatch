// Package diff computes a structured, path-oriented diff between two JSON
// documents (a Kubernetes object before and after a change).
package diff

import (
	"encoding/json"
	"reflect"
	"sort"
)

// Change describes one changed field path. Op is "add", "remove", or "replace".
type Change struct {
	Path string `json:"path"`
	Op   string `json:"op"`
	Old  any    `json:"old,omitempty"`
	New  any    `json:"new,omitempty"`
}

// Compute returns a JSON array of Change describing how newJSON differs from
// oldJSON. Nested objects are walked key by key; arrays and scalars are compared
// as whole values. Output order is deterministic (sorted by path).
func Compute(oldJSON, newJSON []byte) ([]byte, error) {
	var oldV, newV any
	if len(oldJSON) > 0 {
		if err := json.Unmarshal(oldJSON, &oldV); err != nil {
			return nil, err
		}
	}
	if len(newJSON) > 0 {
		if err := json.Unmarshal(newJSON, &newV); err != nil {
			return nil, err
		}
	}
	var changes []Change
	walk("", oldV, newV, &changes)
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	if changes == nil {
		changes = []Change{}
	}
	return json.Marshal(changes)
}

// walk is the top-level entry point. Below the top level, all map traversal
// is delegated to mapWalk, which decides add/remove/replace by key presence
// (v, ok := m[k]) rather than nil-ness, so an explicit JSON null value is
// never confused with an absent key.
func walk(path string, oldV, newV any, out *[]Change) {
	oldMap, oldIsMap := oldV.(map[string]any)
	newMap, newIsMap := newV.(map[string]any)
	switch {
	case oldIsMap && newIsMap:
		mapWalk(path, oldMap, newMap, out)
	case oldIsMap && newV == nil:
		// empty newJSON: every top-level key is a remove.
		mapWalk(path, oldMap, map[string]any{}, out)
	case newIsMap && oldV == nil:
		// empty oldJSON: every top-level key is an add.
		mapWalk(path, map[string]any{}, newMap, out)
	default:
		if oldV == nil && newV == nil {
			return
		}
		if !reflect.DeepEqual(oldV, newV) {
			op := "replace"
			switch {
			case oldV == nil:
				op = "add"
			case newV == nil:
				op = "remove"
			}
			*out = append(*out, Change{Path: path, Op: op, Old: oldV, New: newV})
		}
	}
}

// mapWalk compares two maps key by key using presence rather than nil-ness,
// so a key holding an explicit JSON null is distinguished from an absent key:
//   - present only in newMap  -> add
//   - present only in oldMap  -> remove
//   - present in both, both maps -> recurse
//   - present in both, otherwise  -> replace if unequal (reflect.DeepEqual),
//     which correctly covers value<->null transitions.
func mapWalk(path string, oldMap, newMap map[string]any, out *[]Change) {
	for _, k := range unionKeys(oldMap, newMap) {
		childPath := join(path, k)
		oldVal, oldOK := oldMap[k]
		newVal, newOK := newMap[k]
		switch {
		case oldOK && !newOK:
			*out = append(*out, Change{Path: childPath, Op: "remove", Old: oldVal})
		case !oldOK && newOK:
			*out = append(*out, Change{Path: childPath, Op: "add", New: newVal})
		default:
			oldChildMap, oldChildIsMap := oldVal.(map[string]any)
			newChildMap, newChildIsMap := newVal.(map[string]any)
			if oldChildIsMap && newChildIsMap {
				mapWalk(childPath, oldChildMap, newChildMap, out)
				continue
			}
			if !reflect.DeepEqual(oldVal, newVal) {
				*out = append(*out, Change{Path: childPath, Op: "replace", Old: oldVal, New: newVal})
			}
		}
	}
}

func unionKeys(a, b map[string]any) []string {
	seen := map[string]struct{}{}
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
