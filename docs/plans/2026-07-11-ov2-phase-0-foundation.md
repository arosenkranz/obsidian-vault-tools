# ov v2 Phase 0: Foundation + Mined Spec — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the Go module with `internal/config` and the vault core (lossless frontmatter, slugify, atomic+conditional writes), encode mined old-code behavior as table tests, and ship `ov2 doctor`/`ov2 init`/`ov2 config migrate` — zero vault write commands.

**Architecture:** Single Go module per the approved spec (`docs/plans/2026-07-11-ov-v2-go-rewrite-design.md`). This phase builds only foundation packages (`internal/config`, `internal/vault`, `internal/llm` decoder) plus a thin cobra shell. Old bash/python in `bin/` stays untouched and authoritative.

**Tech Stack:** Go ≥1.22 (1.24.5 installed), cobra, pelletier/go-toml/v2, golang.org/x/text (NFC normalization). No yaml, no viper, no chi.

## Global Constraints

- Module path: `github.com/arosenkranz/obsidian-vault-tools`
- Binary name during transition: `ov2`, built to `dist/ov2` (dist/ is already gitignored)
- Direct deps allowed this phase: `spf13/cobra`, `pelletier/go-toml/v2`, `golang.org/x/text`. Nothing else.
- `bin/vault.sh`, `bin/triage_llm.py`, `bin/moc_cleanup.py` are READ-ONLY — never modify them
- No ov2 command may write inside a vault this phase (`WriteNoteAtomic` exists as a library with tests against temp dirs only)
- Env var names stay `OV_*` exactly (compatibility contract)
- Commit style: imperative mood, no conventional-commit prefix (matches repo history)
- Every mined behavior test carries a comment: `// CONTRACT:`, `// BUG(fixed):`, or `// DECIDE(<resolution>):` referencing the behavior inventory
- Go tests use `t.Setenv` / `t.TempDir` — no global state leaks between tests

---

### Task 1: Module scaffold + cobra root + Makefile wiring

**Files:**
- Create: `go.mod` (via `go mod init`)
- Create: `cmd/ov/main.go`
- Create: `cmd/ov/root.go`
- Modify: `Makefile` (append targets; do not touch existing ones)

**Interfaces:**
- Consumes: nothing
- Produces: `newRootCmd() *cobra.Command` in package `main` — later tasks attach subcommands inside `newRootCmd` via `root.AddCommand(...)`. Build entry: `make build` → `dist/ov2`.

- [ ] **Step 1: Init module and fetch deps**

```bash
go mod init github.com/arosenkranz/obsidian-vault-tools
go get github.com/spf13/cobra@latest github.com/pelletier/go-toml/v2@latest golang.org/x/text@latest
```

- [ ] **Step 2: Write cmd/ov/main.go and cmd/ov/root.go**

```go
// cmd/ov/main.go
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ov2:", err)
		os.Exit(1)
	}
}
```

```go
// cmd/ov/root.go
package main

import "github.com/spf13/cobra"

var version = "0.0.1-phase0"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ov2",
		Short:         "Obsidian vault management (v2)",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// Subcommands are attached here by later tasks.
	return root
}
```

- [ ] **Step 3: Append Makefile targets**

Append to `Makefile` (keep every existing target unchanged):

```makefile
build:
	@mkdir -p dist
	go build -o dist/ov2 ./cmd/ov
	@echo "✓ Built dist/ov2"

gotest:
	go test ./...
```

Add `build gotest` to the existing `.PHONY` line.

- [ ] **Step 4: Verify**

Run: `make build && ./dist/ov2 --version`
Expected: prints `ov2 version 0.0.1-phase0`

Run: `make check` (existing target)
Expected: still passes — old scripts untouched.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum cmd/ Makefile
git commit -m "Scaffold Go module with cobra root and build targets"
```

---

### Task 2: internal/config — Load with precedence

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:

```go
package config

type Config struct {
	VaultDir  string `toml:"vault_dir"`
	Inbox     string `toml:"inbox"`
	Projects  string `toml:"projects"`
	Areas     string `toml:"areas"`
	Resources string `toml:"resources"`
	Archive   string `toml:"archive"`
	Meta      string `toml:"meta"`
	LLMCmd    string `toml:"llm_cmd"`
	Model     string `toml:"model"`
	DocsHost  string `toml:"docs_host"`
	DocsPath  string `toml:"docs_path"`
	DocsURL   string `toml:"docs_url"`
}

// Load reads TOML at explicitPath (or $OV_CONFIG, or ~/.config/ov/config.toml),
// applies OV_* env overrides, then defaults. A missing file is not an error.
func Load(explicitPath string) (*Config, error)

// Validate returns an error if VaultDir is unset or not a directory.
func (c *Config) Validate() error

// ParaRoots returns [Projects, Areas, Resources, Archive] folder names.
func (c *Config) ParaRoots() []string

