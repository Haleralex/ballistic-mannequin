// Package httpapi is the HTTP side of the service: the API of docs/contracts/api.md, share pages, the admin
// dashboard and the static frontend.
package httpapi

import (
	"context"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Haleralex/ballistic-mannequin/server/internal/config"
	"github.com/Haleralex/ballistic-mannequin/server/internal/ratelimit"
	"github.com/Haleralex/ballistic-mannequin/server/internal/store"
)

// Body limits from api.md. /cases gets 1 KB on top of the 64 KB a case may have, for the envelope around it, so
// that an oversized case is a 422 (case-v1.md) rather than a 413.
const (
	maxBody       = 64 << 10
	maxEventsBody = 256 << 10
	maxCaseBody   = 65 << 10
)

// Limits are requests per minute per IP.
type Limits struct {
	Events, Cases, Ratings, Waitlist, Other int
	// AdminFailures is how many wrong admin passwords an IP may try per minute.
	AdminFailures int
}

// DefaultLimits are the rates of api.md.
var DefaultLimits = Limits{Events: 60, Cases: 20, Ratings: 10, Waitlist: 10, Other: 120, AdminFailures: 10}

//go:embed templates/*.html
var templateFS embed.FS

// Server holds the dependencies of the handlers.
type Server struct {
	cfg    config.Config
	store  *store.Store
	log    *slog.Logger
	static *staticHandler
	tmpl   *template.Template

	events, cases, ratings, waitlist, other, adminFail *ratelimit.Limiter

	bg    sync.WaitGroup // background work (view counts)
	bgSem chan struct{}
	now   func() time.Time
}

// New builds a Server. The static directory is opened once; if it cannot be, static paths answer 404.
func New(cfg config.Config, st *store.Store, log *slog.Logger, lim Limits) *Server {
	s := &Server{
		cfg:       cfg,
		store:     st,
		log:       log,
		tmpl:      template.Must(template.New("").Funcs(templateFuncs).ParseFS(templateFS, "templates/*.html")),
		events:    ratelimit.New(lim.Events),
		cases:     ratelimit.New(lim.Cases),
		ratings:   ratelimit.New(lim.Ratings),
		waitlist:  ratelimit.New(lim.Waitlist),
		other:     ratelimit.New(lim.Other),
		adminFail: ratelimit.New(lim.AdminFailures),
		bgSem:     make(chan struct{}, 32),
		now:       time.Now,
	}
	s.static = newStatic(cfg.StaticDir, log, s.notFoundPage)
	return s
}

// Handler is the root handler with every route and middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/assign", s.limit(s.other, s.handleAssign))
	mux.Handle("POST /api/v1/events", s.limit(s.events, s.handleEvents))
	mux.Handle("POST /api/v1/cases", s.limit(s.cases, s.handleCreateCase))
	mux.Handle("GET /api/v1/cases/{id}", s.limit(s.other, s.handleGetCase))
	mux.Handle("POST /api/v1/ratings", s.limit(s.ratings, s.handleRating))
	mux.Handle("POST /api/v1/waitlist", s.limit(s.waitlist, s.handleWaitlist))
	mux.HandleFunc("/api/", s.handleAPINotFound)

	mux.Handle("GET /r/{id}", s.limit(s.other, s.handleShare))
	mux.HandleFunc("GET /healthz", s.handleHealth)

	mux.Handle("GET /admin", s.admin(s.handleAdmin))
	mux.Handle("GET /admin/{$}", s.admin(s.handleAdmin))
	mux.Handle("GET /admin/api/experiments/{key}", s.admin(s.handleAdminExperiment))
	mux.Handle("GET /admin/api/waitlist.csv", s.admin(s.handleAdminWaitlist))
	mux.Handle("/admin/", s.admin(func(w http.ResponseWriter, r *http.Request) { s.notFoundPage(w, r) }))

	mux.Handle("/", s.static)
	return s.logged(s.cors(mux))
}

// Janitor forgets idle rate-limit buckets until ctx is done.
func (s *Server) Janitor(ctx context.Context) {
	ratelimit.Janitor(ctx, time.Minute, 5*time.Minute, s.events, s.cases, s.ratings, s.waitlist, s.other, s.adminFail)
}

// Wait blocks until background work (view counts) has finished.
func (s *Server) Wait() { s.bg.Wait() }

// background runs fn without blocking the request, unless too much is already running: then it is dropped
// (losing a view count is better than piling up goroutines).
func (s *Server) background(name string, fn func(ctx context.Context) error) {
	select {
	case s.bgSem <- struct{}{}:
	default:
		s.log.Warn("background work dropped", "what", name)
		return
	}
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		defer func() { <-s.bgSem }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			s.log.Warn("background work failed", "what", name, "err", err)
		}
	}()
}

var apiEndpoints = map[string]bool{
	"/api/v1/assign": true, "/api/v1/events": true, "/api/v1/cases": true, "/api/v1/ratings": true, "/api/v1/waitlist": true,
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	if apiEndpoints[r.URL.Path] {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, codeMethod, msgMethod)
		return
	}
	writeError(w, http.StatusNotFound, codeNotFound, msgNotFound)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		s.log.Warn("health check: database", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "db_unavailable", "build": s.cfg.Build, "error": codeUnavailable, "message": msgUnavailable,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "build": s.cfg.Build})
}
