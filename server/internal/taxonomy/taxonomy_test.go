package taxonomy

import "testing"

func TestEventsMatchContract(t *testing.T) {
	if len(Events) != 14 {
		t.Fatalf("%d events, events.md lists 14", len(Events))
	}
	for _, n := range []string{"return_d1", "return_d7", "reaction_watched_10s", ""} {
		if Events[n] {
			t.Errorf("%q must not be accepted", n)
		}
	}
}

func TestResolveMetric(t *testing.T) {
	cases := map[string]Metric{
		"activated":       {Name: "activated", Column: "activated"},
		"activation":      {Name: "activated", Column: "activated"},
		"shared":          {Name: "shared", Column: "shared"},
		"waitlist":        {Name: "waitlist_submit", Event: "waitlist_submit"},
		"fake_door_click": {Name: "fake_door_click", Event: "fake_door_click"},
	}
	for in, want := range cases {
		got, ok := ResolveMetric(in)
		if !ok || got != want {
			t.Errorf("%s: %+v %v", in, got, ok)
		}
	}
	if _, ok := ResolveMetric("revenue; DROP TABLE events"); ok {
		t.Error("unknown metric accepted")
	}
}
