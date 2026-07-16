package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newVaultFixture creates a temp vault with the standard PARA dirs, points
// OV_CONFIG at a minimal TOML config, and returns the vault path.
func newVaultFixture(t *testing.T) string {
	t.Helper()
	clearOVEnv(t)
	// EvalSymlinks the temp dir up front (macOS resolves /var to
	// /private/var) so it matches vault.ContainPath's own symlink-resolved
	// return value — same convention as vault package's own
	// mustEvalSymlinks test helper (internal/vault/contain_test.go) and
	// internal/triage/validate_test.go's testConfig.
	vault, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"00-Inbox", "01-Projects", "02-Areas", "03-Resources", "04-Archive", "99-Meta"} {
		if err := os.MkdirAll(filepath.Join(vault, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("vault_dir = \""+vault+"\"\nllm_cmd = \"true\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OV_CONFIG", cfgPath)
	return vault
}

// addNote writes vault/rel with content and sets its mtime `days` days ago.
func addNote(t *testing.T, vault, rel, content string, days int) {
	t.Helper()
	p := filepath.Join(vault, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mod := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
}

// runCmd builds a fresh root command, runs it with args, and returns stdout
// and stderr captured SEPARATELY — the stdout/stderr discipline is the
// contract under test (behavior inventory row #123).
func runCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := newRootCmd()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}
