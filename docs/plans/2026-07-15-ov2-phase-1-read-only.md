# ov v2 Phase 1: Read-only Surface — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the read-only command surface — `ov2 inbox`, `ov2 review`, `ov2 stale`, `ov2 mocs list`, and a reimplemented `ov2 mocs orphan` — over new `internal/vault` query functions, with strict stdout/stderr discipline and zero vault write paths.

**Architecture:** Per the approved spec (`docs/plans/2026-07-11-ov-v2-go-rewrite-design.md`). All vault reads go through pure query functions in `internal/vault` (`ListInbox`, `Stale`, `ModifiedWithin`, `ListProjects`, `MOCs`, `Orphans`, `ParseWikilinks`, `AgeDays`) so an mtime cache can slot in behind the same signatures later (spec §Core contracts, §No index). Cobra commands are thin: parse flags → `resolveConfig` → query verb → render records to stdout, chrome to stderr. Old bash/python in `bin/` stays untouched and authoritative.

**Tech Stack:** Go ≥1.22 (1.25.0 installed), cobra, pelletier/go-toml/v2, golang.org/x/text (NFC normalization). No new dependencies.

## Global Constraints

- Module path: `github.com/arosenkranz/obsidian-vault-tools`
- Binary name during transition: `ov2`, built to `dist/ov2` (`make build`)
- No new dependencies: only `spf13/cobra`, `pelletier/go-toml/v2`, `golang.org/x/text` (already present)
- `bin/vault.sh`, `bin/triage_llm.py`, `bin/moc_cleanup.py` are READ-ONLY — never modify them
- **Read-only by construction:** no phase-1 command may write, move, or delete anything inside a vault. No command calls `vault.WriteNoteAtomic`. Query functions open files read-only. Verified in Task 8's final step by grep.
- **stdout/stderr discipline (spec §CLI/TUI; behavior inventory row #123):** machine-readable results go to **stdout only** (plain text, tab-separated fields, one record per line, no ANSI color, deterministic order). All decoration — headers, counts, empty-state notices, hints — goes to **stderr**. Commands must be pipe-safe: `ov2 inbox | cut -f2` yields note names. No ANSI color is emitted in phase 1.
- **Walk resilience (spec §Core contracts):** vault scans tolerate files/dirs vanishing mid-walk (sync pull) and unreadable directories — skip and continue, never fatal in a scan path.
- Env var names stay `OV_*` exactly (compatibility contract). The new `stale_exclude` config key is file-only (a TOML list, not part of the frozen scalar `OV_*` env surface).
- Commit style: imperative mood, no conventional-commit prefix (matches repo history)
- Every mined behavior test carries a comment `// CONTRACT:`, `// BUG(fixed):`, or `// DECIDE(<resolution>):` referencing a row number in `docs/plans/ov2-behavior-inventory.md`
- Go tests use `t.Setenv` / `t.TempDir` / `os.Chtimes` — no global state leaks between tests; no injected package-level clock

---

### Task 1: Wikilink parser (`internal/vault/wikilink.go`)

Foundational: MOC item counts (row #32) and orphan detection (rows #1, #2, #126) both need real `[[wikilink]]` parsing and a basename comparison key.

**Files:**
- Create: `internal/vault/wikilink.go`
- Test: `internal/vault/wikilink_test.go`

**Interfaces:**
- Consumes: `golang.org/x/text/unicode/norm` (already a dep, used by `slugify.go`)
- Produces:
  - `func ParseWikilinks(text string) []string` — link targets in order; strips `|alias` and `#anchor`; skips empty targets; returns `nil` when none
  - `func linkKey(s string) string` — unexported; NFC + case-folded basename of a target or note name

- [ ] **Step 1: Write the failing tests**

```go
// internal/vault/wikilink_test.go
package vault

import (
	"reflect"
	"testing"
)

// DECIDE(#32): v2 counts parsed wikilinks; two links on one line count twice
// (v1 grep -c counted the LINE once). Targets strip alias and heading anchor.
func TestParseWikilinks(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"plain", "see [[Music]]", []string{"Music"}},
		{"alias", "[[Music|my tunes]]", []string{"Music"}},
		{"heading", "[[Music#Jazz]]", []string{"Music"}},
		{"alias and heading", "[[Music#Jazz|jazz]]", []string{"Music"}},
		{"two on one line", "[[A]] and [[B]]", []string{"A", "B"}},
		{"embed counts", "![[Diagram]]", []string{"Diagram"}},
		{"path target", "[[folder/Note]]", []string{"folder/Note"}},
		{"heading only skipped", "[[#Section]]", nil},
		{"empty skipped", "[[]]", nil},
		{"whitespace trimmed", "[[  Spaced  ]]", []string{"Spaced"}},
		{"none", "no links here", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseWikilinks(c.in); !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseWikilinks(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// DECIDE(#126): orphan matching keys links and notes by NFC + case-folded
// basename, mirroring Obsidian's case-insensitive, basename link resolution.
func TestLinkKey(t *testing.T) {
	for in, want := range map[string]string{
		"Music":        "music",
		"folder/Music": "music",
		"MUSIC":        "music",
		"  Music  ":    "music",
	} {
		if got := linkKey(in); got != want {
			t.Errorf("linkKey(%q) = %q, want %q", in, got, want)
		}
	}
	// NFC/NFD: composed and decomposed é key identically.
	if linkKey("Café") != linkKey("Cafe\u0301") {
		t.Errorf("NFC and NFD forms must key identically")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vault/ -run 'TestParseWikilinks|TestLinkKey' -v`
Expected: FAIL — `undefined: ParseWikilinks`, `undefined: linkKey`

- [ ] **Step 3: Write the implementation**

```go
// internal/vault/wikilink.go
package vault

import (
	"path"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// wikilinkRe matches one [[...]] wikilink (also the [[...]] inside an ![[...]]
// embed), capturing the inner text non-greedily so adjacent links stay
// separate. Wikilinks do not nest brackets.
var wikilinkRe = regexp.MustCompile(`\[\[([^\[\]]+?)\]\]`)

// ParseWikilinks returns the link TARGET of every [[wikilink]] in text, in
// order of appearance. The target is the portion before any '|' alias and
// before any '#' heading anchor, trimmed; links with an empty target
// (e.g. [[#heading]] or [[]]) are skipped. Backs MOC item counts (behavior
// inventory row #32) and orphan detection (rows #1, #2).
func ParseWikilinks(text string) []string {
	var targets []string
	for _, m := range wikilinkRe.FindAllStringSubmatch(text, -1) {
		t := m[1]
		if i := strings.IndexByte(t, '|'); i >= 0 {
			t = t[:i]
		}
		if i := strings.IndexByte(t, '#'); i >= 0 {
			t = t[:i]
		}
		if t = strings.TrimSpace(t); t != "" {
			targets = append(targets, t)
		}
	}
	return targets
}

// linkKey normalizes a wikilink target OR a note name to a comparison key:
// the final path component (so [[folder/Note]] and "Note" match), NFC-
// normalized and case-folded. Mirrors Obsidian's basename-based,
// case-insensitive link resolution (behavior inventory row #126); NFC matches
// the v2 filename policy so an NFD link resolves to its NFC file.
func linkKey(s string) string {
	s = path.Base(strings.TrimSpace(s)) // slash-based; [[folder/Note]] -> "Note"
	return strings.ToLower(norm.NFC.String(s))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vault/ -run 'TestParseWikilinks|TestLinkKey' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/vault/wikilink.go internal/vault/wikilink_test.go
git commit -m "Add wikilink parser and basename link key"
```

---

### Task 2: Note-scan queries (`internal/vault/query.go`)

`Note` type, real age computation (row #18 fix), and the note-scan queries backing `inbox`, `stale`, and the `review` sections other than MOCs.

**Files:**
- Create: `internal/vault/query.go`
- Test: `internal/vault/query_test.go`

**Interfaces:**
- Consumes: nothing from other tasks
- Produces:
  - `type Note struct { Path, Rel, Name string; ModTime time.Time }`
  - `type ProjectEntry struct { Name string; IsDir bool }`
  - `func AgeDays(now, mod time.Time) int`
  - `func ListInbox(vaultDir, inbox string) ([]Note, error)` — top-level *.md, sorted by name; missing dir returns `fs.ErrNotExist`
  - `func Stale(vaultDir string, days int, now time.Time, excludes []string) ([]Note, error)` — `AgeDays > days`, sorted by Rel
  - `func ModifiedWithin(vaultDir string, days int, now time.Time, excludes []string) ([]Note, error)` — `AgeDays < days`, sorted ModTime desc then Name
  - `func ListProjects(vaultDir, projects string) ([]ProjectEntry, error)` — immediate children, sorted; missing dir returns `(nil, nil)`
  - `func collectNotes(vaultDir string, roots []string, excludes map[string]bool) ([]Note, error)` — unexported, walk-resilient; reused by Task 3's `Orphans`
  - `func toSet(names []string) map[string]bool` — unexported helper

- [ ] **Step 1: Write the failing tests**

```go
// internal/vault/query_test.go
package vault

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeAt writes vault/rel with content and sets its mtime to mod. Parents
// are created. Returns the absolute path.
func writeAt(t *testing.T, vault, rel, content string, mod time.Time) string {
	t.Helper()
	p := filepath.Join(vault, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
	return p
}

// BUG(fixed)(#18): v1 get_file_age returned only 0 or 1 for aged files; v2
// AgeDays reports the real floored day count.
func TestAgeDays(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		mod  time.Time
		want int
	}{
		{now, 0},
		{now.Add(-23 * time.Hour), 0},
		{now.Add(-24 * time.Hour), 1},
		{now.Add(-10 * 24 * time.Hour), 10},
		{now.Add(24 * time.Hour), 0}, // future clamps to 0
	}
	for _, c := range cases {
		if got := AgeDays(now, c.mod); got != c.want {
			t.Errorf("AgeDays(now, now%+v) = %d, want %d", c.mod.Sub(now), got, c.want)
		}
	}
}

// CONTRACT(#56): inbox lists top-level *.md only, sorted by name.
func TestListInbox(t *testing.T) {
	vault := t.TempDir()
	now := time.Now()
	writeAt(t, vault, "00-Inbox/b note.md", "x", now)
	writeAt(t, vault, "00-Inbox/a note.md", "x", now)
	writeAt(t, vault, "00-Inbox/notes.txt", "x", now)        // not .md
	writeAt(t, vault, "00-Inbox/sub/deep.md", "x", now)      // not top-level
	got, err := ListInbox(vault, "00-Inbox")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "a note" || got[1].Name != "b note" {
		t.Fatalf("ListInbox = %+v, want [a note, b note]", got)
	}
	if got[0].Rel != "00-Inbox/a note.md" {
		t.Errorf("Rel = %q, want vault-relative slash path", got[0].Rel)
	}
}

func TestListInboxMissing(t *testing.T) {
	_, err := ListInbox(t.TempDir(), "00-Inbox")
	if !os.IsNotExist(err) {
		t.Errorf("missing inbox must return fs.ErrNotExist, got %v", err)
	}
}

// CONTRACT(#62): stale = notes older than `days`, vault-relative path + age.
// BUG(fixed)(#61): exclusions are by configured NAME (here custom archive),
// not hardcoded literal paths, and apply at any depth.
func TestStale(t *testing.T) {
	vault := t.TempDir()
	now := time.Now()
	old := now.Add(-100 * 24 * time.Hour)
	writeAt(t, vault, "03-Resources/old.md", "x", old)
	writeAt(t, vault, "03-Resources/fresh.md", "x", now.Add(-2*24*time.Hour))
	writeAt(t, vault, "Archive-XX/archived.md", "x", old)      // excluded by name
	writeAt(t, vault, "03-Resources/Daily Notes/d.md", "x", old) // excluded, nested
	got, err := Stale(vault, 90, now, []string{"Archive-XX", "Daily Notes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Rel != "03-Resources/old.md" {
		t.Fatalf("Stale = %+v, want only 03-Resources/old.md", got)
	}
}

// DECIDE(#125): modified-within sorts newest first, then by name.
func TestModifiedWithin(t *testing.T) {
	vault := t.TempDir()
	now := time.Now()
	writeAt(t, vault, "02-Areas/older.md", "x", now.Add(-3*24*time.Hour))
	writeAt(t, vault, "02-Areas/newer.md", "x", now.Add(-1*24*time.Hour))
	writeAt(t, vault, "02-Areas/stale.md", "x", now.Add(-40*24*time.Hour)) // excluded by age
	writeAt(t, vault, "04-Archive/arch.md", "x", now.Add(-1*24*time.Hour)) // excluded by name
	got, err := ModifiedWithin(vault, 7, now, []string{"04-Archive"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "newer" || got[1].Name != "older" {
		t.Fatalf("ModifiedWithin = %+v, want [newer, older]", got)
	}
}

// CONTRACT(#60): projects lists immediate children — dirs and top-level notes.
func TestListProjects(t *testing.T) {
	vault := t.TempDir()
	now := time.Now()
	writeAt(t, vault, "01-Projects/Solo Note.md", "x", now)
	if err := os.MkdirAll(filepath.Join(vault, "01-Projects", "Big Project"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeAt(t, vault, "01-Projects/notes.txt", "x", now) // ignored
	got, err := ListProjects(vault, "01-Projects")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "Big Project" || !got[0].IsDir {
		t.Fatalf("ListProjects[0] = %+v, want dir Big Project", got)
	}
	if got[1].Name != "Solo Note" || got[1].IsDir {
		t.Fatalf("ListProjects[1] = %+v, want file Solo Note", got)
	}
}

func TestListProjectsMissing(t *testing.T) {
	got, err := ListProjects(t.TempDir(), "01-Projects")
	if err != nil || got != nil {
		t.Errorf("missing projects dir = (%v, %v), want (nil, nil)", got, err)
	}
}

// Walk resilience (design spec §core contracts): an unreadable directory is
// skipped, never fatal; reachable notes still return.
func TestStaleWalkResilient(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	vault := t.TempDir()
	now := time.Now()
	old := now.Add(-100 * 24 * time.Hour)
	writeAt(t, vault, "03-Resources/reachable.md", "x", old)
	writeAt(t, vault, "03-Resources/locked/hidden.md", "x", old)
	blocked := filepath.Join(vault, "03-Resources", "locked")
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(blocked, 0o755) })
	got, err := Stale(vault, 90, now, nil)
	if err != nil {
		t.Fatalf("walk must be resilient, got %v", err)
	}
	found := false
	for _, n := range got {
		if n.Name == "reachable" {
			found = true
		}
	}
	if !found {
		t.Errorf("reachable note missing after resilient walk: %+v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vault/ -run 'TestAgeDays|TestListInbox|TestStale|TestModifiedWithin|TestListProjects' -v`
Expected: FAIL — `undefined: AgeDays`, `undefined: ListInbox`, etc.

- [ ] **Step 3: Write the implementation**

```go
// internal/vault/query.go
package vault

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Note is a scanned markdown note.
type Note struct {
	Path    string    // absolute path
	Rel     string    // vault-relative path, forward slashes
	Name    string    // basename without the .md extension
	ModTime time.Time // filesystem mtime
}

// ProjectEntry is one immediate child of the projects root.
type ProjectEntry struct {
	Name  string // basename (without .md for files)
	IsDir bool
}

// AgeDays returns whole days between mod and now (floored, clamped at 0 for
// future mtimes). Replaces v1 get_file_age, whose find-based primary path
// returned only 0 or 1 so every aged file reported "1" (behavior inventory
// row #18 BUG); the true mtime day count is now always reachable.
func AgeDays(now, mod time.Time) int {
	d := now.Sub(mod)
	if d < 0 {
		return 0
	}
	return int(d / (24 * time.Hour))
}

// ListInbox returns every *.md file directly inside <vaultDir>/<inbox>
// (top-level only, mirroring v1 inbox_list's "$INBOX_DIR"/*.md glob), sorted
// by name. A missing inbox directory surfaces its read error (fs.ErrNotExist)
// for the caller to report; files that vanish mid-listing are skipped (walk
// resilience). Behavior inventory row #56.
func ListInbox(vaultDir, inbox string) ([]Note, error) {
	dir := filepath.Join(vaultDir, inbox)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var notes []Note
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // vanished between readdir and stat — skip
		}
		notes = append(notes, Note{
			Path:    filepath.Join(dir, e.Name()),
			Rel:     filepath.ToSlash(filepath.Join(inbox, e.Name())),
			Name:    strings.TrimSuffix(e.Name(), ".md"),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Name < notes[j].Name })
	return notes, nil
}

// Stale returns notes anywhere under vaultDir whose AgeDays(now, mtime) is
// strictly greater than days (mirroring find -mtime +N), excluding any note
// with a directory component in excludes, sorted by vault-relative path.
// Behavior inventory rows #62 (CONTRACT) and #61 (BUG: v1 hardcoded the
// exclude paths, ignoring OV_ARCHIVE/OV_META overrides).
func Stale(vaultDir string, days int, now time.Time, excludes []string) ([]Note, error) {
	notes, err := collectNotes(vaultDir, []string{vaultDir}, toSet(excludes))
	if err != nil {
		return nil, err
	}
	var out []Note
	for _, n := range notes {
		if AgeDays(now, n.ModTime) > days {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

// ModifiedWithin returns notes anywhere under vaultDir whose AgeDays(now,
// mtime) is strictly less than days (mirroring find -mtime -N), excluding any
// note with a directory component in excludes, sorted most-recently-modified
// first then by name. Backs the review "modified this week" section (behavior
// inventory rows #60, #125).
func ModifiedWithin(vaultDir string, days int, now time.Time, excludes []string) ([]Note, error) {
	notes, err := collectNotes(vaultDir, []string{vaultDir}, toSet(excludes))
	if err != nil {
		return nil, err
	}
	var out []Note
	for _, n := range notes {
		if AgeDays(now, n.ModTime) < days {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// ListProjects returns the immediate children of <vaultDir>/<projects> sorted
// by name: subdirectories (IsDir) and top-level *.md notes. Mirrors v1
// review_vault's "$PROJECTS_DIR"/* listing. A missing projects directory
// returns (nil, nil) (non-fatal, as in v1). Behavior inventory row #60.
func ListProjects(vaultDir, projects string) ([]ProjectEntry, error) {
	entries, err := os.ReadDir(filepath.Join(vaultDir, projects))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []ProjectEntry
	for _, e := range entries {
		switch {
		case e.IsDir():
			out = append(out, ProjectEntry{Name: e.Name(), IsDir: true})
		case strings.HasSuffix(e.Name(), ".md"):
			out = append(out, ProjectEntry{Name: strings.TrimSuffix(e.Name(), ".md")})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// collectNotes walks each root (absolute paths), returning every *.md file
// found, with Rel computed relative to vaultDir. Walk-resilient: vanished
// entries and unreadable directories are skipped, never fatal (design spec
// §walk resilience). A non-root directory whose base name is in excludes is
// pruned (mirrors v1's -not -path "*/NAME/*", matching at any depth).
func collectNotes(vaultDir string, roots []string, excludes map[string]bool) ([]Note, error) {
	var notes []Note
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // vanished mid-walk or unreadable dir: skip
			}
			if d.IsDir() {
				if p != root && excludes[d.Name()] {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil // vanished between readdir and stat — skip
			}
			rel, err := filepath.Rel(vaultDir, p)
			if err != nil {
				return nil
			}
			notes = append(notes, Note{
				Path:    p,
				Rel:     filepath.ToSlash(rel),
				Name:    strings.TrimSuffix(d.Name(), ".md"),
				ModTime: info.ModTime(),
			})
			return nil
		})
		if err != nil {
			return notes, err
		}
	}
	return notes, nil
}

func toSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		if n != "" {
			m[n] = true
		}
	}
	return m
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vault/ -v`
Expected: PASS (all vault tests, including Task 1's)

- [ ] **Step 5: Commit**

```bash
git add internal/vault/query.go internal/vault/query_test.go
git commit -m "Add note-scan queries: AgeDays, ListInbox, Stale, ModifiedWithin, ListProjects"
```

---

### Task 3: MOC + orphan queries (`internal/vault/moc.go`)

MOC listing (rows #34, #63, #32) and the reimplemented orphan detection (rows #1, #2, #65, #126). Depends on Task 1's `ParseWikilinks`/`linkKey` and Task 2's `collectNotes`/`Note`.

**Files:**
- Create: `internal/vault/moc.go`
- Test: `internal/vault/moc_test.go`

**Interfaces:**
- Consumes: `ParseWikilinks`, `linkKey` (Task 1); `Note`, `collectNotes` (Task 2); `ParseNote` (phase 0, in `frontmatter.go`)
- Produces:
  - `type MOC struct { Path, Rel, Name, Description string; ItemCount int }`
  - `func MOCs(vaultDir string) ([]MOC, error)` — every `MOC*.md` sorted by name
  - `func Orphans(vaultDir string, roots []string) ([]Note, error)` — notes under roots not linked from any MOC
  - `func mocPaths(vaultDir string) []string`, `func mocDescription(content string) string` — unexported

- [ ] **Step 1: Write the failing tests**

```go
// internal/vault/moc_test.go
package vault

import (
	"reflect"
	"testing"
	"time"
)

// CONTRACT(#34): a MOC is any MOC*.md, sorted by name.
// CONTRACT(#63): description = first non-#, non-blank line among the first 5.
// DECIDE(#32): item count = parsed body wikilinks (not v1's grep -c of lines).
func TestMOCs(t *testing.T) {
	vault := t.TempDir()
	now := time.Now()
	writeAt(t, vault, "03-Resources/MOC Music.md",
		"# MOC Music\n\n> Everything about music\n\n## Key Notes\n- [[Jazz]]\n- [[Blues|the blues]]\n", now)
	writeAt(t, vault, "03-Resources/MOC Empty.md",
		"# MOC Empty\n\n## Key Notes\n", now)
	got, err := MOCs(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "MOC Empty" || got[1].Name != "MOC Music" {
		t.Fatalf("MOCs names/order = %+v", got)
	}
	if got[1].Description != "> Everything about music" {
		t.Errorf("description = %q", got[1].Description)
	}
	if got[1].ItemCount != 2 {
		t.Errorf("item count = %d, want 2", got[1].ItemCount)
	}
	if got[0].ItemCount != 0 || got[0].Description != "" {
		t.Errorf("empty MOC = %+v, want count 0, no description", got[0])
	}
}

// BUG(fixed)(#1,#2): v1 reported EVERY note orphaned — the found_in_moc flag
// was set in a pipeline subshell and never propagated, and matching was a
// substring grep. v2 parses [[wikilink]] targets and matches by basename.
// DECIDE(#65): scope is Resources + Areas only (Projects/Archive not scanned).
func TestOrphans(t *testing.T) {
	vault := t.TempDir()
	now := time.Now()
	writeAt(t, vault, "03-Resources/MOC Music.md",
		"# MOC Music\n\n## Key Notes\n- [[Jazz]]\n- [[folder/Meeting]]\n", now)
	writeAt(t, vault, "03-Resources/Jazz.md", "# Jazz\n", now)   // linked
	writeAt(t, vault, "03-Resources/Blues.md", "# Blues\n", now) // ORPHAN
	writeAt(t, vault, "02-Areas/Meeting.md", "# Meeting\n", now) // linked via path target basename
	writeAt(t, vault, "02-Areas/Standup.md", "# Standup\n", now) // ORPHAN
	writeAt(t, vault, "01-Projects/Proj.md", "# Proj\n", now)    // not scanned
	got, err := Orphans(vault, []string{"03-Resources", "02-Areas"})
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	for _, n := range got {
		rels = append(rels, n.Rel)
	}
	want := []string{"02-Areas/Standup.md", "03-Resources/Blues.md"}
	if !reflect.DeepEqual(rels, want) {
		t.Fatalf("orphans = %v, want %v", rels, want)
	}
}

// DECIDE(#126): link matching is case-insensitive — [[jazz]] links Jazz.md.
func TestOrphansCaseInsensitive(t *testing.T) {
	vault := t.TempDir()
	now := time.Now()
	writeAt(t, vault, "03-Resources/MOC X.md", "## Key Notes\n- [[jazz]]\n", now)
	writeAt(t, vault, "03-Resources/Jazz.md", "# Jazz\n", now)
	got, err := Orphans(vault, []string{"03-Resources"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range got {
		if n.Name == "Jazz" {
			t.Error("Jazz linked via [[jazz]] must not be orphaned (row #126)")
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vault/ -run 'TestMOCs|TestOrphans' -v`
Expected: FAIL — `undefined: MOCs`, `undefined: Orphans`

- [ ] **Step 3: Write the implementation**

```go
// internal/vault/moc.go
package vault

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MOC is a Map-of-Content note.
type MOC struct {
	Path        string
	Rel         string
	Name        string // basename without .md (e.g. "MOC Music")
	Description string // first non-heading, non-blank line among the first 5
	ItemCount   int    // parsed [[wikilinks]] in the body
}

// mocPaths returns the absolute paths of every MOC*.md anywhere under
// vaultDir, sorted. The MOC*.md glob IS the MOC definition (behavior
// inventory row #34). Walk-resilient.
func mocPaths(vaultDir string) []string {
	var paths []string
	filepath.WalkDir(vaultDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if name := d.Name(); strings.HasPrefix(name, "MOC") && strings.HasSuffix(name, ".md") {
			paths = append(paths, p)
		}
		return nil
	})
	sort.Strings(paths)
	return paths
}

// MOCs returns every MOC in the vault sorted by name, each with its
// description (behavior inventory row #63) and wikilink item count (row #32 —
// v2 counts parsed BODY wikilinks, replacing v1's line-based grep -c that also
// counted a frontmatter moc: line). Unreadable MOCs are skipped.
func MOCs(vaultDir string) ([]MOC, error) {
	var mocs []MOC
	for _, p := range mocPaths(vaultDir) {
		content, err := os.ReadFile(p)
		if err != nil {
			continue // skip unreadable, never fatal
		}
		rel, err := filepath.Rel(vaultDir, p)
		if err != nil {
			continue
		}
		_, body := ParseNote(string(content))
		mocs = append(mocs, MOC{
			Path:        p,
			Rel:         filepath.ToSlash(rel),
			Name:        strings.TrimSuffix(filepath.Base(p), ".md"),
			Description: mocDescription(string(content)),
			ItemCount:   len(ParseWikilinks(body)),
		})
	}
	sort.Slice(mocs, func(i, j int) bool { return mocs[i].Name < mocs[j].Name })
	return mocs, nil
}

// mocDescription mirrors v1 moc_list: among the first 5 lines of the raw file,
// the first line that is neither blank nor a heading (starts with #). Operates
// on raw content (frontmatter included), exactly as v1's
// `head -5 | grep -v "^#" | grep -v "^$" | head -1`. Behavior inventory
// row #63.
func mocDescription(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 5 {
		lines = lines[:5]
	}
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}

// Orphans returns notes under the given roots (relative folder names) that are
// not the target of any [[wikilink]] in any MOC. This REIMPLEMENTS v1
// moc_orphan, which was doubly broken: its found_in_moc flag was set inside a
// pipeline subshell and never propagated, so every note reported orphaned
// (behavior inventory row #1), and it matched by substring grep rather than
// link parsing (row #2). v2 parses wikilink targets from every MOC's full text
// and compares by NFC + case-folded basename (row #126). Notes whose name
// starts with "MOC" are skipped (they are MOCs, not candidates), matching v1's
// scope (row #65: Resources + Areas only). Sorted by vault-relative path.
func Orphans(vaultDir string, roots []string) ([]Note, error) {
	linked := map[string]bool{}
	for _, p := range mocPaths(vaultDir) {
		content, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, t := range ParseWikilinks(string(content)) {
			linked[linkKey(t)] = true
		}
	}

	absRoots := make([]string, 0, len(roots))
	for _, r := range roots {
		absRoots = append(absRoots, filepath.Join(vaultDir, r))
	}
	notes, err := collectNotes(vaultDir, absRoots, nil)
	if err != nil {
		return nil, err
	}

	var orphans []Note
	for _, n := range notes {
		if strings.HasPrefix(n.Name, "MOC") {
			continue
		}
		if linked[linkKey(n.Name)] {
			continue
		}
		orphans = append(orphans, n)
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].Rel < orphans[j].Rel })
	return orphans, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vault/ -v`
Expected: PASS (whole vault package)

- [ ] **Step 5: Commit**

```bash
git add internal/vault/moc.go internal/vault/moc_test.go
git commit -m "Add MOC listing and reimplemented orphan detection"
```

---

### Task 4: Config `stale_exclude` key (`internal/config/config.go`)

Move the `Daily Notes` stale exclusion to config (spec §CLI/TUI; rows #61, #62). Archive/Meta already have their own configurable keys — the command unions them with `StaleExclude`.

**Files:**
- Modify: `internal/config/config.go` (add field + default)
- Test: `internal/config/config_test.go` (append tests)

**Interfaces:**
- Consumes: nothing
- Produces: `Config.StaleExclude []string` (toml `stale_exclude`); default `["Daily Notes"]` when the key is absent; an explicit empty list stays empty

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go` (add `"reflect"` to its import block):

```go
// CONTRACT(#62)+BUG(fixed)(#61): stale exclusions are config-driven. Default
// keeps v1's "Daily Notes"; Archive/Meta come from their own keys so
// OV_ARCHIVE/OV_META overrides are honored (v1 hardcoded the paths).
func TestStaleExcludeDefault(t *testing.T) {
	clearOVEnv(t)
	cfg, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.StaleExclude, []string{"Daily Notes"}) {
		t.Errorf("default StaleExclude = %v, want [Daily Notes]", cfg.StaleExclude)
	}
}

func TestStaleExcludeFromFile(t *testing.T) {
	clearOVEnv(t)
	p := writeTemp(t, "vault_dir = \"/v\"\nstale_exclude = [\"Templates\", \"Daily Notes\"]\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.StaleExclude, []string{"Templates", "Daily Notes"}) {
		t.Errorf("StaleExclude = %v", cfg.StaleExclude)
	}
}

func TestStaleExcludeExplicitEmpty(t *testing.T) {
	clearOVEnv(t)
	p := writeTemp(t, "vault_dir = \"/v\"\nstale_exclude = []\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StaleExclude == nil || len(cfg.StaleExclude) != 0 {
		t.Errorf("explicit empty list must stay empty, got %v", cfg.StaleExclude)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestStaleExclude -v`
Expected: FAIL — `cfg.StaleExclude undefined`

- [ ] **Step 3: Write the implementation**

In `internal/config/config.go`, add the field to the `Config` struct immediately after the `DocsURL` line:

```go
	DocsURL      string   `toml:"docs_url"`
	StaleExclude []string `toml:"stale_exclude"`
```

In `Load`, immediately before `cfg.VaultDir = expandPath(cfg.VaultDir)`, apply the default (a nil slice means the key was absent; an explicit `[]` stays as a non-nil empty slice):

```go
	if cfg.StaleExclude == nil {
		cfg.StaleExclude = []string{"Daily Notes"}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS (all config tests, including phase 0's)

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "Add configurable stale_exclude key defaulting to Daily Notes"
```

---

### Task 5: Command scaffolding + `ov2 inbox` (`cmd/ov`)

Shared `resolveConfig` helper, the test fixture builder, and the first command wired into the root. Establishes the stdout/stderr discipline every later command follows.

**Files:**
- Create: `cmd/ov/common.go`
- Create: `cmd/ov/inbox.go`
- Create: `cmd/ov/fixture_test.go`
- Create: `cmd/ov/inbox_test.go`
- Modify: `cmd/ov/root.go` (register `newInboxCmd`)

**Interfaces:**
- Consumes: `config.Load`, `config.ExpandPath`, `Config.Validate` (phase 0); `vault.ListInbox`, `vault.AgeDays` (Task 2)
- Produces:
  - `func resolveConfig(vaultFlag string) (*config.Config, error)` — load + `--vault` override + validate (used by all read-only commands)
  - `func ageMarker(age int) string` — `⚠` when `age > 7`, else `•` (row #19)
  - `func newInboxCmd() *cobra.Command`
  - Test helpers (in `fixture_test.go`): `newVaultFixture(t) string`, `addNote(t, vault, rel, content string, days int)`, `runCmd(t, args ...string) (stdout, stderr string, err error)` — reused by Tasks 6-8

- [ ] **Step 1: Write the fixture helpers and the failing test**

```go
// cmd/ov/fixture_test.go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newVaultFixture creates a temp vault with the standard PARA dirs, points
// OV_CONFIG at a minimal TOML config, and returns the vault path.
func newVaultFixture(t *testing.T) string {
	t.Helper()
	clearOVEnv(t)
	vault := t.TempDir()
	for _, d := range []string{"00-Inbox", "01-Projects", "02-Areas", "03-Resources", "04-Archive", "99-Meta"} {
		if err := os.MkdirAll(filepath.Join(vault, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("vault_dir = \""+vault+"\"\nllm_cmd = \"true\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OV_CONFIG", cfgPath)
	return vault
}

// addNote writes vault/rel with content and sets its mtime `days` days ago.
func addNote(t *testing.T, vault, rel, content string, days int) {
	t.Helper()
	p := filepath.Join(vault, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mod := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
}

// runCmd builds a fresh root command, runs it with args, and returns stdout
// and stderr captured SEPARATELY — the stdout/stderr discipline is the
// contract under test (behavior inventory row #123).
func runCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := newRootCmd()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}
```

```go
// cmd/ov/inbox_test.go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CONTRACT(#56): inbox lists notes sorted by name. DECIDE(#123): records go to
// stdout, chrome to stderr. CONTRACT(#19): notes older than 7 days marked ⚠.
func TestInboxLists(t *testing.T) {
	vault := newVaultFixture(t)
	addNote(t, vault, "00-Inbox/2026-01-01 Old Note.md", "x", 30)
	addNote(t, vault, "00-Inbox/2026-07-14 Fresh.md", "x", 1)
	out, errs, err := runCmd(t, "inbox")
	if err != nil {
		t.Fatalf("err: %v\nstderr: %s", err, errs)
	}
	if !strings.Contains(errs, "Inbox contents") {
		t.Errorf("header must be on stderr, got %q", errs)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 stdout records, got %d: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "⚠\t2026-01-01 Old Note\t") {
		t.Errorf("line 0 = %q, want ⚠ + name + age", lines[0])
	}
	if !strings.HasPrefix(lines[1], "•\t2026-07-14 Fresh\t") {
		t.Errorf("line 1 = %q, want • + name + age", lines[1])
	}
}

func TestInboxEmpty(t *testing.T) {
	newVaultFixture(t)
	out, errs, err := runCmd(t, "inbox")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("empty inbox must produce no stdout records, got %q", out)
	}
	if !strings.Contains(errs, "Inbox is empty") {
		t.Errorf("stderr = %q, want empty-state notice", errs)
	}
}

func TestInboxMissingDir(t *testing.T) {
	vault := newVaultFixture(t)
	os.RemoveAll(filepath.Join(vault, "00-Inbox"))
	out, errs, err := runCmd(t, "inbox")
	if err != nil {
		t.Fatalf("missing inbox must be non-fatal: %v", err)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errs, "not found") {
		t.Errorf("stderr = %q, want not-found notice", errs)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ov/ -run TestInbox -v`
Expected: FAIL — `undefined: newInboxCmd` (compile error)

- [ ] **Step 3: Write the implementation**

```go
// cmd/ov/common.go
package main

import "github.com/arosenkranz/obsidian-vault-tools/internal/config"

// resolveConfig loads config, applies a --vault override (with ~/$VAR
// expansion), and validates that the vault directory exists. Shared by the
// read-only commands; mirrors doctor.go's load sequence.
func resolveConfig(vaultFlag string) (*config.Config, error) {
	cfg, err := config.Load("")
	if err != nil {
		return nil, err
	}
	if vaultFlag != "" {
		cfg.VaultDir = config.ExpandPath(vaultFlag)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
```

```go
// cmd/ov/inbox.go
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

func newInboxCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "List inbox notes with ages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
			notes, err := vault.ListInbox(cfg.VaultDir, cfg.Inbox)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					fmt.Fprintf(errw, "inbox directory not found: %s\n", filepath.Join(cfg.VaultDir, cfg.Inbox))
					return nil
				}
				return err
			}
			if len(notes) == 0 {
				fmt.Fprintln(errw, "Inbox is empty")
				return nil
			}
			fmt.Fprintln(errw, "Inbox contents")
			now := time.Now()
			for _, n := range notes {
				age := vault.AgeDays(now, n.ModTime)
				fmt.Fprintf(out, "%s\t%s\t%d\n", ageMarker(age), n.Name, age)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}

// ageMarker mirrors v1 format_age (behavior inventory row #19): ⚠ when a note
// is older than 7 days, • otherwise. The threshold is reachable now that
// AgeDays reports real day counts (row #18 fix).
func ageMarker(age int) string {
	if age > 7 {
		return "⚠"
	}
	return "•"
}
```

In `cmd/ov/root.go`, extend the `AddCommand` call to register the inbox command:

```go
	root.AddCommand(newConfigCmd(), newDoctorCmd(), newInitCmd(), newInboxCmd())
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ov/ -v`
Expected: PASS (all cmd tests, including phase 0's)

- [ ] **Step 5: Verify the binary builds and runs**

Run: `make build && ./dist/ov2 inbox --help`
Expected: builds; prints usage for `inbox`

- [ ] **Step 6: Commit**

```bash
git add cmd/ov/common.go cmd/ov/inbox.go cmd/ov/fixture_test.go cmd/ov/inbox_test.go cmd/ov/root.go
git commit -m "Add ov2 inbox command with stdout/stderr discipline"
```

---

### Task 6: `ov2 stale` (`cmd/ov/stale.go`)

**Files:**
- Create: `cmd/ov/stale.go`
- Create: `cmd/ov/stale_test.go`
- Modify: `cmd/ov/root.go` (register `newStaleCmd`)

**Interfaces:**
- Consumes: `resolveConfig`, `runCmd`/`newVaultFixture`/`addNote` (Task 5); `vault.Stale`, `vault.AgeDays` (Task 2); `Config.Archive`, `Config.Meta`, `Config.StaleExclude` (Task 4)
- Produces: `func newStaleCmd() *cobra.Command` — `ov2 stale [days]`, default 90

- [ ] **Step 1: Write the failing tests**

```go
// cmd/ov/stale_test.go
package main

import (
	"strings"
	"testing"
)

// CONTRACT(#62): notes older than the default 90 days, vault-relative + age.
// BUG(fixed)(#61): Archive excluded by configured name; Daily Notes via the
// default stale_exclude. DECIDE(#123): records to stdout, chrome to stderr.
func TestStaleCommand(t *testing.T) {
	vault := newVaultFixture(t)
	addNote(t, vault, "03-Resources/ancient.md", "x", 200)
	addNote(t, vault, "03-Resources/recent.md", "x", 5)
	addNote(t, vault, "04-Archive/old-archived.md", "x", 200)
	addNote(t, vault, "Daily Notes/2020.md", "x", 200)
	out, errs, err := runCmd(t, "stale")
	if err != nil {
		t.Fatalf("%v\n%s", err, errs)
	}
	if strings.Contains(out, "recent") || strings.Contains(out, "Archive") || strings.Contains(out, "Daily Notes") {
		t.Errorf("stdout leaked excluded/fresh notes: %q", out)
	}
	if !strings.HasPrefix(strings.TrimRight(out, "\n"), "03-Resources/ancient.md\t") {
		t.Errorf("stdout = %q, want ancient.md record", out)
	}
	if !strings.Contains(errs, "90+ days") {
		t.Errorf("stderr = %q, want header naming the threshold", errs)
	}
}

func TestStaleCustomDays(t *testing.T) {
	vault := newVaultFixture(t)
	addNote(t, vault, "03-Resources/n.md", "x", 10)
	out, _, err := runCmd(t, "stale", "7")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "03-Resources/n.md") {
		t.Errorf("stale 7 should include a 10-day-old note: %q", out)
	}
	out2, _, _ := runCmd(t, "stale", "30")
	if strings.Contains(out2, "n.md") {
		t.Errorf("stale 30 should exclude a 10-day-old note: %q", out2)
	}
}

func TestStaleBadDays(t *testing.T) {
	newVaultFixture(t)
	if _, _, err := runCmd(t, "stale", "abc"); err == nil {
		t.Error("non-integer days must error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ov/ -run TestStale -v`
Expected: FAIL — `undefined: newStaleCmd`

- [ ] **Step 3: Write the implementation**

```go
// cmd/ov/stale.go
package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

func newStaleCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "stale [days]",
		Short: "List notes untouched for N+ days (default 90)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			days := 90
			if len(args) == 1 {
				n, err := strconv.Atoi(args[0])
				if err != nil || n < 0 {
					return fmt.Errorf("days must be a non-negative integer, got %q", args[0])
				}
				days = n
			}
			out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
			// Exclusions: configured Archive + Meta (row #61 fix) plus the
			// file-driven stale_exclude list (row #62, default Daily Notes).
			excludes := append([]string{cfg.Archive, cfg.Meta}, cfg.StaleExclude...)
			now := time.Now()
			notes, err := vault.Stale(cfg.VaultDir, days, now, excludes)
			if err != nil {
				return err
			}
			fmt.Fprintf(errw, "Notes untouched in %d+ days\n", days)
			if len(notes) == 0 {
				fmt.Fprintln(errw, "None found")
				return nil
			}
			for _, n := range notes {
				fmt.Fprintf(out, "%s\t%d\n", n.Rel, vault.AgeDays(now, n.ModTime))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}
```

In `cmd/ov/root.go`, add `newStaleCmd()` to the `AddCommand` call:

```go
	root.AddCommand(newConfigCmd(), newDoctorCmd(), newInitCmd(), newInboxCmd(), newStaleCmd())
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ov/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/ov/stale.go cmd/ov/stale_test.go cmd/ov/root.go
git commit -m "Add ov2 stale command with configurable exclusions"
```

---

### Task 7: `ov2 review` (`cmd/ov/review.go`)

Composed dashboard: inbox count, modified-this-week, projects, MOCs — data as labeled `label\tvalue` records to stdout, chrome and hints to stderr.

**Files:**
- Create: `cmd/ov/review.go`
- Create: `cmd/ov/review_test.go`
- Modify: `cmd/ov/root.go` (register `newReviewCmd`)

**Interfaces:**
- Consumes: `resolveConfig` (Task 5); `vault.ListInbox`, `vault.ModifiedWithin`, `vault.ListProjects` (Task 2); `vault.MOCs` (Task 3)
- Produces: `func newReviewCmd() *cobra.Command`

- [ ] **Step 1: Write the failing test**

```go
// cmd/ov/review_test.go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CONTRACT(#60): inbox count, modified-this-week (Archive excluded), projects,
// MOCs. DECIDE(#124): inbox count unified with `inbox` (top-level). DECIDE(#61):
// modified excludes the configured Archive. DECIDE(#123): chrome to stderr.
func TestReviewSummary(t *testing.T) {
	vault := newVaultFixture(t)
	addNote(t, vault, "00-Inbox/a.md", "x", 1)
	addNote(t, vault, "00-Inbox/b.md", "x", 1)
	addNote(t, vault, "01-Projects/Proj One.md", "x", 1)
	if err := os.MkdirAll(filepath.Join(vault, "01-Projects", "Big Project"), 0o755); err != nil {
		t.Fatal(err)
	}
	addNote(t, vault, "02-Areas/Recent.md", "x", 2)
	addNote(t, vault, "02-Areas/Old.md", "x", 40)
	addNote(t, vault, "04-Archive/Archived.md", "x", 1)
	addNote(t, vault, "03-Resources/MOC Music.md", "## Key Notes\n- [[Jazz]]\n", 1)
	out, errs, err := runCmd(t, "review")
	if err != nil {
		t.Fatalf("%v\n%s", err, errs)
	}
	if !strings.Contains(errs, "Weekly Review") || !strings.Contains(errs, "Next steps") {
		t.Errorf("chrome must be on stderr, got %q", errs)
	}
	if !strings.Contains(out, "inbox\t2\n") {
		t.Errorf("want inbox count 2, got %q", out)
	}
	if !strings.Contains(out, "modified\tRecent\n") {
		t.Errorf("Recent should be modified-this-week: %q", out)
	}
	if strings.Contains(out, "modified\tArchived") || strings.Contains(out, "modified\tOld") {
		t.Errorf("Archive/old notes must not appear in modified: %q", out)
	}
	if !strings.Contains(out, "project\tBig Project\n") || !strings.Contains(out, "project\tProj One\n") {
		t.Errorf("projects missing: %q", out)
	}
	if !strings.Contains(out, "moc\tMOC Music\n") {
		t.Errorf("MOC missing: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ov/ -run TestReview -v`
Expected: FAIL — `undefined: newReviewCmd`

- [ ] **Step 3: Write the implementation**

```go
// cmd/ov/review.go
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

func newReviewCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Weekly review summary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
			now := time.Now()
			fmt.Fprintln(errw, "Weekly Review")

			// Inbox count — unified with `ov2 inbox` (top-level *.md, row #124).
			inbox, err := vault.ListInbox(cfg.VaultDir, cfg.Inbox)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			fmt.Fprintf(out, "inbox\t%d\n", len(inbox))

			// Modified this week (< 7 days), Archive excluded (rows #60, #61,
			// #125), newest first, capped at 10.
			modified, err := vault.ModifiedWithin(cfg.VaultDir, 7, now, []string{cfg.Archive})
			if err != nil {
				return err
			}
			if len(modified) > 10 {
				modified = modified[:10]
			}
			for _, n := range modified {
				fmt.Fprintf(out, "modified\t%s\n", n.Name)
			}

			// Projects — immediate children (row #60; #127: dir/file glyph
			// distinction dropped as decoration).
			projects, err := vault.ListProjects(cfg.VaultDir, cfg.Projects)
			if err != nil {
				return err
			}
			for _, p := range projects {
				fmt.Fprintf(out, "project\t%s\n", p.Name)
			}

			// MOCs (rows #60, #34).
			mocs, err := vault.MOCs(cfg.VaultDir)
			if err != nil {
				return err
			}
			for _, m := range mocs {
				fmt.Fprintf(out, "moc\t%s\n", m.Name)
			}

			// Hints are pure decoration → stderr (row #123).
			fmt.Fprintln(errw, "Next steps:")
			fmt.Fprintln(errw, "  - Process inbox with 'ov2 triage'")
			fmt.Fprintln(errw, "  - Check for stale notes with 'ov2 stale'")
			fmt.Fprintln(errw, "  - Update brag document if work-related wins")
			return nil
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}
```

In `cmd/ov/root.go`, add `newReviewCmd()`:

```go
	root.AddCommand(newConfigCmd(), newDoctorCmd(), newInitCmd(), newInboxCmd(), newStaleCmd(), newReviewCmd())
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ov/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/ov/review.go cmd/ov/review_test.go cmd/ov/root.go
git commit -m "Add ov2 review command"
```

---

### Task 8: `ov2 mocs list` + `ov2 mocs orphan` (`cmd/ov/mocs.go`)

Parent `mocs` command with the two read-only subcommands. Final step verifies the read-only invariant across the whole branch.

**Files:**
- Create: `cmd/ov/mocs.go`
- Create: `cmd/ov/mocs_test.go`
- Modify: `cmd/ov/root.go` (register `newMocsCmd`)

**Interfaces:**
- Consumes: `resolveConfig` (Task 5); `vault.MOCs`, `vault.Orphans` (Task 3); `Config.Resources`, `Config.Areas` (phase 0)
- Produces: `func newMocsCmd() *cobra.Command` (parent), with `list` and `orphan` subcommands
  - `mocs list` stdout record: `<name>\t<count>\t<description>` (name + description = row #63; count from resolved row #32)
  - `mocs orphan` stdout record: `<rel_path>` (one per orphan)

- [ ] **Step 1: Write the failing tests**

```go
// cmd/ov/mocs_test.go
package main

import (
	"reflect"
	"strings"
	"testing"
)

// CONTRACT(#63): mocs list shows name + description; count added from the
// resolved row #32 item count. DECIDE(#123): record to stdout, chrome to stderr.
func TestMocsList(t *testing.T) {
	vault := newVaultFixture(t)
	addNote(t, vault, "03-Resources/MOC Music.md",
		"# MOC Music\n\n> All my music notes\n\n## Key Notes\n- [[Jazz]]\n", 1)
	out, errs, err := runCmd(t, "mocs", "list")
	if err != nil {
		t.Fatalf("%v\n%s", err, errs)
	}
	if !strings.Contains(out, "MOC Music\t1\t> All my music notes\n") {
		t.Errorf("stdout = %q, want name\\tcount\\tdescription", out)
	}
	if !strings.Contains(errs, "Maps of Content") {
		t.Errorf("chrome must be on stderr: %q", errs)
	}
}

func TestMocsListEmpty(t *testing.T) {
	newVaultFixture(t)
	out, errs, err := runCmd(t, "mocs", "list")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("no MOCs → no stdout, got %q", out)
	}
	if !strings.Contains(errs, "No MOCs found") {
		t.Errorf("stderr = %q", errs)
	}
}

// BUG(fixed)(#1,#2): v1 reported EVERY note orphaned; v2 parses MOC links so
// linked notes are excluded. DECIDE(#65): scope is Resources + Areas.
func TestMocsOrphan(t *testing.T) {
	vault := newVaultFixture(t)
	addNote(t, vault, "03-Resources/MOC Music.md", "## Key Notes\n- [[Jazz]]\n", 1)
	addNote(t, vault, "03-Resources/Jazz.md", "# Jazz\n", 1)
	addNote(t, vault, "03-Resources/Blues.md", "# Blues\n", 1)
	addNote(t, vault, "02-Areas/Standup.md", "# Standup\n", 1)
	addNote(t, vault, "01-Projects/Proj.md", "# Proj\n", 1)
	out, errs, err := runCmd(t, "mocs", "orphan")
	if err != nil {
		t.Fatalf("%v\n%s", err, errs)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	want := []string{"02-Areas/Standup.md", "03-Resources/Blues.md"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("orphans = %v, want %v", lines, want)
	}
	if strings.Contains(out, "Jazz") {
		t.Error("linked Jazz must not be orphaned")
	}
	if strings.Contains(out, "Proj") {
		t.Error("Projects not in orphan scope (row #65)")
	}
	if !strings.Contains(errs, "Orphaned notes") {
		t.Errorf("chrome: %q", errs)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ov/ -run TestMocs -v`
Expected: FAIL — `undefined: newMocsCmd`

- [ ] **Step 3: Write the implementation**

```go
// cmd/ov/mocs.go
package main

import (
	"fmt"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

func newMocsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mocs",
		Short: "Maps of Content",
	}
	cmd.AddCommand(newMocsListCmd(), newMocsOrphanCmd())
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
```

In `cmd/ov/root.go`, add `newMocsCmd()`:

```go
	root.AddCommand(newConfigCmd(), newDoctorCmd(), newInitCmd(), newInboxCmd(), newStaleCmd(), newReviewCmd(), newMocsCmd())
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ov/ -v`
Expected: PASS

- [ ] **Step 5: Verify the whole suite and the read-only invariant**

Run: `go test ./...`
Expected: PASS (all packages)

Run: `go build ./cmd/ov` then confirm no write path exists in the read-only surface. There must be NO match for `WriteNoteAtomic`, `os.Create`, `os.WriteFile`, `os.Rename`, `os.Remove`, or `os.Mkdir` in the new command and query files:

```bash
grep -nE 'WriteNoteAtomic|os\.(Create|WriteFile|Rename|Remove|RemoveAll|Mkdir|MkdirAll|Chmod|Chtimes)' \
  cmd/ov/inbox.go cmd/ov/stale.go cmd/ov/review.go cmd/ov/mocs.go cmd/ov/common.go \
  internal/vault/query.go internal/vault/moc.go internal/vault/wikilink.go
```

Expected: no output (exit 1) — the read-only surface performs no filesystem mutations. (Write helpers appear only in `*_test.go` fixtures, which build temp vaults, and in phase-0 `write.go`, which nothing here calls.)

- [ ] **Step 6: Commit**

```bash
git add cmd/ov/mocs.go cmd/ov/mocs_test.go cmd/ov/root.go
git commit -m "Add ov2 mocs list and reimplemented mocs orphan"
```

---

## Self-Review

**1. Spec coverage (phase table row: `inbox, review, stale, mocs list, mocs orphan (fixed)`):**
- `ov2 inbox` → Task 5 (`ListInbox` Task 2, `ageMarker` row #19).
- `ov2 review` → Task 7 (`ListInbox`, `ModifiedWithin`, `ListProjects` Task 2; `MOCs` Task 3).
- `ov2 stale` → Task 6 (`Stale` Task 2; `stale_exclude` Task 4).
- `ov2 mocs list` → Task 8 (`MOCs` Task 3; count row #32, description row #63).
- `ov2 mocs orphan` → Task 8 (`Orphans` Task 3; reimplement rows #1, #2, #65, #126).
- Core-contract query seam (`ListInbox`, `Stale`, `Orphans`, `MOCs`, plus `ModifiedWithin`/`ListProjects` for review) → Tasks 2, 3; all reads go through them.
- Walk resilience → `collectNotes` + `mocPaths` error callbacks; `TestStaleWalkResilient`.
- stdout/stderr discipline → every command test asserts records-on-stdout, chrome-on-stderr; `runCmd` captures the two streams separately.
- Read-only invariant → Task 8 Step 5 grep; no command calls a write path.
- Age BUG (#18) fixed → `AgeDays` + `TestAgeDays`.

**2. Placeholder scan:** every code step contains complete, compilable Go — no TBD/TODO, no "add error handling", no "similar to Task N" without repeated code. Shared test helpers are defined once (Task 5 `fixture_test.go`) and named in later Interfaces blocks.

**3. Type consistency:** `vault.Note{Path,Rel,Name string; ModTime time.Time}` used identically across Tasks 2, 3, 6, 7, 8. `vault.AgeDays(now, mod time.Time) int` — Tasks 2, 5, 6. `vault.Stale(vaultDir string, days int, now time.Time, excludes []string)` — Tasks 2, 6. `vault.MOCs(vaultDir string) ([]MOC, error)` — Tasks 3, 7, 8. `vault.Orphans(vaultDir string, roots []string)` — Tasks 3, 8. `resolveConfig(vaultFlag string) (*config.Config, error)` — Tasks 5, 6, 7, 8. `config.Config.StaleExclude []string` — Tasks 4, 6. `runCmd(t, args...) (stdout, stderr string, err error)` — Tasks 5-8. All cross-checked.
