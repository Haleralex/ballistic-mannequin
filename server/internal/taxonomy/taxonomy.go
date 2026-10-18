// Package taxonomy is the list of analytics events of docs/contracts/events.md and the metrics built on them.
package taxonomy

import "sort"

// Events are the event names the server stores; anything else is rejected.
var Events = map[string]bool{
	"visit":            true,
	"landing_cta":      true,
	"sim_ready":        true,
	"shot":             true,
	"reaction_watched": true,
	"share_link":       true,
	"share_open":       true,
	"share_convert":    true,
	"clip_export":      true,
	"rate":             true,
	"fake_door_open":   true,
	"fake_door_click":  true,
	"waitlist_submit":  true,
	"blood_toggle":     true,
}

// Metric is a per-visitor conversion: either a derived column of the visitor_metrics view or "the visitor sent
// at least one event with this name".
type Metric struct {
	Name   string // canonical name
	Column string // visitor_metrics column, for derived metrics
	Event  string // event name, for event metrics
}

// derived maps metric names (with aliases) to visitor_metrics columns.
var derived = map[string]string{
	"activated":  "activated",
	"activation": "activated", // the spelling used in api.md
	"shared":     "shared",
	"share":      "shared",
}

// ResolveMetric turns a metric name into a Metric. Accepted: activated (activation), shared (share),
// waitlist (= waitlist_submit), and every event name.
func ResolveMetric(name string) (Metric, bool) {
	if col, ok := derived[name]; ok {
		return Metric{Name: col, Column: col}, true
	}
	if name == "waitlist" {
		name = "waitlist_submit"
	}
	if Events[name] {
		return Metric{Name: name, Event: name}, true
	}
	return Metric{}, false
}

// MetricNames lists the accepted metric names, for error messages.
func MetricNames() []string {
	out := []string{"activated", "shared", "waitlist"}
	var ev []string
	for k := range Events {
		ev = append(ev, k)
	}
	sort.Strings(ev)
	return append(out, ev...)
}
