package config

import (
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
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
		"OV_MODEL":     "",  // empty: skipped
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

func tomlUnmarshalForTest(b []byte, v any) error { return toml.Unmarshal(b, v) }
