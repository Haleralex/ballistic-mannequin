package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Haleralex/ballistic-mannequin/server/internal/assign"
	"github.com/Haleralex/ballistic-mannequin/server/internal/httpapi"
)

// --- /healthz

func TestHealthz(t *testing.T) {
	e := newEnv(t)
	r := e.get("/healthz")
	if r.status != 200 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	m := r.json(t)
	if m["status"] != "ok" || m["build"] != "test123" {
		t.Fatalf("%v", m)
	}
	if r.header.Get("Cache-Control") != "no-store" {
		t.Errorf("cache-control %q", r.header.Get("Cache-Control"))
	}
	e.pool.Close()
	r = e.get("/healthz")
	if r.status != 503 || r.json(t)["status"] != "db_unavailable" {
		t.Fatalf("with the database gone: %d %s", r.status, r.body)
	}
}

// --- /api/v1/assign

func assignReq(vid string, keys ...string) map[string]any {
	return map[string]any{"visitor_id": vid, "experiments": keys}
}

func assignments(t *testing.T, r resp) map[string]string {
	t.Helper()
	if r.status != 200 {
		t.Fatalf("assign: %d %s", r.status, r.body)
	}
	var out struct {
		Assignments map[string]string `json:"assignments"`
	}
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatal(err)
	}
	return out.Assignments
}

func TestAssignStickyAndDeterministic(t *testing.T) {
	e := newEnv(t)
	body := assignReq(vid1, "t1_positioning", "t2_onboarding", "t3_share_prompt", "no_such_test")
	body["referrer"] = "https://t.me/kino"
	body["utm"] = map[string]string{"utm_source": "telegram", "utm_campaign": "launch"}
	got := assignments(t, e.postJSON("/api/v1/assign", body))
	if len(got) != 2 {
		t.Fatalf("inactive and unknown experiments must be left out: %v", got)
	}
	if want := assign.Pick(vid1, "t1_positioning", []string{"a", "b", "c"}, []int{1, 1, 1}); got["t1_positioning"] != want {
		t.Fatalf("t1 = %s, want the hash pick %s", got["t1_positioning"], want)
	}
	if want := assign.Pick(vid1, "t2_onboarding", []string{"a", "b"}, []int{1, 1}); got["t2_onboarding"] != want {
		t.Fatalf("t2 = %s, want %s", got["t2_onboarding"], want)
	}

	// The visitor was upserted with referrer and utm.
	var ref, src string
	if err := e.pool.QueryRow(context.Background(), "SELECT referrer, utm->>'utm_source' FROM visitors WHERE id = $1", vid1).Scan(&ref, &src); err != nil {
		t.Fatal(err)
	}
	if ref != "https://t.me/kino" || src != "telegram" {
		t.Fatalf("visitor referrer %q utm %q", ref, src)
	}

	// Sticky: weights change so that only the other variant could be drawn, the stored one comes back.
	stored := got["t2_onboarding"]
	other := map[string]string{"a": "b", "b": "a"}[stored]
	e.exec(`UPDATE experiments SET weights = CASE WHEN $1 = 'a' THEN ARRAY[1,0] ELSE ARRAY[0,1] END WHERE key = 't2_onboarding'`, other)
	again := assignments(t, e.postJSON("/api/v1/assign", assignReq(vid1, "t2_onboarding")))
	if again["t2_onboarding"] != stored {
		t.Fatalf("assignment moved from %s to %s after a weight change", stored, again["t2_onboarding"])
	}
	// A new visitor gets the only variant with weight.
	fresh := assignments(t, e.postJSON("/api/v1/assign", assignReq(vid2, "t2_onboarding")))
	if fresh["t2_onboarding"] != other {
		t.Fatalf("new visitor got %s, want %s", fresh["t2_onboarding"], other)
	}
	// Later contact does not overwrite the first referrer.
	body2 := assignReq(vid1, "t1_positioning")
	body2["referrer"] = "https://vk.com"
	e.postJSON("/api/v1/assign", body2)
	if n := e.count("SELECT count(*) FROM visitors WHERE id = $1 AND referrer = 'https://t.me/kino'", vid1); n != 1 {
		t.Fatal("first referrer was overwritten")
	}
	if n := e.count("SELECT count(*) FROM assignments WHERE visitor_id = $1", vid1); n != 2 {
		t.Fatalf("%d assignment rows", n)
	}
}

