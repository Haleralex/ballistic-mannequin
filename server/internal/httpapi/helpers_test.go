package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Haleralex/ballistic-mannequin/server/internal/config"
	"github.com/Haleralex/ballistic-mannequin/server/internal/dbtest"
	"github.com/Haleralex/ballistic-mannequin/server/internal/httpapi"
	"github.com/Haleralex/ballistic-mannequin/server/internal/store"
)

type env struct {
	t    *testing.T
	srv  *httptest.Server
	api  *httpapi.Server
	pool *pgxpool.Pool
	cfg  config.Config
	dir  string // STATIC_DIR
}

type option func(*config.Config, *httpapi.Limits)

func withAdmin(c *config.Config, _ *httpapi.Limits)      { c.AdminUser, c.AdminPassword = "boss", "s3cret" }
func withTrustProxy(c *config.Config, _ *httpapi.Limits) { c.TrustProxy = 1 }
func withLimits(f func(*httpapi.Limits)) option {
	return func(_ *config.Config, l *httpapi.Limits) { f(l) }
}
func withOrigins(o ...string) option {
	return func(c *config.Config, _ *httpapi.Limits) { c.AllowedOrigins = o }
}

func newEnv(t *testing.T, opts ...option) *env {
	t.Helper()
	pool := dbtest.New(t)
	dir := staticFixture(t)
	cfg := config.Config{Port: "0", StaticDir: dir, Build: "test123"}
	lim := httpapi.DefaultLimits
	for _, o := range opts {
		o(&cfg, &lim)
	}
	api := httpapi.New(cfg, store.New(pool), dbtest.Quiet(), lim)
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(func() { srv.Close(); api.Wait() })
	return &env{t: t, srv: srv, api: api, pool: pool, cfg: cfg, dir: dir}
}

// staticFixture is a small STATIC_DIR with the files the static tests poke at, and a secret next to it.
func staticFixture(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	dir := filepath.Join(parent, "site")
	files := map[string]string{
		"index.html":            "<!doctype html><title>sim</title>",
		"app.js":                "export const app = 1;",
		"xbot.js":               "export default 'glb';",
		".env":                  "ADMIN_PASSWORD=leak",
		".git/config":           "[core]",
		"server/main.go":        "package main",
		"deploy/Caddyfile":      "example.com",
		"features/share.js":     "export {};",
		"assets/og/heart.png":   "png",
		"assets/og/abdomen.png": "png",
		"docs/index.html":       "<p>docs</p>",
		"emptydir/readme.txt":   "no index here",
		"tests/smoke.test.mjs":  "test",
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	secret := filepath.Join(parent, "secret.txt")
	if err := os.WriteFile(secret, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	return dir
}

type resp struct {
	status int
	header http.Header
	body   []byte
}

func (r resp) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatalf("body is not JSON (%d): %s", r.status, r.body)
	}
	return m
}

// errCode checks a JSON error answer: status, error code and a non-empty Russian message.
func (r resp) errCode(t *testing.T, status int, code string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("status %d, want %d: %s", r.status, status, r.body)
	}
	m := r.json(t)
	if code != "" && m["error"] != code {
		t.Fatalf("error %v, want %s: %s", m["error"], code, r.body)
	}
	msg, _ := m["message"].(string)
	if msg == "" || !hasCyrillic(msg) {
		t.Fatalf("message %q should be Russian", msg)
	}
}

func hasCyrillic(s string) bool {
	for _, r := range s {
		if r >= 'А' && r <= 'я' {
			return true
		}
	}
	return false
}

func (e *env) do(req *http.Request) resp {
	e.t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{status: res.StatusCode, header: res.Header, body: b}
}

func (e *env) req(method, path string, body io.Reader, hdr ...string) *http.Request {
	e.t.Helper()
	req, err := http.NewRequest(method, e.srv.URL+path, body)
	if err != nil {
		e.t.Fatal(err)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	return req
}

func (e *env) get(path string, hdr ...string) resp { return e.do(e.req("GET", path, nil, hdr...)) }

// postJSON sends v (a value, or a string/[]byte taken as the raw body) as application/json.
func (e *env) postJSON(path string, v any, hdr ...string) resp {
	return e.postAs(path, "application/json", v, hdr...)
}

func (e *env) postAs(path, contentType string, v any, hdr ...string) resp {
	e.t.Helper()
	var body []byte
	switch x := v.(type) {
	case string:
		body = []byte(x)
	case []byte:
		body = x
	default:
		var err error
		if body, err = json.Marshal(v); err != nil {
			e.t.Fatal(err)
		}
	}
	h := append([]string{"Content-Type", contentType}, hdr...)
	if contentType == "" {
		h = hdr
	}
	return e.do(e.req("POST", path, bytes.NewReader(body), h...))
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatalf("%s: %v", sql, err)
	}
}

func contains(t *testing.T, body []byte, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(string(body), p) {
			t.Errorf("body lacks %q:\n%s", p, body)
		}
	}
}

const (
	vid1 = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
	vid2 = "9b2c7d1e-1111-4a2b-8c3d-444455556666"
	sid1 = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
)

// exampleCase is the case of docs/contracts/case-v1.md (with the placeholder hash filled in).
const exampleCase = `{
  "v": 1, "seed": 3141592653, "build": "f2f18ed", "steps": 7800,
  "events": [
    { "step": 0,   "type": "setting", "key": "react",   "value": "flee" },
    { "step": 0,   "type": "setting", "key": "weapon",  "value": "pistol" },
    { "step": 214, "type": "shot", "origin": [0.9, 1.45, 4.2], "dir": [-0.19, -0.09, -0.98] },
    { "step": 216, "type": "script", "source": "ai", "title": "…", "beats": [ { "t": 0.2, "dur": 2, "action": "look", "at": "wound", "intensity": 0.8, "note": "…" } ] }
  ],
  "check": [ { "step": 1200, "hash": "9f3a0c1e" }, { "step": 2400, "hash": "00ff00ff" } ],
  "meta": { "title": "Печень, пистолет", "weapon": "pistol", "kind": "abdomen", "zones": ["живот"], "organs": ["печень"] }
}`
