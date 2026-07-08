package rank

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/gauravrautela/kubewatch/internal/classify"
)

var at = time.Date(2026, 7, 8, 14, 0, 0, 0, time.UTC)

func ev(minsBefore float64, classes []string, actor, user string) Event {
	return Event{
		EventID:   "e1",
		EventTime: at.Add(-time.Duration(minsBefore * float64(time.Minute))),
		Operation: "UPDATE",
		Kind:      "Deployment", Namespace: "payments", Name: "checkout",
		UserName: user, ActorType: actor, Classes: classes,
	}
}

func TestRecencyDecay(t *testing.T) {
	base := ev(0, []string{classify.ClassScale}, classify.ActorServiceAccount, "sa")
	s0, _ := scoreEvent(base, Stats{}, false, at)
	if math.Abs(s0-100) > 0.01 { // 100 × 1.0 recency × 1.0 scale × 1.0 sa × 1.0 no-stats
		t.Fatalf("score at incident time = %v, want 100", s0)
	}
	s30, _ := scoreEvent(ev(30, []string{classify.ClassScale}, classify.ActorServiceAccount, "sa"), Stats{}, false, at)
	if math.Abs(s30-50) > 0.01 { // one half-life
		t.Fatalf("score 30m before = %v, want 50", s30)
	}
	after := base
	after.EventTime = at.Add(7*time.Minute + 30*time.Second) // one after-half-life (30m/4)
	sAfter, _ := scoreEvent(after, Stats{}, false, at)
	if math.Abs(sAfter-50) > 0.01 {
		t.Fatalf("score 7.5m after = %v, want 50", sAfter)
	}
}

func TestScoreCapAndWeights(t *testing.T) {
	// human image change at incident time: 100 × 1.5 × 1.5 = 225 → capped 100
	s, reasons := scoreEvent(ev(0, []string{classify.ClassImage}, classify.ActorHuman, "alice"), Stats{}, false, at)
	if s != 100 {
		t.Fatalf("want capped 100, got %v", s)
	}
	if !slices.Contains(reasons, "image change") || !slices.Contains(reasons, "human: alice") {
		t.Fatalf("missing chips, got %v", reasons)
	}

	// status-only system update: 100 × 0.1 × 0.6 = 6
	s2, _ := scoreEvent(ev(0, []string{classify.ClassStatusOnly}, classify.ActorSystem, "system:x"), Stats{}, false, at)
	if math.Abs(s2-6) > 0.01 {
		t.Fatalf("status-only system = %v, want 6", s2)
	}

	// multi-class takes the max weight: [scale, image] scores as image
	s3, _ := scoreEvent(ev(0, []string{classify.ClassScale, classify.ClassImage}, classify.ActorServiceAccount, "sa"), Stats{}, false, at)
	if s3 != 100 { // 100 × 1.5 × 1.0 = 150 → capped
		t.Fatalf("multi-class = %v, want 100", s3)
	}

	// empty classes score as other (0.8)
	s4, _ := scoreEvent(ev(0, nil, classify.ActorServiceAccount, "sa"), Stats{}, false, at)
	if math.Abs(s4-80) > 0.01 {
		t.Fatalf("classless = %v, want 80", s4)
	}
}

func TestRarityAndChurn(t *testing.T) {
	base := ev(0, []string{classify.ClassScale}, classify.ActorServiceAccount, "sa")

	// churner: heavily dampened, chip present
	s, reasons := scoreEvent(base, Stats{PerDay: 200, PriorTotal: 6000, LastPriorDay: at.AddDate(0, 0, -1)}, true, at)
	if math.Abs(s-20) > 0.01 { // 100 × 1.0 × 1.0 × 0.2
		t.Fatalf("churner = %v, want 20", s)
	}
	if !slices.Contains(reasons, "changes ~200×/day") {
		t.Fatalf("missing churn chip: %v", reasons)
	}

	// rare: first change in 20 days
	s2, reasons2 := scoreEvent(base, Stats{PerDay: 1, PriorTotal: 3, LastPriorDay: at.AddDate(0, 0, -20)}, true, at)
	if math.Abs(s2-100) > 0.01 { // 100 × 1.5 capped
		t.Fatalf("rare = %v, want 100", s2)
	}
	if !slices.Contains(reasons2, "first change in 20d") {
		t.Fatalf("missing rare chip: %v", reasons2)
	}

	// brand new: no prior activity at all
	_, reasons3 := scoreEvent(base, Stats{PerDay: 1}, true, at)
	if !slices.Contains(reasons3, "new resource") {
		t.Fatalf("missing new-resource chip: %v", reasons3)
	}

	// normal cadence: neutral, no rarity chip
	s4, reasons4 := scoreEvent(base, Stats{PerDay: 3, PriorTotal: 40, LastPriorDay: at.AddDate(0, 0, -2)}, true, at)
	if math.Abs(s4-100) > 0.01 {
		t.Fatalf("normal = %v, want 100", s4)
	}
	if len(reasons4) != 0 {
		t.Fatalf("want no chips for neutral sa/scale/normal, got %v", reasons4)
	}
}
