// Package redact removes Kubernetes Secret values from admission bodies before
// the agent forwards them, keeping every key so the hub can still record which
// keys a change touched.
package redact

import (
	"bytes"
	"encoding/json"
	"errors"
)

// The markers that replace a Secret value. They are literal strings, never
// derived from the value: Marker where a key's value is the same on both
// sides of the change (or the key exists on one side only), MarkerBefore and
// MarkerAfter where it differs, which is what makes the hub's diff record the
// key as changed without saying anything about either value.
const (
	Marker       = "<redacted>"
	MarkerBefore = "<redacted:before>"
	MarkerAfter  = "<redacted:after>"
)

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

// Secret returns oldRaw and newRaw with every Secret value replaced by a
// marker. An empty input stays empty (a CREATE has no old body, a DELETE no
// new one). It returns ErrUnreadable when either body cannot be emptied with
// certainty.
func Secret(oldRaw, newRaw []byte) (oldOut, newOut []byte, err error) {
	oldObj, err := body(oldRaw)
	if err != nil {
		return nil, nil, err
	}
	newObj, err := body(newRaw)
	if err != nil {
		return nil, nil, err
	}

	for _, field := range valueFields {
		oldValues, newValues := valueMap(oldObj, field), valueMap(newObj, field)
		changed := changedKeys(oldValues, newValues)
		mark(oldValues, changed, MarkerBefore)
		mark(newValues, changed, MarkerAfter)
	}
	redactLastApplied(oldObj)
	redactLastApplied(newObj)

	if oldOut, err = encode(oldRaw, oldObj); err != nil {
		return nil, nil, err
	}
	if newOut, err = encode(newRaw, newObj); err != nil {
		return nil, nil, err
	}
	return oldOut, newOut, nil
}

// body parses one object body and rejects it unless every value field it has
// is a map of string to string. An empty body yields a nil object.
func body(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	obj, err := decode(raw)
	if err != nil {
		return nil, ErrUnreadable
	}
	for _, field := range valueFields {
		if err := checkField(obj, field); err != nil {
			return nil, err
		}
	}
	return obj, nil
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

// encode marshals a redacted body, leaving an empty body empty.
func encode(raw []byte, obj map[string]any) ([]byte, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, ErrUnreadable
	}
	return out, nil
}

// checkField reports whether one value field can be emptied with certainty: it
// is absent, or a map whose every value is a string.
func checkField(obj map[string]any, field string) error {
	v, ok := obj[field]
	if !ok || v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return ErrUnreadable
	}
	for _, val := range m {
		if _, ok := val.(string); !ok {
			return ErrUnreadable
		}
	}
	return nil
}

// valueMap returns one value field's map, or nil when the body or the field is
// absent. checkField has already proven the shape.
func valueMap(obj map[string]any, field string) map[string]any {
	if obj == nil {
		return nil
	}
	m, _ := obj[field].(map[string]any)
	return m
}

// changedKeys returns the keys held on both sides whose values differ. A key
// on one side only is not listed: the hub's diff already records it as an add
// or a remove.
func changedKeys(oldValues, newValues map[string]any) map[string]bool {
	changed := map[string]bool{}
	for k, oldVal := range oldValues {
		if newVal, ok := newValues[k]; ok && oldVal != newVal {
			changed[k] = true
		}
	}
	return changed
}

// mark replaces every value with its marker: sideMarker for a key whose value
// changed, Marker for every other.
func mark(values map[string]any, changed map[string]bool, sideMarker string) {
	for k := range values {
		if changed[k] {
			values[k] = sideMarker
			continue
		}
		values[k] = Marker
	}
}

// redactLastApplied empties the values inside the last-applied-configuration
// annotation, which holds a second copy of the whole object. Its values take
// the plain marker on both sides: a value-only change is already recorded on
// the body's own key, and repeating it here would say the same thing twice.
// An annotation whose JSON will not parse is replaced wholesale: the body
// itself is intact, so the change keeps its keys, but this one field cannot be
// proven empty.
func redactLastApplied(obj map[string]any) {
	if obj == nil {
		return
	}
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
		if err := checkField(inner, field); err != nil {
			ann[LastAppliedAnnotation] = Marker
			return
		}
		mark(valueMap(inner, field), nil, Marker)
	}
	out, err := json.Marshal(inner)
	if err != nil {
		ann[LastAppliedAnnotation] = Marker
		return
	}
	ann[LastAppliedAnnotation] = string(out)
}
