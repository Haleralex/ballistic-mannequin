package httpapi

import (
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// Top-level directories of the repository that are never served even when STATIC_DIR is the repo root.
var deniedTop = map[string]bool{"server": true, "deploy": true, "node_modules": true}

// Long-lived assets: the 3.9 MB model and media. Everything else that is not HTML gets a short max-age because
// the file names are not fingerprinted.
var longCache = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true, ".svg": true, ".ico": true,
	".mp4": true, ".webm": true, ".woff": true, ".woff2": true, ".glb": true, ".gltf": true,
}

type staticHandler struct {
	root     *os.Root // confines every open to STATIC_DIR, symlinks included
	files    http.Handler
	notFound http.HandlerFunc
}

func newStatic(dir string, log *slog.Logger, notFound http.HandlerFunc) *staticHandler {
	h := &staticHandler{notFound: notFound}
	root, err := os.OpenRoot(dir)
	if err != nil {
		log.Warn("static directory unavailable; only the API is served", "dir", dir, "err", err)
		return h
	}
	h.root = root
	h.files = http.FileServerFS(root.FS())
	return h
}

// exists reports whether a regular file is present under STATIC_DIR (name relative, slash-separated).
func (h *staticHandler) exists(name string) bool {
	if h.root == nil {
		return false
	}
	fi, err := h.root.Stat(name)
	return err == nil && fi.Mode().IsRegular()
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Метод не поддерживается.", http.StatusMethodNotAllowed)
		return
	}
	if h.root == nil || !strings.HasPrefix(r.URL.Path, "/") {
		h.notFound(w, r)
		return
	}
	clean := path.Clean(r.URL.Path)
	segs := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	for _, seg := range segs {
		if strings.HasPrefix(seg, ".") { // dotfiles and dot-directories: .env, .git, .devcontainer
			h.notFound(w, r)
			return
		}
	}
	if deniedTop[segs[0]] {
		h.notFound(w, r)
		return
	}
	name := strings.TrimPrefix(clean, "/")
	if name == "" {
		name = "."
	}
	fi, err := h.root.Stat(name)
	if err != nil {
		h.notFound(w, r)
		return
	}
	hdr := w.Header()
	switch {
	case fi.IsDir():
		if _, err := h.root.Stat(path.Join(name, "index.html")); err != nil { // no directory listings
			h.notFound(w, r)
			return
		}
		hdr.Set("Cache-Control", "no-cache")
	case strings.HasSuffix(name, ".html"):
		hdr.Set("Cache-Control", "no-cache")
	case name == "xbot.js" || longCache[strings.ToLower(path.Ext(name))]:
		hdr.Set("Cache-Control", "public, max-age=86400")
	default:
		hdr.Set("Cache-Control", "public, max-age=300")
	}
	// Big files on slow connections need longer than the server-wide write timeout.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Minute))
	h.files.ServeHTTP(w, r)
}
