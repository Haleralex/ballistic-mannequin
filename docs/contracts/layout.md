# Repository layout, ownership and runtime settings

Each team changes only the paths it owns. Anything else is a request to the owner (or to integration).

| Path | Owner | What |
|------|-------|------|
| `app.js` | A | The simulator. Team C does not edit it; it asks team A for hooks. |
| `bus.js` | A (the `sim` object), C may add events only by agreement | Events and the simulator API. |
| `index.html` | C | Markup and styles of the simulator page (A touches it only for replay controls agreed with C). |
| `features/*.js` | C | Growth features: `api.js` (HTTP client, offline detection), `analytics.js`, `experiments.js`, `share.js`, `clip.js`, `rating.js`, `doors.js`, `landing.js`. |
| `landing.html`, `assets/` | C | Landing page and its media. |
| `server/` | B | Go module `github.com/Haleralex/ballistic-mannequin/server`: `cmd/api`, `internal/...`, `migrations/` (SQL, embedded). |
| `deploy/`, `.devcontainer/`, `.github/workflows/`, `Makefile` | D | Compose files, Caddy, systemd units, backups, CI, Codespaces. |
| `tests/` | shared: each team adds its own `*.test.mjs`; the harness (`tests/lib/`) is changed by integration | Browser tests. Go tests live next to Go code. |
| `docs/contracts/` | integration | Contracts. Changes are proposed, not made silently. |

## Runtime

One Go process serves both the API and the static frontend, so the browser always talks to its own origin. Locally,
in Codespaces and on the VPS there is one port to open. On the VPS Caddy sits in front for TLS.

| Variable | Default | Meaning |
|----------|---------|---------|
| `PORT` | `8080` | HTTP port of the Go server. |
| `DATABASE_URL` | `postgres://mq:mq@localhost:5432/mq?sslmode=disable` | Postgres. |
| `STATIC_DIR` | `.` (repo root) | Directory with `index.html`, `app.js`, `features/`, `xbot.js`, … served at `/`. |
| `ALLOWED_ORIGINS` | empty | Extra CORS origins (e.g. the GitHub Pages site during tests). |
| `ADMIN_USER`, `ADMIN_PASSWORD` | — | Basic auth for `/admin`; without them `/admin` answers 404. |
| `BUILD` | `dev` | Short git sha, exposed at `/healthz` and stamped into cases by the frontend. |

The frontend finds the API by `GET /healthz` on its own origin. If it does not answer (GitHub Pages, a file opened
locally) the online features hide themselves and the simulator works as before.

## Commands (team D makes them real)

```
make dev        # postgres in docker + go run ./server/cmd/api, serves http://localhost:8080
make test       # go test ./... and the browser tests
make db-shell   # psql into the dev database
```
