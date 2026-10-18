# server — Go API for ballistic-mannequin

One Go process serves the HTTP API (`docs/contracts/api.md`), the share pages `/r/{id}`, the admin dashboard and
the static frontend from `STATIC_DIR`. Postgres 16 holds everything. Module
`github.com/Haleralex/ballistic-mannequin/server`; Go 1.24; dependencies: `pgx/v5` and `golang.org/x/time/rate`.

```
server/
  cmd/api/            main: config, DB connect + migrate, HTTP server, graceful shutdown
  migrations/         *.sql, embedded; 001 = docs/contracts/schema.sql, 002 = experiments, 003 = metric views
  internal/config     environment variables
  internal/db         pool with retry, migrator (advisory lock, one transaction per file, schema_migrations)
  internal/store      every SQL query
  internal/httpapi    handlers, middleware (CORS, rate limits, logs), templates for /r, /admin, 404
  internal/caseval    case v1 validation (docs/contracts/case-v1.md)
  internal/assign     deterministic weighted variant choice
  internal/stats      Wilson interval, two-proportion z-test
  internal/ids        base62 case ids, UUID checks
  internal/ratelimit  per-IP token buckets
  internal/taxonomy   event allowlist (docs/contracts/events.md) and metric names
  internal/dbtest     a fresh Postgres schema per integration test
```

## Run

```sh
cd server
DATABASE_URL='postgres://mq:mq@localhost:5432/mq?sslmode=disable' STATIC_DIR=.. go run ./cmd/api
# or
go build -o api ./cmd/api && STATIC_DIR=.. ./api
```

On start the server waits up to 60 s for the database (useful in docker compose), applies pending migrations under
a Postgres advisory lock (several instances may start at once), then listens on `PORT`. SIGTERM or SIGINT stops it
gracefully: no new connections, in-flight requests get 15 s, pending view counts are flushed.

Flags: `-migrate` applies migrations and exits; `-healthcheck` requests `http://127.0.0.1:$PORT/healthz` and
exits 0 on 200 — for a container `HEALTHCHECK` without curl.

The database user needs `CREATE` on the database for the first migration (`CREATE EXTENSION citext`; citext is a
trusted extension, so a database owner can install it without superuser).

## Environment

| Variable | Default | Meaning |
|----------|---------|---------|
| `PORT` | `8080` | HTTP port. |
| `DATABASE_URL` | `postgres://mq:mq@localhost:5432/mq?sslmode=disable` | Postgres (pgx syntax; `pool_max_conns=…` works). |
| `STATIC_DIR` | `.` | Directory served at `/` (the repo root: `index.html`, `app.js`, `features/`, `xbot.js`, `assets/`…). From `server/` that is `..`. |
| `ALLOWED_ORIGINS` | empty | Extra CORS origins, comma-separated, e.g. `https://haleralex.github.io`. `*` allows any. Same-origin needs none. |
| `ADMIN_USER`, `ADMIN_PASSWORD` | — | Basic auth for `/admin`. If either is empty, `/admin*` answers 404. |
| `BUILD` | `dev` | Short git sha, returned by `/healthz`. |
| `TRUST_PROXY` | `0` | Number of reverse proxies in front (`1` or `true` behind Caddy). Only then are `X-Forwarded-For` (for rate limits and logs) and `X-Forwarded-Proto` (for absolute `og:` URLs) believed. The client IP is the address that many hops from the right of `X-Forwarded-For`, so a client-supplied left part cannot spoof it. **Behind a proxy without it, every visitor shares the proxy's IP and its rate limit.** Never set it when the port is reachable directly. |
| `LOG_LEVEL` | `info` | `debug` also logs static file requests. Logs are JSON lines on stdout. |

## Endpoints

| Method, path | Answer | Limit / IP / min |
|--------------|--------|------------------|
| `POST /api/v1/assign` | `200 {assignments}` — sticky variants of the active experiments asked for; upserts the visitor (first `referrer`/`utm` kept) | 120 |
| `POST /api/v1/events` | `202 {accepted, rejected}` — ≤ 100 events, names from events.md, `text/plain` (sendBeacon) accepted, body ≤ 256 KB | 60 |
| `POST /api/v1/cases` | `201 {id, url}` — case v1 validated, 8-char base62 id from crypto/rand | 20 |
| `GET /api/v1/cases/{id}` | `200` case v1 · `404`; counts a view in the background | 120 |
| `POST /api/v1/ratings` | `201 {}` — one per visitor, scenario and UTC day; a later vote replaces value and comment | 10 |
| `POST /api/v1/waitlist` | `201 {}` — also when the address exists; `consent: true` required | 10 |
| `GET /r/{id}` | HTML with Open Graph tags, then `/?case={id}` (meta refresh + `location.replace`) · 404 page | 120 |
| `GET /healthz` | `200 {"status":"ok","build":"…"}` · `503` when the database does not answer | none |
| `GET /admin` | dashboard (basic auth) | 120, plus 10 wrong passwords |
| `GET /admin/api/experiments/{key}?metric=` | JSON per api.md | same |
| `GET /admin/api/waitlist.csv` | CSV of the waitlist | same |
| `GET /…` | static files from `STATIC_DIR` | none |