func TestAssignConcurrentFirstContact(t *testing.T) {
	e := newEnv(t)
	var wg sync.WaitGroup
	results := make([]map[string]string, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = assignments(t, e.postJSON("/api/v1/assign", assignReq(vid2, "t1_positioning")))
		}(i)
	}
	wg.Wait()
	for _, r := range results {
		if r["t1_positioning"] != results[0]["t1_positioning"] || r["t1_positioning"] == "" {
			t.Fatalf("concurrent assigns disagree: %v", results)
		}
	}
}

func TestAssignWeightsAcrossVisitors(t *testing.T) {
	e := newEnv(t, withLimits(func(l *httpapi.Limits) { l.Other = 1000 }))
	counts := map[string]int{}
	for i := 0; i < 300; i++ {
		vid := fmt.Sprintf("%08x-0000-4000-8000-000000000000", i)
		counts[assignments(t, e.postJSON("/api/v1/assign", assignReq(vid, "t1_positioning")))["t1_positioning"]]++
	}
	for _, v := range []string{"a", "b", "c"} {
		if counts[v] < 60 || counts[v] > 140 { // 100 ± 5.8 sd
			t.Errorf("variant %s: %d of 300", v, counts[v])
		}
	}
}

func TestAssignValidation(t *testing.T) {
	e := newEnv(t)
	e.postJSON("/api/v1/assign", assignReq("not-a-uuid", "t1_positioning")).errCode(t, 422, "invalid_visitor_id")
	e.postJSON("/api/v1/assign", `{"visitor_id": "`+vid1+`", "experiments": "t1"}`).errCode(t, 422, "invalid_experiments")
	e.postJSON("/api/v1/assign", `{"visitor_id": `).errCode(t, 400, "bad_json")
	e.postJSON("/api/v1/assign", `[1,2]`).errCode(t, 400, "bad_json")
	e.postAs("/api/v1/assign", "application/x-www-form-urlencoded", "visitor_id=x").errCode(t, 400, "unsupported_media_type")
	keys := make([]string, 21)
	for i := range keys {
		keys[i] = fmt.Sprint("k", i)
	}
	e.postJSON("/api/v1/assign", assignReq(vid1, keys...)).errCode(t, 422, "too_many_experiments")
	// A malformed utm is dropped, not fatal.
	r := e.postJSON("/api/v1/assign", map[string]any{"visitor_id": vid1, "experiments": []string{"t1_positioning"}, "utm": "x"})
	if len(assignments(t, r)) != 1 {
		t.Fatal("assign with bad utm failed")
	}
}

// --- /api/v1/events

func batch(names ...string) map[string]any {
	evs := []map[string]any{}
	for _, n := range names {
		evs = append(evs, map[string]any{"name": n, "at": time.Now().UTC().Format(time.RFC3339), "props": map[string]any{"weapon": "pistol"}})
	}
	return map[string]any{"visitor_id": vid1, "session_id": sid1, "events": evs}
}

func TestEventsAcceptAndReject(t *testing.T) {
	e := newEnv(t)
	r := e.postJSON("/api/v1/events", batch("visit", "shot", "shot", "teleport", "return_d1", "reaction_watched"))
	if r.status != 202 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	m := r.json(t)
	if m["accepted"] != 4.0 || m["rejected"] != 2.0 {
		t.Fatalf("%v", m)
	}
	if n := e.count("SELECT count(*) FROM events WHERE visitor_id = $1 AND session_id = $2", vid1, sid1); n != 4 {
		t.Fatalf("%d events stored", n)
	}
	if n := e.count("SELECT count(*) FROM events WHERE name = 'shot' AND props->>'weapon' = 'pistol'"); n != 2 {
		t.Fatalf("props not stored: %d", n)
	}
}

func TestEventsSendBeaconTextPlain(t *testing.T) {
	e := newEnv(t)
	body, _ := json.Marshal(batch("visit", "sim_ready"))
	r := e.postAs("/api/v1/events", "text/plain;charset=UTF-8", body)
	if r.status != 202 || r.json(t)["accepted"] != 2.0 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	r = e.postAs("/api/v1/events", "", body) // a Blob without a type
	if r.status != 202 {
		t.Fatalf("no content type: %d %s", r.status, r.body)
	}
	// text/plain is only for /events.
	e.postAs("/api/v1/assign", "text/plain", assignReq(vid1)).errCode(t, 400, "unsupported_media_type")
}

