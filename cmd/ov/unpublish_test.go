// cmd/ov/unpublish_test.go
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
)

type fakeRemover struct {
	removed []string
	err     error
}

func (f *fakeRemover) Remove(ctx context.Context, host, remotePath, basename string) error {
	if f.err != nil {
		return f.err
	}
	f.removed = append(f.removed, basename)
	return nil
}

type fakeLister struct {
	files []string
	err   error
}

func (f *fakeLister) List(ctx context.Context, host, remotePath string) ([]string, error) {
	return f.files, f.err
}

func testConfigWithDocsHost(t *testing.T) *config.Config {
	t.Helper()
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	return cfg
}

// CONTRACT(row #68): OV_DOCS_HOST unset -> error.
func TestRunUnpublishRequiresDocsHost(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	cfg.DocsHost = ""
	err := runUnpublish(cfg, []string{"note.html"}, bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, unpublishDeps{})
	if err == nil {
		t.Fatal("expected an error")
	}
}

// CONTRACT(row #76): direct-args mode removes each basename with NO
// confirmation — deps.confirm must never be called.
func TestRunUnpublishDirectArgsNoConfirmation(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	remover := &fakeRemover{}
	confirmCalled := false
	deps := unpublishDeps{
		remover: remover,
		confirm: func(string) (bool, error) { confirmCalled = true; return true, nil },
	}
	err := runUnpublish(cfg, []string{"/some/path/note.html", "other.html"}, bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if confirmCalled {
		t.Error("direct-args mode must never call confirm (row #76)")
	}
	if len(remover.removed) != 2 || remover.removed[0] != "note.html" || remover.removed[1] != "other.html" {
		t.Errorf("removed = %v", remover.removed)
	}
}

// CONTRACT(rows #77,#159): the no-args picker requires an explicit
// confirm before removal.
func TestRunUnpublishInteractiveRequiresConfirm(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	remover := &fakeRemover{}
	lister := &fakeLister{files: []string{"a.html", "b.html"}}
	deps := unpublishDeps{
		remover: remover,
		lister:  lister,
		confirm: func(string) (bool, error) { return false, nil }, // decline
	}
	err := runUnpublish(cfg, nil, bufio.NewReader(strings.NewReader("a\n")), &bytes.Buffer{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(remover.removed) != 0 {
		t.Errorf("expected no removal after a declined confirm, got %v", remover.removed)
	}
}

// CONTRACT(row #159): the interactive picker's numbered-selection path
// removes only the chosen file(s) after confirmation.
func TestRunUnpublishInteractiveNumberSelection(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	remover := &fakeRemover{}
	lister := &fakeLister{files: []string{"a.html", "b.html", "c.html"}}
	deps := unpublishDeps{
		remover: remover,
		lister:  lister,
		confirm: func(string) (bool, error) { return true, nil },
	}
	err := runUnpublish(cfg, nil, bufio.NewReader(strings.NewReader("2\n")), &bytes.Buffer{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(remover.removed) != 1 || remover.removed[0] != "b.html" {
		t.Errorf("removed = %v", remover.removed)
	}
}

// CONTRACT: the interactive picker's "a" choice selects every remote
// file.
func TestRunUnpublishInteractiveAllSelection(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	remover := &fakeRemover{}
	lister := &fakeLister{files: []string{"a.html", "b.html"}}
	deps := unpublishDeps{
		remover: remover,
		lister:  lister,
		confirm: func(string) (bool, error) { return true, nil },
	}
	err := runUnpublish(cfg, nil, bufio.NewReader(strings.NewReader("a\n")), &bytes.Buffer{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(remover.removed) != 2 {
		t.Errorf("removed = %v", remover.removed)
	}
}

// CONTRACT: the interactive picker's "q" choice (and EOF) cancels
// without removing anything.
func TestRunUnpublishInteractiveQuit(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	remover := &fakeRemover{}
	lister := &fakeLister{files: []string{"a.html"}}
	deps := unpublishDeps{remover: remover, lister: lister, confirm: func(string) (bool, error) { return true, nil }}
	err := runUnpublish(cfg, nil, bufio.NewReader(strings.NewReader("q\n")), &bytes.Buffer{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(remover.removed) != 0 {
		t.Error("expected nothing removed after quit")
	}
}

// No remote files -> a plain message, no picker prompt, no error.
func TestRunUnpublishInteractiveNoFiles(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	deps := unpublishDeps{remover: &fakeRemover{}, lister: &fakeLister{files: nil}}
	var errw bytes.Buffer
	err := runUnpublish(cfg, nil, bufio.NewReader(strings.NewReader("")), &errw, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errw.String(), "No files on docs server") {
		t.Errorf("errw = %q", errw.String())
	}
}

// A remover failure surfaces as an error.
func TestRunUnpublishRemoverFailurePropagates(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	deps := unpublishDeps{remover: &fakeRemover{err: errors.New("ssh failed")}}
	err := runUnpublish(cfg, []string{"note.html"}, bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, deps)
	if err == nil {
		t.Fatal("expected an error")
	}
}
