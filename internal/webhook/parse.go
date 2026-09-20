// Package webhook turns Kubernetes admission requests into change events and
// serves the validating webhook endpoint. It never blocks admission.
package webhook

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	admissionv1 "k8s.io/api/admission/v1"

	"github.com/gauravrautela/kubewatch/internal/event"
	"github.com/gauravrautela/kubewatch/internal/redact"
)

// secretKind is the kind whose values never leave the cluster.
const secretKind = "Secret"

type objectMeta struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		UID       string `json:"uid"`
	} `json:"metadata"`
}

func opFor(o admissionv1.Operation) (event.Operation, bool) {
	switch o {
	case admissionv1.Create:
		return event.OpCreate, true
	case admissionv1.Update:
		return event.OpUpdate, true
	case admissionv1.Delete:
		return event.OpDelete, true
	default:
		return "", false
	}
}

// Parse converts an AdmissionReview request into a normalized ChangeEvent. The
// bool is false when the request is nil or the operation is not one we record
// (e.g. CONNECT), in which case the caller should skip it.
func Parse(review *admissionv1.AdmissionReview) (event.ChangeEvent, bool, error) {
	req := review.Request
	if req == nil {
		return event.ChangeEvent{}, false, nil
	}
	op, ok := opFor(req.Operation)
	if !ok {
		return event.ChangeEvent{}, false, nil
	}

	// A Secret's values are emptied here, before the event exists: nothing
	// downstream can turn this off, and the kind filter runs later. A body the
	// agent cannot prove it has emptied costs both bodies — the change is
	// still recorded, with who, when, which Secret and the operation.
	oldRaw, newRaw := req.OldObject.Raw, req.Object.Raw
	failedClosed := false
	if req.Kind.Kind == secretKind {
		if o, n, rerr := redact.Secret(oldRaw, newRaw); rerr != nil {
			slog.Warn("webhook: forwarding a Secret change without its bodies",
				"namespace", req.Namespace, "name", req.Name, "err", rerr)
			oldRaw, newRaw = nil, nil
			failedClosed = true
		} else {
			oldRaw, newRaw = o, n
		}
	}

	ev := event.ChangeEvent{
		EventID:     uuid.NewString(),
		EventTime:   time.Now().UTC(),
		Source:      "webhook",
		Operation:   op,
		APIGroup:    req.Kind.Group,
		APIVersion:  req.Kind.Version,
		Kind:        req.Kind.Kind,
		Namespace:   req.Namespace,
		Name:        req.Name,
		SubResource: req.SubResource,
		UserName:    req.UserInfo.Username,
		UserGroups:  req.UserInfo.Groups,
		UserUID:     req.UserInfo.UID,
		OldObject:   string(oldRaw),
		NewObject:   string(newRaw),
	}
	if req.DryRun != nil {
		ev.DryRun = *req.DryRun
	}

	// On CREATE the request Name/UID may be empty; on DELETE the new object is
	// empty. Pull identity from whichever object body is present.
	raw := req.Object.Raw
	if len(raw) == 0 {
		raw = req.OldObject.Raw
	}
	if len(raw) > 0 {
		var m objectMeta
		if err := json.Unmarshal(raw, &m); err != nil {
			// A Secret we already failed closed on keeps the identity the
			// admission request itself carries rather than being dropped.
			if failedClosed {
				return ev, true, nil
			}
			return event.ChangeEvent{}, false, err
		}
		if ev.Name == "" {
			ev.Name = m.Metadata.Name
		}
		if ev.Namespace == "" {
			ev.Namespace = m.Metadata.Namespace
		}
		ev.ResourceUID = m.Metadata.UID
	}

	return ev, true, nil
}
