// Package rank scores and groups change events for incident triage. Scoring
// is deterministic; most non-neutral factors emit a human-readable reason
// chip.
package rank

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/gauravrautela/kubewatch/internal/classify"
)

// Event is one change event in the incident window.
type Event struct {
	EventID   string
	EventTime time.Time
	Operation string
	Kind      string
	Namespace string
	Name      string
	UserName  string
	ActorType string
	Classes   []string
}

// Key identifies one resource.
type Key struct{ Namespace, Kind, Name string }

// Stats is a resource's 30-day change history.
type Stats struct {
	PerDay       float64
	LastPriorDay time.Time // zero => no activity before the window
	PriorTotal   uint64
}

// scoreEvent returns the event's score (0..100) and its reason chips.
func scoreEvent(e Event, st Stats, hasStats bool, at time.Time) (float64, []string) {
	var reasons []string
	cw, cChip := classWeight(e.Classes)
	if cChip != "" {
		reasons = append(reasons, cChip)
	}
	aw, aChip := actorWeight(e.ActorType, e.UserName)
	if aChip != "" {
		reasons = append(reasons, aChip)
	}
	rw, rChip := rarityWeight(st, hasStats, at)
	if rChip != "" {
		reasons = append(reasons, rChip)
	}
	s := 100 * recency(at, e.EventTime) * cw * aw * rw
	return math.Min(s, 100), reasons
}

// recency decays exponentially with distance from the incident time; events
// after it are kept for clock skew but decay afterDecayFactor times faster.
func recency(at, t time.Time) float64 {
	if !t.After(at) {
		return math.Pow(0.5, at.Sub(t).Minutes()/halfLifeBefore.Minutes())
	}
	return math.Pow(0.5, t.Sub(at).Minutes()/(halfLifeBefore.Minutes()/afterDecayFactor))
}

// classWeight is the max weight across the event's classes, with the chip of
// the class that supplied it. Classless events score as "other".
func classWeight(classes []string) (float64, string) {
	if len(classes) == 0 {
		return classWeights[classify.ClassOther], classChips[classify.ClassOther]
	}
	best, chip := 0.0, ""
	for _, c := range classes {
		w, ok := classWeights[c]
		if !ok {
			w = classWeights[classify.ClassOther]
		}
		if w > best {
			best, chip = w, classChips[c]
		}
	}
	return best, chip
}

func actorWeight(actorType, userName string) (float64, string) {
	switch actorType {
	case classify.ActorHuman:
		return weightHuman, "human: " + userName
	case classify.ActorServiceAccount:
		return weightServiceAccount, ""
	case classify.ActorSystem:
		return weightSystem, ""
	default:
		return weightUnknownActor, ""
	}
}

// rarityWeight dampens churners and boosts rarely-touched or brand-new
// resources. Missing stats are neutral so the endpoint works from day one.
func rarityWeight(st Stats, hasStats bool, at time.Time) (float64, string) {
	if !hasStats {
		return 1.0, ""
	}
	if st.PerDay > churnPerDayMax {
		return weightChurn, fmt.Sprintf("changes ~%.0f×/day", st.PerDay)
	}
	if st.PriorTotal == 0 {
		return weightRare, "new resource"
	}
	gapDays := int(at.Sub(st.LastPriorDay).Hours() / 24)
	if gapDays >= rareGapDays {
		return weightRare, fmt.Sprintf("first change in %dd", gapDays)
	}
	return 1.0, ""
}

// EventSummary is the per-event payload inside a Suspect.
type EventSummary struct {
	EventID   string    `json:"event_id"`
	EventTime time.Time `json:"event_time"`
	Operation string    `json:"operation"`
	Classes   []string  `json:"classes"`
	ActorType string    `json:"actor_type"`
	UserName  string    `json:"user_name"`
}

// Suspect is one ranked resource group.
type Suspect struct {
	Namespace       string         `json:"namespace"`
	Kind            string         `json:"kind"`
	Name            string         `json:"name"`
	Score           int            `json:"score"`
	Reasons         []string       `json:"reasons"`
	EventCount      int            `json:"event_count"`
	LatestEventTime time.Time      `json:"latest_event_time"`
	Events          []EventSummary `json:"events"`
}

// Rank scores every event, groups by resource, and returns at most limit
// suspects ordered by score (desc), latest event time (desc), then name.
// events must be ordered newest-first.
func Rank(events []Event, stats map[Key]Stats, at time.Time, limit int) []Suspect {
	type group struct {
		best    float64
		reasons []string
		count   int
		latest  time.Time
		events  []EventSummary
	}
	groups := map[Key]*group{}
	var order []Key // stable iteration for determinism
	for _, e := range events {
		k := Key{Namespace: e.Namespace, Kind: e.Kind, Name: e.Name}
		st, ok := stats[k]
		score, reasons := scoreEvent(e, st, ok, at)
		g := groups[k]
		if g == nil {
			g = &group{}
			groups[k] = g
			order = append(order, k)
		}
		if score > g.best || g.count == 0 {
			g.best, g.reasons = score, reasons
		}
		if e.EventTime.After(g.latest) {
			g.latest = e.EventTime
		}
		g.count++
		if len(g.events) < maxEventsPerSuspect {
			g.events = append(g.events, EventSummary{
				EventID: e.EventID, EventTime: e.EventTime, Operation: e.Operation,
				Classes: e.Classes, ActorType: e.ActorType, UserName: e.UserName,
			})
		}
	}

	suspects := make([]Suspect, 0, len(groups))
	for _, k := range order {
		g := groups[k]
		reasons := g.reasons
		if reasons == nil {
			reasons = []string{}
		}
		suspects = append(suspects, Suspect{
			Namespace: k.Namespace, Kind: k.Kind, Name: k.Name,
			Score:   int(math.Round(g.best)),
			Reasons: reasons, EventCount: g.count,
			LatestEventTime: g.latest, Events: g.events,
		})
	}
	sort.SliceStable(suspects, func(i, j int) bool {
		if suspects[i].Score != suspects[j].Score {
			return suspects[i].Score > suspects[j].Score
		}
		if !suspects[i].LatestEventTime.Equal(suspects[j].LatestEventTime) {
			return suspects[i].LatestEventTime.After(suspects[j].LatestEventTime)
		}
		return suspects[i].Name < suspects[j].Name
	})
	if limit > 0 && len(suspects) > limit {
		suspects = suspects[:limit]
	}
	return suspects
}
