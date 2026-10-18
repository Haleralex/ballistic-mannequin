package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"

	"github.com/Haleralex/ballistic-mannequin/server/internal/ids"
	"github.com/Haleralex/ballistic-mannequin/server/internal/store"
)

const siteName = "Баллистический манекен"

var weaponNames = map[string]string{
	"pistol": "пистолет",
	"rifle":  "винтовка",
	"buck":   "дробовик, картечь",
	"slug":   "дробовик, пуля",
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type sharePage struct {
	Title       string // og:title and <title>
	Description string
	Image       string // absolute URL of /assets/og/<kind>.png
	URL         string // absolute URL of this page
	Target      string // where the browser goes: /?case=<id>
	SiteName    string
}

type caseForPreview struct {
	Steps int64 `json:"steps"`
	Meta  struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Kind        string   `json:"kind"`
		Weapon      string   `json:"weapon"`
		Zones       []string `json:"zones"`
		Organs      []string `json:"organs"`
	} `json:"meta"`
}

// handleShare serves GET /r/{id}: a small HTML page with Open Graph tags for link previews that sends the
// browser on to the simulator in replay mode. It is a 200 page, not an HTTP redirect, so that preview bots read
// the tags.
func (s *Server) handleShare(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !ids.IsCaseID(id) {
		s.caseNotFoundPage(w, r)
		return
	}
	c, err := s.store.GetCase(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.caseNotFoundPage(w, r)
		return
	}
	if err != nil {
		s.log.Error("share page", "id", id, "err", err)
		s.errorPage(w, http.StatusInternalServerError, "Ошибка сервера", msgInternal)
		return
	}
	var cp caseForPreview
	_ = json.Unmarshal(c.Data, &cp) // validated when stored; missing fields fall back to defaults
	base := s.scheme(r) + "://" + r.Host
	page := sharePage{
		Title:       previewTitle(cp),
		Description: previewDescription(cp),
		Image:       base + "/assets/og/" + s.ogImage(ogKind(cp.Meta.Kind, c.Kind)) + ".png",
		URL:         base + "/r/" + id,
		Target:      "/?case=" + id,
		SiteName:    siteName,
	}
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, "share.html", page); err != nil {
		s.log.Error("share template", "err", err)
		s.errorPage(w, http.StatusInternalServerError, "Ошибка сервера", msgInternal)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(buf.Bytes())
}

// ogImage falls back to the generic picture when there is none for this kind yet: a preview with a broken image
// looks worse than a generic one.
func (s *Server) ogImage(kind string) string {
	if kind != "default" && !s.static.exists("assets/og/"+kind+".png") {
		return "default"
	}
	return kind
}

func ogKind(metaKind string, column *string) string {
	if slugRe.MatchString(metaKind) {
		return metaKind
	}
	if column != nil && slugRe.MatchString(*column) {
		return *column
	}
	return "default"
}

func previewTitle(c caseForPreview) string {
	t := strings.TrimSpace(c.Meta.Title)
	if t == "" {
		t = "Реакция на ранение"
	}
	if sec := int64(math.Round(float64(c.Steps) / 120)); sec > 0 {
		t += fmt.Sprintf(" — реакция за %d с", sec)
	}
	return t
}

func previewDescription(c caseForPreview) string {
	if d := strings.TrimSpace(c.Meta.Description); d != "" {
		return d
	}
	var parts []string
	if w, ok := weaponNames[c.Meta.Weapon]; ok {
		parts = append(parts, w)
	}
	parts = append(parts, c.Meta.Zones...)
	parts = append(parts, c.Meta.Organs...)
	const about = "Физическая симуляция реакции тела на ранение: справочник для актёров и режиссёров."
	if len(parts) == 0 {
		return about
	}
	r := []rune(strings.Join(parts, " · "))
	return strings.ToUpper(string(r[0])) + string(r[1:]) + ". " + about
}

type errorPageData struct {
	Title, Message string
	Home           bool
}

func (s *Server) errorPage(w http.ResponseWriter, status int, title, msg string) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, "error.html", errorPageData{Title: title, Message: msg, Home: true}); err != nil {
		http.Error(w, msg, status)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) notFoundPage(w http.ResponseWriter, _ *http.Request) {
	s.errorPage(w, http.StatusNotFound, "Страница не найдена", "Такой страницы нет.")
}

func (s *Server) caseNotFoundPage(w http.ResponseWriter, _ *http.Request) {
	s.errorPage(w, http.StatusNotFound, "Реакция не найдена", msgCaseNotFound)
}