func TestEventsClockSkewAndProps(t *testing.T) {
	e := newEnv(t)
	recent := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	body := map[string]any{"visitor_id": vid1, "session_id": sid1, "events": []any{
		map[string]any{"name": "visit", "at": "2020-01-01T00:00:00Z", "props": map[string]any{"n": 1}}, // years off
		map[string]any{"name": "visit", "at": time.Now().Add(30 * time.Hour).Format(time.RFC3339), "props": map[string]any{"n": 2}},
		map[string]any{"name": "visit", "at": recent.Format(time.RFC3339), "props": map[string]any{"n": 3}},
		map[string]any{"name": "visit", "at": "yesterday", "props": map[string]any{"n": 4}},
		map[string]any{"name": "visit", "props": nil},      // no time, no props
		map[string]any{"name": "visit", "props": []int{1}}, // props not an object: rejected
		map[string]any{"name": 7}, // wrong type: rejected
		"not an object",           // rejected
		map[string]any{"name": "shot", "at": 12345, "props": map[string]any{}}, // at of the wrong type: rejected
	}}
	r := e.postJSON("/api/v1/events", body)
	m := r.json(t)
	if r.status != 202 || m["accepted"] != 5.0 || m["rejected"] != 4.0 {
		t.Fatalf("%d %v", r.status, m)
	}
	check := func(n int, near time.Time) {
		var at time.Time
		if err := e.pool.QueryRow(context.Background(), "SELECT at FROM events WHERE props->>'n' = $1", fmt.Sprint(n)).Scan(&at); err != nil {
			t.Fatal(err)
		}
		if d := at.Sub(near); d > time.Minute || d < -time.Minute {
			t.Errorf("event %d: at %v, want about %v", n, at, near)
		}
	}
	now := time.Now()
	check(1, now)
	check(2, now)
	check(3, recent)
	check(4, now)
	if n := e.count("SELECT count(*) FROM events WHERE props = '{}'::jsonb"); n != 1 {
		t.Errorf("missing props should be stored as {}: %d", n)
	}
}

func TestEventsValidation(t *testing.T) {
	e := newEnv(t)
	b := batch("visit")
	b["visitor_id"] = "nope"
	e.postJSON("/api/v1/events", b).errCode(t, 422, "invalid_visitor_id")
	b = batch("visit")
	b["session_id"] = ""
	e.postJSON("/api/v1/events", b).errCode(t, 422, "invalid_session_id")

	names := make([]string, 101)
	for i := range names {
		names[i] = "shot"
	}
	e.postJSON("/api/v1/events", batch(names...)).errCode(t, 422, "too_many_events")
	if r := e.postJSON("/api/v1/events", batch(names[:100]...)); r.status != 202 || r.json(t)["accepted"] != 100.0 {
		t.Fatalf("100 events: %d %s", r.status, r.body)
	}

	huge := batch("visit")
	huge["events"].([]map[string]any)[0]["props"] = map[string]any{"pad": strings.Repeat("x", 300<<10)}
	e.postJSON("/api/v1/events", huge).errCode(t, 413, "too_large")
	e.postAs("/api/v1/events", "text/plain", "garbage").errCode(t, 400, "bad_json")
}

// --- /api/v1/cases

