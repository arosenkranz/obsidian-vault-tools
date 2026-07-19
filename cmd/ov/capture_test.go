// cmd/ov/capture_test.go
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

type fakeFetcher struct{}

func (fakeFetcher) FetchTitle(ctx context.Context, url string) (string, error) {
	return "", errors.New("no network in tests")
}

// CONTRACT(#43,#44): positional args join into the body and win over stdin.
func TestCapturePositionalBody(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	deps := captureDeps{
		stdinPiped:  func() bool { t.Fatal("must not read stdin when a positional body is given"); return false },
		interactive: func() bool { return false },
		fetcher:     fakeFetcher{},
		now:         func() time.Time { return time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC) },
	}
	err = runCapture(cfg, captureFlags{title: "My Title", source: "cli"}, []string{"hello", "world"}, strings.NewReader(""), &out, &errBuf, deps)
	if err != nil {
		t.Fatalf("%v\n%s", err, errBuf.String())
	}
	if strings.TrimSpace(out.String()) != "00-Inbox/2026-07-15 0830 My Title.md" {
		t.Errorf("stdout = %q", out.String())
	}
	content, rerr := os.ReadFile(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0830 My Title.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(content), "hello world") {
		t.Errorf("body missing: %s", content)
	}
}

// CONTRACT(#10,#44): body from stdin when piped.
func TestCaptureStdinBody(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := captureDeps{
		stdinPiped:  func() bool { return true },
		interactive: func() bool { return false },
		fetcher:     fakeFetcher{},
		now:         func() time.Time { return time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC) },
	}
	err := runCapture(cfg, captureFlags{title: "Stdin Note", source: "cli"}, nil, strings.NewReader("from stdin"), &out, &errBuf, deps)
	if err != nil {
		t.Fatalf("%v\n%s", err, errBuf.String())
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0830 Stdin Note.md")); statErr != nil {
		t.Errorf("note missing: %v", statErr)
	}
}

// CONTRACT(#44): neither positional nor piped stdin -> error.
func TestCaptureNoBodyErrors(t *testing.T) {
	newVaultFixture(t)
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := captureDeps{stdinPiped: func() bool { return false }, interactive: func() bool { return false }}
	err := runCapture(cfg, captureFlags{}, nil, strings.NewReader(""), &out, &errBuf, deps)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(errBuf.String(), "No content provided") {
		t.Errorf("stderr = %q", errBuf.String())
	}
}

// BUG(fixed)(#136): the MOC picker never launches outside a real
// interactive terminal, even when MOCs exist.
func TestCaptureSkipsPickerNonInteractive(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.MkdirAll(filepath.Join(vaultDir, "03-Resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"), []byte("# MOC Music\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := captureDeps{
		stdinPiped:  func() bool { return true },
		interactive: func() bool { return false },
		pickMOC:     func([]vault.MOC) (string, error) { t.Fatal("picker must not launch outside a tty"); return "", nil },
		fetcher:     fakeFetcher{},
		now:         func() time.Time { return time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC) },
	}
	err := runCapture(cfg, captureFlags{title: "No Picker"}, nil, strings.NewReader("body"), &out, &errBuf, deps)
	if err != nil {
		t.Fatalf("%v\n%s", err, errBuf.String())
	}
}

// CONTRACT(#47,#54): --moc links the note and updates the MOC.
func TestCaptureMOCFlagLinksAndUpdatesMOC(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.MkdirAll(filepath.Join(vaultDir, "03-Resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"), []byte("# MOC Music\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := captureDeps{stdinPiped: func() bool { return true }, interactive: func() bool { return false }, fetcher: fakeFetcher{}, now: func() time.Time { return time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC) }}
	err := runCapture(cfg, captureFlags{title: "Linked Note", moc: "Music"}, nil, strings.NewReader("body text"), &out, &errBuf, deps)
	if err != nil {
		t.Fatalf("%v\n%s", err, errBuf.String())
	}
	moc, rerr := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(moc), "[[Linked Note]]") {
		t.Errorf("MOC not updated: %s", moc)
	}
	if !strings.Contains(errBuf.String(), "Added to [[MOC Music]]") {
		t.Errorf("stderr = %q", errBuf.String())
	}
}

// CONTRACT(#47): --moc requires exact resolution; a miss aborts.
func TestCaptureUnknownMOCErrors(t *testing.T) {
	newVaultFixture(t)
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := captureDeps{stdinPiped: func() bool { return true }, interactive: func() bool { return false }, fetcher: fakeFetcher{}, now: time.Now}
	err := runCapture(cfg, captureFlags{title: "X", moc: "Nonexistent"}, nil, strings.NewReader("body"), &out, &errBuf, deps)
	if err == nil {
		t.Fatal("expected an error for an unresolvable --moc")
	}
}

// CI smoke test named by the design spec (§CLI/TUI): the real binary, real
// piped stdin, verifying tty discipline end to end (row #123 applied to
// capture — stdout is exactly one machine-readable line).
func TestCaptureCLISmoke(t *testing.T) {
	newVaultFixture(t)
	bin := buildOV2(t)
	cmd := exec.Command(bin, "capture", "--title", "x")
	cmd.Stdin = strings.NewReader("hello from stdin\n")
	cmd.Env = os.Environ()
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("capture failed: %v\nstderr: %s", err, errOut.String())
	}
	stdout := strings.TrimRight(out.String(), "\n")
	if strings.Count(stdout, "\n") != 0 {
		t.Errorf("stdout must be exactly one line, got %q", stdout)
	}
	if !strings.HasPrefix(stdout, "00-Inbox/") || !strings.Contains(stdout, "x.md") {
		t.Errorf("stdout = %q, want a vault-relative inbox path for title x", stdout)
	}
}

func buildOV2(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ov")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build ov: %v\n%s", err, out)
	}
	return bin
}
