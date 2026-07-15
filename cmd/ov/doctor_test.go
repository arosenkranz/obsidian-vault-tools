package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearOVEnv mirrors internal/config's test helper: real OV_* vars on the
// machine must not leak into these tests.
func clearOVEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"OV_CONFIG", "OV_VAULT_DIR", "OV_INBOX", "OV_PROJECTS", "OV_AREAS",
		"OV_RESOURCES", "OV_ARCHIVE", "OV_META", "OV_LLM_CMD", "OV_MODEL",
		"OV_DOCS_HOST", "OV_DOCS_PATH", "OV_DOCS_URL",
	} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func runDoctor(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newDoctorCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestDoctorHealthyVault(t *testing.T) {
	clearOVEnv(t)
	vault := t.TempDir()
	for _, d := range []string{"00-Inbox", "01-Projects", "02-Areas", "03-Resources", "04-Archive", "99-Meta"} {
		os.Mkdir(filepath.Join(vault, d), 0o755)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(cfgPath, []byte("vault_dir = \""+vault+"\"\nllm_cmd = \"true\"\n"), 0o644)
	t.Setenv("OV_CONFIG", cfgPath)

	out, err := runDoctor(t)
	if err != nil {
		t.Fatalf("healthy vault must pass: %v\n%s", err, out)
	}
	for _, want := range []string{"vault", "ok", "99-meta"} {
		if !strings.Contains(strings.ToLower(out), want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestDoctorMissingVaultFails(t *testing.T) {
	clearOVEnv(t)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(cfgPath, []byte("vault_dir = \"/nonexistent/vault/path\"\n"), 0o644)
	t.Setenv("OV_CONFIG", cfgPath)

	_, err := runDoctor(t)
	if err == nil {
		t.Fatal("missing vault dir must fail doctor")
	}
}

func TestDoctorVaultFlagOverrides(t *testing.T) {
	// Config points at a bad vault; --vault points at a good one. Flag wins.
	clearOVEnv(t)
	goodVault := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(cfgPath, []byte("vault_dir = \"/nonexistent\"\nllm_cmd = \"true\"\n"), 0o644)
	t.Setenv("OV_CONFIG", cfgPath)

	out, err := runDoctor(t, "--vault", goodVault)
	if err != nil {
		t.Fatalf("--vault override must win over config: %v\n%s", err, out)
	}
}

func TestDoctorVaultFlagExpands(t *testing.T) {
	// --vault values get the same ~/$VAR expansion as config values.
	clearOVEnv(t)
	goodVault := t.TempDir()
	t.Setenv("MY_TEST_VAULT", goodVault)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(cfgPath, []byte("vault_dir = \"/nonexistent\"\nllm_cmd = \"true\"\n"), 0o644)
	t.Setenv("OV_CONFIG", cfgPath)

	out, err := runDoctor(t, "--vault", "$MY_TEST_VAULT")
	if err != nil {
		t.Fatalf("--vault with $VAR must expand and pass: %v\n%s", err, out)
	}
	if !strings.Contains(out, goodVault) {
		t.Errorf("output should show the expanded vault path:\n%s", out)
	}
}

func TestDoctorMissingParaRootWarns(t *testing.T) {
	clearOVEnv(t)
	vault := t.TempDir() // no PARA dirs at all
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(cfgPath, []byte("vault_dir = \""+vault+"\"\nllm_cmd = \"true\"\n"), 0o644)
	t.Setenv("OV_CONFIG", cfgPath)

	out, err := runDoctor(t)
	if err != nil {
		t.Fatalf("missing PARA roots are warnings, not failures: %v", err)
	}
	if !strings.Contains(strings.ToLower(out), "warn") {
		t.Errorf("expected warnings for missing PARA roots:\n%s", out)
	}
}