func TestCaseRoundTrip(t *testing.T) {
	e := newEnv(t)
	r := e.postJSON("/api/v1/cases", `{"visitor_id": "`+vid1+`", "case": `+exampleCase+`}`)
	if r.status != 201 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	m := r.json(t)
	id, _ := m["id"].(string)
	if len(id) != 8 || m["url"] != "/r/"+id {
		t.Fatalf("%v", m)
	}

	var kind, weapon, build string
	var seed int64
	if err := e.pool.QueryRow(context.Background(), "SELECT kind, weapon, build, seed FROM cases WHERE id = $1", id).
		Scan(&kind, &weapon, &build, &seed); err != nil {
		t.Fatal(err)
	}
	if kind != "abdomen" || weapon != "pistol" || build != "f2f18ed" || seed != 3141592653 {
		t.Fatalf("columns %s %s %s %d", kind, weapon, build, seed)
	}

	g := e.get("/api/v1/cases/" + id)
	if g.status != 200 || !strings.HasPrefix(g.header.Get("Content-Type"), "application/json") {
		t.Fatalf("%d %s", g.status, g.body)
	}
	var sent, back any
	_ = json.Unmarshal([]byte(exampleCase), &sent)
	if err := json.Unmarshal(g.body, &back); err != nil {
		t.Fatal(err)
	}
	sj, _ := json.Marshal(sent)
	bj, _ := json.Marshal(back)
	if string(sj) != string(bj) {
		t.Fatalf("case changed on the way:\n%s\n%s", sj, bj)
	}

	// Views are counted in the background.
	e.get("/api/v1/cases/" + id)
	e.api.Wait()
	if n := e.count("SELECT views FROM cases WHERE id = $1", id); n != 2 {
		t.Fatalf("views %d", n)
	}
}

func TestCaseWithoutVisitorAndMeta(t *testing.T) {
	e := newEnv(t)
	r := e.postJSON("/api/v1/cases", `{"case": {"v":1,"seed":7,"build":"dev","steps":600,"events":[{"step":0,"type":"setting","key":"weapon","value":"slug"}]}}`)
	if r.status != 201 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	id := r.json(t)["id"].(string)
	if n := e.count("SELECT count(*) FROM cases WHERE id = $1 AND visitor_id IS NULL AND kind IS NULL AND weapon = 'slug'", id); n != 1 {
		t.Fatal("columns not as expected")
	}
}

func TestCaseValidation(t *testing.T) {
	e := newEnv(t)
	bad := strings.Replace(exampleCase, `"value": "flee"`, `"value": "dance"`, 1)
	r := e.postJSON("/api/v1/cases", `{"case": `+bad+`}`)
	r.errCode(t, 422, "invalid_case")
	contains(t, r.body, "events[0].value")

	e.postJSON("/api/v1/cases", `{"visitor_id": "`+vid1+`"}`).errCode(t, 422, "invalid_case")
	e.postJSON("/api/v1/cases", `{"case": `+exampleCase+`, "visitor_id": "x"}`).errCode(t, 422, "invalid_visitor_id")
	unsorted := strings.Replace(exampleCase, `"step": 214`, `"step": 300`, 1)
	e.postJSON("/api/v1/cases", `{"case": `+unsorted+`}`).errCode(t, 422, "invalid_case")

	// A case over 64 KB (but a body within the 65 KB allowance) is a 422; a bigger body is a 413.
	padded := strings.Replace(exampleCase, `"title": "Печень, пистолет"`, `"title": "Печень, пистолет"`+strings.Repeat(" ", 64<<10-len(exampleCase)+10), 1)
	if len(padded) <= 64<<10 || len(padded)+20 > 65<<10 {
		t.Fatalf("padding: %d", len(padded))
	}
	e.postJSON("/api/v1/cases", `{"case": `+padded+`}`).errCode(t, 422, "invalid_case")
	padded = strings.Replace(exampleCase, `"title": "Печень, пистолет"`, `"title": "Печень, пистолет"`+strings.Repeat(" ", 70<<10), 1)
	e.postJSON("/api/v1/cases", `{"case": `+padded+`}`).errCode(t, 413, "too_large")

	if n := e.count("SELECT count(*) FROM cases"); n != 0 {
		t.Fatalf("%d cases stored from invalid input", n)
	}
}

func TestGetCaseNotFound(t *testing.T) {
	e := newEnv(t)
	e.get("/api/v1/cases/a7F3kQ9x").errCode(t, 404, "not_found")
	e.get("/api/v1/cases/bad").errCode(t, 404, "not_found")
}

// --- /api/v1/ratings

