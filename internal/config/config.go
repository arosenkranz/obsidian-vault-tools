// Package config loads ov settings with precedence flag > env > file > default.
// Flag overrides are applied by the cmd layer after Load.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

type Config struct {
	VaultDir     string   `toml:"vault_dir"`
	Inbox        string   `toml:"inbox"`
	Projects     string   `toml:"projects"`
	Areas        string   `toml:"areas"`
	Resources    string   `toml:"resources"`
	Archive      string   `toml:"archive"`
	Meta         string   `toml:"meta"`
	LLMCmd       string   `toml:"llm_cmd"`
	Model        string   `toml:"model"`
	DocsHost     string   `toml:"docs_host"`
	DocsPath     string   `toml:"docs_path"`
	DocsURL      string   `toml:"docs_url"`
	StaleExclude []string `toml:"stale_exclude"`
}

// envFields: the OV_* env contract (names frozen; design spec §contract) —
// field accessors keyed by env var name, in declaration order.
var envFields = []struct {
	env string
	get func(c *Config) *string
}{
	{"OV_VAULT_DIR", func(c *Config) *string { return &c.VaultDir }},
	{"OV_INBOX", func(c *Config) *string { return &c.Inbox }},
	{"OV_PROJECTS", func(c *Config) *string { return &c.Projects }},
	{"OV_AREAS", func(c *Config) *string { return &c.Areas }},
	{"OV_RESOURCES", func(c *Config) *string { return &c.Resources }},
	{"OV_ARCHIVE", func(c *Config) *string { return &c.Archive }},
	{"OV_META", func(c *Config) *string { return &c.Meta }},
	{"OV_LLM_CMD", func(c *Config) *string { return &c.LLMCmd }},
	{"OV_MODEL", func(c *Config) *string { return &c.Model }},
	{"OV_DOCS_HOST", func(c *Config) *string { return &c.DocsHost }},
	{"OV_DOCS_PATH", func(c *Config) *string { return &c.DocsPath }},
	{"OV_DOCS_URL", func(c *Config) *string { return &c.DocsURL }},
}

// Defaults mirror triage_llm.py CONFIG_DEFAULTS + vault.sh OV_DOCS_PATH.
var defaults = map[string]string{
	"OV_INBOX":     "00-Inbox",
	"OV_PROJECTS":  "01-Projects",
	"OV_AREAS":     "02-Areas",
	"OV_RESOURCES": "03-Resources",
	"OV_ARCHIVE":   "04-Archive",
	"OV_META":      "99-Meta",
	"OV_LLM_CMD":   "claude --print",
	"OV_DOCS_PATH": "/var/www/docs",
}

func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "ov", "config.toml")
}

func Load(explicitPath string) (*Config, error) {
	path := explicitPath
	if path == "" {
		path = os.Getenv("OV_CONFIG")
	}
	if path == "" {
		path = DefaultPath()
	}

	cfg := &Config{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := toml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	case errors.Is(err, os.ErrNotExist):
		// Missing file is fine: env + defaults may fully configure.
	default:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	for _, f := range envFields {
		if v := os.Getenv(f.env); v != "" {
			*f.get(cfg) = v
		}
	}
	for _, f := range envFields {
		if *f.get(cfg) == "" {
			if d, ok := defaults[f.env]; ok {
				*f.get(cfg) = d
			}
		}
	}

	if cfg.StaleExclude == nil {
		cfg.StaleExclude = []string{"Daily Notes"}
	}
	cfg.VaultDir = expandPath(cfg.VaultDir)
	return cfg, nil
}

// expandPath expands a leading ~ and any $VAR/${VAR} references.
// One rule replacing bash eval + python expandvars.
func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return os.ExpandEnv(p)
}

// ExpandPath applies the same ~/$VAR expansion Load gives VaultDir. For
// cmd-layer flag values (e.g. --vault), which bypass Load's expansion.
func ExpandPath(p string) string { return expandPath(p) }

func (c *Config) Validate() error {
	if c.VaultDir == "" {
		return errors.New("OV_VAULT_DIR not set: create " + DefaultPath() + " (ov init) or export OV_VAULT_DIR")
	}
	info, err := os.Stat(c.VaultDir)
	if err != nil {
		return fmt.Errorf("vault dir %s: %w", c.VaultDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("vault dir %s: not a directory", c.VaultDir)
	}
	return nil
}

func (c *Config) ParaRoots() []string {
	return []string{c.Projects, c.Areas, c.Resources, c.Archive}
}
