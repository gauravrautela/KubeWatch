package webhook

import (
	"reflect"
	"testing"

	"github.com/gauravrautela/kubewatch/internal/event"
)

func TestExcludeKindsDropsListedKinds(t *testing.T) {
	var got []string
	sink := ExcludeKinds(func(ev event.ChangeEvent) { got = append(got, ev.Kind) },
		map[string]bool{"Lease": true, "Event": true})

	for _, kind := range []string{"Lease", "Deployment", "Event", "ConfigMap"} {
		sink(event.ChangeEvent{Kind: kind})
	}
	if want := []string{"Deployment", "ConfigMap"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("passed kinds = %v, want %v", got, want)
	}
}

func TestExcludeKindsEmptySetPassesEverything(t *testing.T) {
	var n int
	sink := ExcludeKinds(func(event.ChangeEvent) { n++ }, nil)
	sink(event.ChangeEvent{Kind: "Lease"})
	if n != 1 {
		t.Fatalf("want passthrough with empty set, got %d calls", n)
	}
}

func TestParseKindList(t *testing.T) {
	cases := []struct {
		in   string
		want map[string]bool
	}{
		{"Lease,Event", map[string]bool{"Lease": true, "Event": true}},
		{" Lease , Event ,", map[string]bool{"Lease": true, "Event": true}},
		{"none", map[string]bool{}},
		{"", map[string]bool{}},
	}
	for _, c := range cases {
		if got := ParseKindList(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseKindList(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
