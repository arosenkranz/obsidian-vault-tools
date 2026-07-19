// cmd/ov/publish_test.go
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakePusher struct {
	gotLocal, gotHost, gotRemotePath string
	err                              error
	calls                            int
}

func (f *fakePusher) Push(ctx context.Context, localPath, host, remotePath string) error {
	f.calls++
	f.gotLocal, f.gotHost, f.gotRemotePath = localPath, host, remotePath
	return f.err
}

type fakePublishRunner struct {
	response string
	err      error
}

func (f *fakePublishRunner) Run(ctx context.Context, prompt string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

// CONTRACT(row #68): OV_DOCS_HOST unset -> a plain error (not
// errExitCode2).
func TestRunPublishRequiresDocsHost(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = ""
	target := filepath.Join(vaultDir, "note.html")
	os.WriteFile(target, []byte("<html></html>"), 0o644)

	err = runPublish(cfg, target, false, "", &bytes.Buffer{}, publishDeps{pusher: &fakePusher{}})
	if err == nil || errors.Is(err, errExitCode2) {
		t.Fatalf("err = %v, want a plain (non-exitCode2) error", err)
	}
}

// CONTRACT(row #70): a .md file without --llm refuses with a hint.
func TestRunPublishRefusesMarkdownWithoutLLM(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	target := filepath.Join(vaultDir, "note.md")
	os.WriteFile(target, []byte("# Note\n"), 0o644)

	err = runPublish(cfg, target, false, "", &bytes.Buffer{}, publishDeps{pusher: &fakePusher{}})
	if err == nil || !strings.Contains(err.Error(), "--llm") {
		t.Fatalf("err = %v, want a hint to use --llm", err)
	}
}

// CONTRACT(row #71): --llm on a non-.md file warns and publishes as-is
// (the runner must never be called).
func TestRunPublishLLMIgnoredOnNonMarkdown(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	target := filepath.Join(vaultDir, "already.html")
	os.WriteFile(target, []byte("<html></html>"), 0o644)

	pusher := &fakePusher{}
	var errBuf bytes.Buffer
	runner := &fakePublishRunner{response: "should never be used"}
	if err := runPublish(cfg, target, true, "", &errBuf, publishDeps{runner: runner, pusher: pusher}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "--llm ignored") {
		t.Errorf("errBuf = %q, want a warning", errBuf.String())
	}
	if pusher.gotLocal != target {
		t.Errorf("expected the original file to be pushed as-is, got %q", pusher.gotLocal)
	}
}

// CONTRACT(rows #73,#74): --llm on a .md file converts via the runner,
// extracts the HTML block, writes Published/<slug>.html (row #73's
// lowercase-hyphenated slug rule), and pushes THAT file.
func TestRunPublishLLMConvertsAndPublishes(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	cfg.DocsURL = "https://docs.example.com"
	target := filepath.Join(vaultDir, "My Great Note.md")
	os.WriteFile(target, []byte("# My Great Note\n\nBody.\n"), 0o644)

	pusher := &fakePusher{}
	runner := &fakePublishRunner{response: "chatter\n<html><body>Hi</body></html>\nmore chatter"}
	var errBuf bytes.Buffer
	if err := runPublish(cfg, target, true, "guidance", &errBuf, publishDeps{runner: runner, pusher: pusher}); err != nil {
		t.Fatal(err)
	}

	wantOut := filepath.Join(vaultDir, "Published", "my-great-note.html")
	got, rerr := os.ReadFile(wantOut)
	if rerr != nil {
		t.Fatalf("expected %s to exist: %v", wantOut, rerr)
	}
	if strings.TrimSpace(string(got)) != "<html><body>Hi</body></html>" {
		t.Errorf("published HTML = %q", got)
	}
	if pusher.gotLocal != wantOut {
		t.Errorf("expected the generated HTML to be pushed, got %q", pusher.gotLocal)
	}
	if !strings.Contains(errBuf.String(), "Live at: https://docs.example.com/my-great-note.html") {
		t.Errorf("errBuf = %q, want the live URL line", errBuf.String())
	}
}

// BUG(fixed)(row #160): republishing the SAME note overwrites the
// existing Published/<slug>.html atomically (conditional write) rather
// than refusing — v1 always overwrote on republish, and that behavior
// must survive the atomicity fix.
func TestRunPublishLLMRepublishOverwrites(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	target := filepath.Join(vaultDir, "note.md")
	os.WriteFile(target, []byte("# Note\n"), 0o644)

	pusher := &fakePusher{}
	for i, resp := range []string{"<html>version one</html>", "<html>version two</html>"} {
		runner := &fakePublishRunner{response: resp}
		if err := runPublish(cfg, target, true, "", &bytes.Buffer{}, publishDeps{runner: runner, pusher: pusher}); err != nil {
			t.Fatalf("publish #%d: %v", i, err)
		}
	}
	got, rerr := os.ReadFile(filepath.Join(vaultDir, "Published", "note.html"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(got), "version two") || strings.Contains(string(got), "version one") {
		t.Errorf("expected the republish to overwrite, got %q", got)
	}
}

// A runner failure surfaces as a plain error; the pusher must never be
// called.
func TestRunPublishLLMFailurePropagates(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	target := filepath.Join(vaultDir, "note.md")
	os.WriteFile(target, []byte("# Note\n"), 0o644)

	pusher := &fakePusher{}
	runner := &fakePublishRunner{err: errors.New("llm exploded")}
	err = runPublish(cfg, target, true, "", &bytes.Buffer{}, publishDeps{runner: runner, pusher: pusher})
	if err == nil {
		t.Fatal("expected an error")
	}
	if pusher.calls != 0 {
		t.Error("pusher must never be called when the LLM conversion fails")
	}
}
