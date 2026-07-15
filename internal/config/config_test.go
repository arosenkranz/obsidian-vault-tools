// internal/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Mined from triage_llm.py CONFIG_DEFAULTS (lines 51-60) and
// vault.sh:1182 (OV_DOCS_PATH default /var/www/docs).
func TestLoadDefaultsOnly(t *testing.T) {
	clearOVEnv(t)
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err != nil {
		t.Fatalf("missing file must not error: %v", err)
	}
	for _, row := range []struct{ name, got, want string }{
		{"Inbox", cfg.Inbox, "00-Inbox"},
		{"Projects", cfg.Projects, "01-Projects"},
		{"Areas", cfg.Areas, "02-Areas"},
		{"Resources", cfg.Resources, "03-Resources"},
		{"Archive", cfg.Archive, "04-Archive"},
		{"Meta", cfg.Meta, "99-Meta"},
		{"LLMCmd", cfg.LLMCmd, "claude --print"},
		{"DocsPath", cfg.DocsPath, "/var/www/docs"},
	} {
		if row.got != row.want {
			t.Errorf("%s default: got %q want %q", row.name, row.got, row.want)
		}
	}
	if cfg.VaultDir != "" || cfg.Model != "" || cfg.DocsHost != "" || cfg.DocsURL != "" {
		t.Errorf("keys without defaults must be empty: %+v", cfg)
	}
}

func TestLoadFileValues(t *testing.T) {
	clearOVEnv(t)
	p := writeTemp(t, `
vault_dir = "/tmp/v"
inbox = "Inbox"
llm_cmd = "pi --print -nc -nt --mode json"
docs_host = "docs.example"
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VaultDir != "/tmp/v" || cfg.Inbox != "Inbox" ||
		cfg.LLMCmd != "pi --print -nc -nt --mode json" || cfg.DocsHost != "docs.example" {
		t.Errorf("file values not applied: %+v", cfg)
	}
	if cfg.Projects != "01-Projects" {
		t.Errorf("defaults must fill unset keys, got %q", cfg.Projects)
	}
}

// CONTRACT: env wins over file (triage_llm.py load_config lines 79-82).
func TestEnvBeatsFile(t *testing.T) {
	clearOVEnv(t)
	p := writeTemp(t, `vault_dir = "/from-file"`+"\n")
	t.Setenv("OV_VAULT_DIR", "/from-env")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VaultDir != "/from-env" {
		t.Errorf("env must beat file: got %q", cfg.VaultDir)
	}
}

// DECIDE(one rule): bash used eval, python used expandvars. v2 rule:
// leading ~ and $VAR/${VAR} expand in VaultDir only.
func TestVaultDirExpansion(t *testing.T) {
	clearOVEnv(t)
	home, _ := os.UserHomeDir()
	for raw, want := range map[string]string{
		"~/Documents/vault":     filepath.Join(home, "Documents/vault"),
		"$HOME/Documents/vault": filepath.Join(home, "Documents/vault"),
	} {
		p := writeTemp(t, "vault_dir = \""+raw+"\"\n")
		cfg, err := Load(p)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.VaultDir != want {
			t.Errorf("expansion of %q: got %q want %q", raw, cfg.VaultDir, want)
		}
	}
}

// CONTRACT: $OV_CONFIG overrides the default path (both old readers honor it).
func TestOVConfigEnvSelectsFile(t *testing.T) {
	clearOVEnv(t)
	p := writeTemp(t, `vault_dir = "/via-ov-config"`+"\n")
	t.Setenv("OV_CONFIG", p)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VaultDir != "/via-ov-config" {
		t.Errorf("OV_CONFIG not honored: got %q", cfg.VaultDir)
	}
}

func TestUnknownTOMLKeysIgnored(t *testing.T) {
	clearOVEnv(t)
	p := writeTemp(t, "vault_dir = \"/v\"\nfuture_key = true\n")
	if _, err := Load(p); err != nil {
		t.Fatalf("unknown keys must not error: %v", err)
	}
}

func TestValidate(t *testing.T) {
	clearOVEnv(t)
	cfg := &Config{}
	if err := cfg.Validate(); err == nil {
		t.Error("empty VaultDir must fail Validate")
	}
	cfg.VaultDir = t.TempDir()
	if err := cfg.Validate(); err != nil {
		t.Errorf("existing dir must pass: %v", err)
	}
	cfg.VaultDir = filepath.Join(t.TempDir(), "missing")
	if err := cfg.Validate(); err == nil {
		t.Error("nonexistent VaultDir must fail Validate")
	}
}

func TestParaRoots(t *testing.T) {
	clearOVEnv(t)
	cfg, _ := Load(filepath.Join(t.TempDir(), "none.toml"))
	got := cfg.ParaRoots()
	want := []string{"01-Projects", "02-Areas", "03-Resources", "04-Archive"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ParaRoots = %v, want %v", got, want)
		}
	}
}

// --- helpers ---

var allOVEnv = []string{
	"OV_CONFIG", "OV_VAULT_DIR", "OV_INBOX", "OV_PROJECTS", "OV_AREAS",
	"OV_RESOURCES", "OV_ARCHIVE", "OV_META", "OV_LLM_CMD", "OV_MODEL",
	"OV_DOCS_HOST", "OV_DOCS_PATH", "OV_DOCS_URL",
}

func clearOVEnv(t *testing.T) {
	t.Helper()
	for _, k := range allOVEnv {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
