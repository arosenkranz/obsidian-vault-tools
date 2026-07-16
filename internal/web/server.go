// internal/web/server.go
package web

import (
	"context"
	"embed"
	"html/template"
	"net"
	"net/http"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/capture"
)

//go:embed assets/*.html assets/*.js
var assetsFS embed.FS

// Config is everything the web layer needs from the resolved ov config —
// deliberately a narrow struct, not internal/config.Config, so this package
// stays a thin frontend over the capture/vault/triage core verbs (design
// spec's "stateless verbs" principle).
type Config struct {
	VaultDir  string
	Inbox     string
	Resources string
	Bind      string // the configured bind address, for Host-header validation
	Projects  string
	Areas     string
	Archive   string
	AgentsMD  string // the vault's AGENTS.md content, read once at server construction
}

type Server struct {
	cfg     Config
	mux     *http.ServeMux
	fetcher capture.TitleFetcher
	tmpl    *template.Template
	now     func() time.Time
	runner  llmRunner
	jobs    *triageJobs
}

// New builds a Server around an already-constructed listener seam (the
// caller owns bind-guard decisions and listener construction — design spec
// §Web layer "Listener seam"). fetcher and runner are injected so tests
// never hit the network or spawn a real subprocess; nowFn defaults to
// time.Now when nil.
func New(cfg Config, fetcher capture.TitleFetcher, runner llmRunner, nowFn func() time.Time) *Server {
	if nowFn == nil {
		nowFn = time.Now
	}
	tmpl := template.Must(template.New("web").Funcs(template.FuncMap{
		"diffClass":  diffClass,
		"diffMarker": diffMarker,
	}).ParseFS(assetsFS, "assets/*.html"))
	s := &Server{cfg: cfg, fetcher: fetcher, runner: runner, tmpl: tmpl, now: nowFn, jobs: newTriageJobs()}
	s.mux = http.NewServeMux()
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleInbox)
	s.mux.HandleFunc("GET /capture", s.handleCaptureForm)
	s.mux.HandleFunc("POST /capture", s.handleCaptureSubmit)
	s.mux.HandleFunc("GET /assets/htmx.min.js", s.handleHTMX)
	s.mux.HandleFunc("POST /triage/{note}/propose", s.handleTriagePropose)
	s.mux.HandleFunc("GET /triage/{note}/status", s.handleTriageStatus)
	s.mux.HandleFunc("POST /triage/{note}/approve", s.handleTriageApprove)
	s.mux.HandleFunc("POST /triage/{note}/skip", s.handleTriageSkip)
	s.mux.HandleFunc("GET /triage-health", s.handleTriageHealth)
}

// Handler returns the fully wrapped handler (routes + hygiene middleware),
// for both real serving and httptest.
func (s *Server) Handler() http.Handler {
	return hygieneMiddleware(s.cfg.Bind)(s.mux)
}

// Serve runs the HTTP server over ln until ctx is done or the listener
// errors. Blocking; the caller runs it in its own goroutine or foreground.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	httpSrv := &http.Server{Handler: s.Handler()}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