func DefaultPath() string // ~/.config/ov/config.toml
```

- Precedence implemented here: env > file > default. Flag > env happens at the cmd layer: commands with a `--vault` flag assign `cfg.VaultDir = flagVal` after `Load` (Task 9 shows it).

- [ ] **Step 1: Write the failing tests**

```go
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
	want := map[string]string{
		cfg.Inbox: "00-Inbox", cfg.Projects: "01-Projects", cfg.Areas: "02-Areas",
		cfg.Resources: "03-Resources", cfg.Archive: "04-Archive", cfg.Meta: "99-Meta",
		cfg.LLMCmd: "claude --print", cfg.DocsPath: "/var/www/docs",
	}
	for got, expected := range want {
		if got != expected {
			t.Errorf("default mismatch: got %q want %q", got, expected)
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -v`
Expected: FAIL — package does not compile (`Load` undefined).

- [ ] **Step 3: Implement internal/config/config.go**

```go
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
	VaultDir  string `toml:"vault_dir"`
	Inbox     string `toml:"inbox"`
	Projects  string `toml:"projects"`
	Areas     string `toml:"areas"`
	Resources string `toml:"resources"`
	Archive   string `toml:"archive"`
	Meta      string `toml:"meta"`
	LLMCmd    string `toml:"llm_cmd"`
	Model     string `toml:"model"`
	DocsHost  string `toml:"docs_host"`
	DocsPath  string `toml:"docs_path"`
	DocsURL   string `toml:"docs_url"`
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

func (c *Config) Validate() error {
	if c.VaultDir == "" {
		return errors.New("OV_VAULT_DIR not set: create " + DefaultPath() + " (ov2 init) or export OV_VAULT_DIR")
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS (all 8 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "Add config package: TOML load, env>file>default precedence"
```

---

### Task 3: internal/config — legacy migrate + `ov2 config migrate`

**Files:**
- Create: `internal/config/migrate.go`
- Create: `cmd/ov/config.go`
- Modify: `cmd/ov/root.go` (attach subcommand)
- Test: `internal/config/migrate_test.go`

**Interfaces:**
- Consumes: nothing from other tasks
- Produces:

```go
// ParseLegacy extracts OV_* keys from a bash-style config. Values verbatim
// (no expansion — Load handles ~/$VAR at read time).
func ParseLegacy(text string) map[string]string

// RenderTOML renders known OV_* keys as TOML in envFields order,
// skipping empty values and unknown keys.
func RenderTOML(kv map[string]string) string
```

- CLI: `ov2 config migrate [--from PATH]` prints TOML to stdout (user redirects to `~/.config/ov/config.toml`). Default `--from`: `$OV_CONFIG` or `~/.config/ov/config` (the OLD default, no `.toml`).

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/migrate_test.go
package config

import (
	"strings"
	"testing"
)

// The regex contract is mined from triage_llm.py _CONFIG_LINE_RE (line 62):
// optional whitespace, OV_ key, =, optional quotes, value, optional # comment.
func TestParseLegacy(t *testing.T) {
	input := `
# Example config for the ov CLI.
OV_VAULT_DIR="$HOME/Documents/main-vault"
OV_INBOX="00-Inbox"
  OV_MODEL = ""
OV_LLM_CMD="claude --print"   # trailing comment
NOT_OURS="skip me"
OV_DOCS_HOST=docs.example
`
	got := ParseLegacy(input)
	want := map[string]string{
		"OV_VAULT_DIR": "$HOME/Documents/main-vault",
		"OV_INBOX":     "00-Inbox",
		"OV_MODEL":     "",
		"OV_LLM_CMD":   "claude --print",
		"OV_DOCS_HOST": "docs.example",
	}
	for k, v := range want {
		gv, ok := got[k]
		if !ok || gv != v {
			t.Errorf("%s: got %q (present=%v), want %q", k, gv, ok, v)
		}
	}
	if _, ok := got["NOT_OURS"]; ok {
		t.Error("non-OV_ keys must be skipped")
	}
}

func TestRenderTOML(t *testing.T) {
	kv := map[string]string{
		"OV_VAULT_DIR": "$HOME/Documents/main-vault",
		"OV_LLM_CMD":   "claude --print",
		"OV_MODEL":     "", // empty: skipped
		"OV_UNKNOWN":   "x", // unknown: skipped
	}
	out := RenderTOML(kv)
	if !strings.Contains(out, `vault_dir = "$HOME/Documents/main-vault"`) {
		t.Errorf("vault_dir line missing:\n%s", out)
	}
	if !strings.Contains(out, `llm_cmd = "claude --print"`) {
		t.Errorf("llm_cmd line missing:\n%s", out)
	}
	if strings.Contains(out, "model") || strings.Contains(out, "unknown") {
		t.Errorf("empty/unknown keys must be skipped:\n%s", out)
	}
	// Round-trip: rendered TOML must parse back via Load's parser.
	var cfg Config
	if err := tomlUnmarshalForTest([]byte(out), &cfg); err != nil {
		t.Fatalf("rendered TOML does not parse: %v\n%s", err, out)
	}
	if cfg.VaultDir != "$HOME/Documents/main-vault" {
		t.Errorf("round-trip vault_dir = %q", cfg.VaultDir)
	}
}
```

Add this helper at the bottom of `migrate_test.go`:

```go
import toml "github.com/pelletier/go-toml/v2" // add to imports

func tomlUnmarshalForTest(b []byte, v any) error { return toml.Unmarshal(b, v) }
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run 'Legacy|RenderTOML' -v`
Expected: FAIL — `ParseLegacy` undefined.

- [ ] **Step 3: Implement migrate.go and the subcommand**

```go
// internal/config/migrate.go
package config

import (
	"fmt"
	"regexp"
	"strings"
)

// legacyLineRe is a 1:1 port of triage_llm.py _CONFIG_LINE_RE.
var legacyLineRe = regexp.MustCompile(`^\s*(OV_[A-Z_]+)\s*=\s*"?([^"#]*?)"?\s*(?:#.*)?$`)

var envToTOML = map[string]string{
	"OV_VAULT_DIR": "vault_dir", "OV_INBOX": "inbox", "OV_PROJECTS": "projects",
	"OV_AREAS": "areas", "OV_RESOURCES": "resources", "OV_ARCHIVE": "archive",
	"OV_META": "meta", "OV_LLM_CMD": "llm_cmd", "OV_MODEL": "model",
	"OV_DOCS_HOST": "docs_host", "OV_DOCS_PATH": "docs_path", "OV_DOCS_URL": "docs_url",
}

func ParseLegacy(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		m := legacyLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

func RenderTOML(kv map[string]string) string {
	var b strings.Builder
	b.WriteString("# ov v2 config — migrated from bash-style config\n")
	for _, f := range envFields {
		v, ok := kv[f.env]
		if !ok || v == "" {
			continue
		}
		key := envToTOML[f.env]
		fmt.Fprintf(&b, "%s = %q\n", key, v)
	}
	return b.String()
}
```

```go
// cmd/ov/config.go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Config utilities"}

	var from string
	migrate := &cobra.Command{
		Use:   "migrate",
		Short: "Print TOML converted from the old bash-style config",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := from
			if path == "" {
				path = os.Getenv("OV_CONFIG")
			}
			if path == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				path = filepath.Join(home, ".config", "ov", "config") // OLD default
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read old config: %w", err)
			}
			fmt.Fprint(cmd.OutOrStdout(), config.RenderTOML(config.ParseLegacy(string(data))))
			return nil
		},
	}
	migrate.Flags().StringVar(&from, "from", "", "path to the old bash-style config")
	cmd.AddCommand(migrate)
	return cmd
}
```

In `cmd/ov/root.go`, before `return root`, add:

```go
	root.AddCommand(newConfigCmd())
```

- [ ] **Step 4: Run tests and smoke the command**

Run: `go test ./internal/config/ -v`
Expected: PASS.

Run: `go build -o dist/ov2 ./cmd/ov && ./dist/ov2 config migrate --from examples/ov.config.example`
Expected: TOML on stdout including `vault_dir = "$HOME/Documents/main-vault"` and `llm_cmd = "claude --print"`; `model` absent (empty in example).

- [ ] **Step 5: Commit**

```bash
git add internal/config/migrate.go internal/config/migrate_test.go cmd/ov/
git commit -m "Add legacy config parser and ov2 config migrate"
```

---

### Task 4: internal/vault — lossless Frontmatter

**Files:**
- Create: `internal/vault/frontmatter.go`
- Test: `internal/vault/frontmatter_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:

```go
package vault

// ParseNote splits a note into frontmatter and body. fm is nil when the
// note has no frontmatter block. Delimiters: ^---\n ... \n---\n? (mirrors
// triage_llm.py FM_RE).
func ParseNote(text string) (fm *Frontmatter, body string)

type Frontmatter struct { /* raw inner lines + closing-newline flag */ }

func NewFrontmatter() *Frontmatter
func (f *Frontmatter) Get(key string) (string, bool)     // lenient scalar view
func (f *Frontmatter) GetList(key string) ([]string, bool) // [a, b] shorthand view
func (f *Frontmatter) Set(key, value string)  // patch in place or append before closing
func (f *Frontmatter) Delete(key string)
func (f *Frontmatter) Render() string // "" for empty; else ---\n<lines>\n---(\n)
```

- Invariant (design-spec blocker fix): `ParseNote` + untouched `Render()` + body reassembles the input **byte-identically**. Unrecognized lines (nested YAML, comments, multiline strings) are never dropped or reformatted. `Set` canonicalizes only the patched key's line to `key: value`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/vault/frontmatter_test.go
package vault

import "testing"

// CONTRACT: byte-identical no-op round-trip (design spec, review blocker F6/m5).
func TestRoundTripByteIdentical(t *testing.T) {
	cases := []string{
		// flat kv
		"---\ntype: note\ncreated: 2026-07-11\n---\nbody\n",
		// nested map + block list + comment + no-colon line: all opaque, all preserved
		"---\ntags:\n  - deep\n  - nested\nmeta:\n  owner: alex\n# comment\nplainline\n---\n\n# Heading\n",
		// multiline string
		"---\nsummary: |\n  line one\n  line two\nstatus: raw\n---\nbody\n",
		// no trailing newline after closing delimiter
		"---\ntype: note\n---",
		// no frontmatter at all
		"just a body\nno delimiters\n",
		// empty body
		"---\ntype: note\n---\n",
	}
	for i, in := range cases {
		fm, body := ParseNote(in)
		var out string
		if fm == nil {
			out = body
		} else {
			out = fm.Render() + body
		}
		if out != in {
			t.Errorf("case %d: round-trip mismatch\n in: %q\nout: %q", i, in, out)
		}
	}
}

// CONTRACT: lenient read view — quotes stripped, [a, b] becomes a list
// (triage_llm.py split_frontmatter lines 111-136).
func TestLenientView(t *testing.T) {
	fm, _ := ParseNote("---\ntitle: \"Quoted Title\"\nurl: 'single'\ntags: [music, jazz]\nempty_list: []\n---\n")
	if v, ok := fm.Get("title"); !ok || v != "Quoted Title" {
		t.Errorf("title = %q, %v", v, ok)
	}
	if v, _ := fm.Get("url"); v != "single" {
		t.Errorf("url = %q", v)
	}
	if l, ok := fm.GetList("tags"); !ok || len(l) != 2 || l[0] != "music" || l[1] != "jazz" {
		t.Errorf("tags = %v, %v", l, ok)
	}
	if l, ok := fm.GetList("empty_list"); !ok || len(l) != 0 {
		t.Errorf("empty_list = %v, %v", l, ok)
	}
}

// CONTRACT(by accident, load-bearing): moc: [[MOC Music]] parses as a
// one-element list ["[MOC Music]"]. MOC rename sync depends on this quirk
// (tests/test_triage_llm.py:22-28). Keep exactly.
func TestWikilinkQuirk(t *testing.T) {
	fm, _ := ParseNote("---\nmoc: [[MOC Music]]\n---\n")
	l, ok := fm.GetList("moc")
	if !ok || len(l) != 1 || l[0] != "[MOC Music]" {
		t.Errorf("moc quirk broken: %v, %v", l, ok)
	}
}

// Comments and colon-less lines are invisible to Get but preserved by Render.
func TestOpaqueLinesNotParsed(t *testing.T) {
	fm, _ := ParseNote("---\n# a comment\nnocolonhere\ntype: note\n---\n")
	if _, ok := fm.Get("# a comment"); ok {
		t.Error("comments must not be parsed as keys")
	}
	if v, ok := fm.Get("type"); !ok || v != "note" {
		t.Errorf("type = %q, %v", v, ok)
	}
}

func TestSetPatchesInPlace(t *testing.T) {
	in := "---\ntype: note\n# keep me\nstatus: inbox\n---\nbody"
	fm, body := ParseNote(in)
	fm.Set("status", "filed")
	want := "---\ntype: note\n# keep me\nstatus: filed\n---\nbody"
	if got := fm.Render() + body; got != want {
		t.Errorf("patch in place:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSetAppendsNewKey(t *testing.T) {
	fm, _ := ParseNote("---\ntype: note\n---\n")
	fm.Set("area", "Music")
	want := "---\ntype: note\narea: Music\n---\n"
	if got := fm.Render(); got != want {
		t.Errorf("append:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestDelete(t *testing.T) {
	fm, _ := ParseNote("---\ntype: note\nstatus: inbox\n---\n")
	fm.Delete("status")
	if got := fm.Render(); got != "---\ntype: note\n---\n" {
		t.Errorf("delete: got %q", got)
	}
	fm.Delete("missing") // must not panic
}

// Golden: new-note frontmatter built in preferred key order
// (triage_llm.py render_frontmatter line 145) — the byte contract for
// every note ov has ever filed.
func TestNewFrontmatterGolden(t *testing.T) {
	fm := NewFrontmatter()
	for _, kv := range [][2]string{
		{"type", "note"}, {"created", "2026-07-11"}, {"modified", "2026-07-11"},
		{"tags", "[music, jazz]"}, {"status", "inbox"}, {"source", "cli"},
	} {
		fm.Set(kv[0], kv[1])
	}
	want := "---\ntype: note\ncreated: 2026-07-11\nmodified: 2026-07-11\ntags: [music, jazz]\nstatus: inbox\nsource: cli\n---\n"
	if got := fm.Render(); got != want {
		t.Errorf("golden mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vault/ -v`
Expected: FAIL — `ParseNote` undefined.

- [ ] **Step 3: Implement frontmatter.go**

```go
// Package vault is the pure filesystem domain: notes, frontmatter, naming,
// atomic writes. No network, no LLM, no terminal (design spec §architecture).
package vault

import "strings"

// Frontmatter holds the raw inner lines of a note's frontmatter block.
// Reads are lenient; writes patch single lines; everything else is opaque
// and survives Render byte-for-byte.
type Frontmatter struct {
	lines          []string
	closingNewline bool // whether the closing --- had a trailing \n
}

func NewFrontmatter() *Frontmatter {
	return &Frontmatter{closingNewline: true}
}

// ParseNote mirrors triage_llm.py FM_RE: ^---\n(.*?)\n---\n? with DOTALL.
func ParseNote(text string) (*Frontmatter, string) {
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return nil, text
	}
	// Find the first line that is exactly "---" (mirrors the non-greedy regex).
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return nil, text
	}
	inner := rest[:idx]
	after := rest[idx+len("\n---"):]
	closingNewline := false
	if strings.HasPrefix(after, "\n") {
		closingNewline = true
		after = after[1:]
	} else if after != "" {
		// "---" was a prefix of a longer line (e.g. "----"): not a delimiter.
		// The python regex requires \n or EOF after ---; keep searching is
		// out of scope for the mined corpus — treat as no frontmatter.
		return nil, text
	}
	return &Frontmatter{lines: strings.Split(inner, "\n"), closingNewline: closingNewline}, after
}

func (f *Frontmatter) Render() string {
	if f == nil || len(f.lines) == 0 {
		return ""
	}
	s := "---\n" + strings.Join(f.lines, "\n") + "\n---"
	if f.closingNewline {
		s += "\n"
	}
	return s
}

// keyLine returns the index of the first line declaring key, or -1.
// A declaring line is `key:` at zero indent (comments and indented
// continuation lines never match).
func (f *Frontmatter) keyLine(key string) int {
	for i, line := range f.lines {
		if strings.HasPrefix(line, "#") {
			continue
		}
		k, _, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.TrimSpace(k) == key && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			return i
		}
	}
	return -1
}

func (f *Frontmatter) rawValue(key string) (string, bool) {
	i := f.keyLine(key)
	if i < 0 {
		return "", false
	}
	_, v, _ := strings.Cut(f.lines[i], ":")
	return strings.TrimSpace(v), true
}

func (f *Frontmatter) Get(key string) (string, bool) {
	v, ok := f.rawValue(key)
	if !ok {
		return "", false
	}
	if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
		v = v[1 : len(v)-1]
	}
	return v, true
}

// GetList mirrors the python [a, b] shorthand — including the documented
// moc: [[MOC Music]] -> ["[MOC Music]"] quirk that MOC rename sync relies on.
func (f *Frontmatter) GetList(key string) ([]string, bool) {
	v, ok := f.rawValue(key)
	if !ok || len(v) < 2 || v[0] != '[' || v[len(v)-1] != ']' {
		return nil, false
	}
	inner := strings.TrimSpace(v[1 : len(v)-1])
	if inner == "" {
		return []string{}, true
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out, true
}

func (f *Frontmatter) Set(key, value string) {
	line := key + ": " + value
	if i := f.keyLine(key); i >= 0 {
		f.lines[i] = line
		return
	}
	f.lines = append(f.lines, line)
}

func (f *Frontmatter) Delete(key string) {
	if i := f.keyLine(key); i >= 0 {
		f.lines = append(f.lines[:i], f.lines[i+1:]...)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vault/ -v`
Expected: PASS. Pay special attention to `TestRoundTripByteIdentical` case 3 (`"---\ntype: note\n---"` — no trailing newline) and the nested/multiline cases.

- [ ] **Step 5: Commit**

```bash
git add internal/vault/
git commit -m "Add lossless frontmatter: lenient reads, patch-in-place writes"
```

---

### Task 5: internal/vault — Slugify

**Files:**
- Create: `internal/vault/slugify.go`
- Test: `internal/vault/slugify_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `func Slugify(s string, maxLen int) string`. Callers: capture uses `Slugify(t, 60)`, triage uses `Slugify(t, 80)` (both budgets deliberate — see inventory).

- [ ] **Step 1: Write the failing tests**

```go
// internal/vault/slugify_test.go
package vault

import (
	"strings"
	"testing"
)

// Mined from vault.sh slugify_title (60) and triage_llm.py slugify_title (80).
// DECIDE(kept): the 60/80 budget split is intentional — python comment says
// "filed notes get a slightly larger budget than inbox".
func TestSlugify(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		maxLen int
		want   string
	}{
		{"empty", "", 60, "Untitled"},
		{"whitespace only", "   ", 60, "Untitled"},
		// CONTRACT: leading markdown heading markers stripped
		{"heading", "## My Note Title", 60, "My Note Title"},
		// CONTRACT: forbidden set includes @&# beyond OS-forbidden chars
		{"forbidden", `a/b\c:d*e?f"g<h>i|j`, 60, "a b c d e f g h i j"},
		{"at-amp-hash", "email@host & #tag", 60, "email host tag"},
		// CONTRACT: whitespace runs collapse, ends trimmed
		{"collapse", "  too   many\tspaces  ", 60, "too many spaces"},
		// CONTRACT: case preserved — never lowercased
		{"case", "My COOL Note", 60, "My COOL Note"},
		// CONTRACT: heading strip may leave nothing
		{"only heading markers", "###", 60, "Untitled"},
		// CONTRACT: word-boundary truncation, no trailing space, <= maxLen
		{"truncate 60", strings.Repeat("word ", 20), 60, strings.TrimSpace(strings.Repeat("word ", 11)) + " word"},
		// NFC normalization (design spec §filename policy): NFD é -> NFC é
		{"nfd to nfc", "Caf\u0065\u0301 Notes", 60, "Caf\u00e9 Notes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Slugify(c.in, c.maxLen)
			if got != c.want {
				t.Errorf("Slugify(%q, %d) = %q, want %q", c.in, c.maxLen, got, c.want)
			}
			if len([]rune(got)) > c.maxLen {
				t.Errorf("result exceeds maxLen: %d > %d", len([]rune(got)), c.maxLen)
			}
		})
	}
}

func TestSlugifyBudgets(t *testing.T) {
	long := strings.Repeat("abcde ", 20) // 120 chars
	s60 := Slugify(long, 60)
	s80 := Slugify(long, 80)
	if len([]rune(s60)) > 60 || len([]rune(s80)) > 80 {
		t.Fatalf("budget violated: %d, %d", len([]rune(s60)), len([]rune(s80)))
	}
	if strings.HasSuffix(s60, " ") || strings.HasSuffix(s80, " ") {
		t.Error("no trailing whitespace after truncation")
	}
}
```

Note on the `truncate 60` expected value: `"word "` × 20 = 100 chars; the first 60 chars are 12 full `word ` units; stripping the trailing partial token and whitespace yields 11 full words + `word` = `"word word … word"` (12 words, 59 chars). If your implementation produces a different-but-valid word-boundary result, STOP — the mined contract is bash `cut -c1-60` + strip-trailing-partial-token; adjust the implementation, not the expectation, after hand-verifying against `bash -c 'source' `-free logic: `printf '%s' "word word ..." | cut -c1-60 | sed -E 's/[[:space:]]+[^[:space:]]*$//'`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vault/ -run Slugify -v`
Expected: FAIL — `Slugify` undefined.

- [ ] **Step 3: Implement slugify.go**

```go
package vault

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var (
	headingMarkersRe = regexp.MustCompile(`^\s*#+\s*`)
	// Forbidden filename chars — mined superset used by BOTH old impls,
	// deliberately including @ & # (vault.sh:150, triage_llm.py:191).
	forbiddenFnRe = regexp.MustCompile(`[\\/:*?"<>|@&#]+`)
	wsRunRe       = regexp.MustCompile(`\s+`)
	trailingTokRe = regexp.MustCompile(`\s+\S*$`)
)

// Slugify converts a raw title into a safe filename stem. Case is preserved.
// maxLen is a rune budget; truncation never cuts mid-word. NFC-normalized
// per the v2 filename policy (design spec).
func Slugify(s string, maxLen int) string {
	if s == "" {
		return "Untitled"
	}
	s = norm.NFC.String(s)
	s = headingMarkersRe.ReplaceAllString(s, "")
	s = forbiddenFnRe.ReplaceAllString(s, " ")
	s = wsRunRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxLen {
		s = string(r[:maxLen])
		s = trailingTokRe.ReplaceAllString(s, "")
		s = strings.TrimSpace(s)
	}
	if s == "" {
		return "Untitled"
	}
	return s
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vault/ -run Slugify -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vault/slugify.go internal/vault/slugify_test.go
git commit -m "Add Slugify with rune budget and NFC normalization"
```

---

### Task 6: internal/vault — atomic + conditional writes

**Files:**
- Create: `internal/vault/write.go`
- Test: `internal/vault/write_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:

```go
var ErrChangedOnDisk = errors.New(...) // conditional write conflict
var ErrExists = errors.New(...)        // create-new collision

// ReadNote returns the file content and its sha256 hex hash.
func ReadNote(path string) (content string, hash string, err error)

// WriteNoteAtomic writes via dot-prefixed temp file in the TARGET directory
// + fsync + rename. expectedHash "" means create-new (ErrExists if present);
// otherwise the current on-disk hash must match (ErrChangedOnDisk if not).
func WriteNoteAtomic(path string, content []byte, expectedHash string) error
```

- Every future vault write path (capture, triage apply, MOC edits) goes through `WriteNoteAtomic` — no exceptions (design spec §core contracts).

- [ ] **Step 1: Write the failing tests**

```go
// internal/vault/write_test.go
package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateNew(t *testing.T) {
	p := filepath.Join(t.TempDir(), "note.md")
	if err := WriteNoteAtomic(p, []byte("hello\n"), ""); err != nil {
		t.Fatal(err)
	}
	content, hash, err := ReadNote(p)
	if err != nil || content != "hello\n" || len(hash) != 64 {
		t.Fatalf("content=%q hash=%q err=%v", content, hash, err)
	}
}

func TestCreateNewRefusesExisting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "note.md")
	os.WriteFile(p, []byte("old"), 0o644)
	err := WriteNoteAtomic(p, []byte("new"), "")
	if !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "old" {
		t.Error("original clobbered")
	}
}

func TestConditionalReplace(t *testing.T) {
	p := filepath.Join(t.TempDir(), "note.md")
	WriteNoteAtomic(p, []byte("v1"), "")
	_, hash, _ := ReadNote(p)
	if err := WriteNoteAtomic(p, []byte("v2"), hash); err != nil {
		t.Fatal(err)
	}
	content, _, _ := ReadNote(p)
	if content != "v2" {
		t.Errorf("content = %q", content)
	}
}

// CONTRACT(new in v2, review F1): stale hash = visible refusal, never clobber.
func TestConditionalRefusesStale(t *testing.T) {
	p := filepath.Join(t.TempDir(), "note.md")
	WriteNoteAtomic(p, []byte("v1"), "")
	_, staleHash, _ := ReadNote(p)
	// Simulate Obsidian Sync pulling a newer version between read and write:
	os.WriteFile(p, []byte("synced-change"), 0o644)
	err := WriteNoteAtomic(p, []byte("v2"), staleHash)
	if !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("want ErrChangedOnDisk, got %v", err)
	}
	content, _, _ := ReadNote(p)
	if content != "synced-change" {
		t.Error("concurrent edit was clobbered")
	}
}

// Temp files are dot-prefixed (Obsidian ignores dotfiles), live in the target
// dir (same-filesystem rename), and never survive success or failure.
func TestNoTempLeftovers(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "note.md")
	WriteNoteAtomic(p, []byte("x"), "")
	WriteNoteAtomic(p, []byte("y"), "not-a-real-hash") // fails
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ov-tmp-") {
			t.Errorf("temp leftover: %s", e.Name())
		}
		if e.Name() != "note.md" {
			t.Errorf("unexpected entry: %s", e.Name())
		}
	}
}

func TestConditionalOnMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gone.md")
	err := WriteNoteAtomic(p, []byte("x"), "deadbeef")
	if err == nil {
		t.Fatal("conditional write to missing file must error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vault/ -run 'Create|Conditional|Leftover' -v`
Expected: FAIL — `WriteNoteAtomic` undefined.

- [ ] **Step 3: Implement write.go**

```go
package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	ErrChangedOnDisk = errors.New("note changed on disk since it was read — refresh and retry")
	ErrExists        = errors.New("note already exists")
)

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func ReadNote(path string) (string, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	return string(b), hashBytes(b), nil
}

// WriteNoteAtomic: dot-prefixed temp in the TARGET directory (never
// os.TempDir — rename must be same-filesystem, and Obsidian ignores
// dotfiles so partial writes never sync), fsync, rename, best-effort
// dir fsync. See design spec §core contracts.
func WriteNoteAtomic(path string, content []byte, expectedHash string) error {
	if expectedHash == "" {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("%s: %w", path, ErrExists)
		}
	} else {
		cur, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("conditional write: %w", err)
		}
		if hashBytes(cur) != expectedHash {
			return fmt.Errorf("%s: %w", path, ErrChangedOnDisk)
		}
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ov-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	// Best-effort directory fsync so the rename itself is durable.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vault/ -v`
Expected: PASS (frontmatter + slugify + write suites all green).

- [ ] **Step 5: Commit**

```bash
git add internal/vault/write.go internal/vault/write_test.go
git commit -m "Add atomic conditional note writes with dot-temp and fsync"
```

---

### Task 7: internal/llm — ExtractJSON decoder

**Files:**
- Create: `internal/llm/decode.go`
- Test: `internal/llm/decode_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `func ExtractJSON(text string) (map[string]any, error)` — the tolerant 3-tier decoder. Transport (`Run`) and `ExtractHTMLBlock` land in later phases; this file must not import os/exec.

- [ ] **Step 1: Write the failing tests**

```go
// internal/llm/decode_test.go
package llm

import (
	"strings"
	"testing"
)

// CONTRACT: 3-tier fallback mined from triage_llm.py extract_json (281-304):
// direct parse -> fenced block -> first-{ to last-}.
func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		to   string // expected value of key "to"
	}{
		{"direct", `{"to": "03-Resources/Music"}`, "03-Resources/Music"},
		{"direct with whitespace", "\n  {\"to\": \"x\"}\n", "x"},
		{"fenced json", "Here you go:\n```json\n{\"to\": \"x\"}\n```\nDone.", "x"},
		{"fenced no lang", "```\n{\"to\": \"x\"}\n```", "x"},
		{"prose wrapped", `Sure! The answer is {"to": "x"} — hope that helps.`, "x"},
		{"nested braces", `Result: {"to": "x", "meta": {"inner": true}}`, "x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ExtractJSON(c.in)
			if err != nil {
				t.Fatal(err)
			}
			if got["to"] != c.to {
				t.Errorf("to = %v, want %q", got["to"], c.to)
			}
		})
	}
}

func TestExtractJSONErrors(t *testing.T) {
	if _, err := ExtractJSON("no json here at all"); err == nil ||
		!strings.Contains(err.Error(), "no JSON object found") {
		t.Errorf("want 'no JSON object found', got %v", err)
	}
	if _, err := ExtractJSON("{broken: json,}"); err == nil {
		t.Error("malformed JSON inside braces must error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/llm/ -v`
Expected: FAIL — `ExtractJSON` undefined.

- [ ] **Step 3: Implement decode.go**

```go
// Package llm: subprocess transport and output decoders for OV_LLM_CMD.
// This file is decoders only — pure functions, no exec.
package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var jsonFenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")

// ExtractJSON is a 1:1 port of triage_llm.py extract_json — tolerant of
// LLMs that wrap output in prose or markdown fences.
func ExtractJSON(text string) (map[string]any, error) {
	text = strings.TrimSpace(text)

	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err == nil {
		return out, nil
	}
	if m := jsonFenceRe.FindStringSubmatch(text); m != nil {
		if err := json.Unmarshal([]byte(strings.TrimSpace(m[1])), &out); err == nil {
			return out, nil
		}
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start != -1 && end > start {
		if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
			return nil, fmt.Errorf("could not parse JSON from LLM response: %w\n--- raw ---\n%s", err, text)
		}
		return out, nil
	}
	return nil, fmt.Errorf("no JSON object found in LLM response:\n--- raw ---\n%s", text)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/llm/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/llm/
git commit -m "Add tolerant 3-tier JSON decoder for LLM output"
```

---

### Task 8: internal/vault — PARA folder discovery

**Files:**
- Create: `internal/vault/discover.go`
- Test: `internal/vault/discover_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `func DiscoverFolders(vaultDir string, roots []string) []string` — vault-relative folder paths, each root then its subdirs then sub-subdirs (depth ≤ 2 below root), sorted at each level. Missing roots skipped silently. Used by triage prompt assembly in phase 3.

- [ ] **Step 1: Write the failing tests**

```go
// internal/vault/discover_test.go
package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// CONTRACT: mined from triage_llm.py discover_folders (169-184) —
// depth <= 2 below each root, sorted, missing roots skipped, files ignored.
func TestDiscoverFolders(t *testing.T) {
	vault := t.TempDir()
	mk := func(parts ...string) {
		if err := os.MkdirAll(filepath.Join(append([]string{vault}, parts...)...), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk("01-Projects", "Work", "ClientA")
	mk("01-Projects", "Home")
	mk("03-Resources", "Music")
	mk("01-Projects", "Work", "ClientA", "TooDeep") // depth 3: excluded
	os.WriteFile(filepath.Join(vault, "01-Projects", "stray.md"), []byte("x"), 0o644)

	got := DiscoverFolders(vault, []string{"01-Projects", "02-Areas", "03-Resources"})
	want := []string{
		"01-Projects",
		"01-Projects/Home",
		"01-Projects/Work",
		"01-Projects/Work/ClientA",
		"03-Resources",
		"03-Resources/Music",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vault/ -run Discover -v`
Expected: FAIL — `DiscoverFolders` undefined.

- [ ] **Step 3: Implement discover.go**

```go
package vault

import (
	"os"
	"path/filepath"
	"sort"
)

// DiscoverFolders mirrors triage_llm.py discover_folders: each existing
// root, its subdirectories, and their subdirectories (depth <= 2 below the
// root), sorted at each level. Unreadable directories are skipped, never
// fatal (walk-resilience rule, design spec §core contracts).
func DiscoverFolders(vaultDir string, roots []string) []string {
	var out []string
	for _, root := range roots {
		rootPath := filepath.Join(vaultDir, root)
		if info, err := os.Stat(rootPath); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, root)
		for _, sub := range sortedSubdirs(rootPath) {
			out = append(out, root+"/"+sub)
			for _, sub2 := range sortedSubdirs(filepath.Join(rootPath, sub)) {
				out = append(out, root+"/"+sub+"/"+sub2)
			}
		}
	}
	return out
}

func sortedSubdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/vault/ -run Discover -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vault/discover.go internal/vault/discover_test.go
git commit -m "Add PARA folder discovery, depth-limited and sorted"
```

---

### Task 9: `ov2 doctor` + `ov2 init`

**Files:**
- Create: `cmd/ov/doctor.go`
- Create: `cmd/ov/init.go`
- Modify: `cmd/ov/root.go` (attach both)
- Test: `cmd/ov/doctor_test.go`

**Interfaces:**
- Consumes: `config.Load`, `config.Config.Validate`, `config.Config.ParaRoots`, `config.DefaultPath` (Task 2)
- Produces: `newDoctorCmd() *cobra.Command`, `newInitCmd() *cobra.Command`. Doctor exit codes: 0 = healthy, 1 = hard failure (config unloadable or vault dir invalid). Missing PARA roots and unresolvable LLM argv[0] are warnings, not failures.
- Doctor demonstrates the flag>env precedence rule: `--vault` overrides the loaded config.

- [ ] **Step 1: Write the failing test**

```go
// cmd/ov/doctor_test.go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	for _, want := range []string{"vault", "ok"} {
		if !strings.Contains(strings.ToLower(out), want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestDoctorMissingVaultFails(t *testing.T) {
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
	goodVault := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(cfgPath, []byte("vault_dir = \"/nonexistent\"\nllm_cmd = \"true\"\n"), 0o644)
	t.Setenv("OV_CONFIG", cfgPath)

	out, err := runDoctor(t, "--vault", goodVault)
	if err != nil {
		t.Fatalf("--vault override must win over config: %v\n%s", err, out)
	}
}

func TestDoctorMissingParaRootWarns(t *testing.T) {
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ov/ -v`
Expected: FAIL — `newDoctorCmd` undefined.

- [ ] **Step 3: Implement doctor.go and init.go**

```go
// cmd/ov/doctor.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate config, vault layout, and LLM command",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfg, err := config.Load("")
			if err != nil {
				return err
			}
			if vaultFlag != "" {
				cfg.VaultDir = vaultFlag // flag > env > file > default
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			fmt.Fprintf(out, "vault      ok  %s\n", cfg.VaultDir)

			for _, root := range append([]string{cfg.Inbox}, cfg.ParaRoots()...) {
				p := filepath.Join(cfg.VaultDir, root)
				if info, err := os.Stat(p); err == nil && info.IsDir() {
					fmt.Fprintf(out, "folder     ok  %s\n", root)
				} else {
					fmt.Fprintf(out, "folder   WARN  %s missing\n", root)
				}
			}

			argv0 := strings.Fields(cfg.LLMCmd)
			if len(argv0) == 0 {
				fmt.Fprintf(out, "llm      WARN  OV_LLM_CMD empty\n")
			} else if path, err := exec.LookPath(argv0[0]); err != nil {
				fmt.Fprintf(out, "llm      WARN  %q not found on PATH\n", argv0[0])
			} else {
				fmt.Fprintf(out, "llm        ok  %s\n", path)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}
```

```go
// cmd/ov/init.go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/spf13/cobra"
)

const configTemplate = `# ov v2 config. The only required key is vault_dir.
vault_dir = ""

# PARA folder names (relative to vault). Defaults shown; uncomment to change.
# inbox = "00-Inbox"
# projects = "01-Projects"
# areas = "02-Areas"
# resources = "03-Resources"
# archive = "04-Archive"
# meta = "99-Meta"

# LLM command for triage. Prompt on stdin, response on stdout.
# llm_cmd = "claude --print"
# model = ""

# Publish targets (ov publish); host is your exe.dev VM.
# docs_host = ""
# docs_path = "/var/www/docs"
# docs_url = ""
`

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the config file if it does not exist",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := os.Getenv("OV_CONFIG")
			if path == "" {
				path = config.DefaultPath()
			}
			if _, err := os.Stat(path); err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "→ %s already exists, leaving alone\n", path)
				return nil
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(configTemplate), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ Created %s (set vault_dir)\n", path)
			return nil
		},
	}
}
```

In `cmd/ov/root.go`, extend the AddCommand line:

```go
	root.AddCommand(newConfigCmd(), newDoctorCmd(), newInitCmd())
```

- [ ] **Step 4: Run tests, then doctor against the real vault**

Run: `go test ./cmd/ov/ -v`
Expected: PASS (4 tests).

Run: `make build && ./dist/ov2 doctor`
Expected (on this machine, with `~/.config/ov/config` being the OLD format): doctor reports vault via env/defaults or errors cleanly asking for config — then verify the migrate path end-to-end:

```bash
./dist/ov2 config migrate > /tmp/ov2-config.toml
OV_CONFIG=/tmp/ov2-config.toml ./dist/ov2 doctor
```
Expected: `vault ok <real path>`, `folder ok` lines for the live PARA roots, `llm ok` line. This is the phase exit criterion — record the output in the PR description.

- [ ] **Step 5: Commit**

```bash
git add cmd/ov/
git commit -m "Add ov2 doctor and init commands"
```

---

### Task 10: Behavior inventory (CONTRACT / BUG / DECIDE)

**Files:**
- Create: `docs/plans/ov2-behavior-inventory.md`

**Interfaces:**
- Consumes: read-only access to `bin/vault.sh`, `bin/triage_llm.py`, `bin/moc_cleanup.py`, `tests/`
- Produces: the classification document later phases cite in test comments. Format contract below.

- [ ] **Step 1: Walk every function and classify**

Read each function in the three old scripts (the function list for `vault.sh`: `load_config, show_help, get_file_age, format_age, slugify_title, is_bare_url, fetch_url_title, count_moc_items, find_moc_by_name, list_all_mocs, list_all_para_folders, select_target_folder_interactive, select_moc_interactive, update_moc, capture_note, inbox_list, triage_inbox, new_note, review_vault, find_stale, moc_list, moc_new, moc_orphan, moc_add, moc_cleanup, publish_doc, unpublish_doc` — plus `render` dispatch to `render_html.py`; for python: every function listed in each file's structural header). For each observable behavior, write one table row:

```markdown
# ov2 Behavior Inventory

Every row: source location, behavior, classification, v2 resolution.
Classifications: CONTRACT (port exactly, test it) · BUG (do not port; v2 does
it right; note the fix) · DECIDE (deliberate divergence; record the decision).

| # | Source | Behavior | Class | v2 resolution |
|---|---|---|---|---|
| 1 | vault.sh:990-1003 | `mocs orphan` sets found_in_moc in a pipeline subshell; flag never propagates; every note reported orphaned | BUG | reimplement with real [[wikilink]] parsing (phase 1) |
| 2 | vault.sh:993 | orphan match is substring grep, not link-aware | BUG | same fix as #1 |
| 3 | vault.sh:156 vs triage_llm.py:200 | slugify budget 60 (capture) vs 80 (triage) | DECIDE | kept deliberately — python comment says filed notes get a larger budget; one Slugify(s, maxLen) |
| 4 | triage_llm.py:342-363 | `moc: [[MOC Music]]` frontmatter parses as ["[MOC Music]"]; rename sync depends on it | CONTRACT | ported; TestWikilinkQuirk |
| 5 | triage_llm.py:489-492 | non-null body_patch is APPLIED but never DISPLAYED in approval | BUG | v2 Validate rejects non-null body_patch / non-empty links_to_add (phase 3) |
| 6 | triage_llm.py:456 | vault containment via string prefix — accepts /path/vault-evil | BUG | EvalSymlinks + filepath.Rel (phase 2) |
| 7 | vault.sh:859,966 | obsidian://open?vault=main-vault hardcoded | BUG | vault name from config (phase 2) |
| 8 | vault.sh:470-482 | MOC entry placement prefers emoji headings chain | DECIDE | simplified: append under "## 🔗 Recent Additions", create if missing (spec §CLI/TUI) |
| 9 | vault.sh:372-406 | picker stdout carries ONLY the selection; prompts to stderr; fzf opens /dev/tty | CONTRACT | tui package tty discipline (phase 2) |
| 10 | vault.sh:581-587 | capture body from stdin when piped | CONTRACT | frozen AGENTS.md subset |
```

Continue the walk until every function above has at least one row or an explicit `no observable contract` row. Known additional areas to cover: `fetch_url_title` (5s cap, UA string, Cloudflare-interstitial rejection, never-fatal), `is_bare_url` (trim + regex), `get_file_age`/`format_age` (7-day warn threshold), capture filename stamping + collision suffix, `update_moc` mktemp-in-/tmp non-atomic write (BUG — cross-device, replaced by WriteNoteAtomic), `find_moc_by_name` ("Music" vs "MOC Music" resolution), triage prompt schema fields, moc_cleanup validator rules (bare-wikilink immutable vs URL-anchored retitlable), `render_html.py` RENDER_SOURCE marker splicing, `publish_doc` eval (BUG — argv-exec in v2), config divergence (python reader missing OV_DOCS_* keys — BUG, fixed by Task 2's single inventory).

- [ ] **Step 2: Cross-check against phase 0 tests**

Every `// CONTRACT:`/`// BUG(fixed):`/`// DECIDE:` comment written in Tasks 2-8 must correspond to a row. Add missing rows; do not renumber existing ones.

- [ ] **Step 3: Commit**

```bash
git add docs/plans/ov2-behavior-inventory.md
git commit -m "Add behavior inventory: CONTRACT/BUG/DECIDE classification of v1"
```

---

### Task 11: Phase exit verification

**Files:**
- Modify: `README.md` (add a short v2 status pointer)

**Interfaces:**
- Consumes: everything above
- Produces: phase 0 exit evidence

- [ ] **Step 1: Full verification run**

```bash
make check    # old scripts still syntax-clean (untouched)
make test     # old pytest suite still green (untouched)
make gotest   # all Go packages green
make build && ./dist/ov2 --version && ./dist/ov2 doctor
```
Expected: every command exits 0. `doctor` output against the live vault shows `vault ok`.

- [ ] **Step 2: Confirm the no-vault-writes invariant**

Run: `go run ./cmd/ov --help`
Confirm the only subcommands are `config`, `doctor`, `init`, `completion`, `help`. None writes inside a vault (`init` writes only `~/.config/ov/config.toml`).

- [ ] **Step 3: Add the v2 pointer to README**

Append under the Architecture section of `README.md`:

```markdown
## v2 (Go) — in progress

A Go rewrite is underway on the `ov-v2-design` line: see
[docs/plans/2026-07-11-ov-v2-go-rewrite-design.md](docs/plans/2026-07-11-ov-v2-go-rewrite-design.md).
The transitional binary is `ov2` (`make build` → `dist/ov2`); the bash/python
`ov` remains authoritative until cutover.
```

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "Note v2 rewrite status in README"
```

---

## Self-Review (completed at planning time)

1. **Spec coverage (phase 0 scope only):** module scaffold ✓ (T1); internal/config with precedence, expansion, full key inventory, migrate/init/doctor ✓ (T2, T3, T9); lossless frontmatter ✓ (T4); slugify ✓ (T5); atomic+conditional writes ✓ (T6); mined table corpus incl extract_json ✓ (T2-T8); behavior inventory ✓ (T10); exit criteria `go test ./...` green + doctor validates real config + zero vault write paths ✓ (T11). Deliberately deferred per spec phasing: transport/job model (phase 3), containment/filename-policy enforcement points (phase 2 write paths — the policy tests for Slugify NFC land now), MOC ops, orphan reimplementation (phase 1).
2. **Placeholder scan:** none — every step has complete code or exact commands.
3. **Type consistency:** `config.Load(string) (*Config, error)` used identically in T3/T9; `vault.Slugify(string, int) string`; `vault.WriteNoteAtomic(string, []byte, string) error`; `llm.ExtractJSON(string) (map[string]any, error)`; cross-checked.
