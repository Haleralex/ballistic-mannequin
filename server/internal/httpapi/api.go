package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Haleralex/ballistic-mannequin/server/internal/caseval"
	"github.com/Haleralex/ballistic-mannequin/server/internal/ids"
	"github.com/Haleralex/ballistic-mannequin/server/internal/store"
	"github.com/Haleralex/ballistic-mannequin/server/internal/taxonomy"
)

const (
	maxExperimentsPerAssign = 20
	maxEventsPerBatch       = 100
	maxReferrer             = 2048
	maxUTMBytes             = 2048
	maxComment              = 500
	clockSkew               = 24 * time.Hour
)

// --- POST /api/v1/assign

type assignRequest struct {
	VisitorID   string          `json:"visitor_id"`
	Experiments []string        `json:"experiments"`
	Referrer    *string         `json:"referrer"`
	UTM         json.RawMessage `json:"utm"`
}

func (s *Server) handleAssign(w http.ResponseWriter, r *http.Request) {
	var req assignRequest
	if !decodeJSON(w, r, &req, maxBody, false) {
		return
	}
	vid, ok := ids.NormalizeUUID(req.VisitorID)
	if !ok {
		invalid(w, "invalid_visitor_id", msgVisitorID)
		return
	}
	if len(req.Experiments) > maxExperimentsPerAssign {
		invalid(w, "too_many_experiments", "Слишком много экспериментов в одном запросе.")
		return
	}
	var keys []string
	seen := map[string]bool{}
	for _, k := range req.Experiments {
		if k != "" && len(k) <= 64 && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	v := store.Visitor{ID: vid, UTM: objectOrNil(req.UTM, maxUTMBytes)}
	if req.Referrer != nil {
		if ref := truncate(strings.TrimSpace(*req.Referrer), maxReferrer); ref != "" {
			v.Referrer = &ref
		}
	}
	got, err := s.store.Assign(r.Context(), v, keys)
	if err != nil {
		s.internal(w, r, "assign", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"assignments": got})
}

// objectOrNil returns raw if it is a JSON object of at most max bytes, else nil. utm is optional context, so a
// malformed one is dropped rather than failing the request.
func objectOrNil(raw json.RawMessage, max int) []byte {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' || len(raw) > max {
		return nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil
	}
	return buf.Bytes()
}

func truncate(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes])
}

// --- POST /api/v1/events

type eventsRequest struct {
	VisitorID string            `json:"visitor_id"`
	SessionID string            `json:"session_id"`
	Events    []json.RawMessage `json:"events"`
}

