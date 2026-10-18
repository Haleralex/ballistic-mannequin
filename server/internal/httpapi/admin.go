package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/csv"
	"errors"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Haleralex/ballistic-mannequin/server/internal/stats"
	"github.com/Haleralex/ballistic-mannequin/server/internal/store"
	"github.com/Haleralex/ballistic-mannequin/server/internal/taxonomy"
)

// admin guards h with basic auth. Without ADMIN_USER and ADMIN_PASSWORD the admin pages do not exist (404).
// Wrong credentials are limited per IP (Limits.AdminFailures per minute).
func (s *Server) admin(h http.HandlerFunc) http.Handler {
	return s.limit(s.other, func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.AdminEnabled() {
			s.notFoundPage(w, r)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || !s.adminCredentials(user, pass) {
			if ok {
				if allowed, wait := s.adminFail.Allow(clientIP(r, s.cfg.TrustProxy)); !allowed {
					tooMany(w, wait)
					return
				}
				s.log.Warn("admin: wrong credentials", "ip", clientIP(r, s.cfg.TrustProxy))
			}
			w.Header().Set("WWW-Authenticate", `Basic realm="mq admin", charset="UTF-8"`)
			writeError(w, http.StatusUnauthorized, codeUnauthorized, msgUnauthorized)
			return
		}
		hdr := w.Header()
		hdr.Set("Cache-Control", "no-store")
		hdr.Set("X-Frame-Options", "DENY")
		hdr.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
		h(w, r)
	})
}

func (s *Server) adminCredentials(user, pass string) bool {
	eq := func(a, b string) int {
		ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
		return subtle.ConstantTimeCompare(ha[:], hb[:])
	}
	return eq(user, s.cfg.AdminUser)&eq(pass, s.cfg.AdminPassword) == 1
}

// VariantStats is one variant on the experiment's metric.
type VariantStats struct {
	Variant   string     `json:"variant"`
	Visitors  int64      `json:"visitors"`
	Converted int64      `json:"converted"`
	Rate      float64    `json:"rate"`
	CI        [2]float64 `json:"ci"`
}

// Comparison is a variant against the control (variant a).
type Comparison struct {
	Diff float64  `json:"diff"` // rate − control rate
	P    *float64 `json:"p"`    // two-sided p-value of the pooled z-test; null when a group is empty
}

// ExperimentReport is the JSON of /admin/api/experiments/{key}.
type ExperimentReport struct {
	Key      string                `json:"key"`
	Metric   string                `json:"metric"`
	Variants []VariantStats        `json:"variants"`
	VsA      map[string]Comparison `json:"vs_a"`
}

// buildReport computes per-variant conversion with a 95 % Wilson interval and the z-test against the first
// variant (a). counts may lack variants nobody was assigned to yet (they show as zero) or contain variants that
// were removed from the experiment since (they are listed after the current ones).
func buildReport(key, metric string, variants []string, counts map[string]store.VariantCount) ExperimentReport {
	rep := ExperimentReport{Key: key, Metric: metric, Variants: []VariantStats{}, VsA: map[string]Comparison{}}
	order := append([]string(nil), variants...)
	listed := map[string]bool{}
	for _, v := range variants {
		listed[v] = true
	}
	var extra []string
	for v := range counts {
		if !listed[v] {
			extra = append(extra, v)
		}
	}
	sort.Strings(extra)
	order = append(order, extra...)
	for _, v := range order {
		c := counts[v]
		vs := VariantStats{Variant: v, Visitors: c.Visitors, Converted: c.Converted}
		if c.Visitors > 0 {
			vs.Rate = float64(c.Converted) / float64(c.Visitors)
		}
		vs.CI[0], vs.CI[1] = stats.Wilson(c.Converted, c.Visitors, stats.Z95)
		rep.Variants = append(rep.Variants, vs)
	}
	if len(rep.Variants) == 0 {
		return rep
	}
	ctl := rep.Variants[0]
	for _, v := range rep.Variants[1:] {
		cmp := Comparison{Diff: v.Rate - ctl.Rate}
		if _, p, ok := stats.TwoProportionZ(ctl.Converted, ctl.Visitors, v.Converted, v.Visitors); ok && !math.IsNaN(p) {
			cmp.P = &p
		}
		rep.VsA[v.Variant] = cmp
	}
	return rep
}

func (s *Server) experimentReport(ctx context.Context, e *store.Experiment, metricName string) (*ExperimentReport, error) {
	m, ok := taxonomy.ResolveMetric(metricName)
	if !ok {
		return nil, errUnknownMetric
	}
	counts, err := s.store.Conversions(ctx, e.Key, m)
	if err != nil {
		return nil, err
	}
	rep := buildReport(e.Key, metricName, e.Variants, counts)
	return &rep, nil
}

