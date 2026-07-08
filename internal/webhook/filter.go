package webhook

import (
	"strings"

	"github.com/gauravrautela/kubewatch/internal/event"
)

// ExcludeKinds wraps sink, dropping events whose Kind is in kinds. An empty
// set returns sink unchanged.
func ExcludeKinds(sink Sink, kinds map[string]bool) Sink {
	if len(kinds) == 0 {
		return sink
	}
	return func(ev event.ChangeEvent) {
		if kinds[ev.Kind] {
			return
		}
		sink(ev)
	}
}

// ParseKindList parses a comma-separated kind list into a set, trimming
// whitespace and dropping empty elements. The literal "none" yields an empty
// set, letting operators disable the default exclusions explicitly.
func ParseKindList(s string) map[string]bool {
	out := map[string]bool{}
	if strings.TrimSpace(s) == "none" {
		return out
	}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out[p] = true
		}
	}
	return out
}