type eventIn struct {
	Name  string          `json:"name"`
	At    string          `json:"at"`
	Props json.RawMessage `json:"props"`
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	var req eventsRequest
	if !decodeJSON(w, r, &req, maxEventsBody, true) {
		return
	}
	vid, ok := ids.NormalizeUUID(req.VisitorID)
	if !ok {
		invalid(w, "invalid_visitor_id", msgVisitorID)
		return
	}
	sid, ok := ids.NormalizeUUID(req.SessionID)
	if !ok {
		invalid(w, "invalid_session_id", msgSessionID)
		return
	}
	if len(req.Events) > maxEventsPerBatch {
		invalid(w, "too_many_events", "Не больше 100 событий за один запрос.")
		return
	}
	now := s.now().UTC()
	evs := make([]store.Event, 0, len(req.Events))
	rejected := 0
	for _, raw := range req.Events {
		e, ok := parseEvent(raw, now)
		if !ok {
			rejected++
			continue
		}
		e.VisitorID, e.SessionID = vid, sid
		evs = append(evs, e)
	}
	if err := s.store.InsertEvents(r.Context(), evs); err != nil {
		s.internal(w, r, "events", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int{"accepted": len(evs), "rejected": rejected})
}

// parseEvent checks one event; a bad one is rejected on its own without failing the batch.
func parseEvent(raw json.RawMessage, now time.Time) (store.Event, bool) {
	var in eventIn
	if err := json.Unmarshal(raw, &in); err != nil || !taxonomy.Events[in.Name] {
		return store.Event{}, false
	}
	props := bytes.TrimSpace(in.Props)
	switch {
	case len(props) == 0 || string(props) == "null":
		props = []byte("{}")
	case props[0] != '{':
		return store.Event{}, false
	}
	at := now
	if t, err := time.Parse(time.RFC3339Nano, in.At); err == nil {
		if d := t.Sub(now); d <= clockSkew && d >= -clockSkew {
			at = t.UTC()
		}
	}
	return store.Event{Name: in.Name, Props: props, At: at}, true
}

// --- POST /api/v1/cases, GET /api/v1/cases/{id}

type caseRequest struct {
	VisitorID *string         `json:"visitor_id"`
	Case      json.RawMessage `json:"case"`
}

func (s *Server) handleCreateCase(w http.ResponseWriter, r *http.Request) {
	var req caseRequest
	if !decodeJSON(w, r, &req, maxCaseBody, false) {
		return
	}
	var vid *string
	if req.VisitorID != nil && *req.VisitorID != "" {
		v, ok := ids.NormalizeUUID(*req.VisitorID)
		if !ok {
			invalid(w, "invalid_visitor_id", msgVisitorID)
			return
		}
		vid = &v
	}
	raw := bytes.TrimSpace(req.Case)
	if len(raw) == 0 || string(raw) == "null" {
		invalid(w, "invalid_case", "В запросе нет кейса.")
		return
	}
	sum, err := caseval.Validate(raw)
	if err != nil {
		invalid(w, "invalid_case", "Кейс не прошёл проверку: "+err.Error()+".")
		return
	}
	c := store.Case{VisitorID: vid, Build: sum.Build, Seed: sum.Seed, Data: raw}
	if sum.Kind != "" {
		c.Kind = &sum.Kind
	}
	if sum.Weapon != "" {
		c.Weapon = &sum.Weapon
	}
	for attempt := 0; ; attempt++ {
		if c.ID, err = ids.NewCaseID(); err != nil {
			s.internal(w, r, "case id", err)
			return
		}
		err = s.store.CreateCase(r.Context(), c)
		if !errors.Is(err, store.ErrDuplicate) || attempt >= 4 {
			break
		}
	}
	if err != nil {
		s.internal(w, r, "create case", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": c.ID, "url": "/r/" + c.ID})
}

func (s *Server) handleGetCase(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !ids.IsCaseID(id) {
		writeError(w, http.StatusNotFound, codeNotFound, msgCaseNotFound)
		return
	}
	c, err := s.store.GetCase(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, codeNotFound, msgCaseNotFound)
		return
	}
	if err != nil {
		s.internal(w, r, "get case", err)
		return
	}
	s.background("count view", func(ctx context.Context) error { return s.store.CountView(ctx, id) })
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(c.Data)
}

// --- POST /api/v1/ratings

type ratingRequest struct {
	VisitorID  string  `json:"visitor_id"`
	ScenarioID string  `json:"scenario_id"`
	CaseID     *string `json:"case_id"`
	Value      *int    `json:"value"`
	Comment    *string `json:"comment"`
}

func (s *Server) handleRating(w http.ResponseWriter, r *http.Request) {
	var req ratingRequest
	if !decodeJSON(w, r, &req, maxBody, false) {
		return
	}
	vid, ok := ids.NormalizeUUID(req.VisitorID)
	if !ok {
		invalid(w, "invalid_visitor_id", msgVisitorID)
		return
	}
	scenario := strings.TrimSpace(req.ScenarioID)
	if scenario == "" || utf8.RuneCountInString(scenario) > 100 || strings.IndexFunc(scenario, unicode.IsControl) >= 0 {
		invalid(w, "invalid_scenario_id", "Не указан сценарий.")
		return
	}
	if req.Value == nil || (*req.Value != 1 && *req.Value != -1) {
		invalid(w, "invalid_value", "Оценка должна быть 1 или -1.")
		return
	}
	rt := store.Rating{VisitorID: vid, ScenarioID: scenario, Value: *req.Value, Day: s.now().UTC()}
	if req.CaseID != nil && *req.CaseID != "" {
		if !ids.IsCaseID(*req.CaseID) {
			invalid(w, "invalid_case_id", "Неверный идентификатор реакции.")
			return
		}
		rt.CaseID = req.CaseID
	}
	if req.Comment != nil {
		c := strings.TrimSpace(*req.Comment)
		if utf8.RuneCountInString(c) > maxComment {
			invalid(w, "comment_too_long", "Комментарий не длиннее 500 символов.")
			return
		}
		if c != "" {
			rt.Comment = &c
		}
	}
	err := s.store.UpsertRating(r.Context(), rt)
	if errors.Is(err, store.ErrUnknownCase) {
		invalid(w, "unknown_case", msgCaseNotFound)
		return
	}
	if err != nil {
		s.internal(w, r, "rating", err)
		return
	}
	writeJSON(w, http.StatusCreated, struct{}{})
}

// --- POST /api/v1/waitlist

type waitlistRequest struct {
	VisitorID    *string `json:"visitor_id"`
	Email        string  `json:"email"`
	Source       string  `json:"source"`
	PriceVariant *string `json:"price_variant"`
	Consent      bool    `json:"consent"`
}

var (
	priceVariantRe = regexp.MustCompile(`^[a-z0-9_-]{1,16}$`)
	waitlistDoors  = map[string]bool{"room": true, "pro": true}
)

// validEmail checks the shape of an address that is already trimmed and lower-cased: local@label.label…, with
// letters (any script, so почта.рф works), digits and hyphens in the domain, and a top-level label of at least
// two letters (or punycode xn--…).
func validEmail(e string) bool {
	if len(e) > 254 {
		return false
	}
	at := strings.LastIndexByte(e, '@')
	if at < 1 || at > 64 {
		return false
	}
	local, domain := e[:at], e[at+1:]
	if strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return false
	}
	for _, c := range local {
		if c <= ' ' || c == 0x7f || strings.ContainsRune(`@<>()[]\,;:"`, c) || unicode.IsSpace(c) || unicode.IsControl(c) {
			return false
		}
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if l == "" || utf8.RuneCountInString(l) > 63 || strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") {
			return false
		}
		for _, c := range l {
			if !(unicode.IsLetter(c) || unicode.IsDigit(c) || c == '-') {
				return false
			}
		}
	}
	tld := labels[len(labels)-1]
	if strings.HasPrefix(tld, "xn--") {
		return true
	}
	if utf8.RuneCountInString(tld) < 2 {
		return false
	}
	for _, c := range tld {
		if !unicode.IsLetter(c) {
			return false
		}
	}
	return true
}

