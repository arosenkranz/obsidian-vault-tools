# ov v2 Phase 5: publish/unpublish, render (goldmark), new Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `ov2 publish`, `ov2 unpublish`, `ov2 render`, and `ov2 new` as full replacements for `bin/vault.sh`'s `publish_doc`/`unpublish_doc`/`new_note` and `bin/render_html.py`. Two new packages (`internal/publish`, `internal/render`, both named explicitly in the design spec's `§Architecture`) plus one small embedding-only package (`internal/newnote`) get every bash/python command a Go equivalent, satisfying phase 5's exit criterion.

**Architecture:** Per the approved spec (`docs/plans/2026-07-11-ov-v2-go-rewrite-design.md`), phase 5 row: "publish/unpublish; render (goldmark)", exit criterion "every bash command has a Go equivalent; CLI surface complete". `internal/publish` owns the LLM→HTML prompt/decode (a THIRD, independent prompt-builder package after triage/moc — HTML schema, not JSON, so no shared abstraction is forced) plus rsync/ssh push/remove/list behind injectable interfaces (mirroring how `internal/llm.Runner` is injected everywhere, so no cmd-level test ever attempts a real network call). `internal/render` owns goldmark markdown→HTML conversion plus RENDER_SOURCE/RENDER_BODY/RENDER_TIMESTAMP marker parsing and splicing — the marker-splicing MECHANISM is the ported contract (rows #117-119); the generated HTML's exact bytes are not (render is outside the frozen CLI subset, design spec §Compatibility contract). `internal/newnote` holds ONLY three `go:embed`-ed templates plus pure string substitution — folder resolution, slugification, containment, and the atomic write for `ov2 new` all live directly in `cmd/ov/new.go`, matching phase 4's "no intermediate package for orchestration" convention (`mocs new`/`mocs add`).

**Tech Stack:** Go 1.25.0 (unchanged), cobra, pelletier/go-toml/v2, golang.org/x/text, charm.land/bubbletea/v2, charm.land/lipgloss/v2, charmbracelet/huh, mattn/go-isatty, github.com/sergi/go-diff (all existing, all reused as-is). **One new dependency this phase:** `github.com/yuin/goldmark v1.8.4` (already in the design's approved dep budget — §Architecture: "goldmark" listed, glamour's engine "already in the budget for render" — just not yet in `go.mod`; verified via `grep goldmark go.mod` returning nothing before this phase). Goldmark itself has zero external dependencies (its own `go.mod` requires nothing beyond `go 1.22`), so no new indirect entries are needed. rsync/ssh are subprocess calls, not libraries — no dependency for those.

## Global Constraints

- Module path: `github.com/arosenkranz/obsidian-vault-tools`
- Binary name during transition: `ov2`, built to `dist/ov2` (`make build`)
- **Exactly one new dependency this phase:** `github.com/yuin/goldmark v1.8.4`, added in Task 1 only. No other `go.mod` changes.
- `bin/vault.sh`, `bin/triage_llm.py`, `bin/moc_cleanup.py`, `bin/render_html.py` are READ-ONLY — never modify them.
- **Behavior-inventory prerequisite (already on this branch, commit before Task 1):** rows #59, #73, #74, #75, #76, #117, #118, #119 were re-annotated from stale "(phase 2 `new`)"/"(phase 4)" to "(phase 5)" (mined during phase 0 before the phasing table settled — the same staleness class rows #107-116 had before phase 4's own correction). Rows #158-165 were mined and appended for this phase's new behavior (publish/unpublish picker DECIDE split, publish HTML write atomicity, ssh remote-command quoting, render `--all` short-circuit bug, render write atomicity, `ov2 new`'s embedded-vs-runtime template source, `{{title}}` substitution safety). Every mined test in this plan cites a row via `// CONTRACT:`, `// BUG(fixed):`, or `// DECIDE(...):`.
- **`ov2 new` is genuinely new work, not a re-port of already-shipped code:** despite the phasing table's terse "publish/unpublish; render (goldmark)" content line and the behavior inventory's stale "(phase 2 `new`)" annotation (now corrected), `cmd/ov/root.go` registers no `new` command anywhere in phases 0-4 and no `cmd/ov/new.go` exists — verified before this plan was written. It is in scope for phase 5 because phase 5's own exit criterion ("every bash command has a Go equivalent") is unmeetable without it and phase 6 is web-polish/cutover only.
- **`internal/publish` and `internal/render` scope is fixed by the design spec** (§Architecture): `internal/publish/` — "LLM→HTML convert; rsync/ssh push/remove/list-remote via subprocess"; `internal/render/` — "goldmark md→HTML; RENDER_SOURCE marker splicing (port of render_html.py logic)". Neither package touches the terminal or does CLI orchestration — `cmd/ov`'s `publish.go`/`unpublish.go`/`render.go` own flag parsing, stdin prompts, and stdout/stderr discipline, exactly like every prior phase's command files.
- **`internal/newnote` is intentionally tiny and embedding-only** (row #164, DECIDE): it holds three `go:embed`-ed template strings plus `Substitute`/`Bare` — pure content generation, the same characterization `internal/vault.NewMOCSkeleton` already carries. `go:embed` directives cannot reference a parent or sibling directory of the embedding package, so the three templates are MAINTAINED COPIES of this repo's canonical `templates/99-Meta/{Project Template,Meeting Note Template,Learning Note Template}.md`, embedded at `internal/newnote/templates/{project,meeting,learning}.md` — byte-identical at authoring time. If the canonical `templates/99-Meta/*.md` content changes later, these copies need manual re-syncing; this is a documented, accepted tradeoff, not automatic. Folder resolution, `vault.Slugify`, `vault.ContainPath`, and `vault.WriteNoteAtomic` all stay in `cmd/ov/new.go` — no orchestration logic in `internal/newnote`.
- **Publish's file argument is required — no interactive picker** (row #158, DECIDE for THIS command): v1's no-file-arg gum picker walked the WHOLE vault for `*.md`, an unbounded candidate set impractical as a plain numbered list without gum/fzf. `ov2 publish <file>` (`cobra.ExactArgs(1)`) matches `mocs new`/`mocs add`'s established flags-first precedent.
- **Unpublish's no-args mode is a plain numbered picker, not a bubbletea component** (row #159, DECIDE for THIS command, deliberately different from publish's decision): the candidate set (already-published files on the remote docs host) is typically small and requires only one `ssh ls` round-trip — the same "browse, not recall" case render's own picker has. Rather than inventing a new bubbletea multi-select component for one command, `ov2 unpublish` with no args lists remote files via an injectable `publish.Lister`, reads a plain comma-separated/`"a"`/`"q"` choice from stdin (mirroring render's own v1 numbered-picker style below), then gates removal on an explicit `tui.Confirm` (row #77).
- **Render's no-args mode is ALSO a plain numbered picker** (row #69's resolution for THIS command): v1's `render_html.py` picker (`render_html.py:369-389`) was never gum/fzf-based to begin with — a bare `input()` prompt over a numbered list. `ov2 render` with neither a `<file>` argument nor `--all` ports that exact style directly: no new UI component needed.
- **`--all` (and the interactive `"a"` choice) never short-circuits** (row #162, BUG(fixed)): v1's `ok = all(regenerate(h, m) for h, m in pairs)` is a generator expression fed to Python's `all()`, which stops calling `regenerate()` at the first falsy result while the following `print(f'...{len(pairs)} file(s) processed.')` always reports the full count regardless. `ov2 render --all` processes every pair unconditionally and reports an accurate ok/failed count.
- **Row #120's fix is structural, not incidental:** every marker splice in `internal/render` is index-slice + string concatenation. NEVER `regexp.ReplaceAllString`/`regexp.ReplaceAll` with generated HTML as the replacement argument — Go's replacement-string syntax (`$1`, `${name}`) is the exact same bug CLASS as Python's `re.sub` (`\1`, `\g<...>`), just different metacharacters. `TestSpliceBodyPreservesBackreferenceLookingContent` (Task 1) is the dedicated regression test and MUST assert both the Python-style and the Go-style sequences survive unchanged.
- **Publish's write path fixes two non-atomic v1 writes** (rows #160 for the HTML output, closing the same defect family as #42/#101/#116/#128/#129): `Published/<slug>.html` is written via `vault.WriteNoteAtomic` — create-new (`expectedHash == ""`) on first publish, conditional overwrite (re-read the existing file's hash first) on republish, preserving v1's overwrite-on-republish behavior while adding atomicity/fsync. `internal/render.Regenerate` does the same for the regenerated HTML file (row #163).
- **Publish/unpublish/render's containment posture is decided per argument, not uniformly** (per-task note below): render's `<file>` argument IS vault-relative by construction (v1 joins `vault / args.file`) and is routed through `vault.ContainPath` before matching against the discovered pairs — a traversal-attempting value is rejected outright rather than silently failing the lookup. Publish's `<file>` argument is an arbitrary LOCAL path exactly as v1 treats it (no vault-membership requirement — a user may reasonably publish a scratch file outside the configured vault) — only the WRITE side (`Published/<slug>.html`) is routed through `vault.ContainPath`, matching the established defense-in-depth posture from `mocs new` (row #153) even though `publish.Slug` already strips every non-`[a-z0-9-]` character including `/`.
- **Publish's slug rule is intentionally distinct from `vault.Slugify`** (row #73, DECIDE, unchanged from phase 0's mining): lowercase + spaces→hyphens + strip everything but ASCII `[a-z0-9-]` (mirroring v1's `tr -cd '[:alnum:]-'` C-locale byte-wise behavior — non-ASCII runes are dropped, not transliterated). `internal/publish.Slug` is its own function, never routed through `vault.Slugify`.
- **`ov new`'s third slug rule is retired for real** (row #58, BUG, closed this phase): `ov2 new`'s filename uses `vault.Slugify(title, 80)` — the 80-char budget matches `mocs new`'s Resources-dir precedent (Project/Meeting/Learning titles are closer to MOC titles' descriptive style than capture's 60-char quick-dump budget, row #3's split).
- **`{{title}}` substitution is a literal string replace, never sed** (row #165, BUG(fixed)): `newnote.Substitute` uses `strings.ReplaceAll` — no replacement-metacharacter interpretation exists in Go's plain string replace, unlike v1's `sed -i "s/{{title}}/$title/g"` where a title containing `&`, a `\1`-style sequence, or `/` was never exercised because `new` was never actually shipped.
- **ssh remote-command construction is quoted, not raw-interpolated** (row #161, DECIDE, new hardening): `ssh` always joins its trailing argv into ONE command string for the remote shell — this is inherent to the ssh wire protocol, not a local `eval` (row #72's fix class is unrelated: the LOCAL side of every subprocess call in this phase is a plain `exec.Command`, never `sh -c`). `internal/publish`'s `shellQuoteSingle` helper (in `remove.go`, shared with `list.go`) single-quotes the remote path/basename for that remote command string, escaping an embedded `'` — v1 never escaped this.
- **Timeouts are new in v2 everywhere in this phase** (v1 had none on rsync/ssh at all): `publishLLMTimeout = 180 * time.Second` (`cmd/ov/publish.go`-local constant — matches moc-cleanup's HTML/text-generation budget, NOT triage's 120s, since HTML generation is comparably heavy; NOT shared with `mocCleanupTimeout`/`llmTriageTimeout`), `publishPushTimeout = 60 * time.Second` (rsync push), `sshOpTimeout = 30 * time.Second` (`cmd/ov/unpublish.go`-local, each individual ssh rm/ls call).
- **No `errExitCode2` sentinel anywhere in this phase:** `publish`/`unpublish`/`render`/`new` all use plain `errors.New`/`fmt.Errorf` returns, mapped to exit code 1 by `main.go`'s existing default — matching v1's own `return 1` convention for these four commands (distinct from triage --llm / mocs cleanup's v1-mandated exit code 2, which this phase does not touch or reuse).
- **Security-focused review routing:** Task 1 (`internal/render`'s marker-splicing, the security/correctness-critical row #120 fix) and Task 4 (`cmd/ov publish`/`unpublish`, the write-path + subprocess-adjacent orchestration) are routed to a security-focused reviewer at task-review time, matching phases 2/3/4's pattern for write-path and subprocess-adjacent work.
- Go tests use `t.Setenv`/`t.TempDir` — no global state leaks between tests; no injected package-level clock (clocks are passed as `time.Time` values, matching `triage.Apply`'s `now time.Time` parameter and phase 4's `moc.Apply`/`vault.NewMOCSkeleton`).
- Commit style: imperative mood, no conventional-commit prefix (matches repo history).
- Stub-LLM integration tests (design spec §Testing strategy tier 3) reuse `internal/llmtest`'s TestMain re-exec pattern directly — `cmd/ov/main_test.go` already calls `llmtest.MaybeRunStub()` before `m.Run()`, covering every test in the `main` package including this phase's new files; no new re-exec plumbing is needed.

---

### Task 1: `internal/render` — marker splicing (row #120 fix), goldmark conversion, pair discovery

**Files:**
- Modify: `go.mod` (add `github.com/yuin/goldmark v1.8.4`)
- Create: `internal/render/markers.go`
- Create: `internal/render/markers_test.go`
- Create: `internal/render/markdown.go`
- Create: `internal/render/markdown_test.go`
- Create: `internal/render/pair.go`
- Create: `internal/render/pair_test.go`
- Create: `internal/render/regenerate.go`
- Create: `internal/render/regenerate_test.go`

**Interfaces:**
- Consumes: `vault.ParseNote(text string) (*vault.Frontmatter, string)` (existing), `vault.ContainPath(root, target string) (string, error)` (existing), `vault.ReadNote(path string) (string, string, error)` (existing), `vault.WriteNoteAtomic(path string, content []byte, expectedHash string) error` (existing)
- Produces:
  - `type Pair struct { HTMLPath, MDPath, HTMLRel, MDRel string }`
  - `func FindPairedFiles(vaultDir string) ([]Pair, error)`
  - `func RenderMarkdownBody(mdContent string) (string, error)`
  - `func Regenerate(p Pair, now time.Time) error`
  - `var ErrNoMarkers error`
  - `var ErrSourceMissing error`

- [ ] **Step 1: Add the goldmark dependency**

```bash
cd ~/workspace/obsidian-vault-tools
```

Edit `go.mod`'s main `require (...)` block (the one WITHOUT `// indirect` comments) to add, alphabetically among the existing entries:

```
	github.com/yuin/goldmark v1.8.4
```

Then run:

```bash
go mod tidy
```

Expected: `go.mod`/`go.sum` update with `github.com/yuin/goldmark v1.8.4` and no new indirect entries (goldmark's own `go.mod` requires nothing beyond `go 1.22`).

- [ ] **Step 2: Write the failing marker-splicing tests**

```go
// internal/render/markers_test.go
package render

import (
	"strings"
	"testing"
)

// CONTRACT(#118): the body between START/END markers is replaced;
// everything outside is preserved byte-for-byte.
func TestSpliceBodyReplacesBetweenMarkers(t *testing.T) {
	html := "<html><head></head><body>\n<!-- RENDER_BODY_START -->old body<!-- RENDER_BODY_END -->\n</body></html>"
	got, err := spliceBody(html, "<p>new</p>")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<!-- RENDER_BODY_START -->\n<p>new</p>\n<!-- RENDER_BODY_END -->") {
		t.Errorf("body not spliced correctly:\n%s", got)
	}
	if strings.Contains(got, "old body") {
		t.Errorf("old body survived splice:\n%s", got)
	}
	if !strings.HasPrefix(got, "<html><head></head><body>\n") || !strings.HasSuffix(got, "\n</body></html>") {
		t.Errorf("content outside markers was not preserved:\n%s", got)
	}
}

// CONTRACT(#118): a missing START or END marker returns ErrNoMarkers
// ("skip with warning" — the caller decides how to report it).
func TestSpliceBodyMissingMarkersErrors(t *testing.T) {
	for name, html := range map[string]string{
		"no markers at all": "<html><body>plain</body></html>",
		"start only":        "<!-- RENDER_BODY_START -->only start",
		"end only":          "only end<!-- RENDER_BODY_END -->",
		"end before start":  "<!-- RENDER_BODY_END --><!-- RENDER_BODY_START -->",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := spliceBody(html, "x"); err == nil {
				t.Error("expected ErrNoMarkers, got nil")
			}
		})
	}
}

// BUG(fixed)(#120): a generated HTML body containing sequences that
// look like a regex-replacement backreference — Python's "\1"/"\g<name>"
// AND Go's own "$1"/"${name}" replacement-string syntax — survives the
// splice completely unchanged. This is the dedicated regression test
// for row #120: spliceBody must never pass newBody through ANY
// regexp.*Replace* API as the replacement argument.
func TestSpliceBodyPreservesBackreferenceLookingContent(t *testing.T) {
	html := "<!-- RENDER_BODY_START -->old<!-- RENDER_BODY_END -->"
	body := `Prices: \1 dollars, a group ref \g<name>, and Go-style $1 and ${name} and $$ too.`
	got, err := spliceBody(html, body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, body) {
		t.Errorf("spliceBody corrupted backreference-looking content:\ngot:  %q\nwant substring: %q", got, body)
	}
}

// CONTRACT(#119): a first render (no existing RENDER_TIMESTAMP comment)
// inserts one immediately after RENDER_SOURCE.
func TestSpliceTimestampInsertsAfterSourceOnFirstRender(t *testing.T) {
	html := "<!-- RENDER_SOURCE: guide.md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->"
	got := spliceTimestamp(html, "<!-- RENDER_TIMESTAMP: 2026-07-15 -->")
	want := "<!-- RENDER_SOURCE: guide.md -->\n<!-- RENDER_TIMESTAMP: 2026-07-15 -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->"
	if got != want {
		t.Errorf("spliceTimestamp =\n%q\nwant\n%q", got, want)
	}
}

// CONTRACT(#119): a re-render (existing RENDER_TIMESTAMP comment)
// updates it in place rather than duplicating it.
func TestSpliceTimestampUpdatesExisting(t *testing.T) {
	html := "<!-- RENDER_SOURCE: guide.md -->\n<!-- RENDER_TIMESTAMP: 2020-01-01 -->\nbody"
	got := spliceTimestamp(html, "<!-- RENDER_TIMESTAMP: 2026-07-15 -->")
	want := "<!-- RENDER_SOURCE: guide.md -->\n<!-- RENDER_TIMESTAMP: 2026-07-15 -->\nbody"
	if got != want {
		t.Errorf("spliceTimestamp =\n%q\nwant\n%q", got, want)
	}
	if strings.Count(got, "RENDER_TIMESTAMP") != 1 {
		t.Errorf("expected exactly one RENDER_TIMESTAMP comment, got:\n%s", got)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/render/... -run 'TestSplice' -v`
Expected: FAIL (build failure) — package `render` and `spliceBody`/`spliceTimestamp` don't exist yet.

- [ ] **Step 4: Write the marker-splicing implementation**

```go
// internal/render/markers.go
//
// Package render: goldmark markdown→HTML conversion and RENDER_SOURCE/
// RENDER_BODY/RENDER_TIMESTAMP marker parsing+splicing — a port of
// render_html.py's HTML-pairing mechanism (behavior inventory rows
// #117-119), with row #120's regex-replacement-template bug closed by
// construction: every splice in this file is string index/concatenation
// based, never a regex Replace call fed generated content as the
// replacement argument (Go's regexp.ReplaceAllString treats "$1"/
// "${name}" specially in its replacement string — the exact same bug
// CLASS as Python's re.sub "\1"/"\g<...>", just different
// metacharacters — a generated HTML body containing either family of
// sequence must survive unchanged).
package render

import (
	"errors"
	"regexp"
)

var (
	renderSourceRe    = regexp.MustCompile(`<!--\s*RENDER_SOURCE:\s*(.+?)\s*-->`)
	renderBodyStartRe = regexp.MustCompile(`<!--\s*RENDER_BODY_START\s*-->`)
	renderBodyEndRe   = regexp.MustCompile(`<!--\s*RENDER_BODY_END\s*-->`)
	renderTSRe        = regexp.MustCompile(`<!--\s*RENDER_TIMESTAMP:\s*.+?\s*-->`)
)

// ErrNoMarkers is returned when an HTML file is missing either the
// RENDER_BODY_START or RENDER_BODY_END comment (row #118: "missing
// markers -> skip with warning").
var ErrNoMarkers = errors.New("no RENDER_BODY_START/RENDER_BODY_END markers found")

// spliceBody replaces the content between the RENDER_BODY_START and
// RENDER_BODY_END markers in htmlText with newBody, via direct index
// slicing and string concatenation — NEVER a regexp.ReplaceAllString
// call with newBody as the replacement argument (row #120's fix: a
// generated HTML body containing a literal "\1", "\g<name>", or Go's
// own "$1"/"${name}" replacement-syntax lookalikes must survive
// unchanged; passing it through ANY regex-replacement-template API,
// Python's or Go's, would risk exactly that corruption).
func spliceBody(htmlText, newBody string) (string, error) {
	startLoc := renderBodyStartRe.FindStringIndex(htmlText)
	endLoc := renderBodyEndRe.FindStringIndex(htmlText)
	if startLoc == nil || endLoc == nil || endLoc[0] < startLoc[1] {
		return "", ErrNoMarkers
	}
	return htmlText[:startLoc[1]] + "\n" + newBody + "\n" + htmlText[endLoc[0]:], nil
}

// spliceTimestamp updates an existing RENDER_TIMESTAMP comment in
// place, or inserts one immediately after the RENDER_SOURCE comment on
// first render (row #119). timestampComment is a fully-formatted
// comment string built by the caller (regenerate.go) from a computed
// date — never note-content-derived, so this function carries no row
// #120-class risk on its own; kept in the same index/concatenation
// style as spliceBody for consistency.
func spliceTimestamp(htmlText, timestampComment string) string {
	if loc := renderTSRe.FindStringIndex(htmlText); loc != nil {
		return htmlText[:loc[0]] + timestampComment + htmlText[loc[1]:]
	}
	if loc := renderSourceRe.FindStringIndex(htmlText); loc != nil {
		return htmlText[:loc[1]] + "\n" + timestampComment + htmlText[loc[1]:]
	}
	return htmlText
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/render/... -run 'TestSplice' -v`
Expected: PASS.

- [ ] **Step 6: Write the failing goldmark-conversion tests**

```go
// internal/render/markdown_test.go
package render

import (
	"strings"
	"testing"
)

// CONTRACT: frontmatter is stripped before conversion — it must never
// leak into the rendered HTML body.
func TestRenderMarkdownBodyStripsFrontmatter(t *testing.T) {
	md := "---\ntype: note\nsecret: do-not-leak\n---\n# Title\n\nSome *text*.\n"
	got, err := RenderMarkdownBody(md)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "secret") || strings.Contains(got, "do-not-leak") {
		t.Errorf("frontmatter leaked into rendered body: %s", got)
	}
}

// CONTRACT: goldmark converts headings and emphasis (design spec
// §Architecture: "ov render | port (goldmark)").
func TestRenderMarkdownBodyConvertsBasics(t *testing.T) {
	got, err := RenderMarkdownBody("# Title\n\nSome *text* and **bold**.\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<h1") {
		t.Errorf("expected an <h1>, got: %s", got)
	}
	if !strings.Contains(got, "<em>text</em>") || !strings.Contains(got, "<strong>bold</strong>") {
		t.Errorf("expected emphasis/strong conversion, got: %s", got)
	}
}

// CONTRACT: a note with no frontmatter at all still converts normally.
func TestRenderMarkdownBodyNoFrontmatter(t *testing.T) {
	got, err := RenderMarkdownBody("# Just a title\n\nbody text\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<h1") || !strings.Contains(got, "body text") {
		t.Errorf("got: %s", got)
	}
}
```

- [ ] **Step 7: Run tests to verify they fail**

Run: `go test ./internal/render/... -run TestRenderMarkdownBody -v`
Expected: FAIL — `RenderMarkdownBody` undefined.

- [ ] **Step 8: Write the goldmark-conversion implementation**

```go
// internal/render/markdown.go
package render

import (
	"bytes"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/yuin/goldmark"
)

var md = goldmark.New()

// RenderMarkdownBody strips the note's YAML frontmatter (via
// vault.ParseNote — reusing the same lossless frontmatter detection the
// rest of the codebase uses, rather than hand-rolling a second "---"
// scanner) and converts the remaining body to an HTML fragment via
// goldmark's default configuration (design spec §Architecture: "ov
// render | port (goldmark)"; §Locked decisions pins goldmark, no
// extensions beyond default). Deliberately NOT a byte-for-byte port of
// render_html.py's hand-rolled md_to_html_body (rows #117-119 cover
// only the marker-splicing MECHANISM as a contract; the generated
// HTML's exact bytes are explicitly out of the compatibility contract —
// render is not in the frozen CLI subset, design spec §Compatibility
// contract).
func RenderMarkdownBody(mdContent string) (string, error) {
	_, body := vault.ParseNote(mdContent)
	var buf bytes.Buffer
	if err := md.Convert([]byte(body), &buf); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}
```

- [ ] **Step 9: Run tests to verify they pass**

Run: `go test ./internal/render/... -run TestRenderMarkdownBody -v`
Expected: PASS.

- [ ] **Step 10: Write the failing pair-discovery tests**

```go
// internal/render/pair_test.go
package render

import (
	"os"
	"path/filepath"
	"testing"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// CONTRACT(#117): only HTML files carrying a RENDER_SOURCE comment are
// discovered; unmarked HTML files are invisible to render.
func TestFindPairedFilesDiscoversMarkedHTMLOnly(t *testing.T) {
	vault := t.TempDir()
	mustWriteFile(t, filepath.Join(vault, "guide.md"), "# Guide\n")
	mustWriteFile(t, filepath.Join(vault, "guide.html"),
		"<!-- RENDER_SOURCE: guide.md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->")
	mustWriteFile(t, filepath.Join(vault, "plain.html"), "<html><body>no marker</body></html>")

	pairs, err := FindPairedFiles(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 {
		t.Fatalf("got %d pairs, want 1: %+v", len(pairs), pairs)
	}
	if pairs[0].HTMLRel != "guide.html" || pairs[0].MDRel != "guide.md" {
		t.Errorf("pair = %+v", pairs[0])
	}
}

// BUG(fixed): a RENDER_SOURCE value that would traverse outside the
// vault is skipped, not fatal — same containment posture as row #153,
// applied to this READ path.
func TestFindPairedFilesSkipsTraversalUnsafeSource(t *testing.T) {
	vault := t.TempDir()
	mustWriteFile(t, filepath.Join(vault, "evil.html"), "<!-- RENDER_SOURCE: ../../etc/passwd -->")

	pairs, err := FindPairedFiles(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 0 {
		t.Errorf("expected traversal-unsafe RENDER_SOURCE to be skipped, got %+v", pairs)
	}
}

// CONTRACT: results are sorted by HTML vault-relative path (deterministic,
// same discipline as row #156's `mocs cleanup --all` ordering).
func TestFindPairedFilesSortedByHTMLRel(t *testing.T) {
	vault := t.TempDir()
	mustWriteFile(t, filepath.Join(vault, "b.md"), "# B\n")
	mustWriteFile(t, filepath.Join(vault, "b.html"), "<!-- RENDER_SOURCE: b.md -->")
	mustWriteFile(t, filepath.Join(vault, "a.md"), "# A\n")
	mustWriteFile(t, filepath.Join(vault, "a.html"), "<!-- RENDER_SOURCE: a.md -->")

	pairs, err := FindPairedFiles(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 || pairs[0].HTMLRel != "a.html" || pairs[1].HTMLRel != "b.html" {
		t.Errorf("pairs not sorted: %+v", pairs)
	}
}

// A missing MD source is a discovery-time non-error — Regenerate is
// where a missing source becomes an error; FindPairedFiles only
// resolves the intended path.
func TestFindPairedFilesToleratesMissingSource(t *testing.T) {
	vault := t.TempDir()
	mustWriteFile(t, filepath.Join(vault, "orphaned.html"), "<!-- RENDER_SOURCE: missing.md -->")

	pairs, err := FindPairedFiles(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || pairs[0].MDRel != "missing.md" {
		t.Errorf("pairs = %+v", pairs)
	}
}
```

- [ ] **Step 11: Run tests to verify they fail**

Run: `go test ./internal/render/... -run TestFindPairedFiles -v`
Expected: FAIL — `Pair`/`FindPairedFiles` undefined.

- [ ] **Step 12: Write the pair-discovery implementation**

```go
// internal/render/pair.go
package render

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// Pair is one HTML<->Markdown render pairing discovered via an HTML
// file's RENDER_SOURCE comment (row #117).
type Pair struct {
	HTMLPath, MDPath string // absolute, symlink-resolved
	HTMLRel, MDRel   string // vault-relative, forward-slash
}

// FindPairedFiles walks vaultDir for every *.html file containing a
// RENDER_SOURCE comment and resolves its paired Markdown source (row
// #117). Walk-resilient (design spec §core contracts): unreadable
// files/directories are skipped, never fatal. A RENDER_SOURCE value
// that resolves outside the vault (traversal-unsafe) is skipped, not
// fatal — the same containment posture mocs new/add established (row
// #153) applied here to a READ path rather than a write. vaultDir is
// EvalSymlinks'd once up front so every returned Rel is computed
// against the SAME resolved root Regenerate/vault.ContainPath use
// internally — avoiding the garbled "../../private/tmp/..."-style path
// class of bug found and fixed during phase 4's post-review manual
// testing (.superpowers/sdd/progress.md, Phase 4 "Post-review manual
// testing"): that mistake is prevented here proactively instead of
// waiting for another smoke test to catch it.
func FindPairedFiles(vaultDir string) ([]Pair, error) {
	vaultReal, err := filepath.EvalSymlinks(vaultDir)
	if err != nil {
		return nil, err
	}

	var pairs []Pair
	walkErr := filepath.WalkDir(vaultReal, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // walk resilience: skip, never fatal
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".html") {
			return nil
		}
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		m := renderSourceRe.FindStringSubmatch(string(content))
		if m == nil {
			return nil
		}
		mdAbs, cerr := vault.ContainPath(vaultReal, m[1])
		if cerr != nil {
			return nil // traversal-unsafe RENDER_SOURCE value: skip this file, keep walking
		}
		htmlRel, rerr := filepath.Rel(vaultReal, path)
		if rerr != nil {
			return nil
		}
		mdRel, rerr := filepath.Rel(vaultReal, mdAbs)
		if rerr != nil {
			return nil
		}
		pairs = append(pairs, Pair{
			HTMLPath: path,
			MDPath:   mdAbs,
			HTMLRel:  filepath.ToSlash(htmlRel),
			MDRel:    filepath.ToSlash(mdRel),
		})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].HTMLRel < pairs[j].HTMLRel })
	return pairs, nil
}
```

- [ ] **Step 13: Run tests to verify they pass**

Run: `go test ./internal/render/... -run TestFindPairedFiles -v`
Expected: PASS.

- [ ] **Step 14: Write the failing Regenerate tests**

```go
// internal/render/regenerate_test.go
package render

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// CONTRACT(#117-119): a full regenerate cycle — markdown converts,
// splices between the markers, stamps a fresh RENDER_TIMESTAMP, and
// writes atomically.
func TestRegenerateSplicesAndWrites(t *testing.T) {
	vault := t.TempDir()
	mdPath := filepath.Join(vault, "guide.md")
	htmlPath := filepath.Join(vault, "guide.html")
	mustWriteFile(t, mdPath, "# Guide\n\nHello *world*.\n")
	mustWriteFile(t, htmlPath, "<!-- RENDER_SOURCE: guide.md -->\n<!-- RENDER_BODY_START -->stale<!-- RENDER_BODY_END -->\n")

	p := Pair{HTMLPath: htmlPath, MDPath: mdPath, HTMLRel: "guide.html", MDRel: "guide.md"}
	if err := Regenerate(p, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	got := mustReadFile(t, htmlPath)
	if strings.Contains(got, "stale") {
		t.Errorf("stale body survived regenerate: %s", got)
	}
	if !strings.Contains(got, "<em>world</em>") {
		t.Errorf("expected converted markdown body, got: %s", got)
	}
	if !strings.Contains(got, "<!-- RENDER_TIMESTAMP: 2026-07-15 -->") {
		t.Errorf("expected a fresh RENDER_TIMESTAMP, got: %s", got)
	}
}

// CONTRACT: a missing Markdown source is a reported, non-fatal-to-the-
// batch error.
func TestRegenerateMissingSourceErrors(t *testing.T) {
	vault := t.TempDir()
	htmlPath := filepath.Join(vault, "guide.html")
	mustWriteFile(t, htmlPath, "<!-- RENDER_SOURCE: missing.md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->")

	p := Pair{HTMLPath: htmlPath, MDPath: filepath.Join(vault, "missing.md"), HTMLRel: "guide.html", MDRel: "missing.md"}
	err := Regenerate(p, time.Now())
	if !errors.Is(err, ErrSourceMissing) {
		t.Errorf("err = %v, want ErrSourceMissing", err)
	}
}

// CONTRACT(#118): missing RENDER_BODY markers -> ErrNoMarkers, "skip
// with warning" (the cmd layer decides how to report/continue).
func TestRegenerateMissingMarkersErrors(t *testing.T) {
	vault := t.TempDir()
	mdPath := filepath.Join(vault, "guide.md")
	htmlPath := filepath.Join(vault, "guide.html")
	mustWriteFile(t, mdPath, "# Guide\n")
	mustWriteFile(t, htmlPath, "<!-- RENDER_SOURCE: guide.md -->\n<html>no markers here</html>")

	p := Pair{HTMLPath: htmlPath, MDPath: mdPath, HTMLRel: "guide.html", MDRel: "guide.md"}
	err := Regenerate(p, time.Now())
	if !errors.Is(err, ErrNoMarkers) {
		t.Errorf("err = %v, want ErrNoMarkers", err)
	}
}

// CONTRACT(#119): a second regenerate updates the existing
// RENDER_TIMESTAMP in place instead of duplicating it.
func TestRegenerateUpdatesExistingTimestamp(t *testing.T) {
	vault := t.TempDir()
	mdPath := filepath.Join(vault, "guide.md")
	htmlPath := filepath.Join(vault, "guide.html")
	mustWriteFile(t, mdPath, "# Guide\n")
	mustWriteFile(t, htmlPath, "<!-- RENDER_SOURCE: guide.md -->\n<!-- RENDER_TIMESTAMP: 2020-01-01 -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->")

	p := Pair{HTMLPath: htmlPath, MDPath: mdPath, HTMLRel: "guide.html", MDRel: "guide.md"}
	if err := Regenerate(p, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	got := mustReadFile(t, htmlPath)
	if strings.Count(got, "RENDER_TIMESTAMP") != 1 {
		t.Errorf("expected exactly one RENDER_TIMESTAMP, got: %s", got)
	}
	if !strings.Contains(got, "2026-07-15") || strings.Contains(got, "2020-01-01") {
		t.Errorf("timestamp not updated: %s", got)
	}
}
```

- [ ] **Step 15: Run tests to verify they fail**

Run: `go test ./internal/render/... -run TestRegenerate -v`
Expected: FAIL — `Regenerate`/`ErrSourceMissing` undefined.

- [ ] **Step 16: Write the Regenerate implementation**

```go
// internal/render/regenerate.go
package render

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// ErrSourceMissing is returned when a Pair's Markdown source no longer
// exists on disk.
var ErrSourceMissing = errors.New("markdown source not found")

// Regenerate rebuilds p.HTMLPath's spliced body from p.MDPath's current
// content. Mirrors render_html.py's regenerate() (rows #117-120):
// missing source -> ErrSourceMissing; missing RENDER_BODY markers ->
// ErrNoMarkers (caller reports and continues, never fatal to a batch
// run); the write is via vault.WriteNoteAtomic conditional on the hash
// captured when p.HTMLPath was read at the start of this call — fixing
// v1's plain non-atomic html_path.write_text (row #163, same defect
// family as #42/#101/#116/#128/#129/#160).
func Regenerate(p Pair, now time.Time) error {
	if _, err := os.Stat(p.MDPath); err != nil {
		return fmt.Errorf("%s: %w", p.MDRel, ErrSourceMissing)
	}
	mdContent, err := os.ReadFile(p.MDPath)
	if err != nil {
		return fmt.Errorf("%s: %w", p.MDRel, err)
	}
	htmlContent, hash, err := vault.ReadNote(p.HTMLPath)
	if err != nil {
		return fmt.Errorf("%s: %w", p.HTMLRel, err)
	}
	if !renderBodyStartRe.MatchString(htmlContent) || !renderBodyEndRe.MatchString(htmlContent) {
		return fmt.Errorf("%s: %w", p.HTMLRel, ErrNoMarkers)
	}

	body, err := RenderMarkdownBody(string(mdContent))
	if err != nil {
		return fmt.Errorf("%s: %w", p.MDRel, err)
	}

	spliced, err := spliceBody(htmlContent, body)
	if err != nil {
		return fmt.Errorf("%s: %w", p.HTMLRel, err)
	}
	timestamp := fmt.Sprintf("<!-- RENDER_TIMESTAMP: %s -->", now.Format("2006-01-02"))
	spliced = spliceTimestamp(spliced, timestamp)

	return vault.WriteNoteAtomic(p.HTMLPath, []byte(spliced), hash)
}
```

- [ ] **Step 17: Run tests to verify they pass**

Run: `go test ./internal/render/... -v`
Expected: PASS, full package green.

- [ ] **Step 18: Commit**

```bash
git add go.mod go.sum internal/render/
git commit -m "Add internal/render: goldmark conversion, RENDER_SOURCE/BODY/TIMESTAMP marker splicing (row #120 fix)"
```

---

### Task 2: `cmd/ov render`

**Files:**
- Create: `cmd/ov/render.go`
- Create: `cmd/ov/render_test.go`
- Modify: `cmd/ov/root.go`

**Interfaces:**
- Consumes: `render.FindPairedFiles`, `render.Regenerate`, `render.Pair` (Task 1); `resolveConfig` (existing, `cmd/ov/common.go`); `vault.ContainPath` (existing)
- Produces: `func newRenderCmd() *cobra.Command`; `func runRender(cfg *config.Config, file string, all bool, in *bufio.Reader, out, errw io.Writer) error` (test-only surface)

- [ ] **Step 1: Write the failing tests**

```go
// cmd/ov/render_test.go
package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CONTRACT: a <file> argument regenerates exactly that pair.
func TestRenderSingleFile(t *testing.T) {
	vaultDir := newVaultFixture(t)
	mdPath := filepath.Join(vaultDir, "03-Resources", "guide.md")
	htmlPath := filepath.Join(vaultDir, "03-Resources", "guide.html")
	if err := os.WriteFile(mdPath, []byte("# Guide\n\nHello.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(htmlPath, []byte("<!-- RENDER_SOURCE: 03-Resources/guide.md -->\n<!-- RENDER_BODY_START -->stale<!-- RENDER_BODY_END -->"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runCmd(t, "render", "03-Resources/guide.html", "--vault", vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != "03-Resources/guide.html" {
		t.Errorf("stdout = %q", stdout)
	}
	got, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "stale") {
		t.Errorf("stale content survived: %s", got)
	}
}

// CONTRACT: --all regenerates every discovered pair.
func TestRenderAllFlag(t *testing.T) {
	vaultDir := newVaultFixture(t)
	for _, name := range []string{"a", "b"} {
		mdPath := filepath.Join(vaultDir, "03-Resources", name+".md")
		htmlPath := filepath.Join(vaultDir, "03-Resources", name+".html")
		if err := os.WriteFile(mdPath, []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(htmlPath, []byte("<!-- RENDER_SOURCE: 03-Resources/"+name+".md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stdout, _, err := runCmd(t, "render", "--all", "--vault", vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Fields(stdout)) != 2 {
		t.Fatalf("stdout = %q, want 2 lines", stdout)
	}
}

// BUG(fixed)(#162): --all does not stop at the first failure — it
// processes every pair and reports both the successes and the failure.
func TestRenderAllContinuesPastFailure(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "bad.html"), []byte("<!-- RENDER_SOURCE: 03-Resources/missing.md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "good.md"), []byte("# Good\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "good.html"), []byte("<!-- RENDER_SOURCE: 03-Resources/good.md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runCmd(t, "render", "--all", "--vault", vaultDir)
	if err == nil {
		t.Fatal("expected a non-nil error because one of two pairs failed")
	}
	if !strings.Contains(stdout, "good.html") {
		t.Errorf("expected good.html to still be processed despite bad.html's failure, stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stderr, "2 file(s) processed (1 ok, 1 failed)") {
		t.Errorf("expected an accurate ok/failed summary, stderr=%q", stderr)
	}
}

// CONTRACT: no pairs found prints a message and exits 0.
func TestRenderNoPairsFound(t *testing.T) {
	vaultDir := newVaultFixture(t)
	_, stderr, err := runCmd(t, "render", "--vault", vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "No paired HTML files found") {
		t.Errorf("stderr = %q", stderr)
	}
}

// BUG(fixed): a file argument that attempts to traverse outside the
// vault is rejected, not silently resolved.
func TestRenderFileArgTraversalRejected(t *testing.T) {
	vaultDir := newVaultFixture(t)
	_, _, err := runCmd(t, "render", "../../etc/passwd", "--vault", vaultDir)
	if err == nil {
		t.Fatal("expected an error for a traversal-attempting file argument")
	}
}

// CONTRACT: with no args and no --all, an interactive numbered picker
// (row #69's per-command resolution) reads a plain choice from stdin —
// mirrors v1's own non-gum picker (render_html.py:369-389).
func TestRenderInteractivePickerNumberSelection(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "only.md"), []byte("# Only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "only.html"), []byte("<!-- RENDER_SOURCE: 03-Resources/only.md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var out, errw bytes.Buffer
	if err := runRender(cfg, "", false, bufio.NewReader(strings.NewReader("1\n")), &out, &errw); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "03-Resources/only.html" {
		t.Errorf("out = %q", out.String())
	}
}

// CONTRACT: the interactive picker's "q" choice (and EOF) cancels
// cleanly without regenerating anything.
func TestRenderInteractivePickerQuit(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "only.md"), []byte("# Only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "only.html"), []byte("<!-- RENDER_SOURCE: 03-Resources/only.md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var out, errw bytes.Buffer
	if err := runRender(cfg, "", false, bufio.NewReader(strings.NewReader("q\n")), &out, &errw); err != nil {
		t.Fatal(err)
	}
	if out.String() != "" {
		t.Errorf("expected nothing regenerated, out = %q", out.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ov/... -run TestRender -v`
Expected: FAIL — `render` is an unknown subcommand; `runRender` undefined.

- [ ] **Step 3: Write the implementation**

```go
// cmd/ov/render.go
package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/arosenkranz/obsidian-vault-tools/internal/render"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

func newRenderCmd() *cobra.Command {
	var vaultFlag string
	var all bool
	cmd := &cobra.Command{
		Use:   "render [<file>]",
		Short: "Regenerate HTML guide(s) from their paired Markdown source",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			var file string
			if len(args) == 1 {
				file = args[0]
			}
			return runRender(cfg, file, all, bufio.NewReader(cmd.InOrStdin()), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	cmd.Flags().BoolVar(&all, "all", false, "regenerate every paired HTML file")
	return cmd
}

// runRender is the testable core of `ov2 render`. Three modes, matching
// render_html.py's own dispatch order (rows #117-120): a <file>
// argument (resolved through vault.ContainPath — <file> IS meant to be
// vault-relative, unlike publish's arbitrary local path) regenerates
// exactly that one pair; --all regenerates every discovered pair; with
// neither, an interactive numbered picker mirrors v1's own non-gum
// picker (render_html.py:369-389) — plain stdin prompt, no bubbletea.
// Unlike v1's `all(regenerate(...) for ...)` (row #162's BUG: silently
// stops at the first failure while still claiming every pair was
// processed), --all and the "a" choice here always process every pair
// and report an accurate ok/failed count.
func runRender(cfg *config.Config, file string, all bool, in *bufio.Reader, out, errw io.Writer) error {
	pairs, err := render.FindPairedFiles(cfg.VaultDir)
	if err != nil {
		return err
	}

	regenOne := func(p render.Pair) error {
		if err := render.Regenerate(p, time.Now()); err != nil {
			fmt.Fprintf(errw, "✗ %s: %v\n", p.HTMLRel, err)
			return err
		}
		fmt.Fprintf(errw, "✓ Rendered: %s ← %s\n", p.HTMLRel, p.MDRel)
		fmt.Fprintln(out, p.HTMLRel)
		return nil
	}

	regenAll := func(targets []render.Pair) error {
		var ok, failed int
		for _, p := range targets {
			if err := regenOne(p); err != nil {
				failed++
			} else {
				ok++
			}
		}
		fmt.Fprintf(errw, "\nDone. %d file(s) processed (%d ok, %d failed).\n", len(targets), ok, failed)
		if failed > 0 {
			return fmt.Errorf("%d of %d file(s) failed to render", failed, len(targets))
		}
		return nil
	}

	if file != "" {
		target, err := vault.ContainPath(cfg.VaultDir, file)
		if err != nil {
			return err
		}
		for _, p := range pairs {
			if p.HTMLPath == target {
				return regenOne(p)
			}
		}
		return fmt.Errorf("no RENDER_SOURCE comment found in %s (or file not tracked)", file)
	}

	if all {
		if len(pairs) == 0 {
			fmt.Fprintln(errw, "No paired HTML files found in vault.")
			return nil
		}
		return regenAll(pairs)
	}

	if len(pairs) == 0 {
		fmt.Fprintln(errw, "No paired HTML files found in vault.")
		return nil
	}
	fmt.Fprintln(errw, "Paired HTML guides:")
	for i, p := range pairs {
		fmt.Fprintf(errw, "  [%d] %s\n       ← %s\n", i+1, p.HTMLRel, p.MDRel)
	}
	fmt.Fprint(errw, "\n  [a] All  [q] Quit\n\nRegenerate which? ")
	line, readErr := in.ReadString('\n')
	choice := strings.ToLower(strings.TrimSpace(line))
	if readErr != nil && choice == "" {
		choice = "q"
	}
	switch {
	case choice == "" || choice == "q":
		return nil
	case choice == "a":
		return regenAll(pairs)
	default:
		n, convErr := strconv.Atoi(choice)
		if convErr != nil || n < 1 || n > len(pairs) {
			return fmt.Errorf("invalid choice: %q", choice)
		}
		return regenOne(pairs[n-1])
	}
}
```

Update `cmd/ov/root.go`'s `root.AddCommand(...)` call to add `newRenderCmd()`:

```go
root.AddCommand(newConfigCmd(), newDoctorCmd(), newInitCmd(), newInboxCmd(), newStaleCmd(), newReviewCmd(), newMocsCmd(), newCaptureCmd(), newTriageCmd(), newServeCmd(), newRenderCmd())
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ov/... -v`
Expected: PASS, full package green.

- [ ] **Step 5: Commit**

```bash
git add cmd/ov/render.go cmd/ov/render_test.go cmd/ov/root.go
git commit -m "Add ov2 render: file/--all/interactive dispatch over internal/render"
```

---

### Task 3: `internal/publish` — prompt, LLM convert, slug, rsync/ssh push/remove/list

**Files:**
- Create: `internal/publish/prompt.go`
- Create: `internal/publish/prompt_test.go`
- Create: `internal/publish/testdata/prompt_golden.txt` (generated by Step 4)
- Create: `internal/publish/convert.go`
- Create: `internal/publish/convert_test.go`
- Create: `internal/publish/slug.go`
- Create: `internal/publish/slug_test.go`
- Create: `internal/publish/push.go`
- Create: `internal/publish/remove.go`
- Create: `internal/publish/list.go`
- Create: `internal/publish/transport_test.go`

**Interfaces:**
- Consumes: `llm.ExtractHTMLBlock(text string) string` (existing, built in phase 3, first real consumer)
- Produces:
  - `func BuildPrompt(noteContent, guidance string) string`
  - `type Runner interface { Run(ctx context.Context, prompt string) (string, error) }`
  - `func Convert(ctx context.Context, runner Runner, noteContent, guidance string) (string, error)`
  - `func Slug(stem string) string`
  - `type Pusher interface { Push(ctx context.Context, localPath, host, remotePath string) error }`; `type RsyncPusher struct{}`
  - `type Remover interface { Remove(ctx context.Context, host, remotePath, basename string) error }`; `type SSHRemover struct{}`
  - `type Lister interface { List(ctx context.Context, host, remotePath string) ([]string, error) }`; `type SSHLister struct{}`

- [ ] **Step 1: Write the failing prompt tests**

```go
// internal/publish/prompt_test.go
package publish

import (
	"os"
	"strings"
	"testing"
)

// Golden file (design spec §Testing strategy tier 2: "both LLM prompt
// assemblies" — triage's is phase 3's, moc cleanup's is phase 4's; this
// is publish --llm's, the THIRD and final prompt-builder package).
// Generated by Step 4 below via UPDATE_GOLDEN=1; update deliberately,
// never silently, when the prompt text intentionally changes.
func TestBuildPromptGolden(t *testing.T) {
	got := BuildPrompt("# My Note\n\nSome content.\n", "clean, modern design with good typography and readable line lengths")

	goldenPath := "testdata/prompt_golden.txt"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("BuildPrompt drifted from golden file.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// CONTRACT(vault.sh:1128): an empty guidance falls back to the default
// design guidance text.
func TestBuildPromptDefaultGuidance(t *testing.T) {
	got := BuildPrompt("content", "")
	if !strings.Contains(got, "clean, modern design with good typography and readable line lengths") {
		t.Errorf("expected default guidance text, got:\n%s", got)
	}
}

// CONTRACT(vault.sh:1138-1141): the prompt states the single-file /
// inline-everything / HTML-only-output rules.
func TestBuildPromptStatesRules(t *testing.T) {
	got := BuildPrompt("content", "custom guidance")
	if !strings.Contains(got, "custom guidance") {
		t.Error("prompt missing the custom guidance text")
	}
	lower := strings.ToLower(got)
	if !strings.Contains(lower, "self-contained") {
		t.Error("prompt missing the self-contained-HTML rule")
	}
	if !strings.Contains(lower, "inline all css and js") {
		t.Error("prompt missing the inline-CSS/JS rule")
	}
	if !strings.Contains(lower, "no markdown, no code fences") {
		t.Error("prompt missing the HTML-only-output rule")
	}
}

// CONTRACT: the full note content is embedded verbatim.
func TestBuildPromptIncludesFullContent(t *testing.T) {
	note := "# Title\n\nBody with **markdown** and a [[wikilink]].\n"
	got := BuildPrompt(note, "guidance")
	if !strings.Contains(got, note) {
		t.Error("prompt does not contain the full note content verbatim")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/publish/... -run TestBuildPrompt -v`
Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Write the prompt implementation**

```go
// internal/publish/prompt.go
//
// Package publish: LLM->HTML prompt assembly and decode, output
// slugification, and rsync/ssh push/remove/list behind injectable
// interfaces (design spec §Architecture: "internal/publish/ LLM→HTML
// convert; rsync/ssh push/remove/list-remote via subprocess"). The
// THIRD, independent prompt-builder package after internal/triage and
// internal/moc — this one's schema is raw HTML, not JSON, so it is
// deliberately NOT forced through any shared abstraction with those two.
package publish

import "fmt"

// defaultGuidance ports vault.sh publish_doc's default --desc value
// (vault.sh:1128).
const defaultGuidance = "clean, modern design with good typography and readable line lengths"

// promptTemplate ports vault.sh publish_doc's --llm prompt assembly
// verbatim (vault.sh:1137-1144, row #72's prompt CONTENT — only the
// eval-based dispatch is fixed there, not the prompt text itself).
const promptTemplate = "Convert this Obsidian markdown note into a complete, self-contained HTML file.\n" +
	"Design guidance: %s\n" +
	"Rules: single file, inline all CSS and JS, no external dependencies.\n" +
	"Return ONLY the HTML — no markdown, no code fences, no explanation.\n" +
	"\n" +
	"---\n" +
	"%s"

// BuildPrompt assembles the publish --llm prompt for one note. An empty
// guidance falls back to defaultGuidance (row #71's --desc default).
func BuildPrompt(noteContent, guidance string) string {
	if guidance == "" {
		guidance = defaultGuidance
	}
	return fmt.Sprintf(promptTemplate, guidance, noteContent)
}
```

- [ ] **Step 4: Run tests to verify they pass, generating the golden file**

Run:
```bash
UPDATE_GOLDEN=1 go test ./internal/publish/... -run TestBuildPromptGolden -v
go test ./internal/publish/... -run TestBuildPrompt -v
```
Expected: both PASS; `internal/publish/testdata/prompt_golden.txt` now exists.

- [ ] **Step 5: Write the failing Convert tests**

```go
// internal/publish/convert_test.go
package publish

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	response  string
	err       error
	gotPrompt string
}

func (f *fakeRunner) Run(ctx context.Context, prompt string) (string, error) {
	f.gotPrompt = prompt
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

// CONTRACT(row #74): Convert builds the prompt, calls the runner, and
// decodes the response via llm.ExtractHTMLBlock (the <html>...</html>
// block when present).
func TestConvertExtractsHTMLBlock(t *testing.T) {
	runner := &fakeRunner{response: "Sure, here you go:\n<html><body>Hi</body></html>\nHope that helps!"}
	got, err := Convert(context.Background(), runner, "# Note\n", "guidance")
	if err != nil {
		t.Fatal(err)
	}
	if got != "<html><body>Hi</body></html>" {
		t.Errorf("got = %q", got)
	}
	if !strings.Contains(runner.gotPrompt, "# Note") {
		t.Errorf("prompt missing note content:\n%s", runner.gotPrompt)
	}
}

// CONTRACT(row #74): no <html> block present -> the raw response is
// used, trimmed.
func TestConvertFallsBackToRawResponse(t *testing.T) {
	runner := &fakeRunner{response: "  <div>no html tag here</div>  \n"}
	got, err := Convert(context.Background(), runner, "content", "guidance")
	if err != nil {
		t.Fatal(err)
	}
	if got != "<div>no html tag here</div>" {
		t.Errorf("got = %q", got)
	}
}

// A runner failure propagates so the caller can classify it (e.g.
// llm.ErrAuth).
func TestConvertPropagatesRunnerError(t *testing.T) {
	wantErr := errors.New("llm auth expired")
	runner := &fakeRunner{err: wantErr}
	_, err := Convert(context.Background(), runner, "content", "guidance")
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}
```

- [ ] **Step 6: Run tests to verify they fail**

Run: `go test ./internal/publish/... -run TestConvert -v`
Expected: FAIL — `Convert`/`Runner` undefined.

- [ ] **Step 7: Write the Convert implementation**

```go
// internal/publish/convert.go
package publish

import (
	"context"
	"fmt"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
)

// Runner is the subset of *llm.Runner's method set Convert needs — a
// package-local interface so tests inject a fake without spawning a
// real subprocess (same pattern as internal/moc.Runner/internal/triage's
// own runner seam; deliberately not shared across packages).
type Runner interface {
	Run(ctx context.Context, prompt string) (string, error)
}

// Convert asks the LLM to turn noteContent into a self-contained HTML
// document (BuildPrompt) and decodes the response via
// llm.ExtractHTMLBlock (row #74's contract — the <html>...</html> block
// if present, else the raw response, trimmed). This is publish --llm's
// only consumer of ExtractHTMLBlock, which was built in phase 3 and
// unused in production code until now (design spec §LLM subsystem "Two
// decoders, not one").
func Convert(ctx context.Context, runner Runner, noteContent, guidance string) (string, error) {
	prompt := BuildPrompt(noteContent, guidance)
	raw, err := runner.Run(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("llm html conversion: %w", err)
	}
	return llm.ExtractHTMLBlock(raw), nil
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/publish/... -run TestConvert -v`
Expected: PASS.

- [ ] **Step 9: Write the failing Slug tests**

```go
// internal/publish/slug_test.go
package publish

import "testing"

// CONTRACT(#73): published slug is lowercase + spaces->hyphens + strip
// everything but ASCII [a-z0-9-] — a distinct, documented rule from
// vault.Slugify's case-preserving Unicode-aware note-filename policy
// (row #23).
func TestSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"My Great Note", "my-great-note"},
		{"already-lower-case", "already-lower-case"},
		{"Weird!!! Chars??? Here###", "weird-chars-here"},
		{"  Leading and trailing  ", "--leading-and-trailing--"},
		{"Tabs\tand\nnewlines", "tabsandnewlines"},
		{"Café Résumé", "caf-rsum"}, // ASCII-only strip, row #73 DECIDE
		{"", ""},
		{"123 Numbers 456", "123-numbers-456"},
	}
	for _, c := range cases {
		if got := Slug(c.in); got != c.want {
			t.Errorf("Slug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 10: Run tests to verify they fail**

Run: `go test ./internal/publish/... -run TestSlug -v`
Expected: FAIL — `Slug` undefined.

- [ ] **Step 11: Write the Slug implementation**

```go
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
```

- [ ] **Step 12: Run tests to verify they pass**

Run: `go test ./internal/publish/... -run TestSlug -v`
Expected: PASS.

- [ ] **Step 13: Write the failing rsync/ssh transport tests**

```go
// internal/publish/transport_test.go
package publish

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stubBinOnPath writes an executable shell script named name to a fresh
// temp dir, prepends that dir to PATH for the duration of the test, and
// lets these tests exercise the REAL subprocess transport (argv-exec,
// never a shell on the LOCAL side) against a local stand-in binary
// instead of a real network call — same spirit as internal/llm's own
// subprocess tests, applied here to rsync/ssh instead of an LLM CLI.
func stubBinOnPath(t *testing.T, name, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub shell scripts require a POSIX shell")
	}
	dir := t.TempDir()
	binPath := filepath.Join(dir, name)
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func mustReadTrimmed(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// CONTRACT(row #75): RsyncPusher argv-execs "rsync -avz <local>
// <host>:<remotePath>/" — never a shell.
func TestRsyncPusherArgvExec(t *testing.T) {
	tmp := t.TempDir()
	argvFile := filepath.Join(tmp, "argv.txt")
	stubBinOnPath(t, "rsync", `echo "$@" > `+shellQuoteSingle(argvFile))

	local := filepath.Join(tmp, "note.html")
	if err := os.WriteFile(local, []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := (RsyncPusher{}).Push(context.Background(), local, "docs.example.com", "/var/www/docs"); err != nil {
		t.Fatal(err)
	}
	got := mustReadTrimmed(t, argvFile)
	want := "-avz " + local + " docs.example.com:/var/www/docs/"
	if got != want {
		t.Errorf("rsync argv = %q, want %q", got, want)
	}
}

// CONTRACT(row #76): SSHRemover argv-execs "ssh <host> <remoteCmd>" —
// the LOCAL side is a plain argv-exec, never `sh -c`.
func TestSSHRemoverArgvExec(t *testing.T) {
	tmp := t.TempDir()
	argvFile := filepath.Join(tmp, "argv.txt")
	stubBinOnPath(t, "ssh", `echo "$@" > `+shellQuoteSingle(argvFile))

	if err := (SSHRemover{}).Remove(context.Background(), "docs.example.com", "/var/www/docs", "note.html"); err != nil {
		t.Fatal(err)
	}
	got := mustReadTrimmed(t, argvFile)
	want := "docs.example.com rm -f '/var/www/docs/note.html'"
	if got != want {
		t.Errorf("ssh argv = %q, want %q", got, want)
	}
}

// DECIDE(new in v2, row #161): a basename containing a single quote is
// escaped for the remote command, not passed through raw — v1 never
// escaped this.
func TestSSHRemoverEscapesSingleQuote(t *testing.T) {
	tmp := t.TempDir()
	argvFile := filepath.Join(tmp, "argv.txt")
	stubBinOnPath(t, "ssh", `echo "$@" > `+shellQuoteSingle(argvFile))

	if err := (SSHRemover{}).Remove(context.Background(), "host", "/docs", "it's-a-note.html"); err != nil {
		t.Fatal(err)
	}
	got := mustReadTrimmed(t, argvFile)
	if !strings.Contains(got, `'\''`) {
		t.Errorf("expected an escaped single quote in the remote command, got %q", got)
	}
}

// CONTRACT: SSHLister parses one basename per line from `ssh host ls -1
// remotePath`.
func TestSSHListerParsesOutput(t *testing.T) {
	stubBinOnPath(t, "ssh", `echo "a.html"; echo "b.html"`)

	got, err := (SSHLister{}).List(context.Background(), "docs.example.com", "/var/www/docs")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a.html" || got[1] != "b.html" {
		t.Errorf("got = %v", got)
	}
}

// An empty remote listing returns an empty (not nil-panicking) slice.
func TestSSHListerEmpty(t *testing.T) {
	stubBinOnPath(t, "ssh", `true`)
	got, err := (SSHLister{}).List(context.Background(), "host", "/docs")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got = %v, want empty", got)
	}
}

// CONTRACT: a non-zero exit from the remote side is surfaced as an
// error carrying stderr.
func TestSSHRemoverPropagatesFailure(t *testing.T) {
	stubBinOnPath(t, "ssh", `echo "permission denied" >&2; exit 1`)
	err := (SSHRemover{}).Remove(context.Background(), "host", "/docs", "note.html")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("err = %v, expected stderr to be included", err)
	}
}
```

- [ ] **Step 14: Run tests to verify they fail**

Run: `go test ./internal/publish/... -run 'TestRsync|TestSSH' -v`
Expected: FAIL — `RsyncPusher`/`SSHRemover`/`SSHLister`/`shellQuoteSingle` undefined.

- [ ] **Step 15: Write the push/remove/list implementations**

```go
// internal/publish/push.go
package publish

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Pusher pushes a local file to a remote docs host over rsync — an
// interface (mirroring how internal/llm.Runner is injected everywhere)
// so cmd-level tests never attempt a real network call; RsyncPusher is
// the production implementation, argv-exec'd directly (never a shell).
type Pusher interface {
	Push(ctx context.Context, localPath, host, remotePath string) error
}

// RsyncPusher shells out to the real `rsync` binary via argv-exec (row
// #75). Bare "rsync"/"ssh" resolution via the process's own PATH
// (implicit exec.Command lookup, not a pre-resolved absolute path):
// unlike internal/llm.Runner (row #144, hardened for ov2 serve's
// launchd context with a minimal PATH), publish/unpublish are CLI-only
// per the design's "Web v1 surface" pin — they only ever run from an
// interactive terminal session with a normal PATH.
type RsyncPusher struct{}

func (RsyncPusher) Push(ctx context.Context, localPath, host, remotePath string) error {
	dest := host + ":" + strings.TrimSuffix(remotePath, "/") + "/"
	cmd := exec.CommandContext(ctx, "rsync", "-avz", localPath, dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rsync: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
```

```go
// internal/publish/remove.go
package publish

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Remover removes one published file (by basename) from the remote
// docs host over ssh — own injectable interface, distinct from
// llm.Runner (an ssh rm is not an LLM call). SSHRemover is the
// production implementation.
type Remover interface {
	Remove(ctx context.Context, host, remotePath, basename string) error
}

type SSHRemover struct{}

// shellQuoteSingle wraps s in single quotes for the REMOTE shell,
// escaping any embedded single quote (' -> '\''). ssh always hands its
// trailing argv to the remote user's shell as one joined command string
// (this is inherent to the ssh wire protocol, not a local `eval` — the
// LOCAL side stays a plain argv-exec, never `sh -c`, so row #72's fix
// class does not apply here); escaping the basename before it becomes
// part of that remote command string is a hardening v1 lacked (row
// #161: v1's `ssh "$host" "rm -f '${remote_path}/${base}'"` never
// escaped an embedded single quote in $base). Shared by remove.go and
// list.go.
func shellQuoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (SSHRemover) Remove(ctx context.Context, host, remotePath, basename string) error {
	remoteCmd := "rm -f " + shellQuoteSingle(strings.TrimSuffix(remotePath, "/")+"/"+basename)
	cmd := exec.CommandContext(ctx, "ssh", host, remoteCmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh rm: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
```

```go
// internal/publish/list.go
package publish

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Lister lists basenames currently published on the remote docs host —
// backs unpublish's no-args interactive picker (row #159 DECIDE: a
// plain numbered list read from stdin, matching render's own v1 non-gum
// picker style, not a new bubbletea component).
type Lister interface {
	List(ctx context.Context, host, remotePath string) ([]string, error)
}

type SSHLister struct{}

func (SSHLister) List(ctx context.Context, host, remotePath string) ([]string, error) {
	remoteCmd := "ls -1 " + shellQuoteSingle(strings.TrimSuffix(remotePath, "/"))
	cmd := exec.CommandContext(ctx, "ssh", host, remoteCmd)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return nil, fmt.Errorf("ssh ls: %w: %s", err, stderr)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}
```

- [ ] **Step 16: Run tests to verify they pass**

Run: `go test ./internal/publish/... -v`
Expected: PASS, full package green.

- [ ] **Step 17: Commit**

```bash
git add internal/publish/
git commit -m "Add internal/publish: LLM prompt/convert, output slug rule, rsync/ssh push/remove/list"
```

---

### Task 4: `cmd/ov publish` and `cmd/ov unpublish` — write path, subprocess orchestration, stub-LLM integration tests

**Files:**
- Create: `cmd/ov/publish.go`
- Create: `cmd/ov/publish_test.go`
- Create: `cmd/ov/publish_llm_integration_test.go`
- Create: `cmd/ov/unpublish.go`
- Create: `cmd/ov/unpublish_test.go`
- Modify: `cmd/ov/root.go`
- Modify: `examples/ov.config.example`

**Interfaces:**
- Consumes: `publish.BuildPrompt`, `publish.Convert`, `publish.Runner`, `publish.Slug`, `publish.Pusher`, `publish.RsyncPusher`, `publish.Remover`, `publish.SSHRemover`, `publish.Lister`, `publish.SSHLister` (Task 3); `llm.NewRunner` (existing); `vault.ContainPath`, `vault.ReadNote`, `vault.WriteNoteAtomic` (existing); `tui.Confirm` (existing); `resolveConfig` (existing); `llmtest.SelfCmd`, `llmtest.StubEnv`/`ResponseEnv`/`ExitCodeEnv`/`StderrEnv` (existing, via the already-shared `newLLMIntegrationRunner` helper in `cmd/ov/triage_llm_integration_test.go`)
- Produces: `func newPublishCmd() *cobra.Command`; `func runPublish(cfg *config.Config, file string, useLLM bool, desc string, errw io.Writer, deps publishDeps) error`; `func newUnpublishCmd() *cobra.Command`; `func runUnpublish(cfg *config.Config, files []string, in *bufio.Reader, errw io.Writer, deps unpublishDeps) error`

- [ ] **Step 1: Write the failing publish tests**

```go
// cmd/ov/publish_test.go
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakePusher struct {
	gotLocal, gotHost, gotRemotePath string
	err                              error
	calls                            int
}

func (f *fakePusher) Push(ctx context.Context, localPath, host, remotePath string) error {
	f.calls++
	f.gotLocal, f.gotHost, f.gotRemotePath = localPath, host, remotePath
	return f.err
}

type fakePublishRunner struct {
	response string
	err      error
}

func (f *fakePublishRunner) Run(ctx context.Context, prompt string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

// CONTRACT(row #68): OV_DOCS_HOST unset -> a plain error (not
// errExitCode2).
func TestRunPublishRequiresDocsHost(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = ""
	target := filepath.Join(vaultDir, "note.html")
	os.WriteFile(target, []byte("<html></html>"), 0o644)

	err = runPublish(cfg, target, false, "", &bytes.Buffer{}, publishDeps{pusher: &fakePusher{}})
	if err == nil || errors.Is(err, errExitCode2) {
		t.Fatalf("err = %v, want a plain (non-exitCode2) error", err)
	}
}

// CONTRACT(row #70): a .md file without --llm refuses with a hint.
func TestRunPublishRefusesMarkdownWithoutLLM(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	target := filepath.Join(vaultDir, "note.md")
	os.WriteFile(target, []byte("# Note\n"), 0o644)

	err = runPublish(cfg, target, false, "", &bytes.Buffer{}, publishDeps{pusher: &fakePusher{}})
	if err == nil || !strings.Contains(err.Error(), "--llm") {
		t.Fatalf("err = %v, want a hint to use --llm", err)
	}
}

// CONTRACT(row #71): --llm on a non-.md file warns and publishes as-is
// (the runner must never be called).
func TestRunPublishLLMIgnoredOnNonMarkdown(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	target := filepath.Join(vaultDir, "already.html")
	os.WriteFile(target, []byte("<html></html>"), 0o644)

	pusher := &fakePusher{}
	var errBuf bytes.Buffer
	runner := &fakePublishRunner{response: "should never be used"}
	if err := runPublish(cfg, target, true, "", &errBuf, publishDeps{runner: runner, pusher: pusher}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "--llm ignored") {
		t.Errorf("errBuf = %q, want a warning", errBuf.String())
	}
	if pusher.gotLocal != target {
		t.Errorf("expected the original file to be pushed as-is, got %q", pusher.gotLocal)
	}
}

// CONTRACT(rows #73,#74): --llm on a .md file converts via the runner,
// extracts the HTML block, writes Published/<slug>.html (row #73's
// lowercase-hyphenated slug rule), and pushes THAT file.
func TestRunPublishLLMConvertsAndPublishes(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	cfg.DocsURL = "https://docs.example.com"
	target := filepath.Join(vaultDir, "My Great Note.md")
	os.WriteFile(target, []byte("# My Great Note\n\nBody.\n"), 0o644)

	pusher := &fakePusher{}
	runner := &fakePublishRunner{response: "chatter\n<html><body>Hi</body></html>\nmore chatter"}
	var errBuf bytes.Buffer
	if err := runPublish(cfg, target, true, "guidance", &errBuf, publishDeps{runner: runner, pusher: pusher}); err != nil {
		t.Fatal(err)
	}

	wantOut := filepath.Join(vaultDir, "Published", "my-great-note.html")
	got, rerr := os.ReadFile(wantOut)
	if rerr != nil {
		t.Fatalf("expected %s to exist: %v", wantOut, rerr)
	}
	if strings.TrimSpace(string(got)) != "<html><body>Hi</body></html>" {
		t.Errorf("published HTML = %q", got)
	}
	if pusher.gotLocal != wantOut {
		t.Errorf("expected the generated HTML to be pushed, got %q", pusher.gotLocal)
	}
	if !strings.Contains(errBuf.String(), "Live at: https://docs.example.com/my-great-note.html") {
		t.Errorf("errBuf = %q, want the live URL line", errBuf.String())
	}
}

// BUG(fixed)(row #160): republishing the SAME note overwrites the
// existing Published/<slug>.html atomically (conditional write) rather
// than refusing — v1 always overwrote on republish, and that behavior
// must survive the atomicity fix.
func TestRunPublishLLMRepublishOverwrites(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	target := filepath.Join(vaultDir, "note.md")
	os.WriteFile(target, []byte("# Note\n"), 0o644)

	pusher := &fakePusher{}
	for i, resp := range []string{"<html>version one</html>", "<html>version two</html>"} {
		runner := &fakePublishRunner{response: resp}
		if err := runPublish(cfg, target, true, "", &bytes.Buffer{}, publishDeps{runner: runner, pusher: pusher}); err != nil {
			t.Fatalf("publish #%d: %v", i, err)
		}
	}
	got, rerr := os.ReadFile(filepath.Join(vaultDir, "Published", "note.html"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(got), "version two") || strings.Contains(string(got), "version one") {
		t.Errorf("expected the republish to overwrite, got %q", got)
	}
}

// A runner failure surfaces as a plain error; the pusher must never be
// called.
func TestRunPublishLLMFailurePropagates(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	target := filepath.Join(vaultDir, "note.md")
	os.WriteFile(target, []byte("# Note\n"), 0o644)

	pusher := &fakePusher{}
	runner := &fakePublishRunner{err: errors.New("llm exploded")}
	err = runPublish(cfg, target, true, "", &bytes.Buffer{}, publishDeps{runner: runner, pusher: pusher})
	if err == nil {
		t.Fatal("expected an error")
	}
	if pusher.calls != 0 {
		t.Error("pusher must never be called when the LLM conversion fails")
	}
}
```

- [ ] **Step 2: Write the failing unpublish tests**

```go
// cmd/ov/unpublish_test.go
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
)

type fakeRemover struct {
	removed []string
	err     error
}

func (f *fakeRemover) Remove(ctx context.Context, host, remotePath, basename string) error {
	if f.err != nil {
		return f.err
	}
	f.removed = append(f.removed, basename)
	return nil
}

type fakeLister struct {
	files []string
	err   error
}

func (f *fakeLister) List(ctx context.Context, host, remotePath string) ([]string, error) {
	return f.files, f.err
}

func testConfigWithDocsHost(t *testing.T) *config.Config {
	t.Helper()
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	return cfg
}

// CONTRACT(row #68): OV_DOCS_HOST unset -> error.
func TestRunUnpublishRequiresDocsHost(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	cfg.DocsHost = ""
	err := runUnpublish(cfg, []string{"note.html"}, bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, unpublishDeps{})
	if err == nil {
		t.Fatal("expected an error")
	}
}

// CONTRACT(row #76): direct-args mode removes each basename with NO
// confirmation — deps.confirm must never be called.
func TestRunUnpublishDirectArgsNoConfirmation(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	remover := &fakeRemover{}
	confirmCalled := false
	deps := unpublishDeps{
		remover: remover,
		confirm: func(string) (bool, error) { confirmCalled = true; return true, nil },
	}
	err := runUnpublish(cfg, []string{"/some/path/note.html", "other.html"}, bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if confirmCalled {
		t.Error("direct-args mode must never call confirm (row #76)")
	}
	if len(remover.removed) != 2 || remover.removed[0] != "note.html" || remover.removed[1] != "other.html" {
		t.Errorf("removed = %v", remover.removed)
	}
}

// CONTRACT(rows #77,#159): the no-args picker requires an explicit
// confirm before removal.
func TestRunUnpublishInteractiveRequiresConfirm(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	remover := &fakeRemover{}
	lister := &fakeLister{files: []string{"a.html", "b.html"}}
	deps := unpublishDeps{
		remover: remover,
		lister:  lister,
		confirm: func(string) (bool, error) { return false, nil }, // decline
	}
	err := runUnpublish(cfg, nil, bufio.NewReader(strings.NewReader("a\n")), &bytes.Buffer{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(remover.removed) != 0 {
		t.Errorf("expected no removal after a declined confirm, got %v", remover.removed)
	}
}

// CONTRACT(row #159): the interactive picker's numbered-selection path
// removes only the chosen file(s) after confirmation.
func TestRunUnpublishInteractiveNumberSelection(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	remover := &fakeRemover{}
	lister := &fakeLister{files: []string{"a.html", "b.html", "c.html"}}
	deps := unpublishDeps{
		remover: remover,
		lister:  lister,
		confirm: func(string) (bool, error) { return true, nil },
	}
	err := runUnpublish(cfg, nil, bufio.NewReader(strings.NewReader("2\n")), &bytes.Buffer{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(remover.removed) != 1 || remover.removed[0] != "b.html" {
		t.Errorf("removed = %v", remover.removed)
	}
}

// CONTRACT: the interactive picker's "a" choice selects every remote
// file.
func TestRunUnpublishInteractiveAllSelection(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	remover := &fakeRemover{}
	lister := &fakeLister{files: []string{"a.html", "b.html"}}
	deps := unpublishDeps{
		remover: remover,
		lister:  lister,
		confirm: func(string) (bool, error) { return true, nil },
	}
	err := runUnpublish(cfg, nil, bufio.NewReader(strings.NewReader("a\n")), &bytes.Buffer{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(remover.removed) != 2 {
		t.Errorf("removed = %v", remover.removed)
	}
}

// CONTRACT: the interactive picker's "q" choice (and EOF) cancels
// without removing anything.
func TestRunUnpublishInteractiveQuit(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	remover := &fakeRemover{}
	lister := &fakeLister{files: []string{"a.html"}}
	deps := unpublishDeps{remover: remover, lister: lister, confirm: func(string) (bool, error) { return true, nil }}
	err := runUnpublish(cfg, nil, bufio.NewReader(strings.NewReader("q\n")), &bytes.Buffer{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(remover.removed) != 0 {
		t.Error("expected nothing removed after quit")
	}
}

// No remote files -> a plain message, no picker prompt, no error.
func TestRunUnpublishInteractiveNoFiles(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	deps := unpublishDeps{remover: &fakeRemover{}, lister: &fakeLister{files: nil}}
	var errw bytes.Buffer
	err := runUnpublish(cfg, nil, bufio.NewReader(strings.NewReader("")), &errw, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errw.String(), "No files on docs server") {
		t.Errorf("errw = %q", errw.String())
	}
}

// A remover failure surfaces as an error.
func TestRunUnpublishRemoverFailurePropagates(t *testing.T) {
	cfg := testConfigWithDocsHost(t)
	deps := unpublishDeps{remover: &fakeRemover{err: errors.New("ssh failed")}}
	err := runUnpublish(cfg, []string{"note.html"}, bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, deps)
	if err == nil {
		t.Fatal("expected an error")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./cmd/ov/... -run 'TestRunPublish|TestRunUnpublish' -v`
Expected: FAIL — `publish`/`unpublish` unknown subcommands; `runPublish`/`runUnpublish`/`publishDeps`/`unpublishDeps` undefined.

- [ ] **Step 4: Write the publish implementation**

```go
// cmd/ov/publish.go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
	"github.com/arosenkranz/obsidian-vault-tools/internal/publish"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

// publishDeps injects the LLM runner (nil unless --llm) and the rsync
// pusher so runPublish is fully testable without a real subprocess or
// network call (mirrors llmTriageDeps/mocCleanupDeps' injection
// pattern).
type publishDeps struct {
	runner publish.Runner
	pusher publish.Pusher
}

// publishLLMTimeout is 180s-class, matching moc-cleanup's HTML/text
// generation budget rather than triage's 120s (HTML generation is
// comparably heavy) — a cmd/ov-local constant, not shared with
// mocCleanupTimeout/llmTriageTimeout.
const publishLLMTimeout = 180 * time.Second

// publishPushTimeout bounds the rsync push itself. v1 had no timeout on
// rsync at all (DECIDE, new in v2) — published notes/HTML files are
// small, 60s is generous.
const publishPushTimeout = 60 * time.Second

func newPublishCmd() *cobra.Command {
	var vaultFlag string
	var useLLM bool
	var desc string
	cmd := &cobra.Command{
		Use:   "publish <file>",
		Short: "Publish a note to the docs server (optionally LLM-converted to HTML)",
		// row #158 DECIDE: an explicit file argument is required — no
		// interactive picker (v1's gum picker walked the whole vault for
		// *.md, an unbounded candidate set for a plain numbered list).
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			var runner publish.Runner
			if useLLM {
				runner = llm.NewRunner(cfg.LLMCmd, cfg.Model)
			}
			deps := publishDeps{runner: runner, pusher: publish.RsyncPusher{}}
			return runPublish(cfg, args[0], useLLM, desc, cmd.ErrOrStderr(), deps)
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	cmd.Flags().BoolVar(&useLLM, "llm", false, "convert a .md note to a self-contained HTML file via LLM before publishing")
	cmd.Flags().StringVar(&desc, "desc", "", "design guidance for --llm (default: clean, modern design)")
	return cmd
}

// runPublish is the testable core of `ov2 publish`. Requires
// cfg.DocsHost, else error+exit 1 — NOT exit 2 (row #68, matching v1's
// own exit code for this command, distinct from triage/mocs-cleanup's
// errExitCode2 sentinel). A .md file without --llm refuses with a hint
// (row #70); --llm on a non-.md file warns and publishes as-is (row
// #71, case-sensitive extension match exactly like v1's
// `[ "$ext" = "md" ]`). The --llm path calls the LLM via deps.runner
// (argv-exec, never a shell — row #72's fix) and decodes with
// publish.Convert/llm.ExtractHTMLBlock (row #74). The output slug is
// publish.Slug's lowercase-hyphenated rule (row #73, distinct from
// vault.Slugify), written to $VAULT_DIR/Published/<slug>.html via a
// conditional vault.WriteNoteAtomic — create-new on first publish,
// hash-conditional overwrite on republish (row #160's fix: v1's plain
// `printf > file` was non-atomic). The final push is always an rsync
// (row #75), printing the live URL when cfg.DocsURL is set.
func runPublish(cfg *config.Config, file string, useLLM bool, desc string, errw io.Writer, deps publishDeps) error {
	if cfg.DocsHost == "" {
		return errors.New("OV_DOCS_HOST not set: add docs_host to your config (see examples/ov.config.example)")
	}

	info, err := os.Stat(file)
	if err != nil {
		return fmt.Errorf("file not found: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s: is a directory", file)
	}

	ext := filepath.Ext(file)
	publishFile := file

	if ext == ".md" && !useLLM {
		return fmt.Errorf("%s is a markdown file — use --llm to convert it to HTML first: ov2 publish %q --llm", file, file)
	}
	if useLLM && ext != ".md" {
		fmt.Fprintf(errw, "⚠ --llm ignored: file is already %s, publishing as-is\n", strings.TrimPrefix(ext, "."))
		useLLM = false
	}

	if useLLM {
		content, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		fmt.Fprintln(errw, "🤖 Converting with LLM...")
		ctx, cancel := context.WithTimeout(context.Background(), publishLLMTimeout)
		html, err := publish.Convert(ctx, deps.runner, string(content), desc)
		cancel()
		if err != nil {
			return fmt.Errorf("llm conversion failed: %w", err)
		}

		stem := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		slug := publish.Slug(stem)
		outAbs, err := vault.ContainPath(cfg.VaultDir, filepath.Join("Published", slug+".html"))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(outAbs), 0o755); err != nil {
			return err
		}
		newContent := html + "\n"
		var expectedHash string
		if _, hash, rerr := vault.ReadNote(outAbs); rerr == nil {
			expectedHash = hash // republish: conditional overwrite, not create-new
		}
		if err := vault.WriteNoteAtomic(outAbs, []byte(newContent), expectedHash); err != nil {
			return err
		}
		fmt.Fprintf(errw, "✓ HTML saved: %s\n", outAbs)
		publishFile = outAbs
	}

	filename := filepath.Base(publishFile)
	fmt.Fprintf(errw, "📤 Publishing %s...\n", filename)
	ctx, cancel := context.WithTimeout(context.Background(), publishPushTimeout)
	err = deps.pusher.Push(ctx, publishFile, cfg.DocsHost, cfg.DocsPath)
	cancel()
	if err != nil {
		return fmt.Errorf("rsync push failed: %w", err)
	}

	if cfg.DocsURL != "" {
		fmt.Fprintf(errw, "\n✓ Live at: %s/%s\n", strings.TrimSuffix(cfg.DocsURL, "/"), filename)
	} else {
		fmt.Fprintf(errw, "\n✓ Published to %s:%s/%s\n", cfg.DocsHost, strings.TrimSuffix(cfg.DocsPath, "/"), filename)
	}
	return nil
}
```

- [ ] **Step 5: Write the unpublish implementation**

```go
// cmd/ov/unpublish.go
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/arosenkranz/obsidian-vault-tools/internal/publish"
	"github.com/arosenkranz/obsidian-vault-tools/internal/tui"
	"github.com/spf13/cobra"
)

// unpublishDeps injects the ssh-backed collaborators (and, for the
// interactive path, the tty-dependent confirm) so runUnpublish is fully
// testable without a real subprocess, network call, or terminal.
type unpublishDeps struct {
	remover publish.Remover
	lister  publish.Lister
	confirm func(string) (bool, error)
}

// sshOpTimeout bounds each individual ssh rm/ls call. v1 had no timeout
// on ssh at all (DECIDE, new in v2).
const sshOpTimeout = 30 * time.Second

func newUnpublishCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "unpublish [<file>...]",
		Short: "Remove file(s) from the docs server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			deps := unpublishDeps{remover: publish.SSHRemover{}, lister: publish.SSHLister{}, confirm: tui.Confirm}
			return runUnpublish(cfg, args, bufio.NewReader(cmd.InOrStdin()), cmd.ErrOrStderr(), deps)
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}

// runUnpublish is the testable core of `ov2 unpublish`. Requires
// cfg.DocsHost, else error+exit 1 (matches publish's own exit-1
// convention, not errExitCode2). Direct-args mode removes each basename
// with NO confirmation (row #76, ported exactly). With no args, a plain
// numbered picker (row #159 DECIDE — mirrors render's own v1 non-gum
// picker style, no new bubbletea component) lists the remote docs
// host's files via deps.lister, then an explicit y/Y confirm gates
// removal (row #77, deps.confirm reuses tui.Confirm).
func runUnpublish(cfg *config.Config, files []string, in *bufio.Reader, errw io.Writer, deps unpublishDeps) error {
	if cfg.DocsHost == "" {
		return errors.New("OV_DOCS_HOST not set: add docs_host to your config (see examples/ov.config.example)")
	}
	ctx := context.Background()
	remotePath := cfg.DocsPath

	if len(files) > 0 {
		for _, f := range files {
			base := filepath.Base(f)
			fmt.Fprintf(errw, "🗑 Removing %s...\n", base)
			rctx, cancel := context.WithTimeout(ctx, sshOpTimeout)
			err := deps.remover.Remove(rctx, cfg.DocsHost, remotePath, base)
			cancel()
			if err != nil {
				return fmt.Errorf("remove %s: %w", base, err)
			}
			fmt.Fprintln(errw, "✓ Removed")
		}
		return nil
	}

	fmt.Fprintln(errw, "Fetching published files...")
	lctx, cancel := context.WithTimeout(ctx, sshOpTimeout)
	remote, err := deps.lister.List(lctx, cfg.DocsHost, remotePath)
	cancel()
	if err != nil {
		return fmt.Errorf("listing remote files: %w", err)
	}
	if len(remote) == 0 {
		fmt.Fprintln(errw, "No files on docs server.")
		return nil
	}

	fmt.Fprintln(errw, "\nPublished files:")
	for i, f := range remote {
		fmt.Fprintf(errw, "  [%d] %s\n", i+1, f)
	}
	fmt.Fprint(errw, "\nUnpublish which? (numbers/comma-separated, \"a\" for all, \"q\" to cancel): ")
	line, readErr := in.ReadString('\n')
	choice := strings.ToLower(strings.TrimSpace(line))
	if readErr != nil && choice == "" {
		choice = "q"
	}
	if choice == "" || choice == "q" {
		fmt.Fprintln(errw, "Cancelled.")
		return nil
	}

	var selected []string
	if choice == "a" {
		selected = remote
	} else {
		for _, tok := range strings.Split(choice, ",") {
			tok = strings.TrimSpace(tok)
			n, convErr := strconv.Atoi(tok)
			if convErr != nil || n < 1 || n > len(remote) {
				return fmt.Errorf("invalid choice: %q", tok)
			}
			selected = append(selected, remote[n-1])
		}
	}
	if len(selected) == 0 {
		fmt.Fprintln(errw, "Cancelled.")
		return nil
	}

	fmt.Fprintln(errw, "Will remove:")
	for _, f := range selected {
		fmt.Fprintf(errw, "  • %s\n", f)
	}
	ok, confirmErr := deps.confirm("Unpublish these files?")
	if confirmErr != nil || !ok {
		fmt.Fprintln(errw, "Cancelled.")
		return nil
	}

	for _, f := range selected {
		fmt.Fprintf(errw, "🗑 Removing %s...\n", f)
		rctx, cancel := context.WithTimeout(ctx, sshOpTimeout)
		err := deps.remover.Remove(rctx, cfg.DocsHost, remotePath, f)
		cancel()
		if err != nil {
			return fmt.Errorf("remove %s: %w", f, err)
		}
		fmt.Fprintln(errw, "✓ Removed")
	}
	return nil
}
```

Update `cmd/ov/root.go`'s `root.AddCommand(...)` call to add `newPublishCmd()`, `newUnpublishCmd()`:

```go
root.AddCommand(newConfigCmd(), newDoctorCmd(), newInitCmd(), newInboxCmd(), newStaleCmd(), newReviewCmd(), newMocsCmd(), newCaptureCmd(), newTriageCmd(), newServeCmd(), newRenderCmd(), newPublishCmd(), newUnpublishCmd())
```

Append to `examples/ov.config.example` (after the existing SECURITY comment block, at end of file):

```
# Optional: `ov publish`/`ov unpublish` push/remove files on a docs
# server over rsync/ssh. Uncomment and set if you use them.
# OV_DOCS_HOST="docs.example.com"
# OV_DOCS_PATH="/var/www/docs"
# OV_DOCS_URL="https://docs.example.com"
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./cmd/ov/... -v`
Expected: PASS, full package green.

- [ ] **Step 7: Write the failing stub-LLM integration tests**

```go
// cmd/ov/publish_llm_integration_test.go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CONTRACT(row #74): publish --llm end to end through the REAL
// subprocess transport (internal/llm.Runner, argv-exec) — the stub
// responds with chatter around an <html> block, and publish.Convert
// must extract exactly that block via llm.ExtractHTMLBlock (first real
// consumer of a decoder built in phase 3 and unused until this phase).
func TestPublishLLMIntegrationExtractsHTMLBlock(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	target := filepath.Join(vaultDir, "note.md")
	if err := os.WriteFile(target, []byte("# Note\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := newLLMIntegrationRunner(t, "Sure!\n<html><body>Converted</body></html>\nEnjoy!", 0, "")
	pusher := &fakePusher{}
	var errBuf bytes.Buffer
	if err := runPublish(cfg, target, true, "", &errBuf, publishDeps{runner: runner, pusher: pusher}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(vaultDir, "Published", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "<html><body>Converted</body></html>" {
		t.Errorf("published HTML = %q", got)
	}
}

// BUG(fixed)(row #72): a crafted LLM response containing shell
// metacharacters is never interpreted — argv-exec, not `eval`. The stub
// responder's response VALUE contains shell metacharacters ($(...), ;,
// |, &&) to prove the real transport treats the whole thing as inert
// stdout content, never as something executed.
func TestPublishLLMIntegrationNeverEvalsShellMetacharacters(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	target := filepath.Join(vaultDir, "note.md")
	if err := os.WriteFile(target, []byte("# Note\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(t.TempDir(), "pwned")
	dangerous := "<html><body>$(touch " + marker + "); rm -rf /; echo owned && cat /etc/passwd</body></html>"
	runner := newLLMIntegrationRunner(t, dangerous, 0, "")
	pusher := &fakePusher{}
	var errBuf bytes.Buffer
	if err := runPublish(cfg, target, true, "", &errBuf, publishDeps{runner: runner, pusher: pusher}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(vaultDir, "Published", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(dangerous) {
		t.Errorf("published HTML = %q, want the dangerous text preserved verbatim (never shell-interpreted)", got)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("SECURITY: shell metacharacters in the LLM response were executed — eval-class vulnerability reintroduced")
	}
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./cmd/ov/... -run TestPublishLLMIntegration -v`
Expected: PASS — both tests exercise the real `*llm.Runner` subprocess transport via the re-exec'd test binary (`newLLMIntegrationRunner`, already defined in `cmd/ov/triage_llm_integration_test.go`).

- [ ] **Step 9: Run the full package test suite**

Run: `go test ./... -v`
Expected: PASS across every package, including `internal/render`, `internal/publish`, and `cmd/ov`.

- [ ] **Step 10: Commit**

```bash
git add cmd/ov/publish.go cmd/ov/publish_test.go cmd/ov/publish_llm_integration_test.go cmd/ov/unpublish.go cmd/ov/unpublish_test.go cmd/ov/root.go examples/ov.config.example
git commit -m "Add ov2 publish/unpublish: LLM convert write path, rsync/ssh orchestration, stub-LLM integration tests"
```

---

### Task 5: `internal/newnote` (embedded templates) and `cmd/ov new`

**Files:**
- Create: `internal/newnote/templates/project.md`
- Create: `internal/newnote/templates/meeting.md`
- Create: `internal/newnote/templates/learning.md`
- Create: `internal/newnote/newnote.go`
- Create: `internal/newnote/newnote_test.go`
- Create: `cmd/ov/new.go`
- Create: `cmd/ov/new_test.go`
- Modify: `cmd/ov/root.go`

**Interfaces:**
- Consumes: `vault.Slugify(s string, maxLen int) string`, `vault.ContainPath`, `vault.WriteNoteAtomic` (existing); `resolveConfig` (existing)
- Produces: `var newnote.ProjectTemplate, newnote.MeetingTemplate, newnote.LearningTemplate string`; `func newnote.Substitute(tmpl, title string) string`; `func newnote.Bare(title string) string`; `func newNewCmd() *cobra.Command`; `func runNew(cfg *config.Config, noteType, title string) (string, error)`

- [ ] **Step 1: Create the embedded template files**

`internal/newnote/templates/project.md` — byte-identical to `templates/99-Meta/Project Template.md`:

```
# {{title}}

## 🎯 Goal
> What does "done" look like?

## 📋 Tasks
- [ ] 

## 📝 Notes & Updates
### $(date +%Y-%m-%d)
-

## ✅ Done
```

`internal/newnote/templates/meeting.md` — byte-identical to `templates/99-Meta/Meeting Note Template.md`:

```
# {{title}}

**Date:** $(date +%Y-%m-%d)  
**With:** 
**Duration:** 

## 🎯 Purpose
> Why are we meeting?

## 📝 Notes


## ✅ Action Items
- [ ] 

## 🔗 Related
```

`internal/newnote/templates/learning.md` — byte-identical to `templates/99-Meta/Learning Note Template.md`:

```
# {{title}}

**Source:** 
**Progress:** 
**Tags:** #learning

## 📚 Overview


## 🔑 Key Concepts


## 💡 Takeaways


## 🔗 Resources & Links


## ✅ Action Items
- [ ] 
```

(Row #164, DECIDE: these are maintained copies, not a runtime read of `templates/99-Meta/`. The literal `$(date +%Y-%m-%d)` text in the Project/Meeting templates is v1's OWN template content — v1's `sed` only ever substitutes `{{title}}`, never that text, so it is preserved verbatim, not evaluated.)

- [ ] **Step 2: Write the failing newnote tests**

```go
// internal/newnote/newnote_test.go
package newnote

import (
	"strings"
	"testing"
)

// CONTRACT(#59): {{title}} is substituted into every embedded template.
func TestSubstituteReplacesTitle(t *testing.T) {
	for name, tmpl := range map[string]string{
		"project":  ProjectTemplate,
		"meeting":  MeetingTemplate,
		"learning": LearningTemplate,
	} {
		t.Run(name, func(t *testing.T) {
			got := Substitute(tmpl, "My Title")
			if !strings.Contains(got, "# My Title") {
				t.Errorf("%s template: expected title substituted into the heading, got:\n%s", name, got)
			}
			if strings.Contains(got, "{{title}}") {
				t.Errorf("%s template: {{title}} placeholder survived substitution:\n%s", name, got)
			}
		})
	}
}

// BUG(fixed)(#165): a title containing sed-replacement metacharacters
// (&, a backslash-digit sequence, /) survives a literal, unmodified
// substitution — v1's sed-based substitution would have corrupted or
// broken on at least the "/" case (breaks the s/// delimiter) and the
// "&" case (whole-match backreference in sed's replacement text).
func TestSubstituteHandlesMetacharacterTitles(t *testing.T) {
	dangerous := `Q&A / Notes \1 review`
	got := Substitute("# {{title}}\n", dangerous)
	want := "# " + dangerous + "\n"
	if got != want {
		t.Errorf("Substitute = %q, want %q", got, want)
	}
}

// Substitute replaces every occurrence, not just the first (mirrors
// sed's trailing "g" flag).
func TestSubstituteReplacesEveryOccurrence(t *testing.T) {
	got := Substitute("{{title}} - {{title}}", "X")
	if got != "X - X" {
		t.Errorf("got = %q", got)
	}
}

// CONTRACT(#59): General-type notes with no template get a bare "# Title"
// note.
func TestBare(t *testing.T) {
	got := Bare("Quick Thought")
	if got != "# Quick Thought\n\n" {
		t.Errorf("got = %q", got)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/newnote/... -v`
Expected: FAIL — package doesn't exist yet.

- [ ] **Step 4: Write the newnote implementation**

```go
// internal/newnote/newnote.go
//
// Package newnote renders `ov2 new`'s note templates. Pure content
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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/newnote/... -v`
Expected: PASS, full package green.

- [ ] **Step 6: Write the failing cmd tests**

```go
// cmd/ov/new_test.go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CONTRACT(#59): each type resolves to its fixed destination folder and
// its own template, with {{title}} substituted.
func TestRunNewEachType(t *testing.T) {
	cases := []struct {
		noteType, wantDir, wantHeading string
	}{
		{"project", "01-Projects", "# Project Alpha"},
		{"meeting", "02-Areas/Work", "# Standup"},
		{"learning", "02-Areas/Learning", "# Go Generics"},
		{"general", "00-Inbox", "# Quick Thought"},
	}
	for _, c := range cases {
		t.Run(c.noteType, func(t *testing.T) {
			vaultDir := newVaultFixture(t)
			cfg, err := resolveConfig(vaultDir)
			if err != nil {
				t.Fatal(err)
			}
			title := strings.TrimPrefix(c.wantHeading, "# ")
			rel, err := runNew(cfg, c.noteType, title)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(rel, c.wantDir+"/") {
				t.Errorf("rel = %q, want prefix %q", rel, c.wantDir)
			}
			got, err := os.ReadFile(filepath.Join(vaultDir, filepath.FromSlash(rel)))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(got), c.wantHeading) {
				t.Errorf("content = %q, want heading %q", got, c.wantHeading)
			}
		})
	}
}

// CONTRACT(#59): an empty title is an error.
func TestRunNewEmptyTitleErrors(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runNew(cfg, "project", ""); err == nil {
		t.Fatal("expected an error for an empty title")
	}
}

// An unknown type is an error.
func TestRunNewUnknownTypeErrors(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runNew(cfg, "bogus", "Title"); err == nil {
		t.Fatal("expected an error for an unknown note type")
	}
}

// BUG(fixed)(#58): the filename comes from vault.Slugify — one rule
// everywhere, closing the third disagreeing slug rule for real.
func TestRunNewFilenameUsesVaultSlugify(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := runNew(cfg, "general", "  Weird @@@ Title!!!  ")
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(rel)
	if strings.ContainsAny(base, "@!") {
		t.Errorf("filename = %q, forbidden chars survived", base)
	}
}

// CONTRACT: mirrors row #99's family — an existing target refuses,
// never overwrites.
func TestRunNewRefusesExistingTarget(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runNew(cfg, "general", "Duplicate"); err != nil {
		t.Fatal(err)
	}
	if _, err := runNew(cfg, "general", "Duplicate"); err == nil {
		t.Fatal("expected a refusal on the second create with the same title")
	}
}

// DECIDE (row #7/#154, cited not re-litigated): no auto-open side
// effect exists to test — runNew's only observable effect is the
// created file plus its printed path.
func TestNewCmdPrintsOnlyPath(t *testing.T) {
	vaultDir := newVaultFixture(t)
	stdout, _, err := runCmd(t, "new", "general", "Solo Thought", "--vault", vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != "00-Inbox/Solo Thought.md" {
		t.Errorf("stdout = %q", stdout)
	}
}
```

- [ ] **Step 7: Run tests to verify they fail**

Run: `go test ./cmd/ov/... -run TestRunNew -v`
Expected: FAIL — `new` unknown subcommand; `runNew` undefined.

- [ ] **Step 8: Write the cmd implementation**

```go
// cmd/ov/new.go
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/arosenkranz/obsidian-vault-tools/internal/newnote"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

// newTitleMaxLen mirrors mocs new's Resources-dir 80-char slug budget
// (row #3's split; v1's new_note had no length limit at all, but
// consolidating on "one Slugify everywhere" — row #58, closed for real
// by this task — requires picking a budget: Project/Meeting/Learning
// note titles are closer to MOC titles' deliberate, longer descriptive
// style than capture's quick-dump 60-char budget).
const newTitleMaxLen = 80

func newNewCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "new <type> <title>",
		Short: "Create a new note from a template (project|meeting|learning|general)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			rel, err := runNew(cfg, args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), rel)
			return nil
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}

// runNew is the testable core of `ov2 new`: an empty title is an error
// (row #59). folderRel/content are resolved per type (row #59's fixed
// destinations: Project -> Projects root, Meeting -> Areas/Work,
// Learning -> Areas/Learning, General -> Inbox); the filename is built
// via vault.Slugify (row #58's fix — v1's third, disagreeing slug rule
// is retired here for real) routed through vault.ContainPath (defense
// in depth, same posture as mocs new's row #153 fix), and written via
// WriteNoteAtomic create-new mode — refused if the target already
// exists (mirrors row #99's family). v1's obsidian://open auto-open
// side effect is deliberately not ported (same DECIDE as row #7/#154 —
// cited, not re-litigated).
func runNew(cfg *config.Config, noteType, title string) (string, error) {
	if title == "" {
		return "", errors.New("title cannot be empty")
	}

	var folderRel, content string
	switch strings.ToLower(noteType) {
	case "project":
		folderRel = cfg.Projects
		content = newnote.Substitute(newnote.ProjectTemplate, title)
	case "meeting":
		folderRel = filepath.Join(cfg.Areas, "Work")
		content = newnote.Substitute(newnote.MeetingTemplate, title)
	case "learning":
		folderRel = filepath.Join(cfg.Areas, "Learning")
		content = newnote.Substitute(newnote.LearningTemplate, title)
	case "general":
		folderRel = cfg.Inbox
		content = newnote.Bare(title)
	default:
		return "", fmt.Errorf("unknown note type %q (want project, meeting, learning, or general)", noteType)
	}

	slug := vault.Slugify(title, newTitleMaxLen)
	targetAbs, err := vault.ContainPath(cfg.VaultDir, filepath.Join(folderRel, slug+".md"))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
		return "", err
	}
	if err := vault.WriteNoteAtomic(targetAbs, []byte(content), ""); err != nil {
		return "", err
	}

	// vault.ContainPath resolves symlinks on the root internally; Rel
	// must be computed against that SAME resolved root, else the
	// printed path can garble under a symlinked vault dir (the exact
	// bug class found and fixed in phase 4's post-review manual
	// testing — .superpowers/sdd/progress.md — applied here
	// proactively).
	vaultReal, err := filepath.EvalSymlinks(cfg.VaultDir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(vaultReal, targetAbs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}
```

Update `cmd/ov/root.go`'s `root.AddCommand(...)` call to add `newNewCmd()` (final state, all four phase-5 commands registered):

```go
root.AddCommand(newConfigCmd(), newDoctorCmd(), newInitCmd(), newInboxCmd(), newStaleCmd(), newReviewCmd(), newMocsCmd(), newCaptureCmd(), newTriageCmd(), newServeCmd(), newRenderCmd(), newPublishCmd(), newUnpublishCmd(), newNewCmd())
```

- [ ] **Step 9: Run tests to verify they pass**

Run: `go test ./... -v`
Expected: PASS across every package.

- [ ] **Step 10: Commit**

```bash
git add internal/newnote/ cmd/ov/new.go cmd/ov/new_test.go cmd/ov/root.go
git commit -m "Add ov2 new: embedded project/meeting/learning templates, one Slugify everywhere (row #58 closed)"
```

---

## Self-Review

**1. Spec coverage (phase table row: "publish/unpublish; render (goldmark)", exit criterion "every bash command has a Go equivalent; CLI surface complete"):**
- `publish`/`publish --llm` (rows #68-75) → Task 3 (`internal/publish`) + Task 4 (`cmd/ov publish`), including the eval fix (row #72, argv-exec via `publish.Convert`+`llm.Runner`), the slug rule (row #73), the HTML-block decode (row #74), the rsync push (row #75), and the write-atomicity fix (row #160).
- `unpublish` (rows #76-77) → Task 3 (`internal/publish`'s `Remover`/`Lister`) + Task 4 (`cmd/ov unpublish`), including the no-confirmation direct path (row #76) and the confirm-gated interactive path (row #77), plus new ssh-quoting hardening (row #161).
- `render` (rows #117-120) → Task 1 (`internal/render`) + Task 2 (`cmd/ov render`): marker discovery (row #117), body splicing (row #118), timestamp handling (row #119), and the row #120 backreference-safety fix (the dedicated regression test, `TestSpliceBodyPreservesBackreferenceLookingContent`), plus the newly-mined `--all` short-circuit fix (row #162) and write-atomicity fix (row #163).
- `new` (row #59, flagged in scope by the task brief despite the phasing table's terse content line) → Task 5 (`internal/newnote` + `cmd/ov new`), including the third-slug-rule retirement (row #58, finally closed) and the `{{title}}`-substitution safety fix (row #165).
- Behavior-inventory prerequisite corrections (rows #59/#73-76/#117-119 phase annotations, rows #158-165 newly mined) → already committed on this branch before Task 1, per Global Constraints.
- Stub-LLM integration tests (design spec §Testing strategy tier 3) → Task 4 Step 7: HTML-block extraction and the row #72 shell-metacharacter regression, both through the real subprocess transport.
- Table tests as the clean-room spec (design spec §Testing strategy tier 1) → verified no `tests/test_render_html.py` or `tests/test_publish.py` exists in the repo (confirmed via the plan's own research pass) — mined directly: render marker-splicing (Task 1), publish slug rule (Task 3), traversal/containment posture for both `render`'s `<file>` argument and publish's output path (Tasks 2 and 4).
- The design's dependency-budget note ("no new dependency needed for goldmark specifically... this phase is where it actually gets added") → Task 1 Step 1.
- Exit criterion ("every bash command has a Go equivalent; CLI surface complete") → covered by Manual exit-criterion verification below, plus the fact that after Task 5, `bin/vault.sh`'s entire main dispatch case (`inbox`, `capture`, `triage`, `new`, `review`, `stale`, `render`, `mocs *`, `publish`, `unpublish`, `help`) and `bin/moc_cleanup.py`/`bin/render_html.py` all have a shipped `ov2` command, except `mocs update` (row #80, DECIDE: dropped dead stub, phase 0).

**2. Placeholder scan:** No `TBD`/`implement later`/`add appropriate handling` language; every step shows complete code. No task references a type/function undefined by an earlier task (`render.Pair`/`FindPairedFiles`/`Regenerate`/`RenderMarkdownBody` — Task 1; `publish.BuildPrompt`/`Convert`/`Runner`/`Slug`/`Pusher`/`Remover`/`Lister` — Task 3; `newnote.ProjectTemplate`/`MeetingTemplate`/`LearningTemplate`/`Substitute`/`Bare` — Task 5; all consumed exactly as produced by Tasks 2, 4, and 5's cmd files).

**3. Type consistency:** `render.Pair{HTMLPath, MDPath, HTMLRel, MDRel string}` (Task 1) is the exact shape Task 2's `runRender` constructs comparisons against and iterates. `render.Regenerate(p Pair, now time.Time) error` (Task 1) matches Task 2's `regenOne` call exactly. `publish.Convert(ctx, runner Runner, noteContent, guidance string) (string, error)` (Task 3) matches Task 4's `publish.Convert(ctx, deps.runner, string(content), desc)` call — `deps.runner` is typed `publish.Runner`, the same interface `Convert` declares. `publish.Pusher`/`Remover`/`Lister` (Task 3) match `publishDeps.pusher`/`unpublishDeps.remover`/`unpublishDeps.lister`'s field types (Task 4) exactly — `RsyncPusher{}`/`SSHRemover{}`/`SSHLister{}` are the concrete production values wired in each command's `RunE`. `newnote.Substitute(tmpl, title string) string` (Task 5) is called identically for all three templates in `runNew`.

**4. Architecture-constraint self-check (design spec's package doc comments):** `internal/publish/` contains only prompt assembly, LLM convert, slug, and rsync/ssh interfaces+implementations — no cobra, no stdin/stdout, no flag parsing (all of that lives in `cmd/ov/publish.go`/`unpublish.go`). `internal/render/` contains only marker regexes/splicing, goldmark conversion, and pair discovery — same separation, orchestration lives in `cmd/ov/render.go`. `internal/newnote/` contains only the three embedded templates plus two pure string functions — folder resolution, `vault.Slugify`, `vault.ContainPath`, and `vault.WriteNoteAtomic` all stay in `cmd/ov/new.go`, matching phase 4's `mocs new`/`mocs add` convention exactly (no intermediate package for orchestration).

**5. Import-cycle check:** `internal/publish` imports `internal/llm` (for `ExtractHTMLBlock`) only — no import of `internal/vault`, `internal/render`, `internal/moc`, or `internal/triage`. `internal/render` imports `internal/vault` (for `ParseNote`, `ContainPath`, `ReadNote`, `WriteNoteAtomic`) only. `internal/newnote` imports nothing from this codebase (pure stdlib + `embed`). `cmd/ov` imports all three directly, as it already does for every other core package — no new cross-package coupling between `internal/publish`/`internal/render`/`internal/newnote` themselves (none of the three imports either of the other two). No cycle.

**6. Global Constraints re-check against the finished task list:** exactly one new dependency (goldmark, Task 1 only) → satisfied, verified by `go.mod` diff at Task 1's commit. Publish's file-arg-required vs. unpublish/render's numbered-picker split (rows #158/#159/#69) → Tasks 2 and 4's `Args` cobra configuration and interactive-picker code. Row #120's structural fix (index/concatenation splicing, never regex-replace) → Task 1's `markers.go` plus the dedicated regression test. Write-atomicity fixes (rows #160, #163) → Task 4's conditional `vault.WriteNoteAtomic` call in `runPublish`, Task 1's `Regenerate`. ssh quoting (row #161) → Task 3's `shellQuoteSingle`, exercised by Task 3's `TestSSHRemoverEscapesSingleQuote`. `--all` no-short-circuit (row #162) → Task 2's `regenAll` closure, exercised by `TestRenderAllContinuesPastFailure`. Timeouts (`publishLLMTimeout`/`publishPushTimeout`/`sshOpTimeout`) → Task 4's local constants, distinct from `mocCleanupTimeout`/`llmTriageTimeout`. No `errExitCode2` reuse → verified: no task's `cmd/ov` file imports or wraps `errExitCode2`; `TestRunPublishRequiresDocsHost` explicitly asserts `!errors.Is(err, errExitCode2)`. Security-review routing (Task 1, Task 4) → flagged explicitly in each task's own preamble above and enforced at execution time.

---

## Manual exit-criterion verification (run after all 5 tasks are implemented and reviewed)

The phase's exit criterion is explicit in the design spec: "every bash command has a Go equivalent; CLI surface complete." Per `.superpowers/sdd/progress.md`'s established pattern (Phase 3's dry-run-vs-python check, Phase 4's validator-parity check, and Phase 4's own "Post-review manual testing" section that found a real symlink-path bug no task/security/final review caught), this phase's manual verification has TWO parts: a scripted smoke pass over every new command against a throwaway copy of the real vault, and — because `publish`/`unpublish` push to a REAL remote host that cannot be exercised in CI or by a reviewing subagent — one real end-to-end publish/unpublish cycle against the actual configured `OV_DOCS_HOST`.

1. **Build the binary and prepare a throwaway vault copy:**
   ```bash
   make build   # dist/ov2
   rsync -a --exclude .obsidian "$HOME/Documents/main-vault/" /tmp/ov2-phase5-smoke/
   ```

2. **Smoke-test `ov2 new` for all four types** against the throwaway copy — verify each creates a file in its expected PARA folder with `{{title}}` substituted and no `obsidian://` auto-open occurs:
   ```bash
   dist/ov2 new project "Phase 5 Smoke Project" --vault /tmp/ov2-phase5-smoke
   dist/ov2 new meeting "Phase 5 Smoke Meeting" --vault /tmp/ov2-phase5-smoke
   dist/ov2 new learning "Phase 5 Smoke Learning" --vault /tmp/ov2-phase5-smoke
   dist/ov2 new general "Phase 5 Smoke General" --vault /tmp/ov2-phase5-smoke
   ```
   Confirm each printed a clean vault-relative path (not a garbled symlink-resolved path) and the file exists with the expected heading.

3. **Smoke-test `ov2 render`** — create (or reuse, if the real vault has any) a RENDER_SOURCE-paired HTML/MD pair in the throwaway copy, run `dist/ov2 render <file> --vault /tmp/ov2-phase5-smoke`, confirm the body updates and a RENDER_TIMESTAMP is stamped; run `dist/ov2 render --all --vault /tmp/ov2-phase5-smoke` and confirm it reports an accurate ok/failed count.

4. **Smoke-test `ov2 publish`/`ov2 unpublish` against the REAL configured `OV_DOCS_HOST`** (exe.dev) — this is the one path no test can safely exercise, and the only way to prove the rsync/ssh mechanism actually works end to end:
   - Create a clearly-labeled throwaway note, e.g. `dist/ov2 new general "ZZZ Phase 5 Publish Smoke Test DELETE ME" --vault "$HOME/Documents/main-vault"`.
   - `dist/ov2 publish "$HOME/Documents/main-vault/00-Inbox/ZZZ Phase 5 Publish Smoke Test DELETE ME.md" --llm`
   - Confirm the printed "Live at: ..." URL actually resolves (curl or open it) and shows the expected converted HTML content.
   - `dist/ov2 unpublish` with no args — confirm the file appears in the printed numbered list, select it, confirm, and verify it's gone from the live URL afterward. Then also test the direct-arg path: republish, then `dist/ov2 unpublish <basename>` directly (no confirm prompt should appear).
   - Delete the throwaway inbox note from the real vault afterward (`rm "$HOME/Documents/main-vault/00-Inbox/ZZZ Phase 5 Publish Smoke Test DELETE ME.md"`) — leave no test artifacts on the real vault or the real docs host.

5. **Record the results** (each command's observed behavior, the live-URL confirmation, and any bug found+fixed — same as phase 4's "Post-review manual testing" precedent) in `.superpowers/sdd/progress.md`'s Phase 5 section as the exit-criterion evidence. Any unexpected divergence from the design's stated behavior is a blocking finding — resolve it before considering the phase done; do not treat "all reviews passed" as sufficient on its own.