func TestRatingsOnePerDay(t *testing.T) {
	e := newEnv(t)
	rate := map[string]any{"visitor_id": vid1, "scenario_id": "abd-04", "case_id": nil, "value": 1, "comment": "на 12 с встал — так не бывает"}
	if r := e.postJSON("/api/v1/ratings", rate); r.status != 201 || string(r.body) != "{}\n" {
		t.Fatalf("%d %q", r.status, r.body)
	}
	rate["value"] = -1
	rate["comment"] = nil
	if r := e.postJSON("/api/v1/ratings", rate); r.status != 201 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	if n := e.count("SELECT count(*) FROM ratings WHERE visitor_id = $1 AND scenario_id = 'abd-04'", vid1); n != 1 {
		t.Fatalf("%d rows; a second vote the same day must update", n)
	}
	if n := e.count("SELECT count(*) FROM ratings WHERE value = -1 AND comment IS NULL AND day = (now() AT TIME ZONE 'UTC')::date"); n != 1 {
		t.Fatal("rating not updated")
	}
	// Another scenario is another row; yesterday's vote stays.
	e.exec("UPDATE ratings SET day = day - 1")
	e.postJSON("/api/v1/ratings", rate)
	rate["scenario_id"] = "leg-01"
	e.postJSON("/api/v1/ratings", rate)
	if n := e.count("SELECT count(*) FROM ratings"); n != 3 {
		t.Fatalf("%d rows", n)
	}

	// With a case.
	c := e.postJSON("/api/v1/cases", `{"case": `+exampleCase+`}`).json(t)
	rate["case_id"] = c["id"]
	if r := e.postJSON("/api/v1/ratings", rate); r.status != 201 {
		t.Fatalf("%d %s", r.status, r.body)
	}
}

func TestRatingsValidation(t *testing.T) {
	e := newEnv(t)
	ok := func() map[string]any {
		return map[string]any{"visitor_id": vid1, "scenario_id": "abd-04", "value": 1}
	}
	r := ok()
	r["value"] = 0
	e.postJSON("/api/v1/ratings", r).errCode(t, 422, "invalid_value")
	r = ok()
	delete(r, "value")
	e.postJSON("/api/v1/ratings", r).errCode(t, 422, "invalid_value")
	r = ok()
	r["value"] = "1"
	e.postJSON("/api/v1/ratings", r).errCode(t, 422, "invalid_value")
	r = ok()
	r["scenario_id"] = ""
	e.postJSON("/api/v1/ratings", r).errCode(t, 422, "invalid_scenario_id")
	r = ok()
	r["comment"] = strings.Repeat("ж", 501)
	e.postJSON("/api/v1/ratings", r).errCode(t, 422, "comment_too_long")
	r["comment"] = strings.Repeat("ж", 500)
	if res := e.postJSON("/api/v1/ratings", r); res.status != 201 {
		t.Fatalf("500 characters must fit: %d %s", res.status, res.body)
	}
	r = ok()
	r["case_id"] = "zzzzzzzz"
	e.postJSON("/api/v1/ratings", r).errCode(t, 422, "unknown_case")
	r["case_id"] = "../x"
	e.postJSON("/api/v1/ratings", r).errCode(t, 422, "invalid_case_id")
	r = ok()
	r["visitor_id"] = 5
	e.postJSON("/api/v1/ratings", r).errCode(t, 422, "invalid_visitor_id")
}

// --- /api/v1/waitlist

func TestWaitlistIdempotent(t *testing.T) {
	e := newEnv(t)
	w := map[string]any{"visitor_id": vid1, "email": "  Actor@Mail.RU ", "source": "room", "price_variant": "b", "consent": true}
	for i := 0; i < 3; i++ {
		if r := e.postJSON("/api/v1/waitlist", w); r.status != 201 || string(r.body) != "{}\n" {
			t.Fatalf("attempt %d: %d %s", i, r.status, r.body)
		}
		w["source"] = "pro" // a later submission does not change the stored row
	}
	if n := e.count("SELECT count(*) FROM waitlist"); n != 1 {
		t.Fatalf("%d rows", n)
	}
	var email, source string
	if err := e.pool.QueryRow(context.Background(), "SELECT email::text, source FROM waitlist").Scan(&email, &source); err != nil {
		t.Fatal(err)
	}
	if email != "actor@mail.ru" || source != "room" {
		t.Fatalf("%q %q", email, source)
	}
	// Cyrillic domains are fine.
	w["email"] = "Режиссёр@Почта.РФ"
	if r := e.postJSON("/api/v1/waitlist", w); r.status != 201 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	if n := e.count("SELECT count(*) FROM waitlist WHERE email = 'режиссёр@почта.рф'"); n != 1 {
		t.Fatal("cyrillic address not lower-cased")
	}
}

