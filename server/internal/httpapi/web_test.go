package httpapi_test

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/Haleralex/ballistic-mannequin/server/internal/stats"
)

// --- /r/{id}

func createCase(t *testing.T, e *env, caseJSON string) string {
	t.Helper()
	r := e.postJSON("/api/v1/cases", `{"case": `+caseJSON+`}`)
	if r.status != 201 {
		t.Fatalf("create case: %d %s", r.status, r.body)
	}
	return r.json(t)["id"].(string)
}

func TestSharePageOpenGraphAndRedirect(t *testing.T) {
	e := newEnv(t)
	id := createCase(t, e, exampleCase)
	r := e.get("/r/" + id)
	if r.status != 200 || !strings.HasPrefix(r.header.Get("Content-Type"), "text/html") {
		t.Fatalf("%d %s", r.status, r.body)
	}
	contains(t, r.body,
		`<meta property="og:title" content="Печень, пистолет — реакция за 65 с">`,
		`<meta property="og:description" content="Пистолет · живот · печень. Физическая симуляция`,
		`<meta property="og:image" content="`+e.srv.URL+`/assets/og/abdomen.png">`,
		`<meta property="og:url" content="`+e.srv.URL+`/r/`+id+`">`,
		`<meta http-equiv="refresh" content="0; url=/?case=`+id+`">`,
		`href="/?case=`+id+`"`,
		`location.replace("/?case=`+id+`")`,
	)
	// The share page does not count as a view (preview bots fetch it too).
	e.api.Wait()
	if n := e.count("SELECT views FROM cases WHERE id = $1", id); n != 0 {
		t.Errorf("views %d", n)
	}
}

func TestSharePageEscapesAndDefaults(t *testing.T) {
	e := newEnv(t, withTrustProxy)
	evil := `{"v":1,"seed":1,"build":"dev","steps":120,"events":[],"meta":{"title":"<script>alert(1)</script>\" onload=\"x"}}`
	id := createCase(t, e, evil)
	r := e.get("/r/"+id, "X-Forwarded-Proto", "https")
	if strings.Contains(string(r.body), "<script>alert") || strings.Contains(string(r.body), `" onload="x`) {
		t.Fatalf("title not escaped:\n%s", r.body)
	}
	host := strings.TrimPrefix(e.srv.URL, "http://")
	contains(t, r.body, "&lt;script&gt;alert(1)&lt;/script&gt;", `content="https://`+host+`/assets/og/default.png"`,
		"Физическая симуляция реакции тела на ранение")

	plain := createCase(t, e, `{"v":1,"seed":1,"build":"dev","steps":1,"events":[]}`)
	contains(t, e.get("/r/"+plain).body, `content="Реакция на ранение"`)
}

func TestSharePageNotFound(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/r/a7F3kQ9x", "/r/short", "/r/a7F3kQ9x/extra"} {
		r := e.get(p)
		if r.status != 404 || !strings.HasPrefix(r.header.Get("Content-Type"), "text/html") || !hasCyrillic(string(r.body)) {
			t.Errorf("%s: %d %s %s", p, r.status, r.header.Get("Content-Type"), r.body)
		}
	}
}

// --- static files

func TestStaticFiles(t *testing.T) {
	e := newEnv(t)
	r := e.get("/")
	if r.status != 200 || !strings.Contains(string(r.body), "<title>sim</title>") || r.header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("/: %d %q %s", r.status, r.header.Get("Cache-Control"), r.body)
	}
	if r := e.get("/?case=a7F3kQ9x"); r.status != 200 {
		t.Fatalf("/?case=: %d", r.status)
	}
	if r := e.get("/docs/"); r.status != 200 || r.header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("/docs/: %d", r.status)
	}
	cases := map[string]string{
		"/app.js":              "public, max-age=300",
		"/features/share.js":   "public, max-age=300",
		"/xbot.js":             "public, max-age=86400",
		"/assets/og/heart.png": "public, max-age=86400",
	}
	for p, cc := range cases {
		r := e.get(p)
		if r.status != 200 || r.header.Get("Cache-Control") != cc {
			t.Errorf("%s: %d cache %q", p, r.status, r.header.Get("Cache-Control"))
		}
	}
	if ct := e.get("/app.js").header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("app.js content type %q", ct)
	}
}

func TestStaticNeverLeaks(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{
		"/.env", "/.git/config", "/.git/", "/features/.hidden", "/server/main.go", "/server/", "/deploy/Caddyfile",
		"/escape.txt", "/emptydir/", "/emptydir", "/nope.js", "/%2e%2e/secret.txt", "/..%2fsecret.txt", "/features/..%2f..%2fsecret.txt",
	} {
		r := e.get(p)
		if r.status == 200 || strings.Contains(string(r.body), "leak") || strings.Contains(string(r.body), "outside") ||
			strings.Contains(string(r.body), "package main") || strings.Contains(string(r.body), "readme.txt") {
			t.Errorf("%s: %d %s", p, r.status, r.body)
		}
	}
	if r := e.postAs("/", "text/plain", "x"); r.status != 405 {
		t.Errorf("POST /: %d", r.status)
	}
}