var errUnknownMetric = errors.New("unknown metric")

func (s *Server) handleAdminExperiment(w http.ResponseWriter, r *http.Request) {
	e, err := s.store.Experiment(r.Context(), r.PathValue("key"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, codeNotFound, msgExperimentGone)
		return
	}
	if err != nil {
		s.internal(w, r, "admin experiment", err)
		return
	}
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		metric = e.Metric
	}
	rep, err := s.experimentReport(r.Context(), e, metric)
	if errors.Is(err, errUnknownMetric) {
		invalid(w, "unknown_metric", msgUnknownMetric+" Допустимо: "+strings.Join(taxonomy.MetricNames(), ", ")+".")
		return
	}
	if err != nil {
		s.internal(w, r, "admin experiment", err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// --- the HTML dashboard

type adminVariantRow struct {
	VariantStats
	Funnel store.FunnelRow
	Cmp    *Comparison
}

type adminExperiment struct {
	store.Experiment
	Rows []adminVariantRow
	Err  string
}

type adminPage struct {
	Build       string
	Generated   time.Time
	Summary     *store.Summary
	Experiments []adminExperiment
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	exps, err := s.store.Experiments(ctx)
	if err != nil {
		s.internal(w, r, "admin", err)
		return
	}
	sum, err := s.store.Summary(ctx)
	if err != nil {
		s.internal(w, r, "admin", err)
		return
	}
	page := adminPage{Build: s.cfg.Build, Generated: s.now().UTC(), Summary: sum}
	for i := range exps {
		e := exps[i]
		ae := adminExperiment{Experiment: e}
		rep, err := s.experimentReport(ctx, &e, e.Metric)
		if err != nil {
			ae.Err = err.Error()
			page.Experiments = append(page.Experiments, ae)
			continue
		}
		funnel, err := s.store.Funnel(ctx, e.Key)
		if err != nil {
			s.internal(w, r, "admin funnel", err)
			return
		}
		for _, v := range rep.Variants {
			row := adminVariantRow{VariantStats: v, Funnel: funnel[v.Variant]}
			if c, ok := rep.VsA[v.Variant]; ok {
				row.Cmp = &c
			}
			ae.Rows = append(ae.Rows, row)
		}
		page.Experiments = append(page.Experiments, ae)
	}
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, "admin.html", page); err != nil {
		s.internal(w, r, "admin template", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

// --- GET /admin/api/waitlist.csv

func (s *Server) handleAdminWaitlist(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"email", "source", "price_variant", "visitor_id", "consent_at", "created_at"})
	err := s.store.EachWaitlist(r.Context(), func(e store.WaitlistEntry) error {
		return cw.Write([]string{
			csvSafe(e.Email), csvSafe(e.Source), csvSafe(deref(e.PriceVariant)), deref(e.VisitorID),
			e.ConsentAt.UTC().Format(time.RFC3339), e.CreatedAt.UTC().Format(time.RFC3339),
		})
	})
	cw.Flush()
	if err == nil {
		err = cw.Error()
	}
	if err != nil {
		s.internal(w, r, "waitlist csv", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="waitlist-%s.csv"`, s.now().UTC().Format("2006-01-02")))
	_, _ = w.Write(buf.Bytes())
}

// csvSafe defuses spreadsheet formulas: a cell starting with = + - @ or a control character is prefixed with '.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// --- template helpers

var templateFuncs = template.FuncMap{
	"pct": func(x float64) string { return fmt.Sprintf("%.1f %%", 100*x) },
	"pp": func(x float64) template.HTML { // a difference of rates, in percentage points; only digits and signs
		return template.HTML(fmt.Sprintf("%+.1f п.п.", 100*x))
	},
	"pval": func(p *float64) string {
		if p == nil {
			return "—"
		}
		if *p < 0.001 {
			return "< 0.001"
		}
		return fmt.Sprintf("%.3f", *p)
	},
	"significant": func(p *float64) bool { return p != nil && *p < 0.05 },
	"ratio": func(a, b int64) string {
		if b == 0 {
			return "—"
		}
		return fmt.Sprintf("%.1f %%", 100*float64(a)/float64(b))
	},
	"optpct": func(p *float64) string {
		if p == nil {
			return "—"
		}
		return fmt.Sprintf("%.1f %%", 100**p)
	},
	"join": strings.Join,
	"time": func(t time.Time) string { return t.Format("2006-01-02 15:04 UTC") },
}
