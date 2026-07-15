package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runConfigMigrate(t *testing.T, from string) (string, error) {
	t.Helper()
	cmd := newConfigCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"migrate", "--from", from})
	err := cmd.Execute()
	return buf.String(), err
}

func TestConfigMigrateLegacy(t *testing.T) {
	clearOVEnv(t)
	p := filepath.Join(t.TempDir(), "config")
	legacy := "OV_VAULT_DIR=\"$HOME/vault\"\nOV_LLM_CMD=\"claude --print\"\n"
	if err := os.WriteFile(p, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runConfigMigrate(t, p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `vault_dir = "$HOME/vault"`) {
		t.Errorf("migrated TOML missing vault_dir:\n%s", out)
	}
}

// Pointing migrate at an already-migrated (or any OV_*-free) file must be
// a clear error, not a bare header that looks like a successful migration.
func TestConfigMigrateRejectsZeroKeys(t *testing.T) {
	clearOVEnv(t)
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("vault_dir = \"/v\"\nllm_cmd = \"claude --print\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := runConfigMigrate(t, p)
	if err == nil || !strings.Contains(err.Error(), "no OV_* keys found") {
		t.Fatalf("want zero-keys error naming the path, got %v", err)
	}
	if !strings.Contains(err.Error(), p) {
		t.Errorf("error should name the offending path: %v", err)
	}
}
