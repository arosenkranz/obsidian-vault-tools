// internal/web/handlers.go
package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/capture"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

type inboxViewNote struct {
	Name string
	Age  int
}

func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request) {
	notes, err := vault.ListInbox(s.cfg.VaultDir, s.cfg.Inbox)
	if err != nil {
		s.renderInbox(w, nil)
		return
	}
	now := s.now()
	views := make([]inboxViewNote, 0, len(notes))
	for _, n := range notes {
		views = append(views, inboxViewNote{Name: n.Name, Age: vault.AgeDays(now, n.ModTime)})
	}
	s.renderInbox(w, views)
}

func (s *Server) renderInbox(w http.ResponseWriter, notes []inboxViewNote) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "inbox.html", map[string]any{"Notes": notes}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleCaptureForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "capture.html", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleCaptureSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var tags []string
	for _, t := range strings.Split(r.FormValue("tags"), ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	req := capture.Request{
		Body:       r.FormValue("body"),
		Title:      r.FormValue("title"),
		Tags:       tags,
		Source:     "web",
		MOCName:    r.FormValue("moc"),
		FetchTitle: r.FormValue("fetch_title") == "on", // explicit opt-in checkbox, row #135 — never automatic
	}
	ccfg := capture.CaptureConfig{VaultDir: s.cfg.VaultDir, Inbox: s.cfg.Inbox, Resources: s.cfg.Resources}
	result, err := capture.Capture(context.Background(), ccfg, req, s.fetcher, s.now())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		fmt.Fprintf(w, `<div class="error">Capture failed: %s</div>`, template.HTMLEscapeString(err.Error()))
		return
	}
	if err := s.tmpl.ExecuteTemplate(w, "capture-result.html", result); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleHTMX(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	http.ServeFileFS(w, r, assetsFS, "assets/htmx.min.js")
}