func TestWaitlistValidation(t *testing.T) {
	e := newEnv(t, withLimits(func(l *httpapi.Limits) { l.Waitlist = 100 }))
	base := func() map[string]any {
		return map[string]any{"email": "a@b.ru", "source": "pro", "consent": true}
	}
	w := base()
	w["consent"] = false
	e.postJSON("/api/v1/waitlist", w).errCode(t, 422, "consent_required")
	w = base()
	delete(w, "consent")
	e.postJSON("/api/v1/waitlist", w).errCode(t, 422, "consent_required")
	for _, bad := range []string{"", "a", "a@b", "a@b.", "@b.ru", "a b@c.ru", "a@b..ru", "a@-b.ru", "a@b.r", "a@b.12", "<a>@b.ru", "a@b@c.ru"} {
		w = base()
		w["email"] = bad
		e.postJSON("/api/v1/waitlist", w).errCode(t, 422, "invalid_email")
	}
	w = base()
	w["source"] = "landing"
	e.postJSON("/api/v1/waitlist", w).errCode(t, 422, "invalid_source")
	w = base()
	w["price_variant"] = "<b>"
	e.postJSON("/api/v1/waitlist", w).errCode(t, 422, "invalid_price_variant")
	if n := e.count("SELECT count(*) FROM waitlist"); n != 0 {
		t.Fatalf("%d rows from invalid input", n)
	}
}

// --- rate limits

func TestRateLimitPerIP(t *testing.T) {
	e := newEnv(t)
	w := map[string]any{"email": "a@b.ru", "source": "pro", "consent": true}
	for i := 0; i < 10; i++ {
		if r := e.postJSON("/api/v1/waitlist", w); r.status != 201 {
			t.Fatalf("request %d: %d", i, r.status)
		}
	}
	r := e.postJSON("/api/v1/waitlist", w)
	r.errCode(t, 429, "rate_limited")
	if r.header.Get("Retry-After") == "" {
		t.Error("no Retry-After")
	}
	// Spoofed X-Forwarded-For does not help without TRUST_PROXY.
	e.postJSON("/api/v1/waitlist", w, "X-Forwarded-For", "203.0.113.9").errCode(t, 429, "rate_limited")
	// Buckets are per endpoint class: ratings still work.
	if r := e.postJSON("/api/v1/ratings", map[string]any{"visitor_id": vid1, "scenario_id": "x", "value": 1}); r.status != 201 {
		t.Fatalf("ratings limited by the waitlist bucket: %d", r.status)
	}
}

func TestRateLimitEventsAndCases(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 60; i++ {
		if r := e.postJSON("/api/v1/events", batch("visit")); r.status != 202 {
			t.Fatalf("event batch %d: %d", i, r.status)
		}
	}
	e.postJSON("/api/v1/events", batch("visit")).errCode(t, 429, "rate_limited")
	for i := 0; i < 20; i++ {
		if r := e.postJSON("/api/v1/cases", `{"case": `+exampleCase+`}`); r.status != 201 {
			t.Fatalf("case %d: %d", i, r.status)
		}
	}
	e.postJSON("/api/v1/cases", `{"case": `+exampleCase+`}`).errCode(t, 429, "rate_limited")
}

func TestRateLimitTrustProxy(t *testing.T) {
	e := newEnv(t, withTrustProxy)
	w := map[string]any{"email": "a@b.ru", "source": "pro", "consent": true}
	for i := 0; i < 10; i++ {
		e.postJSON("/api/v1/waitlist", w, "X-Forwarded-For", "198.51.100.1")
	}
	e.postJSON("/api/v1/waitlist", w, "X-Forwarded-For", "198.51.100.1").errCode(t, 429, "rate_limited")
	// The proxy appends the real client; a client-supplied left part is ignored.
	if r := e.postJSON("/api/v1/waitlist", w, "X-Forwarded-For", "198.51.100.1, 198.51.100.2"); r.status != 201 {
		t.Fatalf("another client behind the proxy: %d", r.status)
	}
}