Errors are `{"error": "<code>", "message": "<Russian text>"}`: 400 malformed JSON or wrong `Content-Type`
(`bad_json`, `unsupported_media_type`), 404 `not_found`, 405 `method_not_allowed`, 413 `too_large`, 422 for
validation (`invalid_visitor_id`, `invalid_case`, `consent_required`, `invalid_email`, `too_many_events`, …),
429 `rate_limited` with `Retry-After`, 500 `internal`. The 422 for a case names the field:
`"Кейс не прошёл проверку: events[3].dir: направление должно быть единичным вектором (длина 1.2000)."`

Details worth knowing:

- **Assignment**: SHA-256 of `visitor_id + key` (lower-case canonical UUID, no separator), first 8 bytes big-endian
  modulo the sum of weights, walked over the cumulative weights. Stored in `assignments` on first request; later
  requests return the stored variant even if weights or variants changed. Unknown or inactive experiments are
  left out of the answer.
- **Events**: a bad event (unknown name, non-object props, wrong types) is counted in `rejected` without failing the
  batch. `at` missing, unparsable or more than 24 h from the server clock becomes the server time. Missing props
  are stored as `{}`. Visitor and session ids must be UUIDs (422 otherwise).
- **Cases**: all of case-v1.md is enforced — `v = 1`, `seed` a uint32, `build` `[A-Za-z0-9._-]{1,64}`,
  `1 ≤ steps ≤ 21600`, ≤ 200 events sorted by step with `0 ≤ step ≤ steps`, setting keys and values from the
  enum table (`bleed` as `1`/`10` number or string), `dir` of length 1 ± 0.01, `origin` within 1 km, scripts with
  `source`, `title`, `beats` (each with `action`, `t`, `dur`), `check` hashes of 8 hex digits, `meta` strings and
  string lists; unknown fields at the top level and in events are rejected (meta and beats may carry extra
  fields). The JSON is stored as sent (`jsonb`), with `kind` = `meta.kind`, `weapon` = `meta.weapon` or else the
  first weapon setting, `seed`, `build` in columns.
- **Static files**: served through `os.Root`, so neither `..` nor symlinks leave `STATIC_DIR`. Any path segment
  starting with `.` (`.env`, `.git`, `.devcontainer`) is 404, as are the top-level `server/`, `deploy/` and
  `node_modules/`. No directory listings. `.html` and directories: `Cache-Control: no-cache`; `xbot.js`, images,
  video and fonts: `public, max-age=86400`; other files (JS modules, CSS): `public, max-age=300`.
- **CORS** applies to `/api/` and `/healthz` only. Preflights are answered with `204`.
- **Share page**: `og:title` = `meta.title` (or «Реакция на ранение») + « — реакция за N с»; `og:description` =
  `meta.description`, or weapon · zones · organs and a neutral line; `og:image` =
  `<scheme>://<host>/assets/og/<meta.kind>.png` (`default.png` when there is no kind). It does not count a view.

## Statistics (admin)

The unit is the **visitor** assigned to a variant (`assignments`). A visitor counts as converted if any of their
sessions converted; visitors without events count as not converted. Metrics:

- `activated` (alias `activation`): a session with ≥ 3 `shot` and ≥ 1 `reaction_watched`;
- `shared` (alias `share`): a session with `share_link` or `clip_export`;
- `waitlist` or any event name from events.md (e.g. `fake_door_click`): the visitor sent that event at least once.

Without `?metric=` the experiment's own `metric` column is used. Per variant: `rate = converted / visitors`, a 95 %
**Wilson score interval** (`[0, 1]` when there are no visitors), and against the first variant (`a`) the
difference of rates and the two-sided p-value of the **pooled two-proportion z-test**
(`z = (p₂ − p₁) / √(p̂(1 − p̂)(1/n₁ + 1/n₂))`, `p = erfc(|z|/√2)`; `null` when a group is empty, `1` when both rates
are 0 or both 1). No correction for multiple comparisons or peeking: with three variants, or when checking daily,
treat p ≈ 0.05 with suspicion. Events are counted regardless of whether they happened before or after the
assignment (the assignment is requested on page load, before anything measurable happens).

SQL views (migration 003) for ad-hoc queries in `psql`: `session_metrics`, `visitor_metrics`, `share_conversion`
(distinct visitors who opened a case's link and who then fired their own shot), `waitlist_rate` (visitors with
`waitlist_submit` / activated visitors, plus the count of those who were activated themselves), `visitor_returns`
(`return_d1`: a visit 24–48 h after the first, `return_d7`: 7–8 days after) and `experiment_funnel`.

## Tests

```sh
cd server
go vet ./... && gofmt -l .
go test ./...                       # unit tests; integration tests skip
createdb mq_test
TEST_DATABASE_URL='postgres://localhost/mq_test?sslmode=disable' go test ./...   # with Postgres
```

Each integration test gets its own schema in that database (dropped afterwards) with all migrations applied, so
tests run in parallel and leave nothing behind. `citext` is installed once in `public`.
