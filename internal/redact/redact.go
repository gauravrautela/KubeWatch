// Package redact removes Kubernetes Secret values from admission bodies before
// the agent forwards them, keeping every key so the hub can still record which
// keys a change touched.
package redact

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Marker replaces a Secret value. It is a literal string, not derived from the
// value, so nothing about the value survives it.
const Marker = "<redacted>"

// LastAppliedAnnotation carries a whole copy of the object, values included,
// when a Secret is applied with kubectl.
const LastAppliedAnnotation = "kubectl.kubernetes.io/last-applied-configuration"

// valueFields are the Secret fields that hold values. binaryData is not one:
// the Kubernetes Secret API has data and stringData only.
var valueFields = []string{"data", "stringData"}

// ErrUnreadable reports a body the agent cannot prove it has emptied: one that
// is not a JSON object, or whose data/stringData is not a map of string to
// string. The caller forwards neither body rather than risk a value escaping.
var ErrUnreadable = errors.New("redact: unreadable Secret body")

// Secret returns oldRaw and newRaw with every Secret value replaced by Marker.
// An empty input stays empty (a CREATE has no old body, a DELETE no new one).
// It returns ErrUnreadable when either body cannot be emptied with certainty.
func Secret(oldRaw, newRaw []byte) (oldOut, newOut []byte, err error) {
	oldOut, err = one(oldRaw)
	if err != nil {
		return nil, nil, err
	}
	newOut, err = one(newRaw)
	if err != nil {
		return nil, nil, err
	}
	return oldOut, newOut, nil
}

// one redacts a single body, leaving everything but the Secret's values as it
// found it.
func one(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	obj, err := decode(raw)
	if err != nil {
		return nil, ErrUnreadable
	}
	for _, field := range valueFields {
		if err := redactField(obj, field); err != nil {
			return nil, err
		}
	}
	redactLastApplied(obj)
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, ErrUnreadable
	}
	return out, nil
}

// decode parses a JSON object, keeping numbers as written so re-marshaling a
// body does not reformat fields the change never touched.
func decode(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, errors.New("redact: body is not a JSON object")
	}
	return obj, nil
}

// redactField replaces every value of one Secret value field. A field that is
// absent is nothing to do; a field that is present but not a map of string to
// string cannot be emptied with certainty.
func redactField(obj map[string]any, field string) error {
	v, ok := obj[field]
	if !ok || v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return ErrUnreadable
	}
	for k, val := range m {
		if _, ok := val.(string); !ok {
			return ErrUnreadable
		}
		m[k] = Marker
	}
	return nil
}

// redactLastApplied empties the values inside the last-applied-configuration
// annotation, which holds a second copy of the whole object. An annotation
// whose JSON will not parse is replaced wholesale: the body itself is intact,
// so the change keeps its keys, but this one field cannot be proven empty.
func redactLastApplied(obj map[string]any) {
	meta, ok := obj["metadata"].(map[string]any)
	if !ok {
		return
	}
	ann, ok := meta["annotations"].(map[string]any)
	if !ok {
		return
	}
	raw, ok := ann[LastAppliedAnnotation].(string)
	if !ok {
		return
	}
	inner, err := decode([]byte(raw))
	if err != nil {
		ann[LastAppliedAnnotation] = Marker
		return
	}
	for _, field := range valueFields {
		if err := redactField(inner, field); err != nil {
			ann[LastAppliedAnnotation] = Marker
			return
		}
	}
	out, err := json.Marshal(inner)
	if err != nil {
		ann[LastAppliedAnnotation] = Marker
		return
	}
	ann[LastAppliedAnnotation] = string(out)
}
