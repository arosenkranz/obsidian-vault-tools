// internal/newnote/newnote.go
//
// Package newnote renders `ov new`'s note templates. Pure content
// generation only (mirrors internal/vault.NewMOCSkeleton's
// characterization) — folder resolution, slugification, containment,
// and the atomic write all stay in cmd/ov/new.go, matching this
// codebase's "no intermediate package for orchestration" convention
// (phase 4's mocs new/add). The three embedded templates are MAINTAINED
// COPIES of this repo's canonical templates/99-Meta/{Project Template,
// Meeting Note Template,Learning Note Template}.md (row #164, DECIDE):
// go:embed cannot reference a parent/sibling directory of the embedding
// package, and reading templates/99-Meta at runtime from wherever the
// installed binary happens to live is exactly the kind of
// install-location-dependent filesystem lookup the design's "single
// static binary" philosophy exists to avoid. If templates/99-Meta's
// canonical content changes, these embedded copies must be manually
// re-synced — a documented, accepted tradeoff, not automatic.
package newnote

import (
	_ "embed"
	"strings"
)

//go:embed templates/project.md
var ProjectTemplate string

//go:embed templates/meeting.md
var MeetingTemplate string

//go:embed templates/learning.md
var LearningTemplate string

// Substitute replaces every literal "{{title}}" occurrence in tmpl with
// title (row #59's placeholder substitution) via a plain
// strings.ReplaceAll — never sed or any other replacement-template API.
// v1's `sed -i "s/{{title}}/$title/g"` interpolated title directly into
// a sed REPLACEMENT expression, where "&", a literal "\1"-style
// sequence, or "/" are metacharacter-unsafe (row #165, BUG(fixed) — a
// defect never exercised because `new` was never actually shipped in
// any prior phase). strings.ReplaceAll has no such replacement-syntax
// interpretation at all.
func Substitute(tmpl, title string) string {
	return strings.ReplaceAll(tmpl, "{{title}}", title)
}

// Bare renders the General-type fallback content for note types with no
// template file (row #59: v1's type 4 has none either).
func Bare(title string) string {
	return "# " + title + "\n\n"
}
