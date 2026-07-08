package rank

import (
	"time"

	"github.com/gauravrautela/kubewatch/internal/classify"
)

// Every tunable scoring weight lives in this file.
const (
	halfLifeBefore   = 30 * time.Minute
	afterDecayFactor = 4.0 // events after the incident time decay this much faster

	weightHuman          = 1.5
	weightServiceAccount = 1.0
	weightUnknownActor   = 1.0
	weightSystem         = 0.6

	weightRare  = 1.5
	weightChurn = 0.2

	rareGapDays    = 14
	churnPerDayMax = 50.0

	maxEventsPerSuspect = 20
)

var classWeights = map[string]float64{
	classify.ClassImage:        1.5,
	classify.ClassRBAC:         1.4,
	classify.ClassNetwork:      1.4,
	classify.ClassConfigData:   1.4,
	classify.ClassEnv:          1.2,
	classify.ClassResources:    1.2,
	classify.ClassScale:        1.0,
	classify.ClassOther:        0.8,
	classify.ClassMetadataOnly: 0.4,
	classify.ClassStatusOnly:   0.1,
}

// classChips are the human-readable reason labels per class. Only classes
// that indicate signal get a chip; neutral/noise classes stay silent except
// as implied by the low score.
var classChips = map[string]string{
	classify.ClassImage:        "image change",
	classify.ClassRBAC:         "RBAC change",
	classify.ClassNetwork:      "network change",
	classify.ClassConfigData:   "config change",
	classify.ClassEnv:          "env change",
	classify.ClassResources:    "resource limits change",
	classify.ClassScale:        "", // neutral weight: no chip
	classify.ClassOther:        "",
	classify.ClassMetadataOnly: "metadata only",
	classify.ClassStatusOnly:   "status only",
}
