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

func walk(path string, oldV, newV any, out *[]Change) {
	switch {
	case oldV == nil && newV == nil:
		return
	case oldV == nil:
		newMap, newIsMap := newV.(map[string]any)
		if newIsMap {
			for _, k := range unionKeys(map[string]any{}, newMap) {
				walk(join(path, k), nil, newMap[k], out)
			}
			return
		}
		*out = append(*out, Change{Path: path, Op: "add", New: newV})
	case newV == nil:
		oldMap, oldIsMap := oldV.(map[string]any)
		if oldIsMap {
			for _, k := range unionKeys(oldMap, map[string]any{}) {
				walk(join(path, k), oldMap[k], nil, out)
			}
			return
		}
		*out = append(*out, Change{Path: path, Op: "remove", Old: oldV})
	default:
		oldMap, oldIsMap := oldV.(map[string]any)
		newMap, newIsMap := newV.(map[string]any)
		if oldIsMap && newIsMap {
			for _, k := range unionKeys(oldMap, newMap) {
				walk(join(path, k), oldMap[k], newMap[k], out)
			}
			return
		}
		if !reflect.DeepEqual(oldV, newV) {
			*out = append(*out, Change{Path: path, Op: "replace", Old: oldV, New: newV})
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