func TestUnknownAPIPaths(t *testing.T) {
	e := newEnv(t)
	e.get("/api/v1/nothing").errCode(t, 404, "not_found")
	r := e.get("/api/v1/assign")
	r.errCode(t, 405, "method_not_allowed")
	if r.header.Get("Allow") != "POST" {
		t.Errorf("Allow %q", r.header.Get("Allow"))
	}
}

// --- CORS

func TestCORS(t *testing.T) {
	e := newEnv(t, withOrigins("https://haleralex.github.io"))
	pre := e.do(e.req("OPTIONS", "/api/v1/events", nil, "Origin", "https://haleralex.github.io", "Access-Control-Request-Method", "POST",
		"Access-Control-Request-Headers", "content-type"))
	if pre.status != 204 || pre.header.Get("Access-Control-Allow-Origin") != "https://haleralex.github.io" ||
		!strings.Contains(pre.header.Get("Access-Control-Allow-Methods"), "POST") ||
		!strings.EqualFold(pre.header.Get("Access-Control-Allow-Headers"), "Content-Type") {
		t.Fatalf("preflight: %d %v", pre.status, pre.header)
	}
	bad := e.do(e.req("OPTIONS", "/api/v1/events", nil, "Origin", "https://evil.example", "Access-Control-Request-Method", "POST"))
	if bad.header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("disallowed origin got CORS headers: %v", bad.header)
	}
	r := e.postJSON("/api/v1/assign", assignReq(vid1, "t1_positioning"), "Origin", "https://haleralex.github.io")
	if r.status != 200 || r.header.Get("Access-Control-Allow-Origin") != "https://haleralex.github.io" || r.header.Get("Vary") != "Origin" {
		t.Fatalf("simple request: %d %v", r.status, r.header)
	}
	if h := e.get("/healthz", "Origin", "https://haleralex.github.io"); h.header.Get("Access-Control-Allow-Origin") == "" {
		t.Error("healthz must be reachable cross-origin (the frontend probes it)")
	}
	if s := e.get("/app.js", "Origin", "https://haleralex.github.io"); s.header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("static files do not need CORS")
	}
}

// --- admin

func basic(user, pass string) []string {
	return []string{"Authorization", "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))}
}

func TestAdminDisabledWithoutCredentials(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/admin", "/admin/api/experiments/t1_positioning", "/admin/api/waitlist.csv"} {
		if r := e.get(p, basic("boss", "s3cret")...); r.status != 404 {
			t.Errorf("%s: %d", p, r.status)
		}
	}
}

func TestAdminAuth(t *testing.T) {
	e := newEnv(t, withAdmin)
	r := e.get("/admin")
	r.errCode(t, 401, "unauthorized")
	if !strings.HasPrefix(r.header.Get("WWW-Authenticate"), "Basic ") {
		t.Errorf("WWW-Authenticate %q", r.header.Get("WWW-Authenticate"))
	}
	e.get("/admin/api/waitlist.csv", basic("boss", "wrong")...).errCode(t, 401, "unauthorized")
	e.get("/admin", basic("Boss", "s3cret")...).errCode(t, 401, "unauthorized")
	ok := e.get("/admin", basic("boss", "s3cret")...)
	if ok.status != 200 || ok.header.Get("Cache-Control") != "no-store" || ok.header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("%d %v", ok.status, ok.header)
	}
	contains(t, ok.body, "t1_positioning", "t2_onboarding", "t3_share_prompt", "t4_price", "fake_door_click")

	// Guessing is limited: 10 wrong passwords per minute (two already used above).
	for i := 0; i < 8; i++ {
		e.get("/admin", basic("boss", fmt.Sprint("guess", i))...).errCode(t, 401, "unauthorized")
	}
	e.get("/admin", basic("boss", "guess-again")...).errCode(t, 429, "rate_limited")
	if r := e.get("/admin", basic("boss", "s3cret")...); r.status != 200 {
		t.Fatalf("the right password must still work: %d", r.status)
	}
}

