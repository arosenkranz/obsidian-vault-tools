// internal/web/triage.go
package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
	"github.com/arosenkranz/obsidian-vault-tools/internal/triage"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

const webTriageTimeout = 120 * time.Second

// llmRunner is the subset of *llm.Runner the web server needs:
// triage.Runner's Run method plus HealthCheck (row #149). Tests inject a
// fake satisfying both.
type llmRunner interface {
	triage.Runner
	HealthCheck(ctx context.Context) error
}

// noteParam validates the {note} URL path value: it must be a bare
// filename with no path separator — defense in depth against a crafted
// path segment ever reaching vault.ReadNote (mirrors row #140's
// FindMOCByName traversal defense). net/http's ServeMux decodes
// percent-encoded path segments before PathValue returns them, so this
// check runs against the fully-decoded value.
func noteParam(r *http.Request) (string, error) {
	note := r.PathValue("note")
	if note == "" || strings.ContainsAny(note, "/\\") {
		return "", errors.New("invalid note")
	}
	return note, nil
}

func (s *Server) triageConfig() triage.Config {
	return triage.Config{
		VaultDir:  s.cfg.VaultDir,
		Inbox:     s.cfg.Inbox,
		Projects:  s.cfg.Projects,
		Areas:     s.cfg.Areas,
		Resources: s.cfg.Resources,
		Archive:   s.cfg.Archive,
	}
}

func (s *Server) noteFor(note string) vault.Note {
	p := filepath.Join(s.cfg.VaultDir, s.cfg.Inbox, note)
	return vault.Note{Path: p, Rel: s.cfg.Inbox + "/" + note, Name: strings.TrimSuffix(note, ".md")}
}

func (s *Server) handleTriagePropose(w http.ResponseWriter, r *http.Request) {
	note, err := noteParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	n := s.noteFor(note)
	tcfg := s.triageConfig()
	agentsMD := s.cfg.AgentsMD
	runner := s.runner
	jobID := s.jobs.submit(context.Background(), note, func(ctx context.Context) (triage.Proposal, error) {
		ctx, cancel := context.WithTimeout(ctx, webTriageTimeout)
		defer cancel()
		return triage.Propose(ctx, tcfg, n, agentsMD, runner)
	})
	w.WriteHeader(http.StatusAccepted)
	s.renderTriagePending(w, note, jobID)
}

func (s *Server) handleTriageStatus(w http.ResponseWriter, r *http.Request) {
	note, err := noteParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	job, ok := s.jobs.current(note)
	if !ok {
		http.Error(w, "no pending triage job for this note", http.StatusNotFound)
		return
	}
	switch job.Status {
	case llm.StatusPending:
		s.renderTriagePending(w, note, job.ID)
	case llm.StatusFailed:
		s.renderTriageError(w, note, job.Err)
	default: // llm.StatusDone
		s.renderTriageProposal(w, note, job.Result)
	}
}

func (s *Server) handleTriageApprove(w http.ResponseWriter, r *http.Request) {
	note, err := noteParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	job, ok := s.jobs.current(note)
	if !ok || job.Status != llm.StatusDone {
		http.Error(w, "no completed proposal to approve", http.StatusConflict)
		return
	}
	res, applyErr := triage.Apply(s.triageConfig(), s.noteFor(note), job.Result, s.now(), false)
	s.jobs.clear(note)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if applyErr != nil {
		fmt.Fprintf(w, `<div class="error">Apply failed: %s</div>`, template.HTMLEscapeString(applyErr.Error()))
		return
	}
	fmt.Fprintf(w, `<div class="success">Filed &#8594; %s</div>`, template.HTMLEscapeString(res.Target))
}

func (s *Server) handleTriageSkip(w http.ResponseWriter, r *http.Request) {
	note, err := noteParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.jobs.clear(note)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if tplErr := s.tmpl.ExecuteTemplate(w, "triage-button.html", map[string]any{"NoteEscaped": url.PathEscape(note)}); tplErr != nil {
		http.Error(w, tplErr.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleTriageHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	err := s.runner.HealthCheck(ctx)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err != nil {
		if errors.Is(err, llm.ErrAuth) {
			http.Error(w, "LLM auth expired — run `claude login` on the Mac", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "LLM health check failed: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	fmt.Fprintln(w, "ok")
}

func (s *Server) renderTriagePending(w http.ResponseWriter, note, jobID string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.tmpl.ExecuteTemplate(w, "triage-pending.html", map[string]any{
		"NoteEscaped": url.PathEscape(note), "JobID": jobID,
	})
}

func (s *Server) renderTriageError(w http.ResponseWriter, note string, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	msg := err.Error()
	if errors.Is(err, llm.ErrAuth) {
		msg = "LLM auth expired — run `claude login` on the Mac"
	}
	s.tmpl.ExecuteTemplate(w, "triage-error.html", map[string]any{
		"NoteEscaped": url.PathEscape(note), "Error": msg,
	})
}

func (s *Server) renderTriageProposal(w http.ResponseWriter, note string, p triage.Proposal) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	n := s.noteFor(note)
	content, _, _ := vault.ReadNote(n.Path)
	preview, err := triage.Apply(s.triageConfig(), n, p, s.now(), true)
	if err != nil {
		s.renderTriageError(w, note, err)
		return
	}
	s.tmpl.ExecuteTemplate(w, "triage-proposal.html", map[string]any{
		"NoteEscaped": url.PathEscape(note),
		"Proposal":    p,
		"Diff":        triage.Diff(content, preview.Content),
		"Target":      preview.Target,
	})
}

func diffClass(op byte) string {
	switch op {
	case '+':
		return "diff-add"
	case '-':
		return "diff-del"
	default:
		return "diff-ctx"
	}
}

func diffMarker(op byte) string {
	return string(op)
}
