package httpapi

import (
	"log/slog"
	"math"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/Haleralex/ballistic-mannequin/server/internal/ratelimit"
)

// statusWriter remembers the status code for the access log.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the real writer (write deadlines, flush).
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// clientIP is the address the rate limits and logs use. With TrustProxy = n it is the n-th address from the right
// of X-Forwarded-For (what the outermost trusted proxy saw), else the TCP peer.
func clientIP(r *http.Request, trustProxy int) string {
	peer := r.RemoteAddr
	if host, _, err := net.SplitHostPort(peer); err == nil {
		peer = host
	}
	if trustProxy <= 0 {
		return peer
	}
	var hops []string
	for _, h := range r.Header.Values("X-Forwarded-For") {
		for _, p := range strings.Split(h, ",") {
			if p = strings.TrimSpace(p); p != "" {
				hops = append(hops, p)
			}
		}
	}
	if len(hops) == 0 {
		return peer
	}
	i := len(hops) - trustProxy
	if i < 0 {
		i = 0
	}
	ip := hops[i]
	if host, _, err := net.SplitHostPort(ip); err == nil { // "1.2.3.4:5678" or "[::1]:80"
		ip = host
	}
	if net.ParseIP(ip) == nil {
		return peer
	}
	return ip
}

// scheme is http or https as the browser sees it.
func (s *Server) scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if s.cfg.TrustProxy > 0 {
		if p := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])); p == "https" || p == "http" {
			return p
		}
	}
	return "http"
}

// logged writes one access-log line per request and recovers from panics.
func (s *Server) logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic", "err", v, "path", r.URL.Path, "stack", string(debug.Stack()))
				if sw.status == 0 {
					writeError(sw, http.StatusInternalServerError, codeInternal, msgInternal)
				}
			}
			level := slog.LevelInfo
			if sw.status == 0 {
				sw.status = http.StatusOK
			}
			if !isAPI(r.URL.Path) && sw.status < 400 {
				level = slog.LevelDebug // static files
			}
			if sw.status >= 500 {
				level = slog.LevelError
			}
			s.log.Log(r.Context(), level, "request", "method", r.Method, "path", r.URL.Path, "status", sw.status,
				"bytes", sw.bytes, "ms", time.Since(start).Milliseconds(), "ip", clientIP(r, s.cfg.TrustProxy))
		}()
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(sw, r)
	})
}

func isAPI(path string) bool {
	return strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/admin") || strings.HasPrefix(path, "/r/") ||
		path == "/healthz"
}

// cors answers preflights and adds CORS headers for allowed origins on /api/ and /healthz.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		allowed := origin != "" && s.originAllowed(origin)
		if allowed {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if allowed {
				h := w.Header()
				h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type")
				h.Set("Access-Control-Max-Age", "600")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	for _, o := range s.cfg.AllowedOrigins {
		if o == "*" || strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}

// limit applies the per-IP token bucket l to h.
func (s *Server) limit(l *ratelimit.Limiter, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := l.Allow(clientIP(r, s.cfg.TrustProxy)); !ok {
			tooMany(w, wait)
			return
		}
		h(w, r)
	})
}

func tooMany(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Max(1, math.Ceil(wait.Seconds())))))
	writeError(w, http.StatusTooManyRequests, codeRateLimited, msgRateLimited)
}
