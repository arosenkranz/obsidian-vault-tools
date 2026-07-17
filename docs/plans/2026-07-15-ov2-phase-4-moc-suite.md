# ov v2 Phase 4: MOC suite (mocs new/add, LLM cleanup with ported validator) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `ov2 mocs new`, `ov2 mocs add`, and `ov2 mocs cleanup` as full replacements for `bin/vault.sh`'s `moc_new`/`moc_add` and `bin/moc_cleanup.py`. A new `internal/moc` package owns the LLM cleanup workflow only (`ProposeCleanup`, `Validate`, `Apply`, a thin `Diff` wrapper over `internal/triage.Diff`) per the design spec's explicit split; `mocs new`/`mocs add`'s mechanical file operations live in `internal/vault` alongside the existing `AppendMOCEntry`/`FindMOCByName`/`RenameMOCLink` primitives, orchestrated directly by thin `cmd/ov` commands — no intermediate package, matching how capture.go already calls vault's MOC primitives directly.

**Architecture:** Per the approved spec (`docs/plans/2026-07-11-ov-v2-go-rewrite-design.md`), phase 4 row: "MOC suite: mocs new/add; cleanup with ported validator", exit criterion "validator decisions identical to python on recorded proposal corpus". `internal/moc`'s `Validate` is a byte-for-byte structural safety net independent of prompt compliance — same shape as v1's `moc_cleanup.py validate_proposal` (regex-based frontmatter-block/bare-wikilink/URL diffing, not `vault.ParseNote`, since the validator's job is literal change detection matching v1's exact algorithm, not vault's lenient/lossless parsing with its own deliberate divergences). `internal/moc.Apply` writes via `vault.WriteNoteAtomic` (fixing v1's plain non-atomic `write_text`, row #116) and calls `Validate` itself first as a defense-in-depth gate, mirroring `internal/triage.Apply`'s established posture. `cmd/ov mocs cleanup` reuses the existing `internal/llm.Runner`/`ExtractJSON` transport and `internal/tui.RenderDiff`/`Confirm` unchanged — no new dependency, no `internal/llm` changes.

**Tech Stack:** Go 1.25.0 (unchanged), cobra, pelletier/go-toml/v2, golang.org/x/text, charm.land/bubbletea/v2, charm.land/lipgloss/v2, charmbracelet/huh, mattn/go-isatty, github.com/sergi/go-diff (all existing, all reused as-is). **No new dependency this phase** — `internal/moc` uses only the stdlib (`regexp`, `encoding/json`, `context`, `errors`, `fmt`, `sort`, `strings`) plus `internal/llm`, `internal/triage` (for `Diff` reuse), and `internal/vault`.

## Global Constraints

- Module path: `github.com/arosenkranz/obsidian-vault-tools`
- Binary name during transition: `ov2`, built to `dist/ov2` (`make build`)
- **No new dependency this phase.** Do not add anything to `go.mod`.
- `bin/vault.sh`, `bin/triage_llm.py`, `bin/moc_cleanup.py` are READ-ONLY — never modify them.
- **`internal/moc`'s scope is fixed by the design spec** (§Architecture): "LLM cleanup workflow only: `ProposeCleanup`, structural validator, `Apply` (mechanical ops live in vault; split named in doc comments)." `mocs new`/`mocs add` are pure mechanical file operations with no LLM/proposal/validation workflow — their primitives (`vault.NewMOCSkeleton`, `vault.InsertUnderHeading`, `vault.SanitizeWikilinkText`) live in `internal/vault`, never in `internal/moc`. `cmd/ov` orchestrates them directly (path resolution, `WriteNoteAtomic` calls) — no intermediate package, matching how `cmd/ov/capture.go` already calls `vault.AppendMOCEntry`/`vault.FindMOCByName` directly.
- **The validator is a byte-for-byte structural safety net, independent of prompt compliance** (rows #107-109, ported 1:1 from `tests/test_moc_cleanup.py` before any workflow code): reject if the frontmatter block changed AT ALL (regex-extracted block, byte comparison — `internal/moc` mirrors `moc_cleanup.py`'s own `FRONTMATTER_RE`, not `vault.ParseNote`, because `vault.ParseNote` has its own deliberate closing-fence divergence, row #84, irrelevant to this validator's literal-change-detection job); reject if any *bare* wikilink (no URL on its line) present in the original is missing or renamed in the proposal; reject if any URL present in the original is missing from the proposal. A wikilink sharing its line with a URL is "anchored" and may be retitled freely (the garbled-title-fix feature) — the URL check catches a dropped anchored entry instead. A rejection is reported with the specific reason and the run moves to the next target — **never a partial apply** (row #109).
- **The "unchanged" check runs BEFORE the diff/confirm prompt, exactly matching v1's ordering** (row #157): `moc_cleanup.py main()` checks `new_content == original` immediately after `Validate`/`validate_proposal` passes and BEFORE rendering any diff or asking for y/N — a human is never asked to confirm a no-op. `ov2 mocs cleanup` replicates this exact ordering in its CLI loop. `moc.Apply` additionally no-ops on `proposed == original` as an independent, unreachable-via-normal-flow defense-in-depth backstop (row #115).
- **180s timeout, not 120s** (row #90): `mocCleanupTimeout = 180 * time.Second`, a `cmd/ov`-local constant distinct from `triage.go`'s `llmTriageTimeout = 120 * time.Second`. `internal/llm.Runner.Run` is already timeout-agnostic via the caller-supplied `context.Context` — no `internal/llm` changes needed.
- **`mocs new`'s v1 auto-open side effect is dropped, not fixed-and-ported** (row #7/#154, DECIDE): v1 best-effort opens the new file in Obsidian via a hardcoded `obsidian://open?vault=main-vault` URL. v2 does not port this at all — no other v2 command auto-opens anything, there is no flag to suppress it, and the design's flags-first principle ("every interactive flow fully drivable by flags/args alone") argues against a side effect outside that control surface. `ov2 mocs new`'s only observable effect is the created file plus its vault-relative path on stdout.
- **Filename traversal defense for `mocs new`** (row #153, BUG fixed): v1 interpolates the raw title into `MOC ${title}.md` unsanitized — a title containing `/` escapes the Resources directory (same class as row #140). v2 builds the filename via `vault.Slugify(title, 80)` (NFC-normalized, forbidden-char-stripped including `/`) before joining, then routes the result through `vault.ContainPath` as defense in depth. The visible heading/blockquote content keeps the raw title with only CR/LF stripped (cosmetic parity with v1's output).
- **Wikilink-injection defense for `mocs add`** (row #155, BUG fixed): v1 interpolates `<note-name>` raw into `- [[$note_name]]` with zero sanitization — a name containing CR/LF injects extra lines into the MOC file; a name containing `[`/`]` malforms the wikilink boundary. v2's `vault.SanitizeWikilinkText` strips `\r`, `\n`, `[`, `]` before interpolation. `<note-name>` remains free text otherwise — v1's non-validation that it names a real note is kept unchanged (DECIDE).
- **Conditional writes via `WriteNoteAtomic`** (row #106's discipline, applied to every MOC mutation this phase): every write re-reads and hashes the target immediately before writing, matching every other MOC mutation in the codebase (`syncMOCLink`, `triage.Apply`). `mocs new` creates a new file (`expectedHash == ""`, refused if the file already exists via `vault.ErrExists`).
- **Exit code 2 for `mocs cleanup` usage/resolution errors** (row #114, matching v1's own moc_cleanup.py exit code): MOC name XOR `--all` enforced, else exit 2; no MOC resolved (miss, or an empty vault under `--all`) also exits 2. Both reuse the existing `errExitCode2` sentinel declared in `cmd/ov/triage.go` (`var errExitCode2 = errors.New(...)`) — `main.go`'s existing `errors.Is(err, errExitCode2)` mapping needs no changes.
- **`--all` target ordering** (row #156, DECIDE): v1's `find_moc_files` sorts by full `rglob` path; v2 reuses `vault.MOCs(vaultDir)`, sorted by MOC Name — the same canonical enumeration `mocs list` already uses (row #34) — rather than reimplementing a second path-sorted walk. Cosmetic processing-order difference only.
- **EOF on the confirm prompt is "no" for THAT target, not a global interrupt** (row #116): `moc_cleanup.py`'s `except EOFError: answer = "n"` is a per-iteration catch — the loop continues to the next target. This is a deliberate divergence from `cmd/ov/triage.go`'s `runLLMTriage`, whose EOF handling prints "interrupted" and returns immediately (aborts the whole run) — the two commands have different v1-mandated EOF semantics and must not be unified.
- Every mined behavior test carries a comment `// CONTRACT:`, `// BUG(fixed):`, or `// DECIDE(...):` referencing a row number in `docs/plans/ov2-behavior-inventory.md` (rows 1–157; rows 153-157 were mined for this phase; rows 107-116 were re-annotated from a stale "(phase 3)" to "(phase 4)" as a prerequisite commit already on this branch).
- Go tests use `t.Setenv` / `t.TempDir` — no global state leaks between tests; no injected package-level clock (clocks are passed as `time.Time` values, matching `triage.Apply`'s `now time.Time` parameter).
- Commit style: imperative mood, no conventional-commit prefix (matches repo history)
- **Security-focused review routing:** Task 5 (`internal/moc` `Apply`, the write path) and Task 6 (`cmd/ov mocs cleanup`, the orchestration + subprocess-adjacent command) are routed to a security-focused reviewer at task-review time, matching phases 2/3's pattern for write-path and subprocess-adjacent work (phase 3 Task 5's `Apply` found and closed a CRITICAL frontmatter-injection bug, row #152, under this same review posture — read `.superpowers/sdd/progress.md`'s Phase 3 Task 5 entry before reviewing Task 5/6 of this plan).

---

### Task 1: `internal/vault` — MOC skeleton, `InsertUnderHeading`, `SanitizeWikilinkText`

The mechanical primitives `mocs new` and `mocs add` need, per the design spec's "mechanical ops live in vault" split. `AppendMOCEntry` (capture's `## 🔗 Recent Additions`, creates the heading if missing, blank-line-separated) and the new `InsertUnderHeading` (`mocs add`'s `## Key Notes`, never creates the heading, no blank-line separator) have genuinely different creation/spacing semantics — row #66 vs rows #8/#41/#42 — so they stay two distinct public functions sharing one internal splice helper (`insertAfterHeading`), refactored out of the existing `AppendMOCEntry` with zero behavior change (regression-tested).

**Files:**
- Modify: `internal/vault/moc_write.go` (refactor `AppendMOCEntry` onto a shared `insertAfterHeading` helper; add `InsertUnderHeading`, `SanitizeWikilinkText`)
- Modify: `internal/vault/moc_write_test.go` (add tests; regression-guard `AppendMOCEntry`'s unchanged behavior)
- Create: `internal/vault/moc_new.go` (`NewMOCSkeleton`)
- Create: `internal/vault/moc_new_test.go`

**Interfaces:**
- Consumes: nothing from other tasks (foundational)
- Produces:
  - `func InsertUnderHeading(content, heading, entry string) string`
  - `func SanitizeWikilinkText(s string) string`
  - `func NewMOCSkeleton(title string, now time.Time) string`
  - `AppendMOCEntry`'s existing signature and behavior are unchanged (regression only)

- [ ] **Step 1: Write the failing tests**

```go
// internal/vault/moc_write_test.go — append at end of file

// CONTRACT(#66): inserting under an existing "## Key Notes" heading
// places the entry immediately below it, with NO blank-line separator
// (v1's `sed -i "/## Key Notes/a\..."` inserts directly) — unlike
// AppendMOCEntry's blank-line spacer.
func TestInsertUnderHeadingExistingHeading(t *testing.T) {
	content := "# MOC Music\n\n## Key Notes\n- [[Old]]\n"
	got := InsertUnderHeading(content, "## Key Notes", "- [[New]]")
	want := "# MOC Music\n\n## Key Notes\n- [[New]]\n- [[Old]]\n"
	if got != want {
		t.Errorf("InsertUnderHeading =\n%q\nwant\n%q", got, want)
	}
}

// CONTRACT(#66): a missing heading appends at EOF and does NOT create
// the heading — unlike AppendMOCEntry's create-on-miss behavior.
func TestInsertUnderHeadingMissingHeadingAppendsAtEOF(t *testing.T) {
	content := "# MOC Music\n\n## Resources\n- [[Foo]]\n"
	got := InsertUnderHeading(content, "## Key Notes", "- [[New]]")
	want := "# MOC Music\n\n## Resources\n- [[Foo]]\n- [[New]]\n"
	if got != want {
		t.Errorf("InsertUnderHeading =\n%q\nwant\n%q", got, want)
	}
	if strings.Contains(got, "## Key Notes") {
		t.Error("InsertUnderHeading must never create the missing heading (row #66)")
	}
}

// CONTRACT: AppendMOCEntry's behavior is unchanged by the
// insertAfterHeading refactor (regression guard against Step 3).
func TestAppendMOCEntryStillBlankLineSeparated(t *testing.T) {
	content := "# MOC Music\n\n## 🔗 Recent Additions\n- [[Old]]\n"
	got := AppendMOCEntry(content, "New", "snip")
	want := "# MOC Music\n\n## 🔗 Recent Additions\n\n- [[New]] — snip\n- [[Old]]\n"
	if got != want {
		t.Errorf("AppendMOCEntry regressed:\n%q\nwant\n%q", got, want)
	}
}

// BUG(fixed)(#155): CR/LF and "[" "]" are stripped from caller-supplied
// wikilink display text before interpolation — a name containing a
// newline can no longer inject extra lines into the MOC, and "[" "]"
// can no longer malform the wikilink boundary.
func TestSanitizeWikilinkText(t *testing.T) {
	got := SanitizeWikilinkText("Evil]]\n\n## Injected\nName[[x")
	if strings.ContainsAny(got, "\r\n[]") {
		t.Errorf("SanitizeWikilinkText left unsafe chars: %q", got)
	}
}

func TestSanitizeWikilinkTextLeavesNormalTitleUnchanged(t *testing.T) {
	if got := SanitizeWikilinkText("My Great Note"); got != "My Great Note" {
		t.Errorf("SanitizeWikilinkText = %q", got)
	}
}
```

```go
// internal/vault/moc_new_test.go
package vault

import (
	"strings"
	"testing"
	"time"
)

// CONTRACT(#64): skeleton has Overview/Key Notes/Resources/Related MOCs
// sections plus a created-date stamp, title interpolated into the
// heading and blockquote.
func TestNewMOCSkeleton(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	got := NewMOCSkeleton("Travel", now)
	for _, want := range []string{
		"# MOC Travel\n",
		"> Map of Content for Travel - links to all related notes and resources\n",
		"## Overview\n",
		"## Key Notes\n",
		"## Resources\n",
		"## Related MOCs\n",
		"*Created: 2026-07-15*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("NewMOCSkeleton missing %q in:\n%s", want, got)
		}
	}
}

// A title containing only structural markdown characters still renders —
// NewMOCSkeleton itself does no sanitization (the caller, cmd/ov's
// runMocsNew, strips CR/LF and slugifies the filename separately, row
// #153); this is pure content generation.
func TestNewMOCSkeletonInterpolatesRawTitle(t *testing.T) {
	got := NewMOCSkeleton("Home & Garden", time.Now())
	if !strings.Contains(got, "# MOC Home & Garden") {
		t.Errorf("expected the raw title in the heading, got:\n%s", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vault/... -run 'TestInsertUnderHeading|TestAppendMOCEntryStillBlankLineSeparated|TestSanitizeWikilinkText|TestNewMOCSkeleton' -v`
Expected: FAIL — `InsertUnderHeading`, `SanitizeWikilinkText`, `NewMOCSkeleton` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/vault/moc_write.go — replace the existing AppendMOCEntry
// function with this block (same file, same package, imports unchanged:
// fmt, os, path/filepath, strings)

// insertAfterHeading finds a line exactly equal to heading and inserts
// extraLines immediately after it, returning the modified content and
// true. If no line matches heading, returns content unchanged and
// false. Shared splice primitive behind AppendMOCEntry (creates its
// heading if missing, blank-line-separated, rows #8/#41/#42) and
// InsertUnderHeading (never creates its heading, no blank-line
// separator, row #66) — the two callers' exact create/spacing semantics
// differ, so only the line-splice mechanics are shared, never the
// heading-miss fallback.
func insertAfterHeading(content, heading string, extraLines ...string) (string, bool) {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if line == heading {
			out := make([]string, 0, len(lines)+len(extraLines))
			out = append(out, lines[:i+1]...)
			out = append(out, extraLines...)
			out = append(out, lines[i+1:]...)
			return strings.Join(out, "\n"), true
		}
	}
	return content, false
}

// AppendMOCEntry inserts "- [[title]] — snippet" into content under the
// "## 🔗 Recent Additions" heading, creating that heading (with a leading
// blank line) at EOF if content has no such heading yet. Placement is the v2
// simplification of v1's emoji-heading preference chain (behavior inventory
// rows #8, #41, #42). Pure text transform — the caller re-reads, re-hashes,
// and writes via WriteNoteAtomic (row #129 fix: no more raw >>/mktemp).
func AppendMOCEntry(content, title, snippet string) string {
	const heading = "## 🔗 Recent Additions"
	entry := "- [[" + title + "]] — " + snippet
	if out, ok := insertAfterHeading(content, heading, "", entry); ok {
		return out
	}
	trimmed := strings.TrimRight(content, "\n")
	return trimmed + "\n\n" + heading + "\n" + entry + "\n"
}

// InsertUnderHeading inserts entry immediately below the line matching
// heading (no blank-line separator), or appends entry at EOF, unchanged,
// when heading is missing — heading itself is NEVER created (unlike
// AppendMOCEntry, row #8/#41/#42's simplification). Ports v1 mocs_add's
// exact insertion semantics: `sed -i "/## Key Notes/a\- [[$note_name]]"`
// when the heading exists, else a plain `>>` append (behavior inventory
// row #66). Pure text transform — the caller re-reads, re-hashes, and
// writes via WriteNoteAtomic, same as AppendMOCEntry/RenameMOCLink.
func InsertUnderHeading(content, heading, entry string) string {
	if out, ok := insertAfterHeading(content, heading, entry); ok {
		return out
	}
	trimmed := strings.TrimRight(content, "\n")
	return trimmed + "\n" + entry + "\n"
}

// SanitizeWikilinkText strips characters that would corrupt a "[[text]]"
// wikilink or inject additional lines when text is caller-supplied free
// text never validated against a real note (mocs add's <note-name>, row
// #66/#155): CR/LF (line injection into the MOC file, same defense-in-
// depth class as row #142's frontmatter tag sanitization) and "[" / "]"
// (would prematurely close or malform the wikilink boundary).
func SanitizeWikilinkText(s string) string {
	return strings.NewReplacer("\r", "", "\n", "", "[", "", "]", "").Replace(s)
}
```

Leave `FindMOCByName`, `mocAt`, and `RenameMOCLink` (below `AppendMOCEntry` in the same file) untouched.

```go
// internal/vault/moc_new.go
package vault

import (
	"fmt"
	"time"
)

// mocSkeletonTemplate ports v1 moc_new's heredoc skeleton verbatim
// (Overview/Key Notes/Resources/Related MOCs + created-date stamp,
// behavior inventory row #64).
const mocSkeletonTemplate = `# MOC %s

> Map of Content for %s - links to all related notes and resources

## Overview

## Key Notes

## Resources

## Related MOCs

---
*Created: %s*
`

// NewMOCSkeleton renders the embedded MOC skeleton for title stamped
// with now's date. Pure content generation — the caller (cmd/ov's
// runMocsNew) strips CR/LF from title before calling this and resolves
// the target path via Slugify + ContainPath (row #153's traversal-safety
// half) before writing via WriteNoteAtomic, matching this package's
// "mechanical ops live in vault" split (design spec's internal/moc doc
// comment).
func NewMOCSkeleton(title string, now time.Time) string {
	return fmt.Sprintf(mocSkeletonTemplate, title, title, now.Format("2006-01-02"))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vault/... -v`
Expected: PASS, full package green (no regressions in existing `AppendMOCEntry`/`FindMOCByName`/`RenameMOCLink` tests).

- [ ] **Step 5: Commit**

```bash
git add internal/vault/moc_write.go internal/vault/moc_write_test.go internal/vault/moc_new.go internal/vault/moc_new_test.go
git commit -m "Add vault.InsertUnderHeading, SanitizeWikilinkText, NewMOCSkeleton for phase 4 mocs new/add"
```

---

### Task 2: `cmd/ov mocs new` and `cmd/ov mocs add`

Wires Task 1's vault primitives into two new `mocs` subcommands, matching `mocs list`/`mocs orphan`'s existing thin-command style (`resolveConfig` → vault call → one line on stdout, chrome on stderr, row #123 discipline).

**Files:**
- Modify: `cmd/ov/mocs.go` (add `newMocsNewCmd`, `runMocsNew`, `newMocsAddCmd`, `runMocsAdd`; register both on `newMocsCmd`)
- Modify: `cmd/ov/mocs_test.go` (add tests; add `"os"`, `"path/filepath"` to the import block, which currently has only `"reflect"`, `"strings"`, `"testing"`)

**Interfaces:**
- Consumes: `vault.NewMOCSkeleton`, `vault.InsertUnderHeading`, `vault.SanitizeWikilinkText` (Task 1); `vault.Slugify`, `vault.ContainPath`, `vault.WriteNoteAtomic`, `vault.ReadNote`, `vault.FindMOCByName` (existing, phase 0-2)
- Produces:
  - `func runMocsNew(cfg *config.Config, title string, now time.Time) (string, error)`
  - `func runMocsAdd(cfg *config.Config, mocName, noteName string) (string, error)`

- [ ] **Step 1: Write the failing tests**

```go
// cmd/ov/mocs_test.go — append at end of file

// CONTRACT(#64): mocs new creates "MOC <title>.md" in Resources from the
// embedded skeleton; stdout is the vault-relative path (row #123
// discipline). BUG(fixed)(#7,#154): no obsidian://open side effect —
// the only observable outcome is the file and the printed path.
func TestMocsNew(t *testing.T) {
	vaultDir := newVaultFixture(t)
	out, errs, err := runCmd(t, "mocs", "new", "Travel")
	if err != nil {
		t.Fatalf("%v\n%s", err, errs)
	}
	if strings.TrimSpace(out) != "03-Resources/MOC Travel.md" {
		t.Errorf("stdout = %q", out)
	}
	got, rerr := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Travel.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(got), "# MOC Travel") || !strings.Contains(string(got), "## Key Notes") {
		t.Errorf("skeleton content missing:\n%s", got)
	}
}

// CONTRACT(#64): empty title errors.
func TestMocsNewEmptyTitleErrors(t *testing.T) {
	newVaultFixture(t)
	_, _, err := runCmd(t, "mocs", "new", "")
	if err == nil {
		t.Fatal("expected an error for empty title")
	}
}

// BUG(fixed)(#153): a title containing "/" cannot escape the Resources
// directory — it is slugified (forbidden chars stripped) before joining.
func TestMocsNewTitleSlugifiedForFilename(t *testing.T) {
	vaultDir := newVaultFixture(t)
	out, _, err := runCmd(t, "mocs", "new", "a/b")
	if err != nil {
		t.Fatal(err)
	}
	rel := strings.TrimSpace(out)
	if strings.Contains(rel, "..") || !strings.HasPrefix(rel, "03-Resources/") {
		t.Fatalf("title escaped Resources: %q", rel)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, filepath.FromSlash(rel))); statErr != nil {
		t.Fatalf("file not written inside vault: %v", statErr)
	}
}

// CONTRACT(#99-style): an existing MOC file is refused, never overwritten.
func TestMocsNewRefusesExisting(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Travel.md", "existing", 0)
	_, _, err := runCmd(t, "mocs", "new", "Travel")
	if err == nil {
		t.Fatal("expected an error for an existing MOC")
	}
}

// CONTRACT(#66): mocs add inserts under "## Key Notes".
func TestMocsAddExistingHeading(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Music.md", "# MOC Music\n\n## Key Notes\n- [[Old]]\n", 0)
	out, errs, err := runCmd(t, "mocs", "add", "Music", "New Song")
	if err != nil {
		t.Fatalf("%v\n%s", err, errs)
	}
	if strings.TrimSpace(out) != "03-Resources/MOC Music.md" {
		t.Errorf("stdout = %q", out)
	}
	got, _ := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	want := "# MOC Music\n\n## Key Notes\n- [[New Song]]\n- [[Old]]\n"
	if string(got) != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// CONTRACT(#66): missing "## Key Notes" appends at EOF, no heading created.
func TestMocsAddMissingHeadingAppendsAtEOF(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Music.md", "# MOC Music\n\n## Resources\n- [[Foo]]\n", 0)
	_, _, err := runCmd(t, "mocs", "add", "Music", "New Song")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	if !strings.Contains(string(got), "- [[New Song]]\n") || strings.Contains(string(got), "## Key Notes") {
		t.Errorf("got:\n%s", got)
	}
}

// CONTRACT(#47-style): a MOC name that doesn't resolve is an error.
func TestMocsAddUnknownMOCErrors(t *testing.T) {
	newVaultFixture(t)
	_, _, err := runCmd(t, "mocs", "add", "Nonexistent", "Note")
	if err == nil {
		t.Fatal("expected an error")
	}
}

// BUG(fixed)(#155): a note-name containing a newline cannot inject extra
// lines into the MOC.
func TestMocsAddSanitizesNoteName(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Music.md", "# MOC Music\n\n## Key Notes\n", 0)
	_, _, err := runCmd(t, "mocs", "add", "Music", "Evil]]\n\n## Injected")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	if strings.Contains(string(got), "## Injected") {
		t.Errorf("note-name injection not sanitized:\n%s", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ov/... -run 'TestMocsNew|TestMocsAdd' -v`
Expected: FAIL — `mocs new`/`mocs add` are unknown subcommands.

- [ ] **Step 3: Write the implementation**

```go
// cmd/ov/mocs.go — replace the whole file

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

func newMocsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mocs",
		Short: "Maps of Content",
	}
	cmd.AddCommand(newMocsListCmd(), newMocsOrphanCmd(), newMocsNewCmd(), newMocsAddCmd())
	return cmd
}

func newMocsListCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List MOCs with item counts and descriptions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
			mocs, err := vault.MOCs(cfg.VaultDir)
			if err != nil {
				return err
			}
			if len(mocs) == 0 {
				fmt.Fprintln(errw, "No MOCs found")
				return nil
			}
			fmt.Fprintln(errw, "Maps of Content")
			for _, m := range mocs {
				fmt.Fprintf(out, "%s\t%d\t%s\n", m.Name, m.ItemCount, m.Description)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}

func newMocsOrphanCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "orphan",
		Short: "List notes not linked from any MOC",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
			// Scope: Resources + Areas only (behavior inventory row #65).
			orphans, err := vault.Orphans(cfg.VaultDir, []string{cfg.Resources, cfg.Areas})
			if err != nil {
				return err
			}
			if len(orphans) == 0 {
				fmt.Fprintln(errw, "No orphaned notes")
				return nil
			}
			fmt.Fprintln(errw, "Orphaned notes (not linked from any MOC)")
			for _, n := range orphans {
				fmt.Fprintln(out, n.Rel)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}

func newMocsNewCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a new MOC from the skeleton template",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			rel, err := runMocsNew(cfg, args[0], time.Now())
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

// runMocsNew is the testable core of `ov2 mocs new`: an empty title is an
// error (row #64). The filename is built via a slugified title routed
// through vault.ContainPath (row #153's traversal fix — v1 interpolated
// the raw title unsanitized) and refused if a MOC already exists there
// (WriteNoteAtomic's ErrExists, create-new mode). The visible content
// keeps the CR/LF-stripped-only raw title (row #153). v1's auto-open
// side effect (row #7/#154) is deliberately not ported.
func runMocsNew(cfg *config.Config, title string, now time.Time) (string, error) {
	if title == "" {
		return "", errors.New("MOC title cannot be empty")
	}
	clean := strings.NewReplacer("\r", "", "\n", "").Replace(title)
	slug := vault.Slugify(clean, 80)
	filename := "MOC " + slug + ".md"
	targetAbs, err := vault.ContainPath(cfg.VaultDir, filepath.Join(cfg.Resources, filename))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
		return "", err
	}
	content := vault.NewMOCSkeleton(clean, now)
	if err := vault.WriteNoteAtomic(targetAbs, []byte(content), ""); err != nil {
		return "", err
	}
	rel, err := filepath.Rel(cfg.VaultDir, targetAbs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func newMocsAddCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "add <moc-name> <note-name>",
		Short: "Add a note entry to a MOC's Key Notes section",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			rel, err := runMocsAdd(cfg, args[0], args[1])
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

// runMocsAdd is the testable core of `ov2 mocs add`: resolves mocName via
// vault.FindMOCByName (row #33), sanitizes noteName (row #155 — free
// text, never validated as a real note, matching v1's DECIDE), inserts
// "- [[noteName]]" under "## Key Notes" or appends at EOF (row #66), and
// writes via a conditional WriteNoteAtomic (re-read + hash immediately
// before write, row #106's discipline applied to every MOC mutation).
func runMocsAdd(cfg *config.Config, mocName, noteName string) (string, error) {
	moc, err := vault.FindMOCByName(cfg.VaultDir, cfg.Resources, mocName)
	if err != nil {
		return "", err
	}
	clean := vault.SanitizeWikilinkText(noteName)
	if clean == "" {
		return "", errors.New("note name cannot be empty")
	}
	content, hash, err := vault.ReadNote(moc.Path)
	if err != nil {
		return "", err
	}
	newContent := vault.InsertUnderHeading(content, "## Key Notes", "- [["+clean+"]]")
	if err := vault.WriteNoteAtomic(moc.Path, []byte(newContent), hash); err != nil {
		return "", err
	}
	return moc.Rel, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ov/... -v`
Expected: PASS, full package green.

- [ ] **Step 5: Commit**

```bash
git add cmd/ov/mocs.go cmd/ov/mocs_test.go
git commit -m "Add ov2 mocs new and mocs add"
```

---

### Task 3: `internal/moc` — `Proposal`, prompt builder, `ProposeCleanup`

The read-half of the cleanup core: the JSON response schema, the ported `PROMPT_TEMPLATE` (row #110), and the LLM call + decode into a typed `Proposal` (row #112), mirroring `internal/triage.Propose`'s shape exactly (a package-local `Runner` interface, `llm.ExtractJSON`, marshal-then-unmarshal into the typed struct).

**Files:**
- Create: `internal/moc/proposal.go`
- Create: `internal/moc/prompt.go`
- Create: `internal/moc/prompt_test.go`
- Create: `internal/moc/propose.go`
- Create: `internal/moc/propose_test.go`
- Create: `internal/moc/testdata/prompt_golden.txt` (generated in Step 4, not hand-written)

**Interfaces:**
- Consumes: `llm.ExtractJSON` (existing, phase 0/3)
- Produces:
  - `type Proposal struct { NewContent string; DuplicatesFlagged []string; Summary string }`
  - `type Runner interface { Run(ctx context.Context, prompt string) (string, error) }`
  - `func BuildPrompt(mocContent, mocName string) string`
  - `func ProposeCleanup(ctx context.Context, runner Runner, mocPath, mocContent, mocName string) (Proposal, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/moc/propose_test.go
package moc

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

// CONTRACT(#110,#112): ProposeCleanup builds the prompt, calls the
// runner, and decodes the JSON response into a typed Proposal.
func TestProposeCleanupDecodesResponse(t *testing.T) {
	runner := &fakeRunner{response: `{"new_content":"# MOC Test\n","duplicates_flagged":["a vs b"],"summary":"tidied"}`}
	p, err := ProposeCleanup(context.Background(), runner, "MOC Test.md", "# MOC Test\n\n- [[a]]\n", "MOC Test")
	if err != nil {
		t.Fatal(err)
	}
	if p.NewContent != "# MOC Test\n" || p.Summary != "tidied" || len(p.DuplicatesFlagged) != 1 {
		t.Errorf("Proposal = %+v", p)
	}
	if !strings.Contains(runner.gotPrompt, "MOC name: MOC Test") {
		t.Errorf("prompt missing moc name:\n%s", runner.gotPrompt)
	}
	if !strings.Contains(runner.gotPrompt, "- [[a]]") {
		t.Errorf("prompt missing moc content:\n%s", runner.gotPrompt)
	}
}

// CONTRACT(#112): duplicates_flagged/summary default to Go zero values
// when absent from the response.
func TestProposeCleanupDefaultsMissingFields(t *testing.T) {
	runner := &fakeRunner{response: `{"new_content":"content"}`}
	p, err := ProposeCleanup(context.Background(), runner, "MOC Test.md", "content", "MOC Test")
	if err != nil {
		t.Fatal(err)
	}
	if p.DuplicatesFlagged != nil || p.Summary != "" {
		t.Errorf("Proposal = %+v, want zero-value defaults", p)
	}
}

// Mirrors test_parse_llm_response_rejects_missing_new_content: a response
// missing new_content errors with the raw payload attached.
func TestProposeCleanupRejectsMissingNewContent(t *testing.T) {
	runner := &fakeRunner{response: `{"duplicates_flagged":[],"summary":"x"}`}
	_, err := ProposeCleanup(context.Background(), runner, "MOC Test.md", "content", "MOC Test")
	if err == nil || !strings.Contains(err.Error(), "new_content") {
		t.Errorf("err = %v, want mention of new_content", err)
	}
}

// CONTRACT(#92): a fenced-JSON response still decodes.
func TestProposeCleanupDecodesFencedResponse(t *testing.T) {
	runner := &fakeRunner{response: "```json\n{\"new_content\":\"c\"}\n```"}
	p, err := ProposeCleanup(context.Background(), runner, "x.md", "content", "MOC X")
	if err != nil {
		t.Fatal(err)
	}
	if p.NewContent != "c" {
		t.Errorf("NewContent = %q", p.NewContent)
	}
}

// A runner failure propagates so the caller can classify it (e.g. llm.ErrAuth).
func TestProposeCleanupPropagatesRunnerError(t *testing.T) {
	wantErr := errors.New("llm auth expired")
	runner := &fakeRunner{err: wantErr}
	_, err := ProposeCleanup(context.Background(), runner, "x.md", "content", "MOC X")
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}
```

```go
// internal/moc/prompt_test.go
package moc

import (
	"os"
	"strings"
	"testing"
)

// Golden file (design spec §Testing strategy tier 2: "both LLM prompt
// assemblies (prompt drift = silent behavior change)" — triage's is
// phase 3's; this is moc cleanup's). Generated by Step 4 below via
// UPDATE_GOLDEN=1; update deliberately, never silently, when the prompt
// text intentionally changes.
func TestBuildPromptGolden(t *testing.T) {
	got := BuildPrompt("---\ntype: moc\n---\n# MOC Test\n\n## Resources\n\n- [[Foo]] — https://example.com/foo\n", "MOC Test")

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

// CONTRACT(#110): forbidden/allowed operations and the JSON shape are
// stated explicitly (mirrors test_build_prompt_states_forbidden_operations
// / test_build_prompt_requests_json_shape).
func TestBuildPromptStatesContract(t *testing.T) {
	got := BuildPrompt("content", "MOC Test")
	lower := strings.ToLower(got)
	if !strings.Contains(lower, "must not delete") {
		t.Error("prompt missing the forbidden-delete rule")
	}
	if !strings.Contains(lower, "frontmatter") {
		t.Error("prompt missing frontmatter mention")
	}
	if !strings.Contains(got, `"new_content"`) || !strings.Contains(got, `"duplicates_flagged"`) {
		t.Error("prompt missing the JSON response shape")
	}
}

// CONTRACT(#110): the full MOC content is embedded verbatim (mirrors
// test_build_prompt_includes_full_moc_content).
func TestBuildPromptIncludesFullContent(t *testing.T) {
	mocText := "---\ntype: moc\n---\n# MOC Test\n\n## Resources\n\n- [[foo]] — bar\n"
	got := BuildPrompt(mocText, "MOC Test")
	if !strings.Contains(got, mocText) {
		t.Errorf("prompt does not contain full moc content verbatim")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/moc/... -v`
Expected: FAIL — package `internal/moc` does not exist yet (`Proposal`, `Runner`, `BuildPrompt`, `ProposeCleanup` undefined).

- [ ] **Step 3: Write the implementation**

```go
// internal/moc/proposal.go
package moc

// Proposal matches moc_cleanup.py's JSON response shape exactly
// (behavior inventory row #112): new_content is required; duplicates_
// flagged and summary default to Go's zero values (nil slice / empty
// string) when absent from the LLM's response, mirroring v1's dict.
// setdefault.
type Proposal struct {
	NewContent        string   `json:"new_content"`
	DuplicatesFlagged []string `json:"duplicates_flagged"`
	Summary           string   `json:"summary"`
}
```

```go
// internal/moc/prompt.go
package moc

import "fmt"

// promptTemplate ports moc_cleanup.py's PROMPT_TEMPLATE verbatim (row
// #110). Backtick raw strings need no escaping for the literal "\n"
// sequences the JSON example shows the LLM (Python's "\\n" in that
// source is likewise the literal two-character sequence, not a real
// newline — Go's backtick strings match that directly).
const promptTemplate = `You are reorganizing a single Obsidian "Map of Content" (MOC) file. This is
a hand-curated index of links, and your job is ONLY to tidy its structure —
not to change what it indexes.

MOC name: %s

ALLOWED changes:
- Move existing bullet entries under a more fitting existing "###" subheading
  (e.g. group entries from the same source/site/topic together), or create a
  new "###" subheading if none fits.
- Fix an entry's title/link text if it is obviously garbled — e.g. a
  slugified URL like "[[https example com foo-bar]]" should become a
  readable title if one can be inferred from the URL or surrounding text.
- Fix obvious formatting issues: inconsistent bullet markers, stray blank
  lines, misplaced "---" separators.
- Reorder subsections for readability (e.g. alphabetical, or grouped by
  source) if it clearly improves scannability.

FORBIDDEN changes (must not do these, no exceptions):
- Must not delete any existing bullet, link, or line of content.
- Must not invent new links, URLs, or entries that are not already present.
- Must not alter the YAML frontmatter (the block between the two "---"
  lines at the top of the file) in any way.
- Must not rewrite free-text prose sections (e.g. "## Notes", "## Overview",
  "> blockquote" descriptions) beyond fixing whitespace.
- Must not change top-level "##" heading text or emoji, only reorganize what
  is nested under them (and manage "###" subheadings within them).

If you find two entries that look like duplicates (same URL, or same title
under different phrasing), do NOT merge or delete either one — keep both and
report them in "duplicates_flagged" instead, so a human can decide.

Return ONLY a JSON object with this exact shape, no markdown fences, no
commentary before or after:

{
  "new_content": "<the complete reorganized file content, including the\nunchanged frontmatter block, as a single string with literal \n newlines>",
  "duplicates_flagged": ["<human-readable description of duplicate pair 1>", "..."],
  "summary": "<one or two sentences describing what you changed and why>"
}

Here is the current file content:

---
%s
---
`

// BuildPrompt assembles the moc_cleanup prompt for one MOC, mirroring
// moc_cleanup.py's build_prompt (row #110).
func BuildPrompt(mocContent, mocName string) string {
	return fmt.Sprintf(promptTemplate, mocName, mocContent)
}
```

```go
// internal/moc/propose.go
package moc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
)

// Runner is the subset of *llm.Runner's method set ProposeCleanup needs —
// an interface so tests inject a fake without spawning a real subprocess
// (same injection pattern as triage.Runner; kept as its own package-
// local interface rather than importing triage.Runner, matching this
// codebase's convention of narrow, package-local collaborator
// interfaces with no shared base type).
type Runner interface {
	Run(ctx context.Context, prompt string) (string, error)
}

// ProposeCleanup builds the moc_cleanup.py prompt (row #110) for the MOC
// at mocPath (mocContent is read fresh by the caller — cmd/ov's cleanup
// loop — before calling this, mirroring moc_cleanup.py main()'s own
// per-file read), asks the LLM for a reorganization proposal, and
// decodes the response into a typed Proposal via llm.ExtractJSON's
// 3-tier fallback (row #92) plus the required-field check moc_cleanup.
// py's parse_llm_response performs (row #112): "new_content" must be
// present and a string, else the error carries the raw response.
func ProposeCleanup(ctx context.Context, runner Runner, mocPath, mocContent, mocName string) (Proposal, error) {
	prompt := BuildPrompt(mocContent, mocName)
	raw, err := runner.Run(ctx, prompt)
	if err != nil {
		return Proposal{}, fmt.Errorf("moc cleanup %s: %w", mocPath, err)
	}
	obj, err := llm.ExtractJSON(raw)
	if err != nil {
		return Proposal{}, fmt.Errorf("moc cleanup %s: %w", mocPath, err)
	}
	if _, ok := obj["new_content"].(string); !ok {
		return Proposal{}, fmt.Errorf("moc cleanup %s: LLM response missing required string field 'new_content':\n%s", mocPath, raw)
	}
	buf, err := json.Marshal(obj)
	if err != nil {
		return Proposal{}, fmt.Errorf("moc cleanup %s: re-marshaling decoded JSON: %w", mocPath, err)
	}
	var p Proposal
	if err := json.Unmarshal(buf, &p); err != nil {
		return Proposal{}, fmt.Errorf("moc cleanup %s: proposal did not match the expected schema: %w", mocPath, err)
	}
	return p, nil
}
```

- [ ] **Step 4: Generate the golden file, then run tests to verify they pass**

```bash
UPDATE_GOLDEN=1 go test ./internal/moc/... -run TestBuildPromptGolden -v
go test ./internal/moc/... -v
```

Expected: the first command writes `internal/moc/testdata/prompt_golden.txt`; the second is a full PASS with no `UPDATE_GOLDEN` set (proving the golden file is stable on a second run).

- [ ] **Step 5: Commit**

```bash
git add internal/moc/proposal.go internal/moc/prompt.go internal/moc/prompt_test.go internal/moc/propose.go internal/moc/propose_test.go internal/moc/testdata/prompt_golden.txt
git commit -m "Add internal/moc: Proposal schema, prompt builder, ProposeCleanup"
```

---

### Task 4: `internal/moc` — `Validate`

The structural safety net, ported 1:1 from `tests/test_moc_cleanup.py` first (design spec §Testing strategy tier 1's own "moc validator" bullet), then extended with a few additional table cases. Pure functions, no filesystem or network access — no security-focused review routing needed for this task (routing applies to Task 5/6, the write/subprocess-adjacent work).

**Files:**
- Create: `internal/moc/validate.go`
- Create: `internal/moc/validate_test.go`

**Interfaces:**
- Consumes: nothing from other tasks
- Produces:
  - `var ErrFrontmatterChanged, ErrBareWikilinkDropped, ErrURLDropped error`
  - `func Validate(original, proposed string) error`

- [ ] **Step 1: Write the failing tests**

```go
// internal/moc/validate_test.go
package moc

import (
	"errors"
	"strings"
	"testing"
)

// Ported 1:1 from tests/test_moc_cleanup.py (design spec §Testing
// strategy tier 1: "port existing pytest suites 1:1 first").

const validateOriginal = "---\ntype: moc\n---\n" +
	"# MOC Test\n\n" +
	"## Resources\n\n" +
	"- [[Foo Note]] — https://example.com/foo\n" +
	"- [[Bar Note]] — https://example.com/bar\n"

// CONTRACT(#107,#109): reordering that keeps every link/wikilink is
// accepted (tests/test_moc_cleanup.py:48-58).
func TestValidateAcceptsReorderingThatKeepsAllLinks(t *testing.T) {
	reordered := "---\ntype: moc\n---\n" +
		"# MOC Test\n\n" +
		"## Resources\n\n" +
		"### Example.com\n" +
		"- [[Bar Note]] — https://example.com/bar\n" +
		"- [[Foo Note]] — https://example.com/foo\n"
	if err := Validate(validateOriginal, reordered); err != nil {
		t.Errorf("unexpected rejection: %v", err)
	}
}

// CONTRACT(#109): deleting an entire entry drops its URL — rejected
// (tests/test_moc_cleanup.py:61-76).
func TestValidateRejectsDroppedEntryURLAndWikilink(t *testing.T) {
	missingEntry := "---\ntype: moc\n---\n" +
		"# MOC Test\n\n" +
		"## Resources\n\n" +
		"- [[Foo Note]] — https://example.com/foo\n"
	err := Validate(validateOriginal, missingEntry)
	if !errors.Is(err, ErrURLDropped) {
		t.Fatalf("err = %v, want ErrURLDropped", err)
	}
	if !strings.Contains(err.Error(), "https://example.com/bar") {
		t.Errorf("err = %v, want it to name the dropped URL", err)
	}
}

// CONTRACT(#108): any frontmatter change rejects the whole proposal
// (tests/test_moc_cleanup.py:79-91).
func TestValidateRejectsFrontmatterChange(t *testing.T) {
	changedFM := "---\ntype: moc\nextra: field\n---\n" +
		"# MOC Test\n\n" +
		"## Resources\n\n" +
		"- [[Foo Note]] — https://example.com/foo\n" +
		"- [[Bar Note]] — https://example.com/bar\n"
	err := Validate(validateOriginal, changedFM)
	if !errors.Is(err, ErrFrontmatterChanged) {
		t.Fatalf("err = %v, want ErrFrontmatterChanged", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "frontmatter") {
		t.Errorf("err = %v, want it to mention frontmatter", err)
	}
}

// CONTRACT(#107): a URL-anchored wikilink may be retitled freely — the
// whole point of the garbled-title-fix feature (tests/test_moc_cleanup.
// py:94-106).
func TestValidateAcceptsRetitlingAURLAnchoredWikilink(t *testing.T) {
	retitled := "---\ntype: moc\n---\n" +
		"# MOC Test\n\n" +
		"## Resources\n\n" +
		"- [[Foo Article]] — https://example.com/foo\n" +
		"- [[Bar Note]] — https://example.com/bar\n"
	if err := Validate(validateOriginal, retitled); err != nil {
		t.Errorf("unexpected rejection: %v", err)
	}
}

// CONTRACT(#107): a BARE wikilink (no URL on its line) must survive
// verbatim — renaming it is rejected (tests/test_moc_cleanup.py:109-129).
func TestValidateRejectsRenamedBareWikilinkWithNoURL(t *testing.T) {
	original := "---\ntype: moc\n---\n# MOC Test\n\n## Resources\n\n- [[Neovim]] — my editor setup\n"
	renamed := "---\ntype: moc\n---\n# MOC Test\n\n## Resources\n\n- [[Neovim Notes]] — my editor setup\n"
	err := Validate(original, renamed)
	if !errors.Is(err, ErrBareWikilinkDropped) {
		t.Fatalf("err = %v, want ErrBareWikilinkDropped", err)
	}
	if !strings.Contains(err.Error(), "Neovim") {
		t.Errorf("err = %v, want it to name the dropped wikilink", err)
	}
}

// CONTRACT(#109): title changed AND its URL vanished entirely — the URL
// check catches cases the wikilink check might miss (tests/
// test_moc_cleanup.py:132-146).
func TestValidateRejectsDroppedURL(t *testing.T) {
	missingURL := "---\ntype: moc\n---\n" +
		"# MOC Test\n\n" +
		"## Resources\n\n" +
		"- [[Foo Note]] — https://example.com/foo\n" +
		"- [[Bar Note]] — some text with no url\n"
	err := Validate(validateOriginal, missingURL)
	if !errors.Is(err, ErrURLDropped) {
		t.Fatalf("err = %v, want ErrURLDropped", err)
	}
}

// Additional table cases beyond the ported pytest suite.

// CONTRACT(#108): identical content (no proposal change) validates.
func TestValidateAcceptsIdenticalContent(t *testing.T) {
	if err := Validate(validateOriginal, validateOriginal); err != nil {
		t.Errorf("unexpected rejection of identical content: %v", err)
	}
}

// CONTRACT(#107): a wikilink and a URL on the SAME line as prose (not a
// bullet entry) still counts as anchored — the check is per-line, not
// per-bullet.
func TestValidateBareLinkCheckIsPerLine(t *testing.T) {
	original := "---\ntype: moc\n---\n# MOC Test\n\nSee [[Setup Guide]] at https://example.com/guide for details.\n"
	retitled := "---\ntype: moc\n---\n# MOC Test\n\nSee [[Setup Docs]] at https://example.com/guide for details.\n"
	if err := Validate(original, retitled); err != nil {
		t.Errorf("unexpected rejection: %v", err)
	}
}

// CONTRACT(#108): a MOC with no frontmatter block at all in either
// version validates (both sides have "no block", which compares equal).
func TestValidateNoFrontmatterEitherSide(t *testing.T) {
	original := "# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	proposed := "# MOC Test\n\n### Group\n- [[Foo]] — https://example.com/foo\n"
	if err := Validate(original, proposed); err != nil {
		t.Errorf("unexpected rejection: %v", err)
	}
}

// CONTRACT(#108): proposing to ADD a frontmatter block where none
// existed is still a frontmatter change — rejected.
func TestValidateRejectsAddingFrontmatterBlock(t *testing.T) {
	original := "# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	proposed := "---\ntype: moc\n---\n# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	if err := Validate(original, proposed); !errors.Is(err, ErrFrontmatterChanged) {
		t.Errorf("err = %v, want ErrFrontmatterChanged", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/moc/... -run TestValidate -v`
Expected: FAIL — `Validate`, `ErrFrontmatterChanged`, `ErrBareWikilinkDropped`, `ErrURLDropped` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/moc/validate.go
package moc

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	wikilinkRe    = regexp.MustCompile(`\[\[([^\]|]+)`)
	urlRe         = regexp.MustCompile(`https?://\S+`)
	frontmatterRe = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?`)
)

var (
	// ErrFrontmatterChanged mirrors moc_cleanup.py validate_proposal's
	// frontmatter check (row #108): any change to the block, including
	// adding one where none existed, rejects the whole proposal.
	ErrFrontmatterChanged = errors.New("moc: proposal changes the frontmatter block, which is forbidden")
	// ErrBareWikilinkDropped is row #107's bare-wikilink half: a
	// wikilink with no URL on its line is the only anchor for that
	// entry and must survive verbatim.
	ErrBareWikilinkDropped = errors.New("moc: proposal drops or renames a bare wikilink (no URL to anchor it) present in the original")
	// ErrURLDropped is row #109's URL half — catches dropped entries
	// (anchored or not) the wikilink check alone might miss.
	ErrURLDropped = errors.New("moc: proposal drops a URL present in the original")
)

// frontmatterBlock returns the leading "---\n...\n---\n?" block
// (delimiters included) via the same regex moc_cleanup.py's
// FRONTMATTER_RE uses, or "" if text has none. Deliberately independent
// of vault.ParseNote — this validator's job is byte-for-byte literal
// change detection matching v1's exact algorithm, not vault's lenient
// parsing (which has its own deliberate closing-fence divergence, row
// #84, irrelevant here).
func frontmatterBlock(text string) string {
	return frontmatterRe.FindString(text)
}

// bareWikilinks returns the set of wikilink targets from lines that do
// NOT also contain a URL. A wikilink sharing its line with a URL is
// "anchored" — its display title may be freely corrected (the garbled-
// title-fix feature) because the URL still identifies the entry; a bare
// wikilink has no other anchor, so it must survive verbatim (row #107).
func bareWikilinks(text string) map[string]bool {
	out := make(map[string]bool)
	for _, line := range strings.Split(text, "\n") {
		if urlRe.MatchString(line) {
			continue
		}
		for _, m := range wikilinkRe.FindAllStringSubmatch(line, -1) {
			out[m[1]] = true
		}
	}
	return out
}

func urlSet(text string) map[string]bool {
	out := make(map[string]bool)
	for _, u := range urlRe.FindAllString(text, -1) {
		out[u] = true
	}
	return out
}

func sortedMissing(orig, new map[string]bool) []string {
	var missing []string
	for k := range orig {
		if !new[k] {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	return missing
}

// Validate is the structural safety net, independent of prompt
// compliance (design spec §Architecture, rows #107-109): it does NOT
// guarantee the reorganization is *good* — only that it didn't lose
// frontmatter, URLs, or bare (URL-less) wikilinks present in the
// original. Ports moc_cleanup.py validate_proposal exactly.
func Validate(original, proposed string) error {
	if frontmatterBlock(original) != frontmatterBlock(proposed) {
		return ErrFrontmatterChanged
	}

	dropped := sortedMissing(bareWikilinks(original), bareWikilinks(proposed))
	if len(dropped) > 0 {
		return fmt.Errorf("%w: %s", ErrBareWikilinkDropped, strings.Join(dropped, ", "))
	}

	droppedURLs := sortedMissing(urlSet(original), urlSet(proposed))
	if len(droppedURLs) > 0 {
		return fmt.Errorf("%w: %s", ErrURLDropped, strings.Join(droppedURLs, ", "))
	}

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/moc/... -v`
Expected: PASS, full package green.

- [ ] **Step 5: Commit**

```bash
git add internal/moc/validate.go internal/moc/validate_test.go
git commit -m "Add internal/moc.Validate: ported moc_cleanup.py structural safety net"
```

---

### Task 5: `internal/moc` — `Apply`, `Diff`

The write half: writes the accepted proposal via `vault.WriteNoteAtomic` (row #116 fix), calls `Validate` itself first as defense in depth (mirroring `internal/triage.Apply`'s established posture — phase 3's own Task 5 found and closed a CRITICAL injection bug under this exact review posture; read `.superpowers/sdd/progress.md`'s Phase 3 Task 5 entry before reviewing this task), and independently no-ops when `proposed == original` (row #115's backstop). `Diff` is a thin wrapper reusing `internal/triage.Diff` directly — no reimplementation, no import cycle (`internal/triage` never imports `internal/moc`).

**This task is routed to a security-focused reviewer at task-review time** (Global Constraints) — it is the write path for LLM-controlled content landing on disk, the same class of risk phase 3 Task 5 found a CRITICAL bug in.

**Files:**
- Create: `internal/moc/apply.go`
- Create: `internal/moc/apply_test.go`
- Create: `internal/moc/diff.go`
- Create: `internal/moc/diff_test.go`

**Interfaces:**
- Consumes: `Validate` (Task 4); `vault.WriteNoteAtomic`, `vault.ErrChangedOnDisk` (existing); `triage.Diff`, `triage.DiffLine` (existing, phase 3)
- Produces:
  - `func Apply(mocPath, original, proposed, expectedHash string) error`
  - `func Diff(old, new string) []triage.DiffLine`

- [ ] **Step 1: Write the failing tests**

```go
// internal/moc/apply_test.go
package moc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

func writeMOC(t *testing.T, dir, name, content string) (path, hash string) {
	t.Helper()
	path = filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, hash, err := vault.ReadNote(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, hash
}

// CONTRACT(#116): a valid, changed proposal writes via WriteNoteAtomic.
func TestApplyWritesValidProposal(t *testing.T) {
	dir := t.TempDir()
	original := "---\ntype: moc\n---\n# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	proposed := "---\ntype: moc\n---\n# MOC Test\n\n### Group\n- [[Foo]] — https://example.com/foo\n"
	path, hash := writeMOC(t, dir, "MOC Test.md", original)

	if err := Apply(path, original, proposed, hash); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != proposed {
		t.Errorf("got:\n%s\nwant:\n%s", got, proposed)
	}
}

// CONTRACT(#115): a proposal identical to the original is a no-op —
// Apply itself never writes, even if called directly (defense-in-depth
// backstop; the primary "unchanged" short-circuit lives in cmd/ov's
// cleanup loop, row #157, before Apply is ever called for this case).
func TestApplyNoOpWhenIdentical(t *testing.T) {
	dir := t.TempDir()
	content := "---\ntype: moc\n---\n# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	path, hash := writeMOC(t, dir, "MOC Test.md", content)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := Apply(path, content, content, hash); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("Apply must not touch the file when proposed == original")
	}
}

// BUG(fixed)(#107-109): Apply refuses an invalid proposal even if a
// caller forgot to call Validate first — the gate lives inside Apply
// too (defense in depth, mirroring triage.Apply's posture). No write
// occurs.
func TestApplyRefusesInvalidProposalWithoutExternalValidate(t *testing.T) {
	dir := t.TempDir()
	original := "---\ntype: moc\n---\n# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	invalid := "---\ntype: moc\nextra: field\n---\n# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	path, hash := writeMOC(t, dir, "MOC Test.md", original)

	err := Apply(path, original, invalid, hash)
	if !errors.Is(err, ErrFrontmatterChanged) {
		t.Fatalf("err = %v, want ErrFrontmatterChanged", err)
	}
	got, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(got) != original {
		t.Error("file must be untouched after a rejected Apply")
	}
}

// CONTRACT(#106): a stale expectedHash (disk changed since it was
// computed) is refused, never silently clobbered.
func TestApplyRefusesStaleHash(t *testing.T) {
	dir := t.TempDir()
	original := "---\ntype: moc\n---\n# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	proposed := "---\ntype: moc\n---\n# MOC Test\n\n### Group\n- [[Foo]] — https://example.com/foo\n"
	path, staleHash := writeMOC(t, dir, "MOC Test.md", original)
	// Simulate a concurrent edit after the hash was captured.
	if err := os.WriteFile(path, []byte(original+"\nEdited concurrently.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Apply(path, original, proposed, staleHash)
	if !errors.Is(err, vault.ErrChangedOnDisk) {
		t.Fatalf("err = %v, want vault.ErrChangedOnDisk", err)
	}
}
```

```go
// internal/moc/diff_test.go
package moc

import "testing"

// CONTRACT(#151 reuse, #111): Diff reuses triage.Diff directly rather
// than reimplementing a second diff engine.
func TestDiffReusesTriageDiff(t *testing.T) {
	old := "line1\nline2\n"
	new := "line1\nline2-changed\n"
	lines := Diff(old, new)
	var sawAdd, sawDel bool
	for _, l := range lines {
		if l.Op == '+' && l.Text == "line2-changed" {
			sawAdd = true
		}
		if l.Op == '-' && l.Text == "line2" {
			sawDel = true
		}
	}
	if !sawAdd || !sawDel {
		t.Errorf("Diff(%q, %q) = %+v, missing expected +/- lines", old, new, lines)
	}
}

// Identical input produces no diff lines (mirrors moc_cleanup.py
// render_diff's "(no changes proposed)" case, row #111 — the message
// itself is cmd/ov's presentation, not Diff's job).
func TestDiffEmptyWhenIdentical(t *testing.T) {
	if lines := Diff("same\n", "same\n"); len(lines) != 0 {
		t.Errorf("Diff of identical content = %+v, want empty", lines)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/moc/... -run 'TestApply|TestDiff' -v`
Expected: FAIL — `Apply`, `Diff` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/moc/apply.go
package moc

import "github.com/arosenkranz/obsidian-vault-tools/internal/vault"

// Apply writes proposed to mocPath via vault.WriteNoteAtomic, conditional
// on expectedHash (the caller re-reads and hashes the file immediately
// before calling Apply, row #106's discipline — same pattern as
// syncMOCLink and triage.Apply). Apply calls Validate(original, proposed)
// itself as its first step — defense in depth: it refuses an unsafe
// proposal even if a caller forgot to call Validate first (design
// spec's "never trust the LLM's own output for a gate" posture,
// mirroring triage.Apply exactly, including the CRITICAL-bug-class
// lesson from phase 3 Task 5, row #152). A proposal identical to
// original is a no-op — nothing is written (row #115); the caller
// (cmd/ov's cleanup loop) independently short-circuits this same case
// BEFORE showing the diff/confirm prompt (row #157), so this branch is
// an unreachable-via-normal-flow backstop, not the primary path.
func Apply(mocPath, original, proposed, expectedHash string) error {
	if err := Validate(original, proposed); err != nil {
		return err
	}
	if proposed == original {
		return nil
	}
	return vault.WriteNoteAtomic(mocPath, []byte(proposed), expectedHash)
}
```

```go
// internal/moc/diff.go
package moc

import "github.com/arosenkranz/obsidian-vault-tools/internal/triage"

// Diff reuses internal/triage.Diff directly — a presentation-free,
// line-level unified diff already shared by tui and web (design spec
// row #151). Importing internal/triage from internal/moc introduces no
// cycle: internal/triage never imports internal/moc, and neither
// package depends on the other's mutation types (Proposal, Validate,
// Apply are all independently named and typed per package). A thin
// wrapper rather than a bare re-export keeps internal/moc's public
// surface self-contained per the design spec's package listing (design
// spec §Architecture: "Diff(old, new string) []triage.DiffLine").
func Diff(old, new string) []triage.DiffLine {
	return triage.Diff(old, new)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/moc/... -v`
Expected: PASS, full package green.

- [ ] **Step 5: Commit**

```bash
git add internal/moc/apply.go internal/moc/apply_test.go internal/moc/diff.go internal/moc/diff_test.go
git commit -m "Add internal/moc.Apply and Diff"
```

---

### Task 6: `cmd/ov mocs cleanup` — orchestration loop, stub-LLM integration tests

The CLI surface: resolves the target(s) (single name via `vault.FindMOCByName`, or `vault.MOCs` for `--all`, row #156), then for each target runs propose → validate → (unchanged short-circuit, row #157) → diff+summary/duplicates display → y/N confirm (EOF = "n" for THIS target only, row #116) → apply or skip, accumulating summary counts. 180s timeout per file (row #90). Reuses `internal/llm.Runner`, `internal/tui.RenderDiff` unchanged.

**This task is routed to a security-focused reviewer at task-review time** (Global Constraints) — it is the orchestration layer around the write path (Task 5) and the subprocess-adjacent LLM call, the same class of surface phase 2/3 routed to security review.

**Files:**
- Modify: `cmd/ov/mocs.go` (add `newMocsCleanupCmd`, `mocCleanupDeps`, `mocCleanupTimeout`, `runMocsCleanup`; register on `newMocsCmd`)
- Modify: `cmd/ov/mocs_test.go` (add orchestration tests using a fake `moc.Runner`; add `"context"`, `"errors"`, `"bufio"` to imports as needed)
- Create: `cmd/ov/mocs_cleanup_integration_test.go` (stub-LLM integration tests via `internal/llmtest`, mirroring `triage_llm_integration_test.go`'s pattern)

**Interfaces:**
- Consumes: `moc.ProposeCleanup`, `moc.Validate`, `moc.Apply`, `moc.Diff`, `moc.Runner`, `moc.Proposal` (Tasks 3-5); `vault.FindMOCByName`, `vault.MOCs`, `vault.ReadNote` (existing); `tui.RenderDiff` (existing, phase 3); `errExitCode2` (existing, `cmd/ov/triage.go`); `llm.NewRunner` (existing, phase 3); `llmtest.SelfCmd`/`StubEnv`/`ResponseEnv`/`ExitCodeEnv`/`StderrEnv` (existing, phase 3)
- Produces:
  - `type mocCleanupDeps struct { runner moc.Runner }`
  - `const mocCleanupTimeout = 180 * time.Second`
  - `func runMocsCleanup(cfg *config.Config, name string, all bool, in *bufio.Reader, out, errw io.Writer, deps mocCleanupDeps) error`

- [ ] **Step 1a: Write the failing orchestration tests (fake `moc.Runner`, no subprocess)**

```go
// cmd/ov/mocs_test.go — append at end of file

type fakeMocRunner struct {
	responses []string
	i         int
	gotPrompt []string
}

func (f *fakeMocRunner) Run(ctx context.Context, prompt string) (string, error) {
	f.gotPrompt = append(f.gotPrompt, prompt)
	if f.i >= len(f.responses) {
		return "", errors.New("fakeMocRunner: no more responses")
	}
	r := f.responses[f.i]
	f.i++
	return r, nil
}

// CONTRACT(#114): name XOR --all enforced, else exit 2.
func TestMocsCleanupRequiresNameXorAll(t *testing.T) {
	newVaultFixture(t)
	deps := mocCleanupDeps{runner: &fakeMocRunner{}}
	err := runMocsCleanup(mustResolveConfig(t), "", false, bufio.NewReader(strings.NewReader("")), io.Discard, io.Discard, deps)
	if !errors.Is(err, errExitCode2) {
		t.Fatalf("err = %v, want errExitCode2", err)
	}
}

func TestMocsCleanupRejectsBothNameAndAll(t *testing.T) {
	newVaultFixture(t)
	deps := mocCleanupDeps{runner: &fakeMocRunner{}}
	err := runMocsCleanup(mustResolveConfig(t), "Music", true, bufio.NewReader(strings.NewReader("")), io.Discard, io.Discard, deps)
	if !errors.Is(err, errExitCode2) {
		t.Fatalf("err = %v, want errExitCode2", err)
	}
}

// CONTRACT(#113): an unresolved single name exits 2.
func TestMocsCleanupUnknownNameErrors(t *testing.T) {
	newVaultFixture(t)
	deps := mocCleanupDeps{runner: &fakeMocRunner{}}
	err := runMocsCleanup(mustResolveConfig(t), "Nonexistent", false, bufio.NewReader(strings.NewReader("")), io.Discard, io.Discard, deps)
	if !errors.Is(err, errExitCode2) {
		t.Fatalf("err = %v, want errExitCode2", err)
	}
}

// CONTRACT(#113): --all on a vault with zero MOCs exits 2.
func TestMocsCleanupEmptyVaultAllErrors(t *testing.T) {
	newVaultFixture(t)
	deps := mocCleanupDeps{runner: &fakeMocRunner{}}
	err := runMocsCleanup(mustResolveConfig(t), "", true, bufio.NewReader(strings.NewReader("")), io.Discard, io.Discard, deps)
	if !errors.Is(err, errExitCode2) {
		t.Fatalf("err = %v, want errExitCode2", err)
	}
}

// CONTRACT(#115,#157): an identical proposal is reported "unchanged"
// WITHOUT ever showing a diff or reading from the confirm prompt — the
// unchanged check runs before the y/N prompt, matching v1's ordering
// exactly. Proven here by supplying an empty stdin reader: if the
// implementation incorrectly tried to read a confirm answer, ReadString
// would return io.EOF and (per row #116) be treated as "n"/skip, which
// would make this assertion fail on "unchanged" count — the test would
// only pass by accident unless the ordering is actually right, so the
// count assertion below is the real proof.
func TestMocsCleanupUnchangedSkipsConfirmPrompt(t *testing.T) {
	vaultDir := newVaultFixture(t)
	content := "---\ntype: moc\n---\n# MOC Music\n\n- [[Foo]] — https://example.com/foo\n"
	addNote(t, vaultDir, "03-Resources/MOC Music.md", content, 0)
	runner := &fakeMocRunner{responses: []string{`{"new_content":"---\ntype: moc\n---\n# MOC Music\n\n- [[Foo]] — https://example.com/foo\n","duplicates_flagged":[],"summary":""}`}}
	var errBuf bytes.Buffer
	deps := mocCleanupDeps{runner: runner}
	err := runMocsCleanup(mustResolveConfig(t), "Music", false, bufio.NewReader(strings.NewReader("")), io.Discard, &errBuf, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "already well-organized") {
		t.Errorf("stderr = %q, want the unchanged message", errBuf.String())
	}
	if strings.Contains(errBuf.String(), "Apply this reorganization?") {
		t.Error("an identical proposal must never show the confirm prompt")
	}
}

// BUG(fixed)(#109): a rejected target does not abort the run — --all
// with one rejected and one valid target still processes both, proving
// no partial-apply and no early exit.
func TestMocsCleanupRejectedMovesToNextTarget(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Alpha.md", "---\ntype: moc\n---\n# MOC Alpha\n\n- [[A]] — https://example.com/a\n", 0)
	addNote(t, vaultDir, "03-Resources/MOC Beta.md", "---\ntype: moc\n---\n# MOC Beta\n\n- [[B]] — https://example.com/b\n", 0)
	runner := &fakeMocRunner{responses: []string{
		// Alpha: frontmatter mutated -> rejected.
		`{"new_content":"---\ntype: moc\nextra: x\n---\n# MOC Alpha\n\n- [[A]] — https://example.com/a\n","duplicates_flagged":[],"summary":""}`,
		// Beta: valid reorganization -> applied (confirmed via stdin "y").
		`{"new_content":"---\ntype: moc\n---\n# MOC Beta\n\n### Group\n- [[B]] — https://example.com/b\n","duplicates_flagged":[],"summary":"grouped"}`,
	}}
	var errBuf bytes.Buffer
	deps := mocCleanupDeps{runner: runner}
	err := runMocsCleanup(mustResolveConfig(t), "", true, bufio.NewReader(strings.NewReader("y\n")), io.Discard, &errBuf, deps)
	if err != nil {
		t.Fatal(err)
	}
	out := errBuf.String()
	if !strings.Contains(out, "rejected") {
		t.Errorf("expected a rejection to be reported:\n%s", out)
	}
	got, rerr := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Beta.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(got), "### Group") {
		t.Errorf("Beta should have been applied:\n%s", got)
	}
	betaUnchanged, rerr := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Alpha.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if strings.Contains(string(betaUnchanged), "extra: x") {
		t.Error("Alpha's rejected proposal must never be written")
	}
}

// DECIDE(#116): EOF on the confirm prompt is "no" for THAT target only
// — the run continues to the next target, unlike triage's abort-on-EOF.
func TestMocsCleanupEOFOnConfirmSkipsOnlyThatTarget(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Alpha.md", "---\ntype: moc\n---\n# MOC Alpha\n\n- [[A]] — https://example.com/a\n", 0)
	addNote(t, vaultDir, "03-Resources/MOC Beta.md", "---\ntype: moc\n---\n# MOC Beta\n\n- [[B]] — https://example.com/b\n", 0)
	runner := &fakeMocRunner{responses: []string{
		`{"new_content":"---\ntype: moc\n---\n# MOC Alpha\n\n### G\n- [[A]] — https://example.com/a\n","duplicates_flagged":[],"summary":""}`,
		`{"new_content":"---\ntype: moc\n---\n# MOC Beta\n\n### G\n- [[B]] — https://example.com/b\n","duplicates_flagged":[],"summary":""}`,
	}}
	var errBuf bytes.Buffer
	deps := mocCleanupDeps{runner: runner}
	// Empty stdin: BOTH targets hit EOF on their confirm read; both must
	// be skipped, and the run must still complete (not abort after the
	// first EOF) and process Beta too.
	err := runMocsCleanup(mustResolveConfig(t), "", true, bufio.NewReader(strings.NewReader("")), io.Discard, &errBuf, deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.gotPrompt) != 2 {
		t.Fatalf("expected both targets to be proposed (run must not abort after the first EOF), got %d calls", len(runner.gotPrompt))
	}
	for _, name := range []string{"MOC Alpha.md", "MOC Beta.md"} {
		got, rerr := os.ReadFile(filepath.Join(vaultDir, "03-Resources", name))
		if rerr != nil {
			t.Fatal(rerr)
		}
		if strings.Contains(string(got), "### G") {
			t.Errorf("%s should have been skipped (EOF = no), but was applied:\n%s", name, got)
		}
	}
}

func mustResolveConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
```

Add `"bufio"`, `"bytes"`, `"context"`, `"errors"`, `"io"`, `"os"`, `"path/filepath"` to `cmd/ov/mocs_test.go`'s import block alongside the existing `"reflect"`, `"strings"`, `"testing"`, plus `"github.com/arosenkranz/obsidian-vault-tools/internal/config"`.

- [ ] **Step 1b: Write the failing stub-LLM integration tests (real subprocess transport, design spec §Testing strategy tier 3)**

```go
// cmd/ov/mocs_cleanup_integration_test.go
package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
	"github.com/arosenkranz/obsidian-vault-tools/internal/llmtest"
)

// Temp-vault integration tests (design spec §Testing strategy tier 3):
// OV_LLM_CMD points at this same test binary, re-executed in stub mode
// via internal/llmtest — proving the REAL internal/llm subprocess
// transport, not a fake. Reuses the exact pattern
// triage_llm_integration_test.go established in phase 3.

func newMocCleanupIntegrationRunner(t *testing.T, response string, exitCode int, stderr string) *llm.Runner {
	t.Helper()
	self, err := llmtest.SelfCmd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.ResponseEnv, response)
	if exitCode != 0 {
		t.Setenv(llmtest.ExitCodeEnv, strconv.Itoa(exitCode))
	}
	if stderr != "" {
		t.Setenv(llmtest.StderrEnv, stderr)
	}
	return llm.NewRunner(self, "")
}

// CONTRACT(#116): approve ([y]) writes the proposal, exercised through
// the REAL subprocess transport end to end.
func TestMocCleanupIntegrationApproveWrites(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Music.md",
		"---\ntype: moc\n---\n# MOC Music\n\n- [[Jazz]] — https://example.com/jazz\n", 0)
	runner := newMocCleanupIntegrationRunner(t,
		`{"new_content":"---\ntype: moc\n---\n# MOC Music\n\n### Example.com\n- [[Jazz]] — https://example.com/jazz\n","duplicates_flagged":[],"summary":"grouped"}`,
		0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf strings.Builder
	deps := mocCleanupDeps{runner: runner}
	if err := runMocsCleanup(cfg, "Music", false, bufio.NewReader(strings.NewReader("y\n")), &errBuf, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "### Example.com") {
		t.Errorf("expected the applied reorganization, got:\n%s", got)
	}
}

// BUG(fixed)(#108): frontmatter mutation is rejected through the real
// transport — the file is never touched.
func TestMocCleanupIntegrationRejectsFrontmatterMutation(t *testing.T) {
	vaultDir := newVaultFixture(t)
	original := "---\ntype: moc\n---\n# MOC Music\n\n- [[Jazz]] — https://example.com/jazz\n"
	addNote(t, vaultDir, "03-Resources/MOC Music.md", original, 0)
	runner := newMocCleanupIntegrationRunner(t,
		`{"new_content":"---\ntype: moc\nextra: injected\n---\n# MOC Music\n\n- [[Jazz]] — https://example.com/jazz\n","duplicates_flagged":[],"summary":""}`,
		0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf strings.Builder
	deps := mocCleanupDeps{runner: runner}
	if err := runMocsCleanup(cfg, "Music", false, bufio.NewReader(strings.NewReader("y\n")), &errBuf, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "rejected") {
		t.Errorf("expected a rejection report, got:\n%s", errBuf.String())
	}
	got, err := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("file must be untouched after rejection, got:\n%s", got)
	}
}

// BUG(fixed)(#107): a dropped/renamed BARE wikilink is rejected through
// the real transport.
func TestMocCleanupIntegrationRejectsDroppedBareWikilink(t *testing.T) {
	vaultDir := newVaultFixture(t)
	original := "---\ntype: moc\n---\n# MOC Notes\n\n- [[Neovim]] — my editor setup\n"
	addNote(t, vaultDir, "03-Resources/MOC Notes.md", original, 0)
	runner := newMocCleanupIntegrationRunner(t,
		`{"new_content":"---\ntype: moc\n---\n# MOC Notes\n\n- [[Neovim Notes]] — my editor setup\n","duplicates_flagged":[],"summary":""}`,
		0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf strings.Builder
	deps := mocCleanupDeps{runner: runner}
	if err := runMocsCleanup(cfg, "Notes", false, bufio.NewReader(strings.NewReader("y\n")), &errBuf, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "rejected") {
		t.Errorf("expected a rejection report, got:\n%s", errBuf.String())
	}
	got, err := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("file must be untouched after rejection, got:\n%s", got)
	}
}

// BUG(fixed)(#109): a dropped URL is rejected through the real transport.
func TestMocCleanupIntegrationRejectsDroppedURL(t *testing.T) {
	vaultDir := newVaultFixture(t)
	original := "---\ntype: moc\n---\n# MOC Music\n\n- [[Foo]] — https://example.com/foo\n- [[Bar]] — https://example.com/bar\n"
	addNote(t, vaultDir, "03-Resources/MOC Music.md", original, 0)
	runner := newMocCleanupIntegrationRunner(t,
		`{"new_content":"---\ntype: moc\n---\n# MOC Music\n\n- [[Foo]] — https://example.com/foo\n- [[Bar]] — no url now\n","duplicates_flagged":[],"summary":""}`,
		0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf strings.Builder
	deps := mocCleanupDeps{runner: runner}
	if err := runMocsCleanup(cfg, "Music", false, bufio.NewReader(strings.NewReader("y\n")), &errBuf, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "rejected") {
		t.Errorf("expected a rejection report, got:\n%s", errBuf.String())
	}
	got, err := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("file must be untouched after rejection, got:\n%s", got)
	}
}

// CONTRACT(#115): a proposal identical to the original is reported
// "already well-organized" and nothing is written — no confirm prompt is
// shown, so an empty stdin reader (which would otherwise EOF) proves the
// loop never tried to read one.
func TestMocCleanupIntegrationAlreadyWellOrganized(t *testing.T) {
	vaultDir := newVaultFixture(t)
	original := "---\ntype: moc\n---\n# MOC Music\n\n- [[Jazz]] — https://example.com/jazz\n"
	addNote(t, vaultDir, "03-Resources/MOC Music.md", original, 0)
	runner := newMocCleanupIntegrationRunner(t,
		`{"new_content":"---\ntype: moc\n---\n# MOC Music\n\n- [[Jazz]] — https://example.com/jazz\n","duplicates_flagged":[],"summary":""}`,
		0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf strings.Builder
	deps := mocCleanupDeps{runner: runner}
	if err := runMocsCleanup(cfg, "Music", false, bufio.NewReader(strings.NewReader("")), &errBuf, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "already well-organized") {
		t.Errorf("stderr = %q", errBuf.String())
	}
	got, err := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Error("an unchanged proposal must never be written")
	}
}

// CONTRACT(#113): --all processes every MOC*.md in one run, through the
// real transport. Both fixtures share the identical literal frontmatter
// block ("---\ntype: moc\n---") and the stub returns one canned
// new_content that is a superset of both originals' URLs (both are
// URL-anchored entries, so retitling/reordering is allowed) — Validate
// passes for BOTH targets against this single response, letting the
// test assert both files were actually applied.
func TestMocCleanupIntegrationAllProcessesMultipleMOCs(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Alpha.md", "---\ntype: moc\n---\n# MOC Alpha\n\n- [[A]] — https://example.com/a\n", 0)
	addNote(t, vaultDir, "03-Resources/MOC Beta.md", "---\ntype: moc\n---\n# MOC Beta\n\n- [[B]] — https://example.com/b\n", 0)
	runner := newMocCleanupIntegrationRunner(t,
		`{"new_content":"---\ntype: moc\n---\n# MOC Combined\n\n### Group\n- [[A]] — https://example.com/a\n- [[B]] — https://example.com/b\n","duplicates_flagged":[],"summary":"merged view"}`,
		0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf strings.Builder
	deps := mocCleanupDeps{runner: runner}
	// Two targets, each needs its own "y\n" confirm.
	err = runMocsCleanup(cfg, "", true, bufio.NewReader(strings.NewReader("y\ny\n")), &errBuf, &errBuf, deps)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"MOC Alpha.md", "MOC Beta.md"} {
		got, rerr := os.ReadFile(filepath.Join(vaultDir, "03-Resources", name))
		if rerr != nil {
			t.Fatal(rerr)
		}
		if !strings.Contains(string(got), "### Group") {
			t.Errorf("%s should have been applied via --all, got:\n%s", name, got)
		}
	}
	if !strings.Contains(errBuf.String(), "applied") {
		t.Errorf("expected an applied summary line, got:\n%s", errBuf.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ov/... -run 'TestMocsCleanup|TestMocCleanupIntegration' -v`
Expected: FAIL — `runMocsCleanup`, `mocCleanupDeps` undefined; `mocs cleanup` is an unknown subcommand.

- [ ] **Step 3: Write the implementation**

```go
// cmd/ov/mocs.go — add these imports to the existing import block
// (alongside "errors", "fmt", "os", "path/filepath", "strings", "time",
// the config/vault/cobra imports already there):
//   "bufio"
//   "context"
//   "io"
//
//   "github.com/arosenkranz/obsidian-vault-tools/internal/llm"
//   "github.com/arosenkranz/obsidian-vault-tools/internal/moc"
//   "github.com/arosenkranz/obsidian-vault-tools/internal/tui"

// mocCleanupDeps injects the LLM runner so runMocsCleanup is fully
// testable without spawning a real subprocess (matches llmTriageDeps'
// injection pattern in triage.go).
type mocCleanupDeps struct {
	runner moc.Runner
}

// mocCleanupTimeout is 180s, not triage's 120s (row #90) — a cmd/ov-
// local constant; internal/llm.Runner.Run is timeout-agnostic via the
// caller-supplied context.
const mocCleanupTimeout = 180 * time.Second

func newMocsCleanupCmd() *cobra.Command {
	var vaultFlag string
	var all bool
	cmd := &cobra.Command{
		Use:   "cleanup [name]",
		Short: "LLM-assisted MOC reorganization (suggest-only, diff+confirm)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			runner := llm.NewRunner(cfg.LLMCmd, cfg.Model)
			deps := mocCleanupDeps{runner: runner}
			return runMocsCleanup(cfg, name, all, bufio.NewReader(cmd.InOrStdin()), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps)
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	cmd.Flags().BoolVar(&all, "all", false, "process every MOC in the vault")
	return cmd
}

// runMocsCleanup is the testable core of `ov2 mocs cleanup`: name XOR
// --all, else exit 2 (row #114); resolves target(s) via
// vault.FindMOCByName (single) or vault.MOCs (all, row #156) — no
// targets resolved also exits 2 (row #113). For each target: read once
// (content + hash, row #106), ProposeCleanup (180s timeout, row #90),
// Validate (rows #107-109) — a rejection reports the reason and
// continues to the next target, never a partial apply (row #109); an
// identical proposal is reported "unchanged" BEFORE any diff/confirm is
// shown (row #157) — Apply is never even called for this case; otherwise
// the summary/duplicates are printed, the diff is rendered
// (tui.RenderDiff(moc.Diff(...))), and an explicit y/N confirm gates the
// write (EOF = "n" for THIS target only, row #116 — unlike triage's
// abort-on-EOF). moc.Apply writes using the hash captured at the initial
// read (not a second re-read), which still detects any edit made during
// the LLM call or human think time — WriteNoteAtomic itself re-reads and
// compares immediately before rename (row #106's mechanism). Ends with
// a five-bucket summary (applied/skipped/unchanged/rejected/errored,
// mirroring moc_cleanup.py's own counts dict).
func runMocsCleanup(cfg *config.Config, name string, all bool, in *bufio.Reader, out, errw io.Writer, deps mocCleanupDeps) error {
	if (name == "") == !all {
		return fmt.Errorf("%w: pass a MOC name or --all, not both", errExitCode2)
	}

	var targets []vault.MOC
	if all {
		mocs, err := vault.MOCs(cfg.VaultDir)
		if err != nil {
			return err
		}
		targets = mocs
	} else {
		m, err := vault.FindMOCByName(cfg.VaultDir, cfg.Resources, name)
		if err != nil {
			return fmt.Errorf("%w: no MOC found matching %q", errExitCode2, name)
		}
		targets = []vault.MOC{*m}
	}
	if len(targets) == 0 {
		return fmt.Errorf("%w: no MOC found", errExitCode2)
	}

	var counts struct{ applied, skipped, unchanged, rejected, errored int }

	for _, target := range targets {
		fmt.Fprintf(errw, "\n📄 %s\n", target.Name)

		original, hash, err := vault.ReadNote(target.Path)
		if err != nil {
			fmt.Fprintf(errw, "  error reading file: %v\n", err)
			counts.errored++
			continue
		}

		fmt.Fprintln(errw, "  thinking…")
		ctx, cancel := context.WithTimeout(context.Background(), mocCleanupTimeout)
		proposal, err := moc.ProposeCleanup(ctx, deps.runner, target.Path, original, target.Name)
		cancel()
		if err != nil {
			fmt.Fprintf(errw, "  LLM call failed: %v\n", err)
			counts.errored++
			continue
		}

		if err := moc.Validate(original, proposal.NewContent); err != nil {
			fmt.Fprintf(errw, "  ✗ rejected proposal: %v\n", err)
			counts.rejected++
			continue
		}

		if proposal.NewContent == original {
			fmt.Fprintln(errw, "  ✓ already well-organized, no changes proposed")
			counts.unchanged++
			continue
		}

		if proposal.Summary != "" {
			fmt.Fprintf(errw, "  summary: %s\n", proposal.Summary)
		}
		if len(proposal.DuplicatesFlagged) > 0 {
			fmt.Fprintln(errw, "  ⚠ possible duplicates (not merged, review manually):")
			for _, d := range proposal.DuplicatesFlagged {
				fmt.Fprintf(errw, "    - %s\n", d)
			}
		}
		fmt.Fprintln(errw)
		fmt.Fprintln(errw, tui.RenderDiff(moc.Diff(original, proposal.NewContent)))
		fmt.Fprintln(errw)
		fmt.Fprint(errw, "Apply this reorganization? [y/N] ")
		line, readErr := in.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if readErr != nil && answer == "" {
			answer = "n" // EOF -> no, for THIS target only (row #116)
		}

		if answer != "y" {
			fmt.Fprintln(errw, "  → skipped, no changes written")
			counts.skipped++
			continue
		}
		if err := moc.Apply(target.Path, original, proposal.NewContent, hash); err != nil {
			fmt.Fprintf(errw, "  apply failed: %v\n", err)
			counts.errored++
			continue
		}
		fmt.Fprintf(errw, "  ✓ applied → %s\n", target.Rel)
		counts.applied++
	}

	fmt.Fprintln(errw)
	fmt.Fprintln(errw, "cleanup summary")
	fmt.Fprintf(errw, "  applied    %d\n", counts.applied)
	fmt.Fprintf(errw, "  skipped    %d\n", counts.skipped)
	fmt.Fprintf(errw, "  unchanged  %d\n", counts.unchanged)
	fmt.Fprintf(errw, "  rejected   %d\n", counts.rejected)
	fmt.Fprintf(errw, "  errored    %d\n", counts.errored)
	return nil
}
```

Register the new command in `newMocsCmd`'s `cmd.AddCommand(...)` call (Task 2 already added `newMocsNewCmd()`, `newMocsAddCmd()`; add `newMocsCleanupCmd()` too):

```go
cmd.AddCommand(newMocsListCmd(), newMocsOrphanCmd(), newMocsNewCmd(), newMocsAddCmd(), newMocsCleanupCmd())
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ov/... -v`
Expected: PASS, full package green.

- [ ] **Step 5: Commit**

```bash
git add cmd/ov/mocs.go cmd/ov/mocs_test.go cmd/ov/mocs_cleanup_integration_test.go
git commit -m "Add ov2 mocs cleanup: orchestration loop, stub-LLM integration tests"
```

---

## Self-Review

**1. Spec coverage (phase table row: "MOC suite: mocs new/add; cleanup with ported validator", exit criterion "validator decisions identical to python on recorded proposal corpus"):**
- `mocs new` (row #64) → Task 1 (`vault.NewMOCSkeleton`) + Task 2 (`runMocsNew`), including the traversal fix (row #153) and the dropped auto-open (row #7/#154).
- `mocs add` (row #66) → Task 1 (`vault.InsertUnderHeading`, `vault.SanitizeWikilinkText`) + Task 2 (`runMocsAdd`), including the wikilink-injection fix (row #155).
- `internal/moc`'s "LLM cleanup workflow only" split → Tasks 3-5 (`ProposeCleanup`/`Validate`/`Apply`/`Diff`); mechanical MOC ops stay in `internal/vault` (Task 1), never in `internal/moc` — matches the design spec's package doc comment verbatim.
- Validator (rows #107-109) ported 1:1 from `tests/test_moc_cleanup.py` → Task 4, before any workflow code (Task 6), per the design spec's own command-disposition-table note ("validator table tests land before workflow").
- `mocs cleanup` (row #67, #113-116) → Task 6: XOR/exit-2 (row #114), target resolution (rows #113/#156), 180s timeout (row #90), unchanged-before-confirm ordering (row #157), never-partial-apply (row #109), per-target EOF semantics (row #116), five-bucket summary.
- Stub-LLM integration tests (design spec §Testing strategy tier 3) → Task 6 Step 1b: approve→write, validator rejection ×3 (frontmatter/bare-wikilink/URL), already-well-organized no-op, `--all` multi-MOC — all six explicitly named in the phase brief, all present.
- Exit criterion ("validator decisions identical to python on recorded proposal corpus") → Step "Manual exit-criterion verification" below, not a unit test (mirrors phase 3's own dry-run-vs-python manual check).

**2. Placeholder scan:** No `TBD`/`implement later`/`add appropriate handling` language; every step shows complete code. No task references a type/function undefined by an earlier task (`moc.Proposal`, `moc.Runner` — Task 3; `moc.Validate` + sentinel errors — Task 4; `moc.Apply`/`moc.Diff` — Task 5; `vault.NewMOCSkeleton`/`InsertUnderHeading`/`SanitizeWikilinkText` — Task 1; all consumed exactly as produced).

**3. Type consistency:** `moc.Proposal{NewContent, DuplicatesFlagged, Summary}` (Task 3) is the same shape referenced by Task 6's `proposal.NewContent`/`.Summary`/`.DuplicatesFlagged`. `moc.Validate(original, proposed string) error` (Task 4) matches every call site (Task 5's `Apply`, Task 6's pre-diff check). `moc.Apply(mocPath, original, proposed, expectedHash string) error` (Task 5) matches Task 6's `moc.Apply(target.Path, original, proposal.NewContent, hash)` call exactly — 4 string args, `error` return, no `Result` struct (the "unchanged" signal is the caller's own `proposal.NewContent == original` check before Apply is ever reached in the normal flow, row #157). `runMocsNew`/`runMocsAdd`/`runMocsCleanup` signatures (Tasks 2, 2, 6) are used identically by their `newMocs*Cmd` wiring and their own tests.

**4. Architecture-constraint self-check (design spec's `internal/moc` doc comment: "mechanical ops live in vault; split named in doc comments"):** confirmed no mechanical file-write helper (skeleton generation, heading insertion, sanitization) was placed in `internal/moc` — `internal/moc` contains only `Proposal`, `Runner`, `BuildPrompt`, `ProposeCleanup`, `Validate` + its sentinel errors, `Apply`, `Diff`. `cmd/ov/mocs.go` orchestrates `mocs new`/`mocs add` directly against `internal/vault`, exactly matching how `capture.go` already calls `vault.AppendMOCEntry`/`vault.FindMOCByName` without an intermediate package.

**5. Import-cycle check (Task 5's `Diff` reusing `triage.Diff`):** `internal/moc` imports `internal/triage` (for `Diff`) and `internal/vault` (for `WriteNoteAtomic` in `Apply`) and `internal/llm` (for `ExtractJSON` in `propose.go`). `internal/triage` imports `internal/vault` and `internal/llm`, never `internal/moc`. `internal/vault` and `internal/llm` import neither `internal/triage` nor `internal/moc`. No cycle. `cmd/ov` imports both `internal/moc` and `internal/triage` directly (as it already does for every other core package) — no new cross-package coupling beyond the one documented `Diff` reuse.

**6. Global Constraints re-check against the finished task list:** no new dependency (Tasks 1-6 use only stdlib + existing `internal/llm`/`internal/triage`/`internal/vault` — verified no task adds a `go.mod` line) → satisfied. `internal/moc` scope fixed to the LLM workflow → Task 1 vs Tasks 3-5 split, self-check #4 above. Validator byte-for-byte / independent regex → Task 4. Unchanged-before-confirm ordering → Task 6. 180s timeout → Task 6's `mocCleanupTimeout`. Auto-open dropped → Task 2's `runMocsNew` doc comment + inventory row #154. Filename traversal defense → Task 2's `vault.Slugify`+`vault.ContainPath` call. Wikilink-injection defense → Task 1's `SanitizeWikilinkText`, Task 2's `runMocsAdd`. Conditional writes → every `WriteNoteAtomic` call site (Tasks 2, 5) passes a hash captured at read time. Exit code 2 → Task 6's `errExitCode2` reuse (rows #114/#113). `--all` ordering → Task 6's `vault.MOCs` call (row #156). EOF-is-per-target → Task 6's confirm-read branch (row #116). Security-review routing → named explicitly for Tasks 5 and 6 in both the Global Constraints and each task's own header. No constraint is orphaned without an owning task.

---

## Manual exit-criterion verification (run after all 6 tasks are implemented and reviewed)

The phase's exit criterion is explicit in the design spec: "validator decisions identical to python on recorded proposal corpus." This is a manual verification step, not a unit test (same pattern as phase 3's dry-run-vs-python parity check, `.superpowers/sdd/progress.md` Phase 3's final entry).

1. **Build a corpus of recorded (original, LLM-proposed) MOC content pairs.** Two acceptable sources — use whichever real MOCs exist in the configured vault, or synthesize if the vault has too few:
   - Preferred: run `ov2 mocs cleanup <name>` against a copy of the real vault for 3-5 real MOCs (real `OV_LLM_CMD`), and at the "Apply this reorganization? [y/N]" prompt answer "n" (non-destructive). Copy the note's on-disk content as `original.md` and transcribe the shown diff (or re-derive `proposed.md` by applying the printed +/- lines to `original.md` by hand) as `proposed.md` for each pair. `ov2 mocs cleanup` has no `--dry-run` flag this phase (unlike triage's phase-3 `--dry-run`) — the confirm-then-decline flow is the non-destructive equivalent.
   - Alternative: reuse `tests/test_moc_cleanup.py`'s own fixture pairs (`ORIGINAL` plus each test's proposed variant) as a small deterministic corpus — these are already the ported 1:1 cases from Task 4, so this step is really about confirming the RECORDED CORPUS beyond the ported unit tests, i.e. real or realistic MOC content the ported suite didn't already cover.
2. **For each (original, proposed) pair, run BOTH validators and compare the accept/reject verdict:**
   - Python: `python3 -c "import sys; sys.path.insert(0,'bin'); from moc_cleanup import validate_proposal, ValidationError; validate_proposal(open('original.md').read(), open('proposed.md').read())"` — exits cleanly (accept) or raises `ValidationError` (reject), print which.
   - Go: a throwaway `go run` snippet (or a temporary `_ = moc.Validate(original, proposed)` in a scratch `main.go`, deleted after) calling `internal/moc.Validate` on the same two files — nil (accept) or non-nil (reject).
3. **Record the comparison** (pair description, python verdict, go verdict, match/mismatch) in this plan's execution ledger entry (`.superpowers/sdd/progress.md`'s Phase 4 section) as the exit-criterion evidence. Any mismatch is a blocking finding — resolve it (fix the Go validator or document a deliberate, approved divergence) before considering the phase done; do not skip this step or substitute the Task 4 unit test run as satisfying it, since the exit criterion specifically calls for cross-implementation comparison on recorded proposals, not just the ported test suite passing.
