// internal/web/triage_test.go
package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
)

func triagePOST(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Host = srv.cfg.Bind
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func triageGET(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = srv.cfg.Bind
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func writeInboxTestNote(t *testing.T, vaultDir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(vaultDir, "00-Inbox", name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// DECIDE(new in v2, row #150): propose returns 202 with a pending
// partial carrying the poll target.
func TestHandleTriageProposeReturns202(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	writeInboxTestNote(t, vaultDir, "First.md", "---\ntype: inbox\n---\nbody\n")
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"high","rationale":"r"}`}, nil)
	rec := triagePOST(t, srv, "/triage/"+url.PathEscape("First.md")+"/propose")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "status") {
		t.Errorf("expected the pending partial to carry a status poll target: %s", rec.Body.String())
	}
}

// DECIDE(new in v2, row #150): polling status eventually returns the
// proposal (with a diff) once the job completes.
func TestHandleTriageStatusEventuallyDone(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	writeInboxTestNote(t, vaultDir, "First.md", "---\ntype: inbox\n---\nbody\n")
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"high","rationale":"r"}`}, nil)
	triagePOST(t, srv, "/triage/First.md/propose")

	deadline := time.Now().Add(2 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		rec := triageGET(t, srv, "/triage/First.md/status")
		body = rec.Body.String()
		if strings.Contains(body, "02-Areas/X.md") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("status never showed the proposal; last body: %s", body)
}

// CONTRACT(#151): the proposal partial contains a diff view.
func TestHandleTriageStatusIncludesDiff(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	writeInboxTestNote(t, vaultDir, "First.md", "---\ntype: inbox\ncreated: 2026-05-14\n---\noriginal body\n")
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{response: `{"to":"02-Areas/X.md","new_title":"X","frontmatter_patch":{"type":"note"},"confidence":"high","rationale":"r"}`}, nil)
	triagePOST(t, srv, "/triage/First.md/propose")
	deadline := time.Now().Add(2 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		rec := triageGET(t, srv, "/triage/First.md/status")
		body = rec.Body.String()
		if strings.Contains(body, "type: note") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(body, "type: note") {
		t.Fatalf("expected the diff to show the frontmatter change; got: %s", body)
	}
}

// CONTRACT: approve applies the proposal and writes the note.
func TestHandleTriageApproveWritesNote(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	writeInboxTestNote(t, vaultDir, "First.md", "---\ntype: inbox\n---\nbody\n")
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"high","rationale":"r"}`}, nil)
	triagePOST(t, srv, "/triage/First.md/propose")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec := triageGET(t, srv, "/triage/First.md/status")
		if strings.Contains(rec.Body.String(), "02-Areas/X.md") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	rec := triagePOST(t, srv, "/triage/First.md/approve")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(vaultDir, "02-Areas", "X.md")); err != nil {
		t.Errorf("note not written: %v", err)
	}
}

// BUG(fixed)(#5): the web approve path enforces the same body_patch
// rejection as the CLI — never trust the LLM's own output for a gate.
func TestHandleTriageApproveRejectsBodyPatch(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	writeInboxTestNote(t, vaultDir, "First.md", "---\ntype: inbox\n---\noriginal\n")
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{response: `{"to":"02-Areas/X.md","new_title":"X","body_patch":"INJECTED","confidence":"high","rationale":"r"}`}, nil)
	triagePOST(t, srv, "/triage/First.md/propose")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec := triageGET(t, srv, "/triage/First.md/status")
		if strings.Contains(rec.Body.String(), "02-Areas") || strings.Contains(rec.Body.String(), "error") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	rec := triagePOST(t, srv, "/triage/First.md/approve")
	if _, err := os.Stat(filepath.Join(vaultDir, "02-Areas", "X.md")); !os.IsNotExist(err) {
		t.Fatal("body_patch-carrying proposal must never be written")
	}
	if content, _ := os.ReadFile(filepath.Join(vaultDir, "00-Inbox", "First.md")); strings.Contains(string(content), "INJECTED") {
		t.Fatal("injected content must never reach disk")
	}
	_ = rec
}

// DECIDE(new in v2, row #150): skip discards the pending job for that
// note without writing anything.
func TestHandleTriageSkipClearsJob(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	writeInboxTestNote(t, vaultDir, "First.md", "---\ntype: inbox\n---\nbody\n")
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"high","rationale":"r"}`}, nil)
	triagePOST(t, srv, "/triage/First.md/propose")
	rec := triagePOST(t, srv, "/triage/First.md/skip")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	statusRec := triageGET(t, srv, "/triage/First.md/status")
	if statusRec.Code != http.StatusNotFound {
		t.Errorf("expected 404 after skip cleared the job, got %d", statusRec.Code)
	}
}

// DECIDE(new in v2, row #147): an auth-classified failure renders the
// specific message, not a generic error.
func TestHandleTriageStatusAuthFailure(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	writeInboxTestNote(t, vaultDir, "First.md", "---\ntype: inbox\n---\nbody\n")
	authErr := fmt.Errorf("%w: not logged in", llm.ErrAuth)
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{err: authErr}, nil)
	triagePOST(t, srv, "/triage/First.md/propose")
	deadline := time.Now().Add(2 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		rec := triageGET(t, srv, "/triage/First.md/status")
		body = rec.Body.String()
		if strings.Contains(body, "auth expired") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected an auth-specific message, got: %s", body)
}

// BUG(fixed)(row #140-class): a {note} path parameter containing a
// traversal sequence is rejected before it ever reaches vault.ReadNote.
func TestHandleTriageProposeRejectsTraversalNote(t *testing.T) {
	_, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{}, nil)
	rec := triagePOST(t, srv, "/triage/"+url.PathEscape("../../etc/passwd")+"/propose")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a traversal note param", rec.Code)
	}
}

// DECIDE(new in v2, row #149): the health endpoint reports ok on success.
func TestHandleTriageHealthOK(t *testing.T) {
	_, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{}, nil)
	rec := triageGET(t, srv, "/triage-health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// DECIDE(new in v2, row #149, #147): the health endpoint reports the
// auth-specific message and a 503, not a 500.
func TestHandleTriageHealthAuthFailure(t *testing.T) {
	_, cfg := newTestVault(t)
	authErr := fmt.Errorf("%w: not logged in", llm.ErrAuth)
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{healthErr: authErr}, nil)
	rec := triageGET(t, srv, "/triage-health")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "auth expired") {
		t.Errorf("body = %q", rec.Body.String())
	}
}
