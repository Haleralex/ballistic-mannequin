package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Haleralex/ballistic-mannequin/server/internal/store"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		trust int
		xff   []string
		want  string
	}{
		{0, nil, "10.0.0.1"},
		{0, []string{"203.0.113.5"}, "10.0.0.1"},                       // not trusted
		{1, nil, "10.0.0.1"},                                           // no header
		{1, []string{"203.0.113.5"}, "203.0.113.5"},                    // Caddy sets it
		{1, []string{"1.1.1.1, 203.0.113.5"}, "203.0.113.5"},           // client-supplied left part ignored
		{1, []string{"1.1.1.1", "203.0.113.5"}, "203.0.113.5"},         // several headers
		{2, []string{"1.1.1.1, 203.0.113.5, 10.0.0.2"}, "203.0.113.5"}, // CDN + Caddy
		{3, []string{"203.0.113.5"}, "203.0.113.5"},                    // fewer hops than trusted: leftmost
		{1, []string{"garbage"}, "10.0.0.1"},
		{1, []string{"[2001:db8::1]:443"}, "2001:db8::1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "10.0.0.1:5555"
		for _, h := range c.xff {
			r.Header.Add("X-Forwarded-For", h)
		}
		if got := clientIP(r, c.trust); got != c.want {
			t.Errorf("trust %d xff %q: %s, want %s", c.trust, c.xff, got, c.want)
		}
	}
}

func TestBuildReport(t *testing.T) {
	rep := buildReport("k", "activated", []string{"a", "b", "c"}, map[string]store.VariantCount{
		"a":   {Variant: "a", Visitors: 100, Converted: 20},
		"b":   {Variant: "b", Visitors: 100, Converted: 30},
		"old": {Variant: "old", Visitors: 5, Converted: 1}, // removed from the experiment since
	})
	if len(rep.Variants) != 4 || rep.Variants[2].Variant != "c" || rep.Variants[3].Variant != "old" {
		t.Fatalf("%+v", rep.Variants)
	}
	b := rep.VsA["b"]
	if b.P == nil || *b.P < 0.1024 || *b.P > 0.1025 || b.Diff < 0.0999 || b.Diff > 0.1001 {
		t.Fatalf("b vs a %+v", b)
	}
	if rep.VsA["c"].P != nil {
		t.Fatal("empty variant must have no p-value")
	}
	// It must encode: no NaN or Inf anywhere.
	if _, err := json.Marshal(rep); err != nil {
		t.Fatal(err)
	}
	if rep := buildReport("k", "m", nil, nil); len(rep.Variants) != 0 {
		t.Fatal("no variants")
	}
}

func TestValidEmail(t *testing.T) {
	for _, ok := range []string{"a@b.ru", "first.last+tag@mail.example.com", "режиссёр@почта.рф", "x@xn--80a1acny.xn--p1ai", "a_b-c@d-e.io"} {
		if !validEmail(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "a@b", ".a@b.ru", "a.@b.ru", "a..b@c.ru", "a@b_c.ru", "a@b.c", "a\x00@b.ru", "a@b.ru\n"} {
		if validEmail(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestParseEventSkew(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	for at, want := range map[string]time.Time{
		"2026-10-18T11:00:00Z":        time.Date(2026, 10, 18, 11, 0, 0, 0, time.UTC),
		"2026-10-18T15:00:00+03:00":   time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
		"2026-10-17T12:00:00Z":        time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC), // exactly 24 h: kept
		"2026-10-17T11:59:59Z":        now,                                            // just over: replaced
		"2026-10-19T12:00:01Z":        now,
		"2026-10-18T11:59:59.123456Z": time.Date(2026, 10, 18, 11, 59, 59, 123456000, time.UTC),
		"not a time":                  now,
	} {
		e, ok := parseEvent(json.RawMessage(`{"name":"visit","at":"`+at+`"}`), now)
		if !ok || !e.At.Equal(want) {
			t.Errorf("%s: %v %v, want %v", at, e.At, ok, want)
		}
	}
}

func TestPreviewText(t *testing.T) {
	var c caseForPreview
	c.Steps = 7200
	c.Meta.Weapon = "buck"
	c.Meta.Zones = []string{"бедро"}
	if got := previewTitle(c); got != "Реакция на ранение — реакция за 60 с" {
		t.Errorf("title %q", got)
	}
	if got := previewDescription(c); got[:len("Дробовик, картечь · бедро.")] != "Дробовик, картечь · бедро." {
		t.Errorf("description %q", got)
	}
	if got := ogKind("../x", nil); got != "default" {
		t.Errorf("kind %q", got)
	}
}

func TestCSVSafe(t *testing.T) {
	for in, want := range map[string]string{"=1+1": "'=1+1", "+7": "'+7", "-x": "'-x", "@a": "'@a", "a@b.ru": "a@b.ru", "": ""} {
		if got := csvSafe(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}
