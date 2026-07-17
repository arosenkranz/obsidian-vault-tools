// internal/publish/slug.go
package publish

import "strings"

// Slug ports vault.sh publish_doc's output-filename rule (row #73,
// DECIDE): lowercase + spaces->hyphens + strip everything but
// [a-z0-9-] (ASCII only, mirroring v1's `tr -cd '[:alnum:]-'` byte-wise
// behavior in the C locale — non-ASCII runes are dropped, not
// transliterated). Deliberately distinct from vault.Slugify's
// case-preserving, Unicode-aware note-filename policy (row #23) — kept
// as publish's own documented rule per row #73's DECIDE.
func Slug(stem string) string {
	s := strings.ToLower(stem)
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
