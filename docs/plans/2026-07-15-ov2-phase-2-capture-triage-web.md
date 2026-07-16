# ov v2 Phase 2: Capture, Manual Triage, TUI Pickers, Web Capture — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the first write paths — `ov2 capture` (with URL title fetch, MOC entry append, filename collision handling), manual `ov2 triage`, the bubbletea folder/MOC pickers behind them, and `ov2 serve` (capture form + inbox list web UI) — all writes going through phase 0's `vault.WriteNoteAtomic`, with vault containment enforced on every new write path.

**Architecture:** Per the approved spec (`docs/plans/2026-07-11-ov-v2-go-rewrite-design.md`). Two new core packages sit beside `internal/vault`: `internal/capture` (title derivation, SSRF-hardened URL title fetch, filename stamping, note assembly, capture orchestration) and `internal/tui` (bubbletea v2 folder/MOC pickers, huh delete-confirm). Both CLI (`cmd/ov/capture.go`, `cmd/ov/triage.go`, `cmd/ov/serve.go`) and the new `internal/web` server call the SAME `capture.Capture` verb — the design's "stateless verbs, two frontends one core" principle made concrete for the first time this phase. `internal/vault` gains its first mutation helpers beyond `WriteNoteAtomic` itself: `ContainPath` (symlink-safe containment), `EnsureFolder`, `AppendMOCEntry`, `FindMOCByName`, `ListAllFolders`, `MoveNote`.

**Tech Stack:** Go 1.25.0, cobra, pelletier/go-toml/v2, golang.org/x/text (existing). New this phase: `charm.land/bubbletea/v2` (TUI runtime), `charm.land/lipgloss/v2` (styling), `github.com/charmbracelet/huh` (delete confirmation), `github.com/mattn/go-isatty` (tty detection — already a transitive dependency of huh; promoted to a direct import, not a new module in the graph). `charm.land/bubbles/v2` is in the design's approved dep budget but is **not** added this phase: the picker's only text-entry need (typing a new folder path) is met by ~15 lines of hand-rolled key handling that is fully unit-testable without a tty, and pulling in `bubbles/list.Model`'s much larger API surface for a two-screen picker is unjustified YAGNI. Revisit if a later phase's TUI needs grow.

## Global Constraints

