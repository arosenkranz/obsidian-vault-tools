package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runInit(t *testing.T) (string, error) {
	t.Helper()
	cmd := newInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(nil)
	err := cmd.Execute()
	return buf.String(), err
}

// First run creates the template; a second run must never touch an
// existing config (mirrors the Makefile config target's leave-alone rule).
func TestInitCreatesThenLeavesAlone(t *testing.T) {
	clearOVEnv(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("OV_CONFIG", path)

	out, err := runInit(t)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Created") {
		t.Errorf("first run should report creation:\n%s", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "vault_dir") {
		t.Errorf("template must contain vault_dir:\n%s", data)
	}

	// User edits the file; init must leave it byte-for-byte alone.
	edited := "vault_dir = \"/my/vault\"\n"
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	out2, err := runInit(t)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "already exists") {
		t.Errorf("second run should report already exists:\n%s", out2)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != edited {
		t.Errorf("second run modified the file:\ngot:  %q\nwant: %q", got, edited)
	}
}
