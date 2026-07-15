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

// ParseLegacy extracts OV_* keys from a bash-style config. Values verbatim
// (no expansion — Load handles ~/$VAR at read time).
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

// RenderTOML renders known OV_* keys as TOML in envFields order,
// skipping empty values and unknown keys.
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
