package dashboardapi

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gauravrautela/kubewatch/internal/rank"
	"github.com/gauravrautela/kubewatch/internal/storage"
)

const (
	defaultLookback = time.Hour
	maxLookback     = 24 * time.Hour
	skewAllowance   = 10 * time.Minute // events slightly after "at" still shown, heavily discounted
	defaultSuspects = 50
	maxSuspects     = 100
)

type incidentMeta struct {
	Cluster  string    `json:"cluster"`
	At       time.Time `json:"at"`
	Lookback string    `json:"lookback"`
}

type incidentResponse struct {
	Incident incidentMeta   `json:"incident"`
	Suspects []rank.Suspect `json:"suspects"`
}

func (h *Handler) incident(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cluster := q.Get("cluster")
	if cluster == "" {
		writeErr(w, http.StatusBadRequest, "cluster is required")
		return
	}
	at := time.Now().UTC()
	if v := q.Get("at"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid at: must be RFC3339")
			return
		}
		at = t.UTC()
	}
	lookback := defaultLookback
	if v := q.Get("lookback"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			writeErr(w, http.StatusBadRequest, "invalid lookback")
			return
		}
		if d > maxLookback {
			writeErr(w, http.StatusBadRequest, "lookback exceeds 24h")
			return
		}
		lookback = d
	}
	limit := defaultSuspects
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeErr(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = min(n, maxSuspects)
	}

	from := at.Add(-lookback)
	events, err := h.store.IncidentEvents(r.Context(), cluster, from, at.Add(skewAllowance))
	if err != nil {
		log.Printf("incident: events query: %v", err)
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	stats, err := h.store.ResourceStats(r.Context(), cluster, from)
	if err != nil {
		log.Printf("incident: stats query: %v", err)
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}

	suspects := rank.Rank(toRankEvents(events), toRankStats(stats), at, limit)
	writeJSON(w, http.StatusOK, incidentResponse{
		Incident: incidentMeta{Cluster: cluster, At: at, Lookback: lookback.String()},
		Suspects: suspects,
	})
}

func toRankEvents(rows []storage.IncidentRow) []rank.Event {
	out := make([]rank.Event, len(rows))
	for i, r := range rows {
		out[i] = rank.Event{
			EventID: r.EventID, EventTime: r.EventTime, Operation: r.Operation,
			Kind: r.Kind, Namespace: r.Namespace, Name: r.Name,
			UserName: r.UserName, ActorType: r.ActorType, Classes: r.Classes,
		}
	}
	return out
}

func toRankStats(stats map[storage.ResourceKey]storage.ResourceStats) map[rank.Key]rank.Stats {
	out := make(map[rank.Key]rank.Stats, len(stats))
	for k, v := range stats {
		out[rank.Key{Namespace: k.Namespace, Kind: k.Kind, Name: k.Name}] = rank.Stats{
			PerDay: v.PerDay, LastPriorDay: v.LastPriorDay, PriorTotal: v.PriorTotal,
		}
	}
	return out
}