// seedExperiment puts 40 visitors in t2_onboarding/a (10 activated) and 50 in b (25 activated), plus a few
// near misses that must not count.
func seedExperiment(t *testing.T, e *env) {
	t.Helper()
	ctx := context.Background()
	vid := func(i int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", i) }
	sid := func(i, s int) string { return fmt.Sprintf("00000000-%04d-4000-8000-%012d", s, i) }
	event := func(i, s int, name string, props string) {
		e.exec(`INSERT INTO events (visitor_id, session_id, name, props, at) VALUES ($1, $2, $3, $4::jsonb, now())`, vid(i), sid(i, s), name, props)
	}
	activate := func(i int) {
		for k := 0; k < 3; k++ {
			event(i, 1, "shot", `{}`)
		}
		event(i, 1, "reaction_watched", `{}`)
	}
	for i := 1; i <= 90; i++ {
		variant := "a"
		if i > 40 {
			variant = "b"
		}
		e.exec(`INSERT INTO visitors (id) VALUES ($1)`, vid(i))
		e.exec(`INSERT INTO assignments (visitor_id, experiment_key, variant) VALUES ($1, 't2_onboarding', $2)`, vid(i), variant)
		if (i >= 1 && i <= 10) || (i >= 41 && i <= 65) {
			activate(i)
		}
	}
	_ = ctx
	// Visitor 11: three shots in one session, the reaction in another: not activated.
	for k := 0; k < 3; k++ {
		event(11, 1, "shot", `{}`)
	}
	event(11, 2, "reaction_watched", `{}`)
	// Visitor 12: two shots and a reaction: not activated; but shares a link.
	event(12, 1, "shot", `{}`)
	event(12, 1, "shot", `{}`)
	event(12, 1, "reaction_watched", `{}`)
	event(12, 1, "share_link", `{"case_id": "a7F3kQ9x"}`)
	// Visitor 41 clicks the Pro door and leaves an email.
	event(41, 1, "fake_door_click", `{"door": "pro", "price_variant": "b"}`)
	event(41, 1, "waitlist_submit", `{"door": "pro"}`)
	// Share conversion on a7F3kQ9x: two visitors open it (one twice), one converts.
	event(70, 1, "share_open", `{"case_id": "a7F3kQ9x"}`)
	event(70, 2, "share_open", `{"case_id": "a7F3kQ9x"}`)
	event(70, 2, "share_convert", `{"case_id": "a7F3kQ9x"}`)
	event(71, 1, "share_open", `{"case_id": "a7F3kQ9x"}`)
	// Activation of a visitor outside the experiment is irrelevant to it.
	activate(500)
}

type report struct {
	Key      string `json:"key"`
	Metric   string `json:"metric"`
	Variants []struct {
		Variant   string     `json:"variant"`
		Visitors  int64      `json:"visitors"`
		Converted int64      `json:"converted"`
		Rate      float64    `json:"rate"`
		CI        [2]float64 `json:"ci"`
	} `json:"variants"`
	VsA map[string]struct {
		Diff float64  `json:"diff"`
		P    *float64 `json:"p"`
	} `json:"vs_a"`
}

func getReport(t *testing.T, e *env, path string) report {
	t.Helper()
	r := e.get(path, basic("boss", "s3cret")...)
	if r.status != 200 {
		t.Fatalf("%s: %d %s", path, r.status, r.body)
	}
	var rep report
	if err := json.Unmarshal(r.body, &rep); err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestAdminExperimentStats(t *testing.T) {
	e := newEnv(t, withAdmin)
	seedExperiment(t, e)

	rep := getReport(t, e, "/admin/api/experiments/t2_onboarding")
	if rep.Key != "t2_onboarding" || rep.Metric != "activated" || len(rep.Variants) != 2 {
		t.Fatalf("%+v", rep)
	}
	a, b := rep.Variants[0], rep.Variants[1]
	if a.Variant != "a" || a.Visitors != 40 || a.Converted != 10 || a.Rate != 0.25 {
		t.Fatalf("a: %+v", a)
	}
	if b.Variant != "b" || b.Visitors != 50 || b.Converted != 25 || b.Rate != 0.5 {
		t.Fatalf("b: %+v", b)
	}
	lo, hi := stats.Wilson(10, 40, stats.Z95)
	if a.CI != [2]float64{lo, hi} || math.Abs(lo-0.1419) > 1e-4 || math.Abs(hi-0.4019) > 1e-4 {
		t.Fatalf("a ci %v", a.CI)
	}
	_, p, _ := stats.TwoProportionZ(10, 40, 25, 50)
	cmp := rep.VsA["b"]
	if math.Abs(cmp.Diff-0.25) > 1e-12 || cmp.P == nil || math.Abs(*cmp.P-p) > 1e-12 || math.Abs(p-0.01563) > 1e-4 {
		t.Fatalf("vs a: %+v (want p %v)", cmp, p)
	}
	if _, ok := rep.VsA["a"]; ok {
		t.Error("a is not compared with itself")
	}

	// The api.md spelling and other metrics.
	if r := getReport(t, e, "/admin/api/experiments/t2_onboarding?metric=activation"); r.Metric != "activation" || r.Variants[1].Converted != 25 {
		t.Fatalf("activation: %+v", r)
	}
	if r := getReport(t, e, "/admin/api/experiments/t2_onboarding?metric=shared"); r.Variants[0].Converted != 1 || r.Variants[1].Converted != 0 {
		t.Fatalf("shared: %+v", r)
	}
	if r := getReport(t, e, "/admin/api/experiments/t2_onboarding?metric=fake_door_click"); r.Variants[0].Converted != 0 || r.Variants[1].Converted != 1 {
		t.Fatalf("fake_door_click: %+v", r)
	}

	// An experiment nobody is in: all variants listed with zeros and no p-value.
	empty := getReport(t, e, "/admin/api/experiments/t1_positioning")
	if len(empty.Variants) != 3 || empty.Variants[2].Visitors != 0 || empty.Variants[2].CI != [2]float64{0, 1} || empty.VsA["c"].P != nil {
		t.Fatalf("empty: %+v", empty)
	}

	e.get("/admin/api/experiments/t9_nothing", basic("boss", "s3cret")...).errCode(t, 404, "not_found")
	e.get("/admin/api/experiments/t2_onboarding?metric=revenue", basic("boss", "s3cret")...).errCode(t, 422, "unknown_metric")

	page := e.get("/admin", basic("boss", "s3cret")...)
	contains(t, page.body, "25.0 %", "50.0 %", "+25.0 п.п.", `class="sig">0.016<`)
}

func TestMetricViews(t *testing.T) {
	e := newEnv(t, withAdmin)
	seedExperiment(t, e)
	if n := e.count("SELECT count(*) FROM visitor_metrics WHERE activated"); n != 36 { // 10 + 25 + visitor 500
		t.Fatalf("activated visitors %d", n)
	}
	if n := e.count("SELECT count(*) FROM session_metrics WHERE visitor_id = '00000000-0000-4000-8000-000000000011' AND activated"); n != 0 {
		t.Fatal("activation must happen within one session")
	}
	var opened, converted int
	var rate float64
	if err := e.pool.QueryRow(context.Background(), "SELECT opened, converted, rate FROM share_conversion WHERE case_id = 'a7F3kQ9x'").
		Scan(&opened, &converted, &rate); err != nil {
		t.Fatal(err)
	}
	if opened != 2 || converted != 1 || rate != 0.5 {
		t.Fatalf("share conversion %d %d %v", opened, converted, rate)
	}
	var act, wl, wlAct int
	var wrate float64
	if err := e.pool.QueryRow(context.Background(), "SELECT activated_visitors, waitlist_visitors, waitlist_activated_visitors, rate FROM waitlist_rate").
		Scan(&act, &wl, &wlAct, &wrate); err != nil {
		t.Fatal(err)
	}
	if act != 36 || wl != 1 || wlAct != 1 || math.Abs(wrate-1.0/36) > 1e-12 {
		t.Fatalf("waitlist rate %d %d %d %v", act, wl, wlAct, wrate)
	}
	// Returns: a visit 30 h after the first is d1, one 7.5 days after is d7.
	e.exec(`INSERT INTO events (visitor_id, session_id, name, at) VALUES
		('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001', 'visit', now() - interval '10 days'),
		('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000002', 'visit', now() - interval '10 days' + interval '30 hours'),
		('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000003', 'visit', now() - interval '10 days' + interval '180 hours')`)
	if n := e.count("SELECT count(*) FROM visitor_returns WHERE return_d1 AND return_d7"); n != 1 {
		t.Fatalf("returns %d", n)
	}
}

func TestAdminWaitlistCSV(t *testing.T) {
	e := newEnv(t, withAdmin)
	for _, w := range []map[string]any{
		{"visitor_id": vid1, "email": "Actor@mail.ru", "source": "room", "consent": true},
		{"email": "=1+2@evil.ru", "source": "pro", "price_variant": "b", "consent": true},
	} {
		if r := e.postJSON("/api/v1/waitlist", w); r.status != 201 {
			t.Fatalf("%d %s", r.status, r.body)
		}
	}
	r := e.get("/admin/api/waitlist.csv", basic("boss", "s3cret")...)
	if r.status != 200 || !strings.HasPrefix(r.header.Get("Content-Type"), "text/csv") ||
		!strings.HasPrefix(r.header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("%d %v", r.status, r.header)
	}
	rows, err := csv.NewReader(strings.NewReader(string(r.body))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0][0] != "email" || rows[1][0] != "actor@mail.ru" || rows[1][3] != vid1 ||
		rows[2][0] != "'=1+2@evil.ru" || rows[2][2] != "b" {
		t.Fatalf("%q", rows)
	}
}
