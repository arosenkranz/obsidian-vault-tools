// internal/triage/mocsync_test.go
package triage

import (
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// CONTRACT(#4): extractMOCName unwraps the moc: [[MOC Music]] frontmatter
// quirk (a one-item list ["[MOC Music]"], per Frontmatter.GetList's
// documented behavior) as well as a plain string value.
func TestExtractMOCNameQuirk(t *testing.T) {
	cases := []struct {
		content string
		want    string
	}{
		{"---\nmoc: [[MOC Music]]\n---\nbody\n", "MOC Music"},
		{"---\nmoc: MOC Music\n---\nbody\n", "MOC Music"},
		{"---\ntype: inbox\n---\nbody\n", ""},
	}
	for _, c := range cases {
		fm, _ := vault.ParseNote(c.content)
		got := extractMOCName(fm)
		if got != c.want {
			t.Errorf("extractMOCName(%q) = %q, want %q", c.content, got, c.want)
		}
	}
}
