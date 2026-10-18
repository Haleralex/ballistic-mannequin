package caseval

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// example is the case from docs/contracts/case-v1.md, with the placeholder hash filled in.
const example = `{
  "v": 1,
  "seed": 3141592653,
  "build": "f2f18ed",
  "steps": 7800,
  "events": [
    { "step": 0,   "type": "setting", "key": "react",   "value": "flee" },
    { "step": 0,   "type": "setting", "key": "weapon",  "value": "pistol" },
    { "step": 214, "type": "shot", "origin": [0.9, 1.45, 4.2], "dir": [-0.19, -0.09, -0.98] },
    { "step": 216, "type": "script", "source": "ai", "title": "…", "beats": [ { "t": 0.2, "dur": 2, "action": "look", "at": "wound", "intensity": 0.8, "note": "…" } ] }
  ],
  "check": [ { "step": 1200, "hash": "9f3a0c1e" }, { "step": 2400, "hash": "00ff00ff" } ],
  "meta": { "title": "Печень, пистолет", "weapon": "pistol", "kind": "abdomen", "zones": ["живот"], "organs": ["печень"] }
}`

func base(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(example), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func events(m map[string]any) []any { return m["events"].([]any) }
func ev(m map[string]any, i int) map[string]any {
	return events(m)[i].(map[string]any)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestExampleIsValid(t *testing.T) {
	s, err := Validate([]byte(example))
	if err != nil {
		t.Fatal(err)
	}
	if s.Seed != 3141592653 || s.Build != "f2f18ed" || s.Steps != 7800 || s.Kind != "abdomen" || s.Weapon != "pistol" {
		t.Fatalf("summary %+v", s)
	}
	if s.Meta.Title != "Печень, пистолет" || len(s.Meta.Zones) != 1 || s.Meta.Organs[0] != "печень" {
		t.Fatalf("meta %+v", s.Meta)
	}
}

func TestMinimalCase(t *testing.T) {
	s, err := Validate([]byte(`{"v":1,"seed":0,"build":"dev","steps":1,"events":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Kind != "" || s.Weapon != "" {
		t.Fatalf("summary %+v", s)
	}
}

func TestWeaponFallsBackToSetting(t *testing.T) {
	m := base(t)
	delete(m["meta"].(map[string]any), "weapon")
	ev(m, 1)["value"] = "buck"
	s, err := Validate(mustJSON(t, m))
	if err != nil {
		t.Fatal(err)
	}
	if s.Weapon != "buck" {
		t.Fatalf("weapon %q", s.Weapon)
	}
}

func TestBleedAcceptsNumberAndString(t *testing.T) {
	for _, v := range []any{1, 10, "1", "10"} {
		m := base(t)
		ev(m, 0)["key"] = "bleed"
		ev(m, 0)["value"] = v
		if _, err := Validate(mustJSON(t, m)); err != nil {
			t.Errorf("bleed %v: %v", v, err)
		}
	}
}

func TestInvalid(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		mutate func(m map[string]any)
	}{
		{"wrong version", "v", func(m map[string]any) { m["v"] = 2 }},
		{"version as string", "v", func(m map[string]any) { m["v"] = "1" }},
		{"missing seed", "seed", func(m map[string]any) { delete(m, "seed") }},
		{"negative seed", "seed", func(m map[string]any) { m["seed"] = -1 }},
		{"seed above uint32", "seed", func(m map[string]any) { m["seed"] = 4294967296 }},
		{"fractional seed", "seed", func(m map[string]any) { m["seed"] = 1.5 }},
		{"empty build", "build", func(m map[string]any) { m["build"] = "" }},
		{"build with html", "build", func(m map[string]any) { m["build"] = "<b>" }},
		{"zero steps", "steps", func(m map[string]any) { m["steps"] = 0 }},
		{"too many steps", "steps", func(m map[string]any) { m["steps"] = 21601 }},
		{"unknown top field", "", func(m map[string]any) { m["extra"] = true }},
		{"events not array", "events", func(m map[string]any) { m["events"] = map[string]any{} }},
		{"events missing", "events", func(m map[string]any) { delete(m, "events") }},
		{"too many events", "events", func(m map[string]any) {
			list := make([]any, 201)
			for i := range list {
				list[i] = map[string]any{"step": 0, "type": "setting", "key": "react", "value": "auto"}
			}
			m["events"] = list
		}},
		{"unsorted steps", "events[2].step", func(m map[string]any) { ev(m, 2)["step"] = 0; ev(m, 1)["step"] = 5 }},
		{"step beyond steps", "events[3].step", func(m map[string]any) { ev(m, 3)["step"] = 7801 }},
		{"negative step", "events[0].step", func(m map[string]any) { ev(m, 0)["step"] = -1 }},
		{"unknown type", "events[0].type", func(m map[string]any) { ev(m, 0)["type"] = "teleport" }},
		{"unknown setting", "events[0].key", func(m map[string]any) { ev(m, 0)["key"] = "gravity" }},
		{"bad setting value", "events[0].value", func(m map[string]any) { ev(m, 0)["value"] = "dance" }},
		{"bleed 5", "events[0].value", func(m map[string]any) { ev(m, 0)["key"] = "bleed"; ev(m, 0)["value"] = 5 }},
		{"weapon as number", "events[1].value", func(m map[string]any) { ev(m, 1)["value"] = 1 }},
		{"extra field on setting", "events[0]", func(m map[string]any) { ev(m, 0)["x"] = 1 }},
		{"dir not unit", "events[2].dir", func(m map[string]any) { ev(m, 2)["dir"] = []any{0, 0, 2} }},
		{"dir zero", "events[2].dir", func(m map[string]any) { ev(m, 2)["dir"] = []any{0, 0, 0} }},
		{"dir two numbers", "events[2].dir", func(m map[string]any) { ev(m, 2)["dir"] = []any{0, 1} }},
		{"dir with string", "events[2].dir", func(m map[string]any) { ev(m, 2)["dir"] = []any{0, "1", 0} }},
		{"origin missing", "events[2].origin", func(m map[string]any) { delete(ev(m, 2), "origin") }},
		{"origin far away", "events[2].origin", func(m map[string]any) { ev(m, 2)["origin"] = []any{1e6, 0, 0} }},
		{"script without beats", "events[3].beats", func(m map[string]any) { delete(ev(m, 3), "beats") }},
		{"beat without action", "events[3].beats[0].action", func(m map[string]any) {
			delete(ev(m, 3)["beats"].([]any)[0].(map[string]any), "action")
		}},
		{"beat t as string", "events[3].beats[0].t", func(m map[string]any) {
			ev(m, 3)["beats"].([]any)[0].(map[string]any)["t"] = "soon"
		}},
		{"check bad hash", "check[1].hash", func(m map[string]any) { m["check"].([]any)[1].(map[string]any)["hash"] = "…" }},
		{"check beyond steps", "check[0].step", func(m map[string]any) { m["check"].([]any)[0].(map[string]any)["step"] = 9000 }},
		{"meta not object", "meta", func(m map[string]any) { m["meta"] = "x" }},
		{"meta kind with path", "meta.kind", func(m map[string]any) { m["meta"].(map[string]any)["kind"] = "../etc" }},
		{"meta title too long", "meta.title", func(m map[string]any) { m["meta"].(map[string]any)["title"] = strings.Repeat("я", 201) }},
		{"meta zones not strings", "meta.zones[0]", func(m map[string]any) { m["meta"].(map[string]any)["zones"] = []any{1} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := base(t)
			c.mutate(m)
			_, err := Validate(mustJSON(t, m))
			var ve *Error
			if !errors.As(err, &ve) {
				t.Fatalf("want validation error, got %v", err)
			}
			if ve.Path != c.path {
				t.Fatalf("path %q, want %q (%v)", ve.Path, c.path, err)
			}
			if ve.Reason == "" {
				t.Fatal("empty reason")
			}
		})
	}
}

func TestInvalidJSON(t *testing.T) {
	for _, raw := range []string{``, `[]`, `"case"`, `{"v":1`, `{"v":1} {}`, `null`} {
		if _, err := Validate([]byte(raw)); err == nil {
			t.Errorf("%q accepted", raw)
		}
	}
}

func TestSizeLimit(t *testing.T) {
	m := base(t)
	m["meta"].(map[string]any)["description"] = "x"
	small := mustJSON(t, m)
	pad := MaxBytes - len(small) + 1
	raw := []byte(strings.Replace(string(small), `"description":"x"`, `"description":"x"`+strings.Repeat(" ", pad), 1))
	if len(raw) <= MaxBytes {
		t.Fatalf("padding failed: %d", len(raw))
	}
	if _, err := Validate(raw); err == nil {
		t.Fatal("oversized case accepted")
	}
	if _, err := Validate(raw[:0:0]); err == nil {
		t.Fatal("empty accepted")
	}
}

func TestDirTolerance(t *testing.T) {
	m := base(t)
	ev(m, 2)["dir"] = []any{0, 0, 1.009}
	if _, err := Validate(mustJSON(t, m)); err != nil {
		t.Fatalf("within tolerance rejected: %v", err)
	}
	ev(m, 2)["dir"] = []any{0, 0, 0.989}
	if _, err := Validate(mustJSON(t, m)); err == nil {
		t.Fatal("outside tolerance accepted")
	}
}