- Module path: `github.com/arosenkranz/obsidian-vault-tools`
- Binary name during transition: `ov2`, built to `dist/ov2` (`make build`)
- New direct dependencies this phase, exact pins: `charm.land/bubbletea/v2@v2.0.8`, `charm.land/lipgloss/v2@v2.0.5`, `github.com/charmbracelet/huh@v1.0.0`, `github.com/mattn/go-isatty` (version resolved by `go mod tidy`, currently v0.0.20). No other new dependency without checking the budget in design spec §Architecture first.
- `bin/vault.sh`, `bin/triage_llm.py`, `bin/moc_cleanup.py` are READ-ONLY — never modify them
- **First write paths.** Every new write goes through `vault.WriteNoteAtomic` — no `os.WriteFile`/`os.Create`/raw redirects on note content anywhere in `internal/capture`, `internal/vault`'s new mutation helpers, `cmd/ov/capture.go`, `cmd/ov/triage.go` (moves are `os.Rename` via `vault.MoveNote`, not content writes — atomicity there is the filesystem rename, not `WriteNoteAtomic`), or `internal/web`.
- **Containment.** Every write target computed from user/picker input (triage move destination, including a typed new-folder path) MUST pass through `vault.ContainPath` before any filesystem mutation. Table-tested: sibling dir, traversal, symlinked subdir (escaping and staying inside), not-yet-existing nested path.
- **Conditional writes.** Any read-modify-write on an existing file (MOC entry append) re-reads and passes the fresh hash to `WriteNoteAtomic` immediately before writing — never write a hash captured earlier in the flow.
- **stdout/stderr discipline (spec §CLI/TUI; behavior inventory row #123, extended to write commands here):** `ov2 capture`'s stdout is exactly one line — the captured note's vault-relative path — nothing else. All decoration (banners, MOC-link status, warnings) goes to stderr. `ov2 triage` and `ov2 serve` are fully interactive/network surfaces with no machine-readable stdout contract to protect.
- **tty discipline (row #9/#36, extended by row #136):** `ov2 capture`'s MOC picker launches only when stdin AND stdout are both a real interactive terminal (`github.com/mattn/go-isatty`) — never blocks a piped/cron/agent invocation. `ov2 triage` requires an interactive terminal on both stdin and stdout and errors immediately otherwise (it has no non-interactive mode this phase). Bubbletea v2 always opens the controlling tty for its own input regardless of the process's stdin redirection (confirmed via the v2 upgrade guide: `tea.WithInputTTY()` was removed because v2 does this unconditionally) — this is what keeps a piped capture body safe from the picker.
- **SSRF hardening (design spec §Safety item 4 / row #131):** `capture.HTTPTitleFetcher` performs a post-DNS IP check on every resolved address before connecting, refusing loopback/private/link-local/CGNAT (`100.64.0.0/10`) targets, and dials the checked IP directly (never the hostname) to close the DNS-rebinding TOCTOU window. 5s timeout, fixed UA, both ported from row #28.
- **Web bind guard (row #133):** `ov2 serve --bind` default `127.0.0.1:8420`; refuses any other address unless it is `localhost`, a loopback IP, a Tailscale address (`100.64.0.0/10`), or `--allow-nonlocal-bind` is set.
- **Web hygiene (row #134):** every request's `Host` header is validated against the configured bind; every `POST` requires an absent-or-same-origin `Origin` header AND the `HX-Request` header.
- **Web title-fetch opt-in (row #135):** the web capture form never auto-fetches a URL title; only an explicit `fetch_title` checkbox triggers it. CLI capture keeps the v1 automatic-fetch-on-bare-URL behavior (row #46) — the two frontends deliberately differ here because a web request's URL is more directly attacker-reachable.
- `internal/web/assets` (not `templates/` — collides with the repo's existing vault templates) is embedded via `go:embed`. `htmx.min.js` is vendored at pinned version 1.9.12 (`https://unpkg.com/htmx.org@1.9.12/dist/htmx.min.js`), sha256 `449317ade7881e949510db614991e195c3a099c4c791c24dacec55f9f4a2a452`, 48101 bytes.
- Every mined behavior test carries a comment `// CONTRACT:`, `// BUG(fixed):`, or `// DECIDE(<resolution>):` referencing a row number in `docs/plans/ov2-behavior-inventory.md` (rows 1–142; rows 128–142 were mined for this phase).
- Go tests use `t.Setenv` / `t.TempDir` / `os.Chtimes` — no global state leaks between tests; no injected package-level clock (clocks are passed as `time.Time` values or `func() time.Time`).
- Commit style: imperative mood, no conventional-commit prefix (matches repo history)
- Interactive commands (`triage`, pickers, `huh.Confirm`) inject their tty-dependent collaborators as function values so `cmd/ov` tests never open a real tty; `internal/tui`'s cursor/selection logic is a pure `state.handleKey(key string) state` function tested without `tea.Program` at all — only the ~10-line `RunFolderPicker`/`RunMOCPicker` wrapper functions touch `tea.NewProgram`, and those are exercised only by the CLI smoke test (via the real built binary), never by unit tests.

---

### Task 1: `internal/vault` write & full-depth query additions

Foundational mutation and query primitives every later task in this phase builds on: symlink-safe containment, MOC-entry text transform, MOC lookup by name, the human triage picker's full-depth folder listing (distinct from phase 1's `DiscoverFolders`, which caps at depth 2 for the future LLM prompt), and a collision-refusing note move.

**Files:**
- Create: `internal/vault/contain.go`
- Create: `internal/vault/contain_test.go`
- Create: `internal/vault/moc_write.go`
- Create: `internal/vault/moc_write_test.go`
- Create: `internal/vault/folders.go`
- Create: `internal/vault/folders_test.go`
- Create: `internal/vault/move.go`
- Create: `internal/vault/move_test.go`

**Interfaces:**
- Consumes: `vault.MOC`, `mocPaths`, `mocDescription`, `ParseNote`, `ParseWikilinks` (phase 1, `internal/vault/moc.go`/`wikilink.go`); `vault.ErrExists` (phase 0, `internal/vault/write.go`)
- Produces:
  - `var ErrEscapesVault error`
  - `func ContainPath(root, target string) (string, error)`
  - `func EnsureFolder(vaultDir, rel string) (string, error)`
  - `func AppendMOCEntry(content, title, snippet string) string`
  - `func FindMOCByName(vaultDir, resourcesDir, name string) (*MOC, error)`
  - `func ListAllFolders(vaultDir string, roots []string) []string`
  - `func MoveNote(srcAbs, destDirAbs string) (string, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/vault/contain_test.go
package vault

import (
	"os"
	"path/filepath"
	"testing"
)

// BUG(fixed)(#6,#130): v1 used a string-prefix check that accepts a sibling
// "vault-evil" directory sharing the vault's name as a prefix; v1 bash
// manual triage had no containment check at all.
func TestContainPathRejectsSibling(t *testing.T) {
	parent := t.TempDir()
	vaultDir := filepath.Join(parent, "vault")
	evil := filepath.Join(parent, "vault-evil")
	if err := os.MkdirAll(vaultDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(evil, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ContainPath(vaultDir, evil); err == nil {
		t.Fatal("expected rejection of a sibling directory sharing a name prefix")
	}
}

func TestContainPathRejectsTraversal(t *testing.T) {
	vaultDir := t.TempDir()
	if _, err := ContainPath(vaultDir, "../../etc/passwd"); err == nil {
		t.Fatal("expected rejection of a traversal path")
	}
}

func TestContainPathRejectsSymlinkedEscape(t *testing.T) {
	parent := t.TempDir()
	vaultDir := filepath.Join(parent, "vault")
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(vaultDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(vaultDir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ContainPath(vaultDir, filepath.Join(link, "note.md")); err == nil {
		t.Fatal("expected rejection through a symlink pointing outside the vault")
	}
}

func TestContainPathAllowsSymlinkedSubdirInside(t *testing.T) {
	vaultDir := t.TempDir()
	real := filepath.Join(vaultDir, "03-Resources")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(vaultDir, "res-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	got, err := ContainPath(vaultDir, filepath.Join(link, "note.md"))
	if err != nil {
		t.Fatalf("expected an in-vault symlink target to be allowed: %v", err)
	}
	want := filepath.Join(real, "note.md")
	if got != want {
		t.Errorf("ContainPath = %q, want %q", got, want)
	}
}

// CONTRACT(#37): a not-yet-created nested path is allowed (the folder
// picker creates it on demand).
func TestContainPathAllowsNotYetExisting(t *testing.T) {
	vaultDir := t.TempDir()
	got, err := ContainPath(vaultDir, "02-Areas/Brand New Folder")
	if err != nil {
		t.Fatalf("expected a new in-vault path to be allowed: %v", err)
	}
	want := filepath.Join(vaultDir, "02-Areas", "Brand New Folder")
	if got != want {
		t.Errorf("ContainPath = %q, want %q", got, want)
	}
}

// CONTRACT(#37): EnsureFolder creates the folder (after ~-trim) and returns
// its absolute path.
func TestEnsureFolderCreatesOnDemand(t *testing.T) {
	vaultDir := t.TempDir()
	got, err := EnsureFolder(vaultDir, "/02-Areas/New Folder/")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(vaultDir, "02-Areas", "New Folder")
	if got != want {
		t.Errorf("EnsureFolder = %q, want %q", got, want)
	}
	info, err := os.Stat(want)
	if err != nil || !info.IsDir() {
		t.Fatalf("folder not created: %v", err)
	}
}

func TestEnsureFolderRejectsEscape(t *testing.T) {
	vaultDir := t.TempDir()
	if _, err := EnsureFolder(vaultDir, "../outside"); err == nil {
		t.Fatal("expected rejection of an escaping folder")
	}
}
```

```go
// internal/vault/moc_write_test.go
package vault

import (
	"os"
	"path/filepath"
	"testing"
)

// CONTRACT(#42): inserting into an existing "## 🔗 Recent Additions"
// heading places the entry immediately below the heading.
func TestAppendMOCEntryExistingHeading(t *testing.T) {
	content := "# MOC Music\n\n## 🔗 Recent Additions\n- [[Old Entry]] — old\n"
	got := AppendMOCEntry(content, "New Song", "a great tune")
	want := "# MOC Music\n\n## 🔗 Recent Additions\n\n- [[New Song]] — a great tune\n- [[Old Entry]] — old\n"
	if got != want {
		t.Errorf("AppendMOCEntry =\n%q\nwant\n%q", got, want)
	}
}

// CONTRACT(#8,#41): no matching heading -> a new "## 🔗 Recent Additions"
// section is created at EOF (the v2 simplification of v1's emoji-heading
// preference chain).
func TestAppendMOCEntryCreatesHeading(t *testing.T) {
	content := "# MOC Music\n\nSome description.\n"
	got := AppendMOCEntry(content, "New Song", "a great tune")
	want := "# MOC Music\n\nSome description.\n\n## 🔗 Recent Additions\n- [[New Song]] — a great tune\n"
	if got != want {
		t.Errorf("AppendMOCEntry =\n%q\nwant\n%q", got, want)
	}
}

// CONTRACT(#40): entry format is "- [[Title]] — snippet".
func TestAppendMOCEntryFormat(t *testing.T) {
	got := AppendMOCEntry("# X\n", "My Title", "my snippet")
	if want := "- [[My Title]] — my snippet"; !containsLine(got, want) {
		t.Errorf("AppendMOCEntry =\n%q\nwant a line %q", got, want)
	}
}

func containsLine(s, line string) bool {
	for _, l := range splitLines(s) {
		if l == line {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i, r := range s {
		if r == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}

// CONTRACT(#33): accepts "Music" or "MOC Music"; prefers Resources, then
// vault-wide.
func TestFindMOCByName(t *testing.T) {
	vaultDir := t.TempDir()
	resources := filepath.Join(vaultDir, "03-Resources")
	if err := os.MkdirAll(resources, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, "MOC Music.md"), []byte("# MOC Music\n\n> tunes\n\n[[Jazz]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Music", "MOC Music"} {
		got, err := FindMOCByName(vaultDir, "03-Resources", name)
		if err != nil {
			t.Fatalf("FindMOCByName(%q): %v", name, err)
		}
		if got.Name != "MOC Music" {
			t.Errorf("Name = %q", got.Name)
		}
	}
}

func TestFindMOCByNameNotFound(t *testing.T) {
	vaultDir := t.TempDir()
	if _, err := FindMOCByName(vaultDir, "03-Resources", "Nope"); err == nil {
		t.Fatal("expected an error for an unknown MOC")
	}
}

// CONTRACT(#33): a vault-wide match outside Resources is still found.
func TestFindMOCByNameVaultWideFallback(t *testing.T) {
	vaultDir := t.TempDir()
	other := filepath.Join(vaultDir, "04-Archive")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "MOC Old.md"), []byte("# MOC Old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := FindMOCByName(vaultDir, "03-Resources", "Old")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "MOC Old" {
		t.Errorf("Name = %q", got.Name)
	}
}
```

```go
// internal/vault/folders_test.go
package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// DECIDE(#35): the human picker walks full depth (unlike DiscoverFolders'
// depth<=2 used for the LLM prompt), mirroring v1 bash's
// list_all_para_folders.
func TestListAllFolders(t *testing.T) {
	vaultDir := t.TempDir()
	for _, d := range []string{"01-Projects", "02-Areas/Work", "02-Areas/Work/Clients", "03-Resources"} {
		if err := os.MkdirAll(filepath.Join(vaultDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := ListAllFolders(vaultDir, []string{"01-Projects", "02-Areas", "03-Resources", "04-Archive"})
	want := []string{"01-Projects", "02-Areas", "02-Areas/Work", "02-Areas/Work/Clients", "03-Resources"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListAllFolders = %v, want %v", got, want)
	}
}

func TestListAllFoldersMissingRootSkipped(t *testing.T) {
	vaultDir := t.TempDir()
	got := ListAllFolders(vaultDir, []string{"01-Projects"})
	if got != nil {
		t.Errorf("ListAllFolders = %v, want nil for an all-missing root set", got)
	}
}
```

```go
// internal/vault/move_test.go
package vault

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMoveNote(t *testing.T) {
	vaultDir := t.TempDir()
	src := filepath.Join(vaultDir, "00-Inbox", "note.md")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	destDir := filepath.Join(vaultDir, "02-Areas")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := MoveNote(src, destDir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(destDir, "note.md")
	if got != want {
		t.Errorf("MoveNote = %q, want %q", got, want)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source should no longer exist")
	}
}

// BUG(fixed)(#139): a plain `mv` would silently overwrite; v2 refuses.
func TestMoveNoteRefusesExisting(t *testing.T) {
	vaultDir := t.TempDir()
	src := filepath.Join(vaultDir, "00-Inbox", "note.md")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	destDir := filepath.Join(vaultDir, "02-Areas")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "note.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MoveNote(src, destDir); !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
	got, err := os.ReadFile(filepath.Join(destDir, "note.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Error("existing destination must not be overwritten")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vault/ -run 'TestContainPath|TestEnsureFolder|TestAppendMOCEntry|TestFindMOCByName|TestListAllFolders|TestMoveNote' -v`
Expected: FAIL — `undefined: ContainPath`, `undefined: EnsureFolder`, `undefined: AppendMOCEntry`, `undefined: FindMOCByName`, `undefined: ListAllFolders`, `undefined: MoveNote`

- [ ] **Step 3: Write the implementation**

```go
// internal/vault/contain.go
package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrEscapesVault is returned by ContainPath when target resolves outside root.
var ErrEscapesVault = errors.New("target escapes vault")

// ContainPath resolves target (absolute, or relative to root) against root,
// following symlinks on both sides, and rejects any result that is absolute
// or ".."-prefixed relative to root's real path. target need not exist yet:
// ContainPath walks up to the nearest existing ancestor to resolve symlinks,
// then rejoins the not-yet-created suffix. Behavior inventory row #6 (BUG:
// v1 used a string-prefix check) and row #130 (BUG: v1 manual triage had no
// containment check at all).
func ContainPath(root, target string) (string, error) {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("vault root: %w", err)
	}
	abs := target
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, target)
	}
	abs = filepath.Clean(abs)

	existing := abs
	var suffix []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("%s: %w", target, ErrEscapesVault)
		}
		suffix = append([]string{filepath.Base(existing)}, suffix...)
		existing = parent
	}
	existingReal, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	full := existingReal
	if len(suffix) > 0 {
		full = filepath.Join(append([]string{existingReal}, suffix...)...)
	}
	rel, err := filepath.Rel(rootReal, full)
	if err != nil {
		return "", fmt.Errorf("%s: %w", target, ErrEscapesVault)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s: %w", target, ErrEscapesVault)
	}
	return full, nil
}

// EnsureFolder resolves rel against vaultDir via ContainPath (after
// trimming surrounding slashes, mirroring v1's chosen_display trim), then
// creates it — and any missing parents — if it doesn't already exist.
// Returns the resolved absolute path. Behavior inventory row #37.
func EnsureFolder(vaultDir, rel string) (string, error) {
	rel = strings.Trim(strings.TrimSpace(rel), "/")
	abs, err := ContainPath(vaultDir, rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	return abs, nil
}
```

```go
// internal/vault/moc_write.go
package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AppendMOCEntry inserts "- [[title]] — snippet" into content under the
// "## 🔗 Recent Additions" heading, creating that heading (with a leading
// blank line) at EOF if content has no such heading yet. Placement is the v2
// simplification of v1's emoji-heading preference chain (behavior inventory
// rows #8, #41, #42). Pure text transform — the caller re-reads, re-hashes,
// and writes via WriteNoteAtomic (row #129 fix: no more raw >>/mktemp).
func AppendMOCEntry(content, title, snippet string) string {
	const heading = "## 🔗 Recent Additions"
	entry := "- [[" + title + "]] — " + snippet
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if line == heading {
			out := make([]string, 0, len(lines)+2)
			out = append(out, lines[:i+1]...)
			out = append(out, "", entry)
			out = append(out, lines[i+1:]...)
			return strings.Join(out, "\n")
		}
	}
	trimmed := strings.TrimRight(content, "\n")
	return trimmed + "\n\n" + heading + "\n" + entry + "\n"
}

// FindMOCByName resolves a MOC by name: accepts "Music" or "MOC Music"
// (mirroring v1 find_moc_by_name's "MOC " prefix handling exactly — a
// literal "MOC " prefix is stripped if present, otherwise the name is used
// as-is). Preference: an exact "MOC <bare-name>.md" directly in
// resourcesDir; then the first vault-wide match by sorted path. Behavior
// inventory row #33.
func FindMOCByName(vaultDir, resourcesDir, name string) (*MOC, error) {
	bare := strings.TrimPrefix(name, "MOC ")
	target := "MOC " + bare + ".md"

	resourcesPath := filepath.Join(vaultDir, resourcesDir, target)
	if info, err := os.Stat(resourcesPath); err == nil && !info.IsDir() {
		return mocAt(vaultDir, resourcesPath)
	}
	for _, p := range mocPaths(vaultDir) {
		if filepath.Base(p) == target {
			return mocAt(vaultDir, p)
		}
	}
	return nil, fmt.Errorf("MOC not found: %s", name)
}

func mocAt(vaultDir, p string) (*MOC, error) {
	content, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(vaultDir, p)
	if err != nil {
		return nil, err
	}
	_, body := ParseNote(string(content))
	return &MOC{
		Path:        p,
		Rel:         filepath.ToSlash(rel),
		Name:        strings.TrimSuffix(filepath.Base(p), ".md"),
		Description: mocDescription(string(content)),
		ItemCount:   len(ParseWikilinks(body)),
	}, nil
}
```

```go
// internal/vault/folders.go
package vault

import (
	"os"
	"path/filepath"
	"sort"
)

// ListAllFolders returns every PARA root plus every subdirectory beneath it,
// at any depth, deduplicated, in preorder with siblings sorted at each
// level. Backs the manual triage/capture folder picker (behavior inventory
// row #35: unlike DiscoverFolders' depth<=2 used for the LLM prompt, the
// human picker walks full depth like v1 bash's list_all_para_folders).
// Unreadable/vanished directories are skipped (walk resilience).
func ListAllFolders(vaultDir string, roots []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, root := range roots {
		rootPath := filepath.Join(vaultDir, root)
		if info, err := os.Stat(rootPath); err != nil || !info.IsDir() {
			continue
		}
		walkFolders(rootPath, root, seen, &out)
	}
	return out
}

func walkFolders(absPath, relPath string, seen map[string]bool, out *[]string) {
	if seen[relPath] {
		return
	}
	seen[relPath] = true
	*out = append(*out, relPath)
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		walkFolders(filepath.Join(absPath, d), relPath+"/"+d, seen, out)
	}
}
```

```go
// internal/vault/move.go
package vault

import (
	"fmt"
	"os"
	"path/filepath"
)

// MoveNote moves srcAbs into destDirAbs, keeping its basename. Refuses
// (never silently overwrites, row #139 fix of v1's plain `mv`) when a file
// with that name already exists at the destination. The rename itself is
// the OS's atomic operation on same-filesystem moves — WriteNoteAtomic is
// for content writes, not renames.
func MoveNote(srcAbs, destDirAbs string) (string, error) {
	dest := filepath.Join(destDirAbs, filepath.Base(srcAbs))
	if _, err := os.Lstat(dest); err == nil {
		return "", fmt.Errorf("%s: %w", dest, ErrExists)
	}
	if err := os.Rename(srcAbs, dest); err != nil {
		return "", err
	}
	return dest, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vault/ -v`
Expected: PASS (all vault package tests, phase 1 and phase 2)

- [ ] **Step 5: Commit**

```bash
git add internal/vault/contain.go internal/vault/contain_test.go \
  internal/vault/moc_write.go internal/vault/moc_write_test.go \
  internal/vault/folders.go internal/vault/folders_test.go \
  internal/vault/move.go internal/vault/move_test.go
git commit -m "Add vault write primitives for phase 2: containment, MOC entry append, full-depth folder listing, note move"
```

---

### Task 2: `internal/capture` — title derivation & SSRF-hardened URL fetch

The bare-URL detector and the network-touching half of capture: an injectable `TitleFetcher` interface plus its real HTTP implementation, hardened against SSRF per the design's new-in-v2 safety requirement.

**Files:**
- Create: `internal/capture/title.go`
- Create: `internal/capture/title_test.go`
- Create: `internal/capture/fetch.go`
- Create: `internal/capture/fetch_test.go`

**Interfaces:**
- Consumes: nothing from other tasks
- Produces:
  - `package capture` (new package, `github.com/arosenkranz/obsidian-vault-tools/internal/capture`)
  - `func IsBareURL(s string) bool`
  - `type TitleFetcher interface { FetchTitle(ctx context.Context, url string) (string, error) }`
  - `type HTTPTitleFetcher struct{ ... }`
  - `func NewHTTPTitleFetcher() *HTTPTitleFetcher`
  - `func (f *HTTPTitleFetcher) FetchTitle(ctx context.Context, url string) (string, error)`
  - `func ExtractTitle(htmlBody string) (string, error)` — exported for direct unit testing
  - `func rejectUnsafeIP(ip net.IP) error` — unexported, tested in-package

- [ ] **Step 1: Write the failing tests**

```go
// internal/capture/title_test.go
package capture

import "testing"

// CONTRACT(#27): bare URL = trimmed whitespace, then a whole-string
// http(s) URL with no internal whitespace.
func TestIsBareURL(t *testing.T) {
	cases := map[string]bool{
		"https://example.com":          true,
		"http://example.com/a/b":       true,
		"  https://example.com  ":      true,
		"https://example.com and more": false,
		"see https://example.com":      false,
		"not a url":                    false,
		"":                             false,
	}
	for in, want := range cases {
		if got := IsBareURL(in); got != want {
			t.Errorf("IsBareURL(%q) = %v, want %v", in, got, want)
		}
	}
}
```

```go
// internal/capture/fetch_test.go
package capture

import (
	"context"
	"net"
	"strings"
	"testing"
)

// CONTRACT(#31): first <title> tag, case-insensitive/dotall, entities
// unescaped, whitespace collapsed.
func TestExtractTitle(t *testing.T) {
	cases := []struct {
		name    string
		html    string
		want    string
		wantErr bool
	}{
		{"simple", "<html><head><TITLE>Hello &amp; World</TITLE></head></html>", "Hello & World", false},
		{"whitespace", "<title>\n  Multi\n  Line  \n</title>", "Multi Line", false},
		{"missing", "<html><body>no title here</body></html>", "", true},
		{"empty", "<title></title>", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ExtractTitle(c.html)
			if c.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("ExtractTitle = %q, want %q", got, c.want)
			}
		})
	}
}

// CONTRACT(#29): bot-challenge interstitials are rejected, not returned as
// the title.
func TestExtractTitleRejectsChallenge(t *testing.T) {
	html := `<title>Just a moment...</title><script src="https://challenges.cloudflare.com/x.js"></script>`
	if _, err := ExtractTitle(html); err == nil {
		t.Fatal("expected a challenge-interstitial rejection")
	}
}

// DECIDE(#131, new in v2): rejectUnsafeIP refuses loopback, private,
// link-local, and CGNAT (100.64.0.0/10) addresses; public addresses pass.
func TestRejectUnsafeIP(t *testing.T) {
	cases := []struct {
		ip     string
		reject bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.1.1", true},
		{"100.64.0.1", true},
		{"100.100.100.100", true},
		{"100.127.255.254", true},
		{"100.128.0.1", false},
		{"100.63.255.255", false},
		{"::1", true},
		{"fe80::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", c.ip)
		}
		err := rejectUnsafeIP(ip)
		if c.reject && err == nil {
			t.Errorf("rejectUnsafeIP(%s): expected rejection", c.ip)
		}
		if !c.reject && err != nil {
			t.Errorf("rejectUnsafeIP(%s): expected acceptance, got %v", c.ip, err)
		}
	}
}

// DECIDE(#131): the real HTTP fetcher refuses a loopback target through the
// same DialContext guard, not just in a standalone helper.
func TestHTTPTitleFetcherRefusesLoopback(t *testing.T) {
	f := NewHTTPTitleFetcher()
	_, err := f.FetchTitle(context.Background(), "http://127.0.0.1:1/")
	if err == nil {
		t.Fatal("expected the SSRF guard to refuse a loopback target")
	}
	if !strings.Contains(err.Error(), "refus") {
		t.Errorf("error = %v, want a refusal message from the SSRF guard", err)
	}
}

// CONTRACT(#30): fetch failure is never fatal — errors return for the
// caller to fall back on.
func TestHTTPTitleFetcherBadHost(t *testing.T) {
	f := NewHTTPTitleFetcher()
	if _, err := f.FetchTitle(context.Background(), "http://this-host-does-not-resolve.invalid/"); err == nil {
		t.Fatal("expected a DNS resolution error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capture/ -v`
Expected: FAIL — `no Go files in .../internal/capture` (package doesn't exist yet)

- [ ] **Step 3: Write the implementation**

```go
// internal/capture/title.go
package capture

import (
	"regexp"
	"strings"
)

// bareURLRe matches a whole string that is exactly one http(s) URL with no
// internal whitespace (behavior inventory row #27).
var bareURLRe = regexp.MustCompile(`^https?://\S+$`)

// IsBareURL reports whether s (after trimming surrounding whitespace) is
// exactly one http(s) URL with nothing else on the line.
func IsBareURL(s string) bool {
	return bareURLRe.MatchString(strings.TrimSpace(s))
}
```

```go
// internal/capture/fetch.go
package capture

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// TitleFetcher fetches the <title> of a URL. Errors are never fatal to a
// capture — callers fall back to a slugified URL (behavior inventory row
// #30). Injected so tests never hit the network.
type TitleFetcher interface {
	FetchTitle(ctx context.Context, url string) (string, error)
}

const fetchUserAgent = "Mozilla/5.0 (compatible; ov-capture/1.0)"

var (
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	wsRe    = regexp.MustCompile(`\s+`)
)

// challengeMarkers mirrors v1's bot-challenge interstitial detection
// (behavior inventory row #29): reject rather than return a fake title.
var challengeMarkers = []string{
	"challenges.cloudflare.com",
	"cf-browser-verification",
	"cf_chl_opt",
	"just a moment",
}

// HTTPTitleFetcher is the real network implementation: 5s timeout, a fixed
// UA (row #28), and a post-DNS IP check refusing loopback/private/
// link-local/CGNAT targets (row #131 — new in v2, design spec §Safety item
// 4). The check dials the validated IP directly, never the hostname,
// closing the DNS-rebinding TOCTOU window a check-then-connect-by-name
// approach would leave open.
type HTTPTitleFetcher struct {
	client *http.Client
}

func NewHTTPTitleFetcher() *HTTPTitleFetcher {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, ip := range ips {
				if rejErr := rejectUnsafeIP(ip); rejErr != nil {
					lastErr = rejErr
					continue
				}
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
			}
			if lastErr == nil {
				lastErr = errors.New("no resolvable address")
			}
			return nil, lastErr
		},
	}
	return &HTTPTitleFetcher{client: &http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return nil // each hop re-resolves and re-checks via DialContext
		},
	}}
}

// rejectUnsafeIP refuses loopback, private (RFC1918/RFC4193), link-local,
// unspecified, and CGNAT (100.64.0.0/10) addresses. Behavior inventory row
// #131.
func rejectUnsafeIP(ip net.IP) error {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("refusing unsafe address %s", ip)
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1]&0xc0 == 64 {
		return fmt.Errorf("refusing CGNAT address %s", ip) // 100.64.0.0/10
	}
	return nil
}

func (f *HTTPTitleFetcher) FetchTitle(ctx context.Context, rawURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", fetchUserAgent)
	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2MiB cap
	if err != nil {
		return "", err
	}
	return ExtractTitle(string(body))
}

// ExtractTitle mirrors v1's fetch_url_title HTML parsing (row #31): first
// <title> tag, case-insensitive/dotall, HTML entities unescaped, whitespace
// collapsed. Bot-challenge interstitials are rejected outright (row #29).
func ExtractTitle(htmlBody string) (string, error) {
	lower := strings.ToLower(htmlBody)
	for _, marker := range challengeMarkers {
		if strings.Contains(lower, marker) {
			return "", errors.New("bot-challenge interstitial detected")
		}
	}
	m := titleRe.FindStringSubmatch(htmlBody)
	if m == nil {
		return "", errors.New("no <title> found")
	}
	text := html.UnescapeString(m[1])
	text = strings.TrimSpace(wsRe.ReplaceAllString(text, " "))
	if text == "" {
		return "", errors.New("empty title")
	}
	return text, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capture/ -v`
Expected: PASS (`TestHTTPTitleFetcherBadHost` needs DNS resolution to genuinely fail for an `.invalid` TLD — this is standard and works offline; if the test environment intercepts all DNS, skip via `t.Skip` is acceptable but should not be needed)

- [ ] **Step 5: Commit**

```bash
git add internal/capture/title.go internal/capture/title_test.go \
  internal/capture/fetch.go internal/capture/fetch_test.go
git commit -m "Add internal/capture: bare-URL detection and SSRF-hardened title fetch"
```

---

### Task 3: `internal/capture` — filename stamping, note assembly, capture orchestration

The rest of the capture core: collision-safe filename stamping, frontmatter/body assembly, and the `Capture` verb both frontends call.

**Files:**
- Create: `internal/capture/filename.go`
- Create: `internal/capture/filename_test.go`
- Create: `internal/capture/note.go`
- Create: `internal/capture/note_test.go`
- Create: `internal/capture/capture.go`
- Create: `internal/capture/capture_test.go`

**Interfaces:**
- Consumes: `IsBareURL`, `TitleFetcher` (Task 2); `vault.Slugify`, `vault.ReadNote`, `vault.WriteNoteAtomic`, `vault.AppendMOCEntry`, `vault.FindMOCByName`, `vault.MOC` (phase 0/1/Task 1)
- Produces:
  - `const maxCollisionAttempts = 1000`
  - `func NextAvailablePath(dir, stem, ext string) (path, name string, err error)`
  - `type Params struct { Title, Body, Source, MOCName, Created string; Tags []string }`
  - `func BuildNote(p Params) string`
  - `type CaptureConfig struct { VaultDir, Inbox, Resources string }`
  - `type Request struct { Body, Title, Source, MOCName string; Tags []string; FetchTitle bool }`
  - `type Result struct { Path, Rel, Title, MOCLinked, MOCWarning string }`
  - `func Capture(ctx context.Context, cfg CaptureConfig, req Request, fetcher TitleFetcher, now time.Time) (Result, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/capture/filename_test.go
package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// CONTRACT(#50): a collision probes " (2)", " (3)", ... until free.
func TestNextAvailablePath(t *testing.T) {
	dir := t.TempDir()
	path, name, err := NextAvailablePath(dir, "2026-07-15 0830 Foo", ".md")
	if err != nil {
		t.Fatal(err)
	}
	if name != "2026-07-15 0830 Foo.md" {
		t.Errorf("name = %q", name)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	path2, name2, err := NextAvailablePath(dir, "2026-07-15 0830 Foo", ".md")
	if err != nil {
		t.Fatal(err)
	}
	if name2 != "2026-07-15 0830 Foo (2).md" {
		t.Errorf("name2 = %q", name2)
	}
	if err := os.WriteFile(path2, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, name3, err := NextAvailablePath(dir, "2026-07-15 0830 Foo", ".md")
	if err != nil {
		t.Fatal(err)
	}
	if name3 != "2026-07-15 0830 Foo (3).md" {
		t.Errorf("name3 = %q", name3)
	}
}

// DECIDE(#51): uniqueness is case-insensitive against a real directory
// listing, filesystem-independent.
func TestNextAvailablePathCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "2026-07-15 0830 Foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, name, err := NextAvailablePath(dir, "2026-07-15 0830 foo", ".md")
	if err != nil {
		t.Fatal(err)
	}
	if name != "2026-07-15 0830 foo (2).md" {
		t.Errorf("name = %q, want a case-insensitive collision suffix", name)
	}
}

// DECIDE(#137): the collision probe is bounded and errors instead of
// looping forever.
func TestNextAvailablePathBoundedProbe(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Stem.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for n := 2; n <= maxCollisionAttempts; n++ {
		name := fmt.Sprintf("Stem (%d).md", n)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := NextAvailablePath(dir, "Stem", ".md"); err == nil {
		t.Fatal("expected an error once the probe bound is exhausted")
	}
}
```

```go
// internal/capture/note_test.go
package capture

import "testing"

// Golden: exact captured-note bytes under an injected clock (design spec
// §Testing strategy tier 2).
func TestBuildNoteGolden(t *testing.T) {
	p := Params{
		Title:   "Test Capture",
		Body:    "first line content\nsecond line",
		Tags:    []string{"a", "b"},
		Source:  "cli",
		Created: "2026-07-15",
	}
	want := "---\ntype: inbox\ncreated: 2026-07-15\nmodified: 2026-07-15\nsource: cli\ntags: [a, b]\n---\n\n# Test Capture\n\nfirst line content\nsecond line\n"
	if got := BuildNote(p); got != want {
		t.Errorf("BuildNote =\n%q\nwant\n%q", got, want)
	}
}

// CONTRACT(#54): a MOC-linked capture gets a footer.
func TestBuildNoteWithMOC(t *testing.T) {
	p := Params{Title: "Linked", Body: "body text", Source: "cli", MOCName: "MOC Music", Created: "2026-07-15"}
	want := "---\ntype: inbox\ncreated: 2026-07-15\nmodified: 2026-07-15\nsource: cli\nmoc: [[MOC Music]]\n---\n\n# Linked\n\nbody text\n\n---\n*Added to [[MOC Music]] on 2026-07-15*\n"
	if got := BuildNote(p); got != want {
		t.Errorf("BuildNote =\n%q\nwant\n%q", got, want)
	}
}

// CONTRACT(#53): the body's first line is dropped when it duplicates the
// title, with or without a leading heading marker; leading blank lines are
// also dropped.
func TestBodyWithoutTitleEchoDropsMatchingFirstLine(t *testing.T) {
	cases := []struct{ body, title, want string }{
		{"Test Capture\nmore content", "Test Capture", "more content"},
		{"# Test Capture\nmore content", "Test Capture", "more content"},
		{"\n\nTest Capture\nmore content", "Test Capture", "more content"},
		{"Something Else\nmore content", "Test Capture", "Something Else\nmore content"},
	}
	for _, c := range cases {
		if got := bodyWithoutTitleEcho(c.body, c.title); got != c.want {
			t.Errorf("bodyWithoutTitleEcho(%q, %q) = %q, want %q", c.body, c.title, got, c.want)
		}
	}
}
```

```go
// internal/capture/capture_test.go
package capture

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type stubFetcher struct {
	title string
	err   error
}

func (f stubFetcher) FetchTitle(ctx context.Context, url string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.title, nil
}

// Temp-vault integration test (design spec §Testing strategy tier 3):
// capture -> file content, MOC entry appended.
func TestCaptureWritesNoteAndUpdatesMOC(t *testing.T) {
	vaultDir := t.TempDir()
	inbox := filepath.Join(vaultDir, "00-Inbox")
	resources := filepath.Join(vaultDir, "03-Resources")
	if err := os.MkdirAll(resources, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, "MOC Music.md"), []byte("# MOC Music\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := CaptureConfig{VaultDir: vaultDir, Inbox: "00-Inbox", Resources: "03-Resources"}
	req := Request{Body: "some idea", Title: "New Idea", Source: "cli", MOCName: "Music"}
	now := time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC)
	result, err := Capture(context.Background(), cfg, req, stubFetcher{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Rel != "00-Inbox/2026-07-15 0830 New Idea.md" {
		t.Errorf("Rel = %q", result.Rel)
	}
	if result.MOCLinked != "MOC Music" {
		t.Errorf("MOCLinked = %q", result.MOCLinked)
	}
	if _, err := os.Stat(filepath.Join(inbox, "2026-07-15 0830 New Idea.md")); err != nil {
		t.Errorf("note missing: %v", err)
	}
	moc, err := os.ReadFile(filepath.Join(resources, "MOC Music.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(moc), "[[New Idea]]") {
		t.Errorf("MOC not updated: %s", moc)
	}
}

// CONTRACT(#46): a bare-URL first line uses the fetched title.
func TestCaptureBareURLUsesFetchedTitle(t *testing.T) {
	vaultDir := t.TempDir()
	cfg := CaptureConfig{VaultDir: vaultDir, Inbox: "00-Inbox"}
	req := Request{Body: "https://example.com/article", FetchTitle: true}
	now := time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC)
	result, err := Capture(context.Background(), cfg, req, stubFetcher{title: "A Great Article"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "A Great Article" {
		t.Errorf("Title = %q", result.Title)
	}
}

// CONTRACT(#30): a failed fetch falls back to the slugified URL.
func TestCaptureBareURLFetchFailureFallsBack(t *testing.T) {
	vaultDir := t.TempDir()
	cfg := CaptureConfig{VaultDir: vaultDir, Inbox: "00-Inbox"}
	req := Request{Body: "https://example.com/article", FetchTitle: true}
	now := time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC)
	result, err := Capture(context.Background(), cfg, req, stubFetcher{err: errors.New("network down")}, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "https example.com article" {
		t.Errorf("Title = %q, want slugified URL fallback", result.Title)
	}
}

// BUG(fixed)(#128): the note is written via vault.WriteNoteAtomic, not a
// raw redirect — no leftover temp file after a successful capture.
func TestCaptureUsesAtomicWrite(t *testing.T) {
	vaultDir := t.TempDir()
	cfg := CaptureConfig{VaultDir: vaultDir, Inbox: "00-Inbox"}
	req := Request{Body: "content", Title: "Atomic Test"}
	if _, err := Capture(context.Background(), cfg, req, stubFetcher{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(vaultDir, "00-Inbox"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ov-tmp-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

// CONTRACT(#47): --moc requires exact resolution; a miss aborts the
// capture (and the inbox note is never written — the MOC is resolved
// before any write happens).
func TestCaptureUnknownMOCFails(t *testing.T) {
	vaultDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vaultDir, "03-Resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := CaptureConfig{VaultDir: vaultDir, Inbox: "00-Inbox", Resources: "03-Resources"}
	req := Request{Body: "x", Title: "X", MOCName: "Nonexistent"}
	if _, err := Capture(context.Background(), cfg, req, stubFetcher{}, time.Now()); err == nil {
		t.Fatal("expected an error for an unresolvable MOC")
	}
	if _, err := os.Stat(filepath.Join(vaultDir, "00-Inbox")); err == nil {
		t.Error("the inbox note must not be written when the MOC is unresolvable")
	}
}

// CONTRACT(#45): whitespace-only body is refused.
func TestCaptureEmptyBodyRefused(t *testing.T) {
	vaultDir := t.TempDir()
	cfg := CaptureConfig{VaultDir: vaultDir, Inbox: "00-Inbox"}
	if _, err := Capture(context.Background(), cfg, Request{Body: "   \n  "}, stubFetcher{}, time.Now()); err == nil {
		t.Fatal("expected empty-body refusal")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capture/ -run 'TestNextAvailablePath|TestBuildNote|TestBodyWithoutTitleEcho|TestCapture' -v`
Expected: FAIL — `undefined: NextAvailablePath`, `undefined: BuildNote`, `undefined: bodyWithoutTitleEcho`, `undefined: CaptureConfig`, `undefined: Capture`

- [ ] **Step 3: Write the implementation**

```go
// internal/capture/filename.go
package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// maxCollisionAttempts bounds the same-minute collision suffix probe
// (behavior inventory row #137: v1's while-loop was unbounded).
const maxCollisionAttempts = 1000

// NextAvailablePath returns an unused path for stem (without extension) in
// dir, probing " (2)", " (3)", ... on a collision (row #50), decided
// case-insensitively against a REAL directory listing rather than a
// filesystem exists() check (row #51 — v2's filename policy is
// case-insensitive and filesystem-independent, unlike v1's `-e` test which
// inherited whatever case sensitivity the host filesystem happened to
// have).
func NextAvailablePath(dir, stem, ext string) (path, name string, err error) {
	existing, statErr := lowerNames(dir)
	if statErr != nil && !os.IsNotExist(statErr) {
		return "", "", statErr
	}
	base := norm.NFC.String(stem)
	name = base + ext
	if !existing[strings.ToLower(name)] {
		return filepath.Join(dir, name), name, nil
	}
	for n := 2; n <= maxCollisionAttempts; n++ {
		name = fmt.Sprintf("%s (%d)%s", base, n, ext)
		if !existing[strings.ToLower(name)] {
			return filepath.Join(dir, name), name, nil
		}
	}
	return "", "", fmt.Errorf("could not find an available name for %q after %d attempts", stem, maxCollisionAttempts)
}

func lowerNames(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return map[string]bool{}, err
	}
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[strings.ToLower(e.Name())] = true
	}
	return names, nil
}
```

```go
// internal/capture/note.go
package capture

import (
	"fmt"
	"regexp"
	"strings"
)

// Params holds everything needed to assemble a captured note's content.
type Params struct {
	Title   string
	Body    string
	Tags    []string
	Source  string
	MOCName string
	Created string // YYYY-MM-DD
}

var headingStripRe = regexp.MustCompile(`^\s*#+\s*`)

// BuildNote assembles frontmatter + heading + body exactly as v1
// capture_note (behavior inventory row #52: type/created/modified/source/
// [tags]/[moc]; row #53: "# Title" heading, first body line dropped iff it
// duplicates the title modulo leading #s; row #54: MOC footer when linked;
// row #128: the caller writes this content via vault.WriteNoteAtomic,
// never a raw redirect).
func BuildNote(p Params) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("type: inbox\n")
	fmt.Fprintf(&b, "created: %s\n", p.Created)
	fmt.Fprintf(&b, "modified: %s\n", p.Created)
	fmt.Fprintf(&b, "source: %s\n", p.Source)
	if len(p.Tags) > 0 {
		fmt.Fprintf(&b, "tags: [%s]\n", strings.Join(p.Tags, ", "))
	}
	if p.MOCName != "" {
		fmt.Fprintf(&b, "moc: [[%s]]\n", p.MOCName)
	}
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", p.Title)
	b.WriteString(bodyWithoutTitleEcho(p.Body, p.Title))
	b.WriteString("\n")
	if p.MOCName != "" {
		fmt.Fprintf(&b, "\n---\n*Added to [[%s]] on %s*\n", p.MOCName, p.Created)
	}
	return b.String()
}

// bodyWithoutTitleEcho drops leading blank lines and the body's first
// non-blank line iff it equals title after stripping leading markdown
// heading markers (row #53); every other line is kept verbatim.
func bodyWithoutTitleEcho(body, title string) string {
	lines := strings.Split(body, "\n")
	decided := false
	var out []string
	for _, line := range lines {
		if !decided {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if headingStripRe.ReplaceAllString(line, "") == title {
				decided = true
				continue
			}
			decided = true
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
```

```go
// internal/capture/capture.go
package capture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// CaptureConfig is the narrow subset of ov config this package needs — kept
// separate from internal/config.Config so this package stays a thin,
// dependency-light core verb (design spec's "stateless verbs" principle).
type CaptureConfig struct {
	VaultDir  string
	Inbox     string
	Resources string
}

// Request is the frontend-agnostic capture input — same shape whether it
// came from CLI flags or a web form.
type Request struct {
	Body       string
	Title      string // explicit; empty triggers auto-derivation
	Tags       []string
	Source     string
	MOCName    string // explicit MOC name; "" = no MOC link
	FetchTitle bool   // whether to attempt a bare-URL title fetch (CLI: always true, row #46; web: opt-in checkbox, row #135)
}

// Result reports what Capture did, for the frontend to render.
type Result struct {
	Path       string // absolute path of the written note
	Rel        string // vault-relative path
	Title      string // resolved title (post-slugify)
	MOCLinked  string // MOC name if linked, else ""
	MOCWarning string // non-fatal MOC update failure message, row #55
}

var trailingWordRe = regexp.MustCompile(`\s+\S*$`)

// Capture is the core capture verb: derive title, resolve MOC, stamp a
// filename, write the note via WriteNoteAtomic, and best-effort append a
// MOC entry. Never aborts on a MOC-update failure (row #55) — the note is
// already safely on disk by the time the MOC is touched. Both cmd/ov
// capture and internal/web's capture handler call this same function.
func Capture(ctx context.Context, cfg CaptureConfig, req Request, fetcher TitleFetcher, now time.Time) (Result, error) {
	body := strings.TrimRight(req.Body, " \t\n\r")
	if body == "" {
		return Result{}, errors.New("empty body, refusing to capture")
	}

	firstLine := firstNonEmptyLine(body)
	urlTitle := ""
	if req.FetchTitle && IsBareURL(firstLine) && fetcher != nil {
		if t, err := fetcher.FetchTitle(ctx, strings.TrimSpace(firstLine)); err == nil {
			urlTitle = t
		}
	}

	title := req.Title
	if title == "" {
		if urlTitle != "" {
			title = urlTitle
		} else {
			title = firstLine
		}
	}
	title = vault.Slugify(title, 60)

	var moc *vault.MOC
	if req.MOCName != "" {
		m, err := vault.FindMOCByName(cfg.VaultDir, cfg.Resources, req.MOCName)
		if err != nil {
			return Result{}, fmt.Errorf("MOC not found: %s", req.MOCName)
		}
		moc = m
	}

	snippet := firstNonEmptyLine(body)
	if IsBareURL(firstLine) && urlTitle != "" {
		snippet = urlTitle
	}
	snippet = truncateSnippet(snippet, IsBareURL(firstLine))

	inboxDir := filepath.Join(cfg.VaultDir, cfg.Inbox)
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		return Result{}, err
	}
	stamp := now.Format("2006-01-02 1504")
	stem := stamp + " " + title
	path, _, err := NextAvailablePath(inboxDir, stem, ".md")
	if err != nil {
		return Result{}, err
	}

	mocName := ""
	if moc != nil {
		mocName = moc.Name
	}
	content := BuildNote(Params{
		Title:   title,
		Body:    body,
		Tags:    req.Tags,
		Source:  req.Source,
		MOCName: mocName,
		Created: now.Format("2006-01-02"),
	})
	if err := vault.WriteNoteAtomic(path, []byte(content), ""); err != nil {
		return Result{}, err
	}

	rel, _ := filepath.Rel(cfg.VaultDir, path)
	result := Result{Path: path, Rel: filepath.ToSlash(rel), Title: title}

	if moc != nil {
		result.MOCLinked = moc.Name
		if err := appendMOCConditional(moc.Path, title, snippet); err != nil {
			result.MOCWarning = fmt.Sprintf("captured, but failed to update MOC %s: %v", moc.Name, err)
		}
	}
	return result, nil
}

// appendMOCConditional re-reads the MOC (hash-conditional per design spec
// §core contracts) immediately before writing, so a concurrent Obsidian
// Sync edit surfaces as a refusal rather than a silent clobber.
func appendMOCConditional(mocPath, title, snippet string) error {
	content, hash, err := vault.ReadNote(mocPath)
	if err != nil {
		return err
	}
	newContent := vault.AppendMOCEntry(content, title, snippet)
	return vault.WriteNoteAtomic(mocPath, []byte(newContent), hash)
}

func firstNonEmptyLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

// truncateSnippet mirrors v1: >60 chars gets word-boundary truncation plus
// "..."; bare URLs are never truncated mid-path (row #48).
func truncateSnippet(s string, isURL bool) string {
	if len([]rune(s)) <= 60 || isURL {
		return s
	}
	r := []rune(s)[:60]
	truncated := trailingWordRe.ReplaceAllString(string(r), "")
	return strings.TrimSpace(truncated) + "..."
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capture/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/capture/filename.go internal/capture/filename_test.go \
  internal/capture/note.go internal/capture/note_test.go \
  internal/capture/capture.go internal/capture/capture_test.go
git commit -m "Add internal/capture: filename stamping, note assembly, Capture orchestration verb"
```

---

### Task 4: `internal/tui` — folder picker, MOC picker, delete confirm

The bubbletea v2 pickers and the huh delete-confirmation, wired for both `cmd/ov capture` and `cmd/ov triage`. Selection/cursor logic is a pure, tty-free `state.handleKey` function; only the thin `RunFolderPicker`/`RunMOCPicker` wrappers touch `tea.NewProgram`.

**Files:**
- Create: `internal/tui/folderpicker.go`
- Create: `internal/tui/folderpicker_test.go`
- Create: `internal/tui/mocpicker.go`
- Create: `internal/tui/mocpicker_test.go`
- Create: `internal/tui/confirm.go`
- Create: `internal/tui/run.go`

**Interfaces:**
- Consumes: `vault.MOC` (phase 1)
- Produces:
  - `package tui` (new package, `github.com/arosenkranz/obsidian-vault-tools/internal/tui`)
  - `var ErrCancelled error`
  - `func RunFolderPicker(folders []string) (string, error)`
  - `func RunMOCPicker(mocs []vault.MOC) (string, error)`
  - `func Confirm(title string) (bool, error)`

- [ ] **Step 1: Add the new dependencies**

```bash
go get charm.land/bubbletea/v2@v2.0.8 charm.land/lipgloss/v2@v2.0.5 github.com/charmbracelet/huh@v1.0.0
go mod tidy
```

Verify: `go list -m all | grep -E 'bubbletea|lipgloss|huh'` shows `charm.land/bubbletea/v2 v2.0.8`, `charm.land/lipgloss/v2 v2.0.5`, `github.com/charmbracelet/huh v1.0.0` (huh pulls its own `github.com/charmbracelet/bubbletea v1.3.6`/`lipgloss v1.1.0` transitively for its internal forms — this is expected, huh is a self-contained toolkit built on bubbletea v1; it coexists fine with our direct v2 usage since v1 and v2 are different module paths).

- [ ] **Step 2: Write the failing tests**

```go
// internal/tui/folderpicker_test.go
package tui

import "testing"

func TestFolderPickerBrowseNavigation(t *testing.T) {
	s := folderPickerState{folders: []string{"a", "b", "c"}}
	s = s.handleKey("down")
	if s.cursor != 1 {
		t.Fatalf("cursor = %d", s.cursor)
	}
	s = s.handleKey("down")
	if s.cursor != 2 {
		t.Fatalf("cursor = %d", s.cursor)
	}
	s = s.handleKey("down") // clamped at last index
	if s.cursor != 2 {
		t.Fatalf("cursor should clamp at 2, got %d", s.cursor)
	}
	s = s.handleKey("up")
	if s.cursor != 1 {
		t.Fatalf("cursor = %d", s.cursor)
	}
}

func TestFolderPickerSelect(t *testing.T) {
	s := folderPickerState{folders: []string{"a", "b"}, cursor: 1}
	s = s.handleKey("enter")
	if !s.done || s.result != "b" {
		t.Fatalf("state = %+v", s)
	}
}

// CONTRACT(#39): cancel is always available and never fails the caller.
func TestFolderPickerCancel(t *testing.T) {
	s := folderPickerState{folders: []string{"a"}}
	s = s.handleKey("q")
	if !s.cancelled {
		t.Fatal("expected cancelled")
	}
}

// CONTRACT(#37): typing a new path and pressing enter selects it verbatim.
func TestFolderPickerTypeNewPath(t *testing.T) {
	s := folderPickerState{folders: []string{"a"}}
	s = s.handleKey("n")
	if s.mode != modeTyping {
		t.Fatal("expected typing mode")
	}
	for _, ch := range "02-Areas/New" {
		s = s.handleKey(string(ch))
	}
	s = s.handleKey("enter")
	if !s.done || s.result != "02-Areas/New" {
		t.Fatalf("state = %+v", s)
	}
}

func TestFolderPickerTypeNewPathBackspace(t *testing.T) {
	s := folderPickerState{folders: []string{"a"}}
	s = s.handleKey("n")
	s = s.handleKey("x")
	s = s.handleKey("y")
	s = s.handleKey("backspace")
	if s.inputVal != "x" {
		t.Fatalf("inputVal = %q", s.inputVal)
	}
}

func TestFolderPickerTypeEscBackToBrowse(t *testing.T) {
	s := folderPickerState{folders: []string{"a"}}
	s = s.handleKey("n")
	s = s.handleKey("x")
	s = s.handleKey("esc")
	if s.mode != modeBrowse || s.inputVal != "" {
		t.Fatalf("state = %+v", s)
	}
}
```

```go
// internal/tui/mocpicker_test.go
package tui

import (
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

func TestMOCPickerSelect(t *testing.T) {
	mocs := []vault.MOC{{Name: "MOC Music"}, {Name: "MOC Code"}}
	s := mocPickerState{mocs: mocs}
	s = s.handleKey("down")
	s = s.handleKey("enter")
	if !s.done || s.result != "MOC Code" {
		t.Fatalf("state = %+v", s)
	}
}

// CONTRACT(#39): MOC selection is always optional — cancelling never fails.
func TestMOCPickerCancel(t *testing.T) {
	s := mocPickerState{mocs: []vault.MOC{{Name: "MOC Music"}}}
	s = s.handleKey("esc")
	if !s.cancelled {
		t.Fatal("expected cancelled")
	}
}

func TestMOCPickerEmptyListCancelsOnEnter(t *testing.T) {
	s := mocPickerState{}
	s = s.handleKey("enter")
	if !s.cancelled {
		t.Fatal("expected cancelled with an empty list")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/tui/ -v`
Expected: FAIL — `no Go files in .../internal/tui` (package doesn't exist yet)

- [ ] **Step 4: Write the implementation**

```go
// internal/tui/folderpicker.go
package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type pickerMode int

const (
	modeBrowse pickerMode = iota
	modeTyping
)

// folderPickerState is the pure, tty-free core of the folder picker: it
// consumes bubbletea's msg.String() key form and returns the next state.
// Selection ("done"+result) and cancellation are terminal; the bubbletea
// Update method (below) is a two-line adapter that quits the program once
// either is set.
type folderPickerState struct {
	folders   []string
	cursor    int
	mode      pickerMode
	inputVal  string
	result    string
	cancelled bool
	done      bool
}

// handleKey applies one key (bubbletea's msg.String() form: "up", "down",
// "enter", "q", "n", "esc", "backspace", or a single printed rune) to
// state. Browse mode: j/k or arrows move the cursor, enter selects the
// highlighted folder, n switches to typing a brand-new path (behavior
// inventory row #37), q/esc/ctrl+c cancels (row #39: cancel never fails
// the caller). Typing mode: printable runes append, backspace deletes,
// enter confirms the typed path, esc returns to browse.
func (s folderPickerState) handleKey(key string) folderPickerState {
	if s.mode == modeTyping {
		switch key {
		case "esc":
			s.mode = modeBrowse
			s.inputVal = ""
		case "enter":
			if path := strings.TrimSpace(s.inputVal); path != "" {
				s.result = path
				s.done = true
			}
		case "backspace":
			if s.inputVal != "" {
				s.inputVal = s.inputVal[:len(s.inputVal)-1]
			}
		default:
			if len([]rune(key)) == 1 {
				s.inputVal += key
			}
		}
		return s
	}
	switch key {
	case "q", "esc", "ctrl+c":
		s.cancelled = true
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.folders)-1 {
			s.cursor++
		}
	case "n":
		s.mode = modeTyping
	case "enter":
		if len(s.folders) > 0 {
			s.result = s.folders[s.cursor]
			s.done = true
		}
	}
	return s
}

type folderPickerModel struct {
	state folderPickerState
}

func newFolderPickerModel(folders []string) folderPickerModel {
	return folderPickerModel{state: folderPickerState{folders: folders}}
}

func (m folderPickerModel) Init() tea.Cmd { return nil }

func (m folderPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		m.state = m.state.handleKey(keyMsg.String())
		if m.state.done || m.state.cancelled {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m folderPickerModel) View() tea.View {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Choose destination folder ([n] type a new path, q to cancel):"))
	b.WriteString("\n\n")
	if m.state.mode == modeTyping {
		fmt.Fprintf(&b, "New folder: %s\n", m.state.inputVal)
	} else {
		for i, f := range m.state.folders {
			if i == m.state.cursor {
				fmt.Fprintf(&b, "%s %s\n", cursorStyle.Render(">"), f)
			} else {
				fmt.Fprintf(&b, "  %s\n", f)
			}
		}
	}
	return tea.NewView(b.String())
}
```

```go
// internal/tui/mocpicker.go
package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// mocPickerState is the pure, tty-free core of the MOC picker. q/esc
// cancels (behavior inventory row #39: MOC selection is always optional,
// and cancelling never fails the calling capture) — the new cursor-list
// paradigm replaces v1 fzf's specific Enter/ESC affordances (row #38
// DECIDE already covers "v2 ships its own TUI picker") with Enter=select,
// q/esc=skip.
type mocPickerState struct {
	mocs      []vault.MOC
	cursor    int
	result    string
	cancelled bool
	done      bool
}

func (s mocPickerState) handleKey(key string) mocPickerState {
	switch key {
	case "q", "esc", "ctrl+c":
		s.cancelled = true
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.mocs)-1 {
			s.cursor++
		}
	case "enter":
		if len(s.mocs) > 0 {
			s.result = s.mocs[s.cursor].Name
			s.done = true
		} else {
			s.cancelled = true
		}
	}
	return s
}

type mocPickerModel struct{ state mocPickerState }

func newMOCPickerModel(mocs []vault.MOC) mocPickerModel {
	return mocPickerModel{state: mocPickerState{mocs: mocs}}
}

func (m mocPickerModel) Init() tea.Cmd { return nil }

func (m mocPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		m.state = m.state.handleKey(keyMsg.String())
		if m.state.done || m.state.cancelled {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m mocPickerModel) View() tea.View {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Choose MOC to link (q to cancel/skip):"))
	b.WriteString("\n\n")
	for i, moc := range m.state.mocs {
		line := fmt.Sprintf("%s (%d items)", moc.Name, moc.ItemCount)
		if i == m.state.cursor {
			fmt.Fprintf(&b, "%s %s\n", cursorStyle.Render(">"), line)
		} else {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	return tea.NewView(b.String())
}
```

```go
// internal/tui/confirm.go
package tui

import "github.com/charmbracelet/huh"

// Confirm asks a yes/no question on the controlling tty (used by triage
// delete — behavior inventory row #132: v2 requires explicit confirmation
// on every triage path, unlike v1 bash's confirmless delete).
func Confirm(title string) (bool, error) {
	var ok bool
	err := huh.NewConfirm().
		Title(title).
		Affirmative("Delete").
		Negative("Cancel").
		Value(&ok).
		Run()
	return ok, err
}
```

```go
// internal/tui/run.go
package tui

import (
	"errors"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// ErrCancelled is returned by RunFolderPicker/RunMOCPicker when the user
// quits without choosing.
var ErrCancelled = errors.New("cancelled")

var (
	headingStyle = lipgloss.NewStyle().Bold(true)
	cursorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
)

// RunFolderPicker launches an interactive folder picker over folders
// (vault-relative paths, e.g. from vault.ListAllFolders). Renders to
// os.Stderr, never stdout. Bubbletea v2 always opens the controlling tty
// for its own input regardless of the process's stdin redirection, which
// is what keeps a piped capture body safe from the picker (design spec
// §CLI/TUI tty discipline).
func RunFolderPicker(folders []string) (string, error) {
	p := tea.NewProgram(newFolderPickerModel(folders), tea.WithOutput(os.Stderr))
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	m := final.(folderPickerModel)
	if m.state.cancelled {
		return "", ErrCancelled
	}
	return m.state.result, nil
}

// RunMOCPicker launches an interactive MOC picker. See RunFolderPicker for
// the tty/output contract.
func RunMOCPicker(mocs []vault.MOC) (string, error) {
	p := tea.NewProgram(newMOCPickerModel(mocs), tea.WithOutput(os.Stderr))
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	m := final.(mocPickerModel)
	if m.state.cancelled {
		return "", ErrCancelled
	}
	return m.state.result, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -v`
Expected: PASS (only the pure `handleKey` tests run in CI; `RunFolderPicker`/`RunMOCPicker` are exercised end-to-end only by Task 5's CLI smoke test against the real binary)

Run: `go build ./...`
Expected: builds clean (confirms `run.go`'s `tea.NewProgram`/`tea.WithOutput`/`lipgloss` usage compiles against the pinned versions)

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/tui/folderpicker.go internal/tui/folderpicker_test.go \
  internal/tui/mocpicker.go internal/tui/mocpicker_test.go \
  internal/tui/confirm.go internal/tui/run.go
git commit -m "Add internal/tui: folder picker, MOC picker, delete confirm (bubbletea v2 + huh)"
```

---

### Task 5: `cmd/ov capture` command

The frozen CLI surface, wired to `internal/capture` and `internal/tui`, with tty-safe MOC picking and a real-binary CI smoke test for piped stdin.

**Files:**
- Create: `cmd/ov/capture.go`
- Create: `cmd/ov/capture_test.go`
- Modify: `cmd/ov/root.go` (register `newCaptureCmd`)

**Interfaces:**
- Consumes: `resolveConfig` (phase 1, `cmd/ov/common.go`); `capture.Request`, `capture.Result`, `capture.CaptureConfig`, `capture.Capture`, `capture.TitleFetcher`, `capture.NewHTTPTitleFetcher` (Task 3); `tui.RunMOCPicker`, `tui.ErrCancelled` (Task 4); `vault.MOCs` (phase 1)
- Produces:
  - `func newCaptureCmd() *cobra.Command` — flags: `--title`, `--tags`, `--source`, `--moc`, `--vault` (frozen: `--title`, `--tags`, `--source`, `--moc` per design spec §Compatibility contract item 2)
  - `type captureFlags struct { title, tags, source, moc string }`
  - `type captureDeps struct { stdinPiped func() bool; interactive func() bool; pickMOC func([]vault.MOC) (string, error); fetcher capture.TitleFetcher; now func() time.Time }`
  - `func runCapture(cfg *config.Config, flags captureFlags, args []string, in io.Reader, out, errw io.Writer, deps captureDeps) error` — the testable core (mirrors Task 6's `runTriage` pattern)

- [ ] **Step 1: Write the failing tests**

```go
// cmd/ov/capture_test.go
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

type fakeFetcher struct{}

func (fakeFetcher) FetchTitle(ctx context.Context, url string) (string, error) {
	return "", errors.New("no network in tests")
}

// CONTRACT(#43,#44): positional args join into the body and win over stdin.
func TestCapturePositionalBody(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	deps := captureDeps{
		stdinPiped:  func() bool { t.Fatal("must not read stdin when a positional body is given"); return false },
		interactive: func() bool { return false },
		fetcher:     fakeFetcher{},
		now:         func() time.Time { return time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC) },
	}
	err = runCapture(cfg, captureFlags{title: "My Title", source: "cli"}, []string{"hello", "world"}, strings.NewReader(""), &out, &errBuf, deps)
	if err != nil {
		t.Fatalf("%v\n%s", err, errBuf.String())
	}
	if strings.TrimSpace(out.String()) != "00-Inbox/2026-07-15 0830 My Title.md" {
		t.Errorf("stdout = %q", out.String())
	}
	content, rerr := os.ReadFile(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0830 My Title.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(content), "hello world") {
		t.Errorf("body missing: %s", content)
	}
}

// CONTRACT(#10,#44): body from stdin when piped.
func TestCaptureStdinBody(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := captureDeps{
		stdinPiped:  func() bool { return true },
		interactive: func() bool { return false },
		fetcher:     fakeFetcher{},
		now:         func() time.Time { return time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC) },
	}
	err := runCapture(cfg, captureFlags{title: "Stdin Note", source: "cli"}, nil, strings.NewReader("from stdin"), &out, &errBuf, deps)
	if err != nil {
		t.Fatalf("%v\n%s", err, errBuf.String())
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0830 Stdin Note.md")); statErr != nil {
		t.Errorf("note missing: %v", statErr)
	}
}

// CONTRACT(#44): neither positional nor piped stdin -> error.
func TestCaptureNoBodyErrors(t *testing.T) {
	newVaultFixture(t)
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := captureDeps{stdinPiped: func() bool { return false }, interactive: func() bool { return false }}
	err := runCapture(cfg, captureFlags{}, nil, strings.NewReader(""), &out, &errBuf, deps)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(errBuf.String(), "No content provided") {
		t.Errorf("stderr = %q", errBuf.String())
	}
}

// BUG(fixed)(#136): the MOC picker never launches outside a real
// interactive terminal, even when MOCs exist.
func TestCaptureSkipsPickerNonInteractive(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.MkdirAll(filepath.Join(vaultDir, "03-Resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"), []byte("# MOC Music\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := captureDeps{
		stdinPiped:  func() bool { return true },
		interactive: func() bool { return false },
		pickMOC:     func([]vault.MOC) (string, error) { t.Fatal("picker must not launch outside a tty"); return "", nil },
		fetcher:     fakeFetcher{},
		now:         func() time.Time { return time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC) },
	}
	err := runCapture(cfg, captureFlags{title: "No Picker"}, nil, strings.NewReader("body"), &out, &errBuf, deps)
	if err != nil {
		t.Fatalf("%v\n%s", err, errBuf.String())
	}
}

// CONTRACT(#47,#54): --moc links the note and updates the MOC.
func TestCaptureMOCFlagLinksAndUpdatesMOC(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.MkdirAll(filepath.Join(vaultDir, "03-Resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"), []byte("# MOC Music\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := captureDeps{stdinPiped: func() bool { return true }, interactive: func() bool { return false }, fetcher: fakeFetcher{}, now: func() time.Time { return time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC) }}
	err := runCapture(cfg, captureFlags{title: "Linked Note", moc: "Music"}, nil, strings.NewReader("body text"), &out, &errBuf, deps)
	if err != nil {
		t.Fatalf("%v\n%s", err, errBuf.String())
	}
	moc, rerr := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(moc), "[[Linked Note]]") {
		t.Errorf("MOC not updated: %s", moc)
	}
	if !strings.Contains(errBuf.String(), "Added to [[MOC Music]]") {
		t.Errorf("stderr = %q", errBuf.String())
	}
}

// CONTRACT(#47): --moc requires exact resolution; a miss aborts.
func TestCaptureUnknownMOCErrors(t *testing.T) {
	newVaultFixture(t)
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := captureDeps{stdinPiped: func() bool { return true }, interactive: func() bool { return false }, fetcher: fakeFetcher{}, now: time.Now}
	err := runCapture(cfg, captureFlags{title: "X", moc: "Nonexistent"}, nil, strings.NewReader("body"), &out, &errBuf, deps)
	if err == nil {
		t.Fatal("expected an error for an unresolvable --moc")
	}
}

// CI smoke test named by the design spec (§CLI/TUI): the real binary, real
// piped stdin, verifying tty discipline end to end (row #123 applied to
// capture — stdout is exactly one machine-readable line).
func TestCaptureCLISmoke(t *testing.T) {
	newVaultFixture(t)
	bin := buildOV2(t)
	cmd := exec.Command(bin, "capture", "--title", "x")
	cmd.Stdin = strings.NewReader("hello from stdin\n")
	cmd.Env = os.Environ()
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("capture failed: %v\nstderr: %s", err, errOut.String())
	}
	stdout := strings.TrimRight(out.String(), "\n")
	if strings.Count(stdout, "\n") != 0 {
		t.Errorf("stdout must be exactly one line, got %q", stdout)
	}
	if !strings.HasPrefix(stdout, "00-Inbox/") || !strings.Contains(stdout, "x.md") {
		t.Errorf("stdout = %q, want a vault-relative inbox path for title x", stdout)
	}
}

func buildOV2(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ov2")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build ov2: %v\n%s", err, out)
	}
	return bin
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ov/ -run TestCapture -v`
Expected: FAIL — `undefined: runCapture`, `undefined: captureFlags`, `undefined: captureDeps`

- [ ] **Step 3: Write the implementation**

```go
// cmd/ov/capture.go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/capture"
	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/arosenkranz/obsidian-vault-tools/internal/tui"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

type captureFlags struct {
	title, tags, source, moc string
}

// captureDeps injects every tty-dependent or side-effecting collaborator so
// runCapture is fully testable without a real terminal or network.
type captureDeps struct {
	stdinPiped  func() bool
	interactive func() bool
	pickMOC     func([]vault.MOC) (string, error)
	fetcher     capture.TitleFetcher
	now         func() time.Time
}

func newCaptureCmd() *cobra.Command {
	var vaultFlag string
	var flags captureFlags
	cmd := &cobra.Command{
		Use:   "capture [text]",
		Short: "Quick-dump a note into the inbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			deps := captureDeps{
				stdinPiped:  stdinPiped,
				interactive: interactiveTTY,
				pickMOC:     tui.RunMOCPicker,
				fetcher:     capture.NewHTTPTitleFetcher(),
				now:         time.Now,
			}
			return runCapture(cfg, flags, args, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps)
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	cmd.Flags().StringVar(&flags.title, "title", "", "explicit title (default: derived from first line)")
	cmd.Flags().StringVar(&flags.tags, "tags", "", "comma-separated tags")
	cmd.Flags().StringVar(&flags.source, "source", "cli", "source: cli | web | llm")
	cmd.Flags().StringVar(&flags.moc, "moc", "", "link note to a MOC by name (exact match, non-interactive)")
	return cmd
}

// runCapture is the testable core of ov2 capture: resolve the body
// (positional wins over piped stdin, row #44), resolve the MOC (flag, or an
// interactive picker gated on a real tty, row #136), and delegate to
// capture.Capture. Writes exactly one line to out (the captured note's
// vault-relative path, row #123 discipline extended to a write command);
// everything else goes to errw.
func runCapture(cfg *config.Config, flags captureFlags, args []string, in io.Reader, out, errw io.Writer, deps captureDeps) error {
	var body string
	if len(args) > 0 {
		body = strings.Join(args, " ")
	} else if deps.stdinPiped() {
		b, err := io.ReadAll(in)
		if err != nil {
			return err
		}
		body = string(b)
	} else {
		fmt.Fprintln(errw, "No content provided. Pass body as arg or pipe via stdin.")
		fmt.Fprintln(errw, "Try: ov2 capture --help")
		return errors.New("no content provided")
	}

	var tagList []string
	for _, t := range strings.Split(flags.tags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tagList = append(tagList, t)
		}
	}
	source := flags.source
	if source == "" {
		source = "cli"
	}

	mocName := flags.moc
	if mocName == "" && deps.interactive() {
		mocs, merr := vault.MOCs(cfg.VaultDir)
		if merr == nil && len(mocs) > 0 {
			chosen, perr := deps.pickMOC(mocs)
			if perr == nil {
				mocName = chosen
			} else if !errors.Is(perr, tui.ErrCancelled) {
				fmt.Fprintf(errw, "MOC picker error: %v\n", perr)
			}
		}
	}

	req := capture.Request{
		Body:       body,
		Title:      flags.title,
		Tags:       tagList,
		Source:     source,
		MOCName:    mocName,
		FetchTitle: true, // CLI always attempts a bare-URL fetch (row #46)
	}
	ccfg := capture.CaptureConfig{VaultDir: cfg.VaultDir, Inbox: cfg.Inbox, Resources: cfg.Resources}
	result, err := capture.Capture(context.Background(), ccfg, req, deps.fetcher, deps.now())
	if err != nil {
		return err
	}

	fmt.Fprintln(out, result.Rel)
	fmt.Fprintf(errw, "Captured: %s\n", result.Rel)
	if result.MOCLinked != "" {
		if result.MOCWarning != "" {
			fmt.Fprintln(errw, result.MOCWarning)
		} else {
			fmt.Fprintf(errw, "Added to [[%s]]\n", result.MOCLinked)
		}
	}
	return nil
}

// stdinPiped reports whether os.Stdin is a pipe/redirect rather than a real
// terminal — used to source the capture body (row #10/#44).
func stdinPiped() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

// interactiveTTY reports whether BOTH stdin and stdout are a real
// interactive terminal — the gate for launching the MOC picker (row #136).
func interactiveTTY() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}
```

Update `cmd/ov/root.go`:

```go
	root.AddCommand(newConfigCmd(), newDoctorCmd(), newInitCmd(), newInboxCmd(), newStaleCmd(), newReviewCmd(), newMocsCmd(), newCaptureCmd())
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ov/ -v`
Expected: PASS

Run: `go mod tidy && go build ./...`
Expected: `go-isatty` promoted to a direct require (no `// indirect` comment); build clean

- [ ] **Step 5: Commit**

```bash
git add cmd/ov/capture.go cmd/ov/capture_test.go cmd/ov/root.go go.mod go.sum
git commit -m "Add ov2 capture command: frozen flag surface, tty-gated MOC picker, CLI smoke test"
```

---

### Task 6: `cmd/ov triage` command

Manual (non-LLM) triage over `internal/vault`'s Task 1 additions and `internal/tui`'s pickers, with an injectable-dependency core matching Task 5's testability pattern.

**Files:**
- Create: `cmd/ov/triage.go`
- Create: `cmd/ov/triage_test.go`
- Modify: `cmd/ov/root.go` (register `newTriageCmd`)

**Interfaces:**
- Consumes: `resolveConfig`, `vault.ListInbox` (phase 1); `vault.ListAllFolders`, `vault.EnsureFolder`, `vault.MoveNote` (Task 1); `tui.RunFolderPicker`, `tui.Confirm` (Task 4); `Config.ParaRoots()` (phase 0, `internal/config`)
- Produces:
  - `func newTriageCmd() *cobra.Command`
  - `type triageDeps struct { pickFolder func([]string) (string, error); confirm func(string) (bool, error) }`
  - `func runTriage(cfg *config.Config, in *bufio.Reader, out, errw io.Writer, deps triageDeps) error`

- [ ] **Step 1: Write the failing tests**

```go
// cmd/ov/triage_test.go
package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTriageSkip(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	deps := triageDeps{
		pickFolder: func([]string) (string, error) { t.Fatal("pickFolder should not be called"); return "", nil },
		confirm:    func(string) (bool, error) { t.Fatal("confirm should not be called"); return false, nil },
	}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("s\n")), &out, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Errorf("skipped note should remain: %v", statErr)
	}
	if !strings.Contains(errBuf.String(), "Skipped") {
		t.Errorf("stderr = %q", errBuf.String())
	}
}

// CONTRACT(#57): Enter (empty choice) triggers the folder picker + move.
func TestTriageMove(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := triageDeps{pickFolder: func(folders []string) (string, error) { return "02-Areas", nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("\n")), &out, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "02-Areas", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Errorf("note should be moved: %v", statErr)
	}
}

// DECIDE(#132): delete requires explicit confirmation.
func TestTriageDeleteConfirmed(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := triageDeps{confirm: func(string) (bool, error) { return true, nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("d\n")), &out, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); !os.IsNotExist(statErr) {
		t.Error("note should be deleted")
	}
}

// DECIDE(#132): declining the confirmation leaves the note untouched.
func TestTriageDeleteDeclined(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := triageDeps{confirm: func(string) (bool, error) { return false, nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("d\n")), &out, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Errorf("declined delete must leave the note: %v", statErr)
	}
}

// CONTRACT(#102): q exits immediately without processing remaining notes.
func TestTriageQuit(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 A.md", "# A\n", 0)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0801 B.md", "# B\n", 0)
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := triageDeps{pickFolder: func([]string) (string, error) { t.Fatal("should not reach the second note"); return "", nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("q\n")), &out, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 A.md")); statErr != nil {
		t.Errorf("A should remain untouched after quit")
	}
}

// CONTRACT(#102): EOF (Ctrl-D) also quits cleanly.
func TestTriageEOFQuits(t *testing.T) {
	newVaultFixture(t)
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("")), &out, &errBuf, triageDeps{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "Inbox is empty") && !strings.Contains(errBuf.String(), "interrupted") {
		t.Errorf("stderr = %q", errBuf.String())
	}
}

// BUG(fixed)(#130): a typed destination cannot escape the vault.
func TestTriageMoveRejectsEscape(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := triageDeps{pickFolder: func([]string) (string, error) { return "../../../etc", nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("\n")), &out, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Errorf("note must remain in inbox after a rejected escape: %v", statErr)
	}
	if !strings.Contains(errBuf.String(), "escapes vault") {
		t.Errorf("stderr = %q, want an escape rejection message", errBuf.String())
	}
}

// BUG(fixed)(#139): a picker result naming an occupied destination refuses
// rather than overwriting.
func TestTriageMoveRefusesExistingDestination(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "new\n", 0)
	addNote(t, vaultDir, "02-Areas/2026-07-15 0800 Foo.md", "old\n", 0)
	cfg, _ := resolveConfig("")
	var out, errBuf bytes.Buffer
	deps := triageDeps{pickFolder: func([]string) (string, error) { return "02-Areas", nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("\n")), &out, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	got, rerr := os.ReadFile(filepath.Join(vaultDir, "02-Areas", "2026-07-15 0800 Foo.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(got), "old") {
		t.Error("existing destination note must not be overwritten")
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Error("source note must remain after a refused move")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ov/ -run TestTriage -v`
Expected: FAIL — `undefined: runTriage`, `undefined: triageDeps`

- [ ] **Step 3: Write the implementation**

```go
// cmd/ov/triage.go
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/arosenkranz/obsidian-vault-tools/internal/tui"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// triageDeps injects the tty-dependent picker/confirm collaborators so
// runTriage is fully testable without a real terminal.
type triageDeps struct {
	pickFolder func([]string) (string, error)
	confirm    func(string) (bool, error)
}

func newTriageCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "triage",
		Short: "Interactively process inbox notes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stdout.Fd()) {
				return errors.New("ov2 triage requires an interactive terminal")
			}
			deps := triageDeps{pickFolder: tui.RunFolderPicker, confirm: tui.Confirm}
			return runTriage(cfg, bufio.NewReader(cmd.InOrStdin()), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps)
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}

// runTriage is the testable core of ov2 triage: for each inbox note, read
// one key choice from in and act on it. deps.pickFolder/deps.confirm are
// injected so tests never open a real tty (production wires them to the
// bubbletea/huh implementations in internal/tui). Key map mirrors v1
// (behavior inventory row #57, #102): Enter/f = folder picker + move, s =
// skip, d = delete (with an explicit confirm, row #132), q = quit, EOF =
// quit.
func runTriage(cfg *config.Config, in *bufio.Reader, out, errw io.Writer, deps triageDeps) error {
	notes, err := vault.ListInbox(cfg.VaultDir, cfg.Inbox)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(errw, "No inbox directory found")
			return nil
		}
		return err
	}
	if len(notes) == 0 {
		fmt.Fprintln(errw, "Inbox is empty")
		return nil
	}
	for _, n := range notes {
		if _, err := os.Stat(n.Path); err != nil {
			continue // vanished mid-loop (walk resilience)
		}
		fmt.Fprintf(errw, "\n%s\n", n.Name)
		fmt.Fprint(errw, "  [Enter] Pick folder   [s] Skip   [d] Delete   [q] Quit   Choice: ")
		line, readErr := in.ReadString('\n')
		choice := strings.TrimSpace(line)
		if readErr != nil && line == "" {
			fmt.Fprintln(errw, "\ninterrupted")
			return nil
		}
		switch choice {
		case "q", "Q":
			fmt.Fprintln(errw, "Triage complete")
			return nil
		case "d", "D":
			ok, cerr := deps.confirm(fmt.Sprintf("Delete %q?", n.Name))
			if cerr != nil || !ok {
				fmt.Fprintln(errw, "  -> Skipped")
				continue
			}
			if rerr := os.Remove(n.Path); rerr != nil {
				fmt.Fprintf(errw, "  -> Delete failed: %v\n", rerr)
				continue
			}
			fmt.Fprintln(errw, "  -> Deleted")
		case "s", "S":
			fmt.Fprintln(errw, "  -> Skipped")
		case "", "f", "F":
			folders := vault.ListAllFolders(cfg.VaultDir, cfg.ParaRoots())
			if len(folders) == 0 {
				fmt.Fprintln(errw, "  -> No PARA folders found, skipped")
				continue
			}
			dest, perr := deps.pickFolder(folders)
			if perr != nil {
				fmt.Fprintln(errw, "  -> Skipped")
				continue
			}
			destAbs, eerr := vault.EnsureFolder(cfg.VaultDir, dest)
			if eerr != nil {
				fmt.Fprintf(errw, "  -> %v\n", eerr)
				continue
			}
			newPath, merr := vault.MoveNote(n.Path, destAbs)
			if merr != nil {
				fmt.Fprintf(errw, "  -> Move failed: %v\n", merr)
				continue
			}
			rel, _ := filepath.Rel(cfg.VaultDir, newPath)
			fmt.Fprintf(errw, "  -> Moved to %s\n", filepath.ToSlash(rel))
		default:
			fmt.Fprintln(errw, "  -> Invalid choice, skipped")
		}
	}
	fmt.Fprintln(errw, "\nTriage complete")
	return nil
}
```

Update `cmd/ov/root.go`:

```go
	root.AddCommand(newConfigCmd(), newDoctorCmd(), newInitCmd(), newInboxCmd(), newStaleCmd(), newReviewCmd(), newMocsCmd(), newCaptureCmd(), newTriageCmd())
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ov/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/ov/triage.go cmd/ov/triage_test.go cmd/ov/root.go
git commit -m "Add ov2 triage command: manual key-driven loop over the folder picker and delete confirm"
```

---

### Task 7: `internal/web` — server, bind guard, hygiene middleware, capture form, inbox list

The web v1 surface: `web.New(cfg, fetcher, nowFn)` builds a server around the SAME `capture.Capture` verb Task 5 wired into the CLI; `AllowBind` and the hygiene middleware are the two safety layers named by the design spec.

**Files:**
- Create: `internal/web/server.go`
- Create: `internal/web/bind.go`
- Create: `internal/web/bind_test.go`
- Create: `internal/web/hygiene.go`
- Create: `internal/web/handlers.go`
- Create: `internal/web/handlers_test.go`
- Create: `internal/web/fixture_test.go`
- Create: `internal/web/assets/inbox.html`
- Create: `internal/web/assets/capture.html`
- Create: `internal/web/assets/capture-result.html`
- Create: `internal/web/assets/htmx.min.js` (vendored asset, not hand-written — see Step 1)

**Interfaces:**
- Consumes: `capture.Request`, `capture.Result`, `capture.CaptureConfig`, `capture.Capture`, `capture.TitleFetcher` (Task 3); `vault.ListInbox`, `vault.AgeDays` (phase 1)
- Produces:
  - `package web` (new package, `github.com/arosenkranz/obsidian-vault-tools/internal/web`)
  - `type Config struct { VaultDir, Inbox, Resources, Bind string }`
  - `func New(cfg Config, fetcher capture.TitleFetcher, nowFn func() time.Time) *Server`
  - `func (s *Server) Handler() http.Handler`
  - `func (s *Server) Serve(ctx context.Context, ln net.Listener) error`
  - `func AllowBind(host string, override bool) error`

- [ ] **Step 1: Vendor htmx**

```bash
mkdir -p internal/web/assets
curl -fsSL https://unpkg.com/htmx.org@1.9.12/dist/htmx.min.js -o internal/web/assets/htmx.min.js
shasum -a 256 internal/web/assets/htmx.min.js
```

Expected checksum: `449317ade7881e949510db614991e195c3a099c4c791c24dacec55f9f4a2a452` (48101 bytes). If the checksum doesn't match, stop — do not proceed with a different file; report the mismatch.

- [ ] **Step 2: Write the templates**

```html
<!-- internal/web/assets/inbox.html -->
{{define "inbox.html"}}
<!doctype html>
<html>
<head><title>ov2 — Inbox</title><script src="/assets/htmx.min.js"></script></head>
<body>
<h1>Inbox</h1>
<p><a href="/capture">Capture a note</a></p>
{{if .Notes}}
<ul>
{{range .Notes}}<li>{{.Name}} — {{.Age}}d old</li>{{end}}
</ul>
{{else}}
<p>Inbox is empty.</p>
{{end}}
</body>
</html>
{{end}}
```

```html
<!-- internal/web/assets/capture.html -->
{{define "capture.html"}}
<!doctype html>
<html>
<head><title>ov2 — Capture</title><script src="/assets/htmx.min.js"></script></head>
<body>
<h1>Capture</h1>
<form hx-post="/capture" hx-target="#result">
  <p><label>Title <input type="text" name="title"></label></p>
  <p><label>Body<br><textarea name="body" rows="8" cols="60" required></textarea></label></p>
  <p><label>Tags <input type="text" name="tags" placeholder="a,b,c"></label></p>
  <p><label>MOC <input type="text" name="moc"></label></p>
  <p><label><input type="checkbox" name="fetch_title"> Fetch title from URL</label></p>
  <button type="submit">Capture</button>
</form>
<div id="result"></div>
</body>
</html>
{{end}}
```

```html
<!-- internal/web/assets/capture-result.html -->
{{define "capture-result.html"}}
<div class="success">
  Captured: {{.Rel}}
  {{if .MOCLinked}}<br>Linked to [[{{.MOCLinked}}]]{{end}}
  {{if .MOCWarning}}<br><span class="warning">{{.MOCWarning}}</span>{{end}}
</div>
{{end}}
```

- [ ] **Step 3: Write the failing tests**

```go
// internal/web/fixture_test.go
package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// newTestVault creates a temp vault with the standard PARA dirs and a
// matching Config, mirroring cmd/ov/fixture_test.go's newVaultFixture
// pattern (design spec §Testing strategy tier 3) — duplicated in miniature
// here because internal/web cannot import cmd/ov's unexported test helper
// across package boundaries.
func newTestVault(t *testing.T) (vaultDir string, cfg Config) {
	t.Helper()
	vaultDir = t.TempDir()
	for _, d := range []string{"00-Inbox", "01-Projects", "02-Areas", "03-Resources", "04-Archive", "99-Meta"} {
		if err := os.MkdirAll(filepath.Join(vaultDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return vaultDir, Config{VaultDir: vaultDir, Inbox: "00-Inbox", Resources: "03-Resources", Bind: "127.0.0.1:4173"}
}

type stubFetcher struct {
	title string
	err   error
}

func (f stubFetcher) FetchTitle(ctx context.Context, url string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.title, nil
}
```

```go
// internal/web/bind_test.go
package web

import "testing"

// DECIDE(#133, new in v2): loopback and Tailscale addresses are allowed by
// default; anything else needs the override.
func TestAllowBind(t *testing.T) {
	cases := []struct {
		host     string
		override bool
		allow    bool
	}{
		{"127.0.0.1", false, true},
		{"localhost", false, true},
		{"::1", false, true},
		{"100.64.0.5", false, true},   // Tailscale CGNAT range
		{"100.100.100.100", false, true},
		{"100.127.255.254", false, true},
		{"100.128.0.1", false, false}, // just outside the /10
		{"", false, false},            // wildcard bind, not localhost
		{"0.0.0.0", false, false},
		{"192.168.1.5", false, false},
		{"8.8.8.8", false, false},
		{"0.0.0.0", true, true}, // override always wins
		{"8.8.8.8", true, true},
	}
	for _, c := range cases {
		err := AllowBind(c.host, c.override)
		if c.allow && err != nil {
			t.Errorf("AllowBind(%q, %v): expected allow, got %v", c.host, c.override, err)
		}
		if !c.allow && err == nil {
			t.Errorf("AllowBind(%q, %v): expected refusal", c.host, c.override)
		}
	}
}
```

```go
// internal/web/handlers_test.go
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandleInboxEmpty(t *testing.T) {
	_, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = cfg.Bind
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Inbox is empty") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

// DECIDE(#138): the web inbox list renders through vault.ListInbox, the
// same query the CLI's `ov2 inbox` uses.
func TestHandleInboxListsNotes(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md"), []byte("# Foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, stubFetcher{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = cfg.Bind
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "2026-07-15 0800 Foo") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

// Temp-vault integration test (design spec §Testing strategy tier 3): the
// web capture handler writes a real note.
func TestHandleCaptureSubmitWritesNote(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{}, func() time.Time { return time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC) })
	form := url.Values{"title": {"Web Capture"}, "body": {"hello from web"}}
	req := httptest.NewRequest(http.MethodPost, "/capture", strings.NewReader(form.Encode()))
	req.Host = cfg.Bind
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Captured:") {
		t.Errorf("body = %q", rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0830 Web Capture.md")); err != nil {
		t.Errorf("note not written: %v", err)
	}
}

// CONTRACT(#135): the opt-in checkbox triggers a title fetch.
func TestHandleCaptureSubmitFetchTitleOptIn(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{title: "Fetched Title"}, func() time.Time { return time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC) })
	form := url.Values{"body": {"https://example.com/article"}, "fetch_title": {"on"}}
	req := httptest.NewRequest(http.MethodPost, "/capture", strings.NewReader(form.Encode()))
	req.Host = cfg.Bind
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if _, err := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0830 Fetched Title.md")); err != nil {
		t.Fatalf("note with fetched title not written: %v\nbody: %s", err, rec.Body.String())
	}
}

// DECIDE(#135): without the opt-in checkbox, title-fetch never happens even
// for a bare-URL body.
func TestHandleCaptureSubmitNeverAutoFetches(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{title: "Should Not Be Used"}, func() time.Time { return time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC) })
	form := url.Values{"body": {"https://example.com/article"}}
	req := httptest.NewRequest(http.MethodPost, "/capture", strings.NewReader(form.Encode()))
	req.Host = cfg.Bind
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if _, err := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0830 Should Not Be Used.md")); err == nil {
		t.Fatal("title fetch happened without the opt-in checkbox")
	}
	if _, err := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0830 https example.com article.md")); err != nil {
		t.Errorf("expected the slugified-URL fallback title, note missing: %v", err)
	}
}

// DECIDE(#134, new in v2): Host-header validation.
func TestHygieneRejectsWrongHost(t *testing.T) {
	_, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "evil.example.com"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// DECIDE(#134): a cross-origin POST is rejected even with HX-Request set.
func TestHygieneRejectsCrossOriginPost(t *testing.T) {
	_, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{}, nil)
	form := url.Values{"title": {"x"}, "body": {"y"}}
	req := httptest.NewRequest(http.MethodPost, "/capture", strings.NewReader(form.Encode()))
	req.Host = cfg.Bind
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Origin", "http://evil.example.com")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

// DECIDE(#134): a POST without the HX-Request header is rejected (kills
// drive-by form CSRF that CORS alone would not stop).
func TestHygieneRejectsMissingHXRequestHeader(t *testing.T) {
	_, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{}, nil)
	form := url.Values{"title": {"x"}, "body": {"y"}}
	req := httptest.NewRequest(http.MethodPost, "/capture", strings.NewReader(form.Encode()))
	req.Host = cfg.Bind
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/web/ -v`
Expected: FAIL — `no Go files in .../internal/web` (package doesn't exist yet)

- [ ] **Step 5: Write the implementation**

```go
// internal/web/bind.go
package web

import (
	"fmt"
	"net"
)

// AllowBind reports whether host is safe to listen on without an explicit
// override: loopback and "localhost" are always allowed; a Tailscale
// address (100.64.0.0/10 — the CGNAT range Tailscale assigns from) is
// allowed because it is not reachable from an open network; everything
// else (an empty host meaning a wildcard bind, 0.0.0.0, a LAN IP, ...)
// requires override=true. Behavior inventory row #133 — new in v2, design
// spec §Web layer "Bind guard".
func AllowBind(host string, override bool) error {
	if override {
		return nil
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("refusing to bind %q: not a recognized loopback or Tailscale address; pass --allow-nonlocal-bind to bind anyway", host)
	}
	if ip.IsLoopback() {
		return nil
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1]&0xc0 == 64 {
		return nil // 100.64.0.0/10
	}
	return fmt.Errorf("refusing to bind %s: not loopback or a Tailscale address; pass --allow-nonlocal-bind to bind anyway", host)
}
```

```go
// internal/web/hygiene.go
package web

import (
	"net"
	"net/http"
	"net/url"
)

// hygieneMiddleware validates the Host header against the configured bind
// (kills DNS rebinding) and, for state-changing requests, requires a
// same-origin/absent Origin AND the HX-Request header (kills drive-by form
// CSRF — CORS alone does not stop cross-origin form POSTs to 127.0.0.1).
// Distinct from auth: any local process can still reach the API (accepted
// for v1, design spec §Web layer). Behavior inventory row #134.
func hygieneMiddleware(bind string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Host != bind && !isLoopbackHost(r.Host) {
				http.Error(w, "invalid host header", http.StatusBadRequest)
				return
			}
			if r.Method == http.MethodPost {
				if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
					http.Error(w, "cross-origin request rejected", http.StatusForbidden)
					return
				}
				if r.Header.Get("HX-Request") != "true" {
					http.Error(w, "missing HX-Request header", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isLoopbackHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func sameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == host
}
```

```go
// internal/web/handlers.go
package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/capture"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

type inboxViewNote struct {
	Name string
	Age  int
}

func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request) {
	notes, err := vault.ListInbox(s.cfg.VaultDir, s.cfg.Inbox)
	if err != nil {
		s.renderInbox(w, nil)
		return
	}
	now := s.now()
	views := make([]inboxViewNote, 0, len(notes))
	for _, n := range notes {
		views = append(views, inboxViewNote{Name: n.Name, Age: vault.AgeDays(now, n.ModTime)})
	}
	s.renderInbox(w, views)
}

func (s *Server) renderInbox(w http.ResponseWriter, notes []inboxViewNote) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "inbox.html", map[string]any{"Notes": notes}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleCaptureForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "capture.html", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleCaptureSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var tags []string
	for _, t := range strings.Split(r.FormValue("tags"), ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	req := capture.Request{
		Body:       r.FormValue("body"),
		Title:      r.FormValue("title"),
		Tags:       tags,
		Source:     "web",
		MOCName:    r.FormValue("moc"),
		FetchTitle: r.FormValue("fetch_title") == "on", // explicit opt-in checkbox, row #135 — never automatic
	}
	ccfg := capture.CaptureConfig{VaultDir: s.cfg.VaultDir, Inbox: s.cfg.Inbox, Resources: s.cfg.Resources}
	result, err := capture.Capture(context.Background(), ccfg, req, s.fetcher, s.now())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		fmt.Fprintf(w, `<div class="error">Capture failed: %s</div>`, template.HTMLEscapeString(err.Error()))
		return
	}
	if err := s.tmpl.ExecuteTemplate(w, "capture-result.html", result); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleHTMX(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	http.ServeFileFS(w, r, assetsFS, "assets/htmx.min.js")
}
```

```go
// internal/web/server.go
package web

import (
	"context"
	"embed"
	"html/template"
	"net"
	"net/http"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/capture"
)

//go:embed assets/*.html assets/*.js
var assetsFS embed.FS

// Config is everything the web layer needs from the resolved ov config —
// deliberately a narrow struct, not internal/config.Config, so this package
// stays a thin frontend over the capture/vault core verbs (design spec's
// "stateless verbs" principle).
type Config struct {
	VaultDir  string
	Inbox     string
	Resources string
	Bind      string // the configured bind address, for Host-header validation
}

type Server struct {
	cfg     Config
	mux     *http.ServeMux
	fetcher capture.TitleFetcher
	tmpl    *template.Template
	now     func() time.Time
}

// New builds a Server around an already-constructed listener seam (the
// caller owns bind-guard decisions and listener construction — design spec
// §Web layer "Listener seam"). fetcher is injected so tests never hit the
// network; nowFn defaults to time.Now when nil.
func New(cfg Config, fetcher capture.TitleFetcher, nowFn func() time.Time) *Server {
	if nowFn == nil {
		nowFn = time.Now
	}
	tmpl := template.Must(template.ParseFS(assetsFS, "assets/*.html"))
	s := &Server{cfg: cfg, fetcher: fetcher, tmpl: tmpl, now: nowFn}
	s.mux = http.NewServeMux()
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleInbox)
	s.mux.HandleFunc("GET /capture", s.handleCaptureForm)
	s.mux.HandleFunc("POST /capture", s.handleCaptureSubmit)
	s.mux.HandleFunc("GET /assets/htmx.min.js", s.handleHTMX)
}

// Handler returns the fully wrapped handler (routes + hygiene middleware),
// for both real serving and httptest.
func (s *Server) Handler() http.Handler {
	return hygieneMiddleware(s.cfg.Bind)(s.mux)
}

// Serve runs the HTTP server over ln until ctx is done or the listener
// errors. Blocking; the caller runs it in its own goroutine or foreground.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	httpSrv := &http.Server{Handler: s.Handler()}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/web/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/web/
git commit -m "Add internal/web: server, bind guard, hygiene middleware, capture form, inbox list"
```

---

### Task 8: `cmd/ov serve` command

Wires `internal/web` to a real listener, applying the bind guard before the socket ever opens.

**Files:**
- Create: `cmd/ov/serve.go`
- Create: `cmd/ov/serve_test.go`
- Modify: `cmd/ov/root.go` (register `newServeCmd`)

**Interfaces:**
- Consumes: `resolveConfig` (phase 1); `web.Config`, `web.New`, `web.AllowBind` (Task 7); `capture.NewHTTPTitleFetcher` (Task 3)
- Produces: `func newServeCmd() *cobra.Command` — flags: `--vault`, `--bind` (default `127.0.0.1:8420`), `--allow-nonlocal-bind`

- [ ] **Step 1: Write the failing tests**

```go
// cmd/ov/serve_test.go
package main

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// DECIDE(#133): the bind guard runs before the listener opens — an
// unauthorized bind never touches the network.
func TestServeBindGuardRejectsNonLoopback(t *testing.T) {
	newVaultFixture(t)
	_, _, err := runCmd(t, "serve", "--bind", "0.0.0.0:0")
	if err == nil {
		t.Fatal("expected the bind guard to refuse 0.0.0.0 without --allow-nonlocal-bind")
	}
}

func TestServeStartsAndStopsOnLoopback(t *testing.T) {
	newVaultFixture(t)
	root := newRootCmd()
	ctx, cancel := context.WithCancel(context.Background())
	root.SetArgs([]string{"serve", "--bind", "127.0.0.1:0"})
	var errBuf bytes.Buffer
	root.SetErr(&errBuf)
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned an error on shutdown: %v\nstderr: %s", err, errBuf.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not shut down within 5s of context cancellation")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ov/ -run TestServe -v`
Expected: FAIL — `undefined: newServeCmd` (via `unknown command "serve"`)

- [ ] **Step 3: Write the implementation**

```go
// cmd/ov/serve.go
package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/arosenkranz/obsidian-vault-tools/internal/capture"
	"github.com/arosenkranz/obsidian-vault-tools/internal/web"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var vaultFlag, bindFlag string
	var allowNonlocal bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the ov2 web server (capture form + inbox)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			errw := cmd.ErrOrStderr()
			host, _, err := net.SplitHostPort(bindFlag)
			if err != nil {
				return fmt.Errorf("--bind must be host:port: %w", err)
			}
			if err := web.AllowBind(host, allowNonlocal); err != nil {
				return err
			}
			ln, err := net.Listen("tcp", bindFlag)
			if err != nil {
				return err
			}
			defer ln.Close()
			srv := web.New(web.Config{
				VaultDir:  cfg.VaultDir,
				Inbox:     cfg.Inbox,
				Resources: cfg.Resources,
				Bind:      bindFlag,
			}, capture.NewHTTPTitleFetcher(), nil)
			fmt.Fprintf(errw, "ov2 serve: listening on http://%s\n", ln.Addr())
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return srv.Serve(ctx, ln)
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	cmd.Flags().StringVar(&bindFlag, "bind", "127.0.0.1:8420", "address to listen on")
	cmd.Flags().BoolVar(&allowNonlocal, "allow-nonlocal-bind", false, "bind to a non-loopback, non-Tailscale address (dangerous)")
	return cmd
}
```

Update `cmd/ov/root.go`:

```go
	root.AddCommand(newConfigCmd(), newDoctorCmd(), newInitCmd(), newInboxCmd(), newStaleCmd(), newReviewCmd(), newMocsCmd(), newCaptureCmd(), newTriageCmd(), newServeCmd())
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ov/ -v`
Expected: PASS

- [ ] **Step 5: Run the whole suite**

Run: `go build ./... && go test ./...`
Expected: PASS across every package (`internal/config`, `internal/vault`, `internal/capture`, `internal/tui`, `internal/web`, `cmd/ov`)

- [ ] **Step 6: Commit**

```bash
git add cmd/ov/serve.go cmd/ov/serve_test.go cmd/ov/root.go
git commit -m "Add ov2 serve command: bind-guarded listener wired to internal/web"
```

---

## Self-Review

**1. Spec coverage (phase table row: "capture (+URL title, MOC entry, collision); manual triage; tui pickers; web capture form + inbox"):**
- `ov2 capture` frozen flag surface (`--title`, `--tags`, `--source`, `--moc`) → Task 5; body-from-positional-or-stdin (rows #10, #43, #44) → Task 5; URL title fetch (rows #27–#31, #46) → Task 2/3; SSRF hardening (row #131, design spec §Safety item 4) → Task 2; MOC entry append (rows #8, #40–#42, #54, #129) → Task 1/3; filename stamping + collision (rows #49–#51, #137) → Task 3; atomic write (row #128) → Task 3.
- Manual `ov2 triage` (rows #57, #102) → Task 6, including the delete-confirm alignment (row #132) and the existing-destination refusal (row #139).
- TUI pickers (rows #9, #35–#39, design spec §CLI/TUI bubbletea requirement) → Task 4, wired into capture (Task 5, MOC picker) and triage (Task 6, folder picker).
- `ov2 serve` web capture form + inbox (design spec §Web layer) → Task 7 (server/handlers/templates/bind guard/hygiene) + Task 8 (listener wiring, CLI flags).
- Core contracts — atomic writes (every new write path routes through `vault.WriteNoteAtomic` or `vault.MoveNote`), containment (`vault.ContainPath`, Task 1, consumed by Task 6's `EnsureFolder` call and table-tested standalone), conditional writes (`appendMOCConditional`, Task 3) — all present.
- tty discipline (design spec §CLI/TUI; rows #9, #36, #123, #136) → Task 5's `captureDeps.interactive` gate + CLI smoke test; Task 6's isatty gate in `newTriageCmd`.
- Safety §4 SSRF hardening → Task 2's `rejectUnsafeIP` + DialContext guard, table-tested including the CGNAT boundary.
- Web bind guard (row #133) → Task 7's `AllowBind`, invoked by Task 8 before `net.Listen`.
- Web hygiene (row #134) → Task 7's `hygieneMiddleware`, applied to every route via `Handler()`.
- Web opt-in title-fetch (row #135) → Task 7's `fetch_title` form field, tested both ways (opt-in fetch, and no-fetch-without-opt-in).
- Behavior inventory mining (rows 128–139) → done ahead of this plan, appended to `docs/plans/ov2-behavior-inventory.md`; every test above cites its row.

**2. Placeholder scan:** every code step contains complete, compilable Go (or, for the vendored `htmx.min.js`, an exact pinned URL + checksum, not a TBD) — no TODO/"add error handling"/"similar to Task N" without repeated code. Shared test helpers are defined once per package (`cmd/ov/fixture_test.go` from phase 1, reused by Tasks 5/6/8; `internal/web/fixture_test.go`, new in Task 7) and named in later Interfaces blocks.

**3. Type consistency:** `capture.CaptureConfig{VaultDir, Inbox, Resources string}`, `capture.Request{Body, Title, Tags, Source, MOCName, FetchTitle}`, `capture.Result{Path, Rel, Title, MOCLinked, MOCWarning}`, `capture.Capture(ctx, cfg, req, fetcher, now)` — identical across Tasks 3, 5, 7. `vault.ContainPath(root, target string) (string, error)`, `vault.EnsureFolder(vaultDir, rel string) (string, error)`, `vault.MoveNote(srcAbs, destDirAbs string) (string, error)`, `vault.ListAllFolders(vaultDir string, roots []string) []string`, `vault.AppendMOCEntry(content, title, snippet string) string`, `vault.FindMOCByName(vaultDir, resourcesDir, name string) (*MOC, error)` — defined in Task 1, consumed identically by Tasks 3 and 6. `tui.RunFolderPicker([]string) (string, error)`, `tui.RunMOCPicker([]vault.MOC) (string, error)`, `tui.Confirm(string) (bool, error)`, `tui.ErrCancelled` — defined in Task 4, consumed identically by Tasks 5 and 6. `web.Config{VaultDir, Inbox, Resources, Bind}`, `web.New(cfg, fetcher, nowFn)`, `web.AllowBind(host string, override bool) error` — defined in Task 7, consumed identically by Task 8. `captureDeps`/`captureFlags`/`runCapture` (Task 5) and `triageDeps`/`runTriage` (Task 6) follow the same injectable-collaborator pattern, cross-checked for naming consistency.