func (s *Server) handleWaitlist(w http.ResponseWriter, r *http.Request) {
	var req waitlistRequest
	if !decodeJSON(w, r, &req, maxBody, false) {
		return
	}
	if !req.Consent {
		invalid(w, "consent_required", "Нужно согласие на обработку персональных данных.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !validEmail(email) {
		invalid(w, "invalid_email", "Похоже, в адресе почты ошибка.")
		return
	}
	if !waitlistDoors[req.Source] {
		invalid(w, "invalid_source", "Неизвестный источник: ожидается room или pro.")
		return
	}
	e := store.WaitlistEntry{Email: email, Source: req.Source, ConsentAt: s.now().UTC()}
	if req.VisitorID != nil && *req.VisitorID != "" {
		v, ok := ids.NormalizeUUID(*req.VisitorID)
		if !ok {
			invalid(w, "invalid_visitor_id", msgVisitorID)
			return
		}
		e.VisitorID = &v
	}
	if req.PriceVariant != nil && *req.PriceVariant != "" {
		if !priceVariantRe.MatchString(*req.PriceVariant) {
			invalid(w, "invalid_price_variant", "Неверный вариант цены.")
			return
		}
		e.PriceVariant = req.PriceVariant
	}
	if err := s.store.AddToWaitlist(r.Context(), e); err != nil {
		s.internal(w, r, "waitlist", err)
		return
	}
	writeJSON(w, http.StatusCreated, struct{}{}) // the same answer whether or not the address was there
}

func (s *Server) internal(w http.ResponseWriter, r *http.Request, what string, err error) {
	if errors.Is(r.Context().Err(), context.Canceled) {
		s.log.Info("client went away", "what", what, "path", r.URL.Path)
	} else {
		s.log.Error("request failed", "what", what, "path", r.URL.Path, "err", err)
	}
	writeError(w, http.StatusInternalServerError, codeInternal, msgInternal)
}
