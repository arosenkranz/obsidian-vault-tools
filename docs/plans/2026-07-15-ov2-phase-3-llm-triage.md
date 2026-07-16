# ov v2 Phase 3: LLM Triage (transport, job model, Propose/Validate/Apply, tui + web approval) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `ov2 triage --llm` as a full replacement for `bin/triage_llm.py`: a hardened subprocess transport (`internal/llm`), a generic in-process job model shared by two frontends, and a new `internal/triage` core (`Propose`/`Validate`/`Apply`) that closes the v1 `body_patch`/`links_to_add` enforcement hole in code. Both the CLI's interactive a/e/s/d/r/q approval loop and the new web triage propose/approve flow (`ov2 serve`) call the SAME `triage.Propose`/`triage.Validate`/`triage.Apply` verbs — phase 2's "stateless verbs, two frontends one core" principle extended to the LLM path. `--dry-run` renders every proposal's diff and writes nothing.

**Architecture:** Per the approved spec (`docs/plans/2026-07-11-ov-v2-go-rewrite-design.md`), phase 3 row. `internal/llm` gains a subprocess transport (`Runner.Run`), failure classification, a generic `JobManager[T]`, and an `ExtractHTMLBlock` decoder (contract defined now, consumed in phase 4/5); `ExtractJSON` already shipped in phase 0 and is reused unchanged. A new `internal/triage` package owns the AGENTS.md §7 `Proposal` type, prompt assembly, the `body_patch`/`links_to_add`/PARA-root safety gates, frontmatter merge + MOC-rename-sync apply logic, and a presentation-free unified diff (`triage.Diff`) consumed by both `internal/tui` (colorized terminal) and `internal/web` (HTML diff card). `cmd/ov/triage.go` grows a `--llm` flag reusing the existing container command and `resolveConfig`; `internal/web` grows a `POST/GET /triage/{note}/...` route family built on the same job manager. A new `internal/llmtest` package provides the TestMain re-exec stub-LLM responder (design spec §Testing strategy tier 3) shared by `cmd/ov` and `internal/web`'s temp-vault integration tests, so every test in this phase exercises the real `exec.Cmd` transport instead of mocking it away.

**Tech Stack:** Go 1.25.0, cobra, pelletier/go-toml/v2, golang.org/x/text, charm.land/bubbletea/v2, charm.land/lipgloss/v2, charmbracelet/huh, mattn/go-isatty (all existing). New this phase: `github.com/sergi/go-diff@v1.4.0` (`diffmatchpatch` — line-level diff engine backing `triage.Diff`; the last un-added package from the design's approved dep budget, needed now because phase 3 is the first phase with a triage diff-confirm view in both TUI and web — phase 6's "diff views" line in the phasing table refers to later UI polish on top of this functional diff, not its introduction).

## Global Constraints

- Module path: `github.com/arosenkranz/obsidian-vault-tools`
- Binary name during transition: `ov2`, built to `dist/ov2` (`make build`)
- New direct dependency this phase, exact pin: `github.com/sergi/go-diff v1.4.0`. No other new dependency without checking the budget in design spec §Architecture first.
- `bin/vault.sh`, `bin/triage_llm.py`, `bin/moc_cleanup.py` are READ-ONLY — never modify them. `bin/moc_cleanup.py`'s validator/cleanup workflow is phase 4 — do not port it in this phase; only its `render_diff`-adjacent idea (a diff view) is relevant here, and phase 3's `triage.Diff` is an independent, purpose-built implementation, not a port of `render_diff`.
- **The v1 safety hole this phase closes (design spec §Safety item 1, row #5):** `triage.Validate` MUST reject any `Proposal` with a non-null `body_patch` or a non-empty `links_to_add`, unconditionally, in code — never inferred from prompt compliance. `triage.Apply` MUST call `Validate` itself as its first step (defense in depth: `Apply` must refuse unsafe input even if a caller forgot to call `Validate` first). No code path may write `body_patch` content to disk or append `links_to_add` wikilinks in this phase.
- **PARA-root gate (row #97, layered on phase 2's `vault.ContainPath`):** `Validate` resolves `p.To` through `vault.ContainPath(cfg.VaultDir, p.To)` (filesystem containment, phase 2) AND additionally checks that the resolved path's first vault-relative path component is one of the configured PARA roots or the inbox folder name. A path can pass `ContainPath` (stay inside the vault) yet still fail this second, PARA-root-specific check (e.g. a proposal targeting `99-Meta/evil.md`) — both checks are required, neither subsumes the other.
- **Never trust the LLM's own output for a gate.** Every safety check (`body_patch`/`links_to_add` rejection, PARA-root containment, target-exists refusal) lives in `internal/triage`, is re-run by `Apply` against freshly-read disk content at write time (not the snapshot `Propose` read), and is never conditioned on anything the LLM said about its own compliance (design spec §LLM subsystem "Prompt injection posture").
- **Prompt injection posture (design spec, load-bearing):** the LLM subprocess's working directory is always a freshly created, empty scratch temp directory — **never** the vault. Note bodies and previously-fetched URL titles are attacker-influenced and get interpolated into the prompt (`internal/triage.BuildPrompt`). Tool-disabling is provider-specific (varies `claude` vs `pi` vs others) and is a **documented settings-profile recommendation** in `examples/ov.config.example`, not a flag `internal/llm` injects into the user-controlled `OV_LLM_CMD` argv (row #143).
- **Transport hardening (rows #144-146):** `OV_LLM_CMD`'s argv[0] is resolved via `exec.LookPath` to an absolute path before every invocation and exec'd directly — never a shell, never a bare name left to the child's own PATH search. The subprocess runs in its own process group (`Setpgid: true`); on context cancellation or deadline, the **whole group** is killed (`kill(-pgid, SIGKILL)`) via `exec.Cmd.Cancel`, never just the direct child (v1's `subprocess.run(timeout=...)` orphaned `claude`'s node subprocess tree — row #145, `BUG(fixed)`). Concurrent LLM subprocesses are bounded to a semaphore of 2 (`llm.Runner`, row #146).
- **Failure classification (row #147):** exit code + stderr are classified into typed errors — `llm.ErrAuth` when stderr contains an auth/login marker, a generic exit-code error otherwise, `llm.ErrBinaryNotFound` when `exec.LookPath` fails, `llm.ErrTimeout` when the context deadline fired. The web triage handlers render `llm.ErrAuth` distinctly ("LLM auth expired — run `claude login` on the Mac"), never a generic 500.
- **Job model (row #148):** `llm.JobManager[T]` is a generic, goroutine-based submit/poll/result table — no callbacks into frontend code, no UI types crossing into `internal/llm` or `internal/triage` (design spec's "stateless verbs" / "core packages expose Propose/Validate/Apply-shaped functions" invariant). `internal/web`'s `triageJobs` wraps one `*llm.JobManager[triage.Proposal]` and additionally tracks "current job id for this note" so the web polling routes don't need job ids echoed back to the client — design spec's "in-process map keyed by note path" (Core interaction contract), regenerated on restart, fine for one user.
- **Exit codes for `ov2 triage --llm` (row #104):** missing `AGENTS.md` at the vault root or a missing inbox directory both exit **2** (distinct from the exit-1 convention used elsewhere in the CLI). This mirrors v1 `triage_llm.py`'s own exit code for that binary specifically. Manual (non-`--llm`) `ov2 triage` keeps its existing phase-2 behavior (prints "No inbox directory found" to stderr and exits 0) — unrelated code path, not touched this phase. This divergence between the two triage modes is intentional and is the "confirm this is still the right call or document a deliberate divergence" resolution: **documented divergence, not a bug.**
- **`--dry-run` never writes.** `triage.Apply(cfg, note, p, now, dryRun=true)` runs every resolution/validation/merge step and returns the would-be `Result` (including the full rendered `Content` for diff display) without touching the filesystem. The CLI's real apply and the dry-run preview call the identical `Apply` function with only the `dryRun` bool differing — this is what keeps dry-run output and real behavior from drifting apart.
- **Stub-LLM integration tests (design spec §Testing strategy tier 3).** Every new job-model/failure-classification/apply-safety behavior gets a temp-vault integration test with `OV_LLM_CMD` pointed at the CURRENT test binary re-executing itself in stub mode (`internal/llmtest`), never a hand-rolled fake `exec.Cmd`. Fast unit tests of `triage.Propose`/`Validate` inject a fake `triage.Runner` (interface, no subprocess) directly; the stub-subprocess path is reserved for tests that must prove the real `internal/llm` transport (job timeout → process-group kill, auth-failure classification, argv-exec-never-shell).
- Every mined behavior test carries a comment `// CONTRACT:`, `// BUG(fixed):`, or `// DECIDE(<resolution>):` referencing a row number in `docs/plans/ov2-behavior-inventory.md` (rows 1–151; rows 143–151 were mined for this phase).
- Go tests use `t.Setenv` / `t.TempDir` / `os.Chtimes` — no global state leaks between tests; no injected package-level clock (clocks are passed as `time.Time` values or `func() time.Time`).
- Commit style: imperative mood, no conventional-commit prefix (matches repo history)
- Interactive commands inject their tty-dependent and network/subprocess-dependent collaborators as function values or interfaces (matching phase 2's `captureDeps`/`triageDeps` pattern) so `cmd/ov` unit tests never open a real tty or spawn a real subprocess unless the test is specifically exercising the stub-subprocess integration path.
---

### Task 1: `internal/vault` — frontmatter `Pairs()` and `RenameMOCLink`

Two small, independently testable additions every later task in this phase builds on: a read-only "all top-level keys" view of a note's frontmatter (for interpolating into the LLM prompt) and a pure MOC-body wikilink-rename text transform (the mechanical half of v1's `update_moc_entry_title`, row #96).

**Files:**
- Modify: `internal/vault/frontmatter.go` (add `Pairs`)
- Modify: `internal/vault/frontmatter_test.go` (add `TestPairs`)
- Modify: `internal/vault/moc_write.go` (add `RenameMOCLink`)
- Modify: `internal/vault/moc_write_test.go` (add `TestRenameMOCLink*`)

**Interfaces:**
- Consumes: `Frontmatter` type, `ParseNote` (phase 0, `internal/vault/frontmatter.go`)
- Produces:
  - `func (f *Frontmatter) Pairs() map[string]string`
  - `func RenameMOCLink(content, oldTitle, newTitle string) (string, bool)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/vault/frontmatter_test.go — append at end of file

// CONTRACT(#85): Pairs generalizes the already-ported lenient single-key
// view (Get) to every top-level key at once — quotes stripped the same
// way, comments and indented continuation lines invisible, same as
// keyLine's zero-indent predicate. Read/display only (e.g. interpolating a
// note's current frontmatter into the phase 3 LLM prompt); never used for
// writes.
func TestPairs(t *testing.T) {
	content := "---\ntype: inbox\ncreated: 2026-05-14\nsource: \"cli\"\n# a comment\ntags:\n  - a\n  - b\n  bad: not a key\nmoc: [[MOC Music]]\n---\nbody\n"
	fm, _ := ParseNote(content)
	got := fm.Pairs()
	want := map[string]string{
		"type":    "inbox",
		"created": "2026-05-14",
		"source":  "cli",
		"tags":    "",
		"moc":     "[[MOC Music]]",
	}
	if len(got) != len(want) {
		t.Fatalf("Pairs() = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Pairs()[%q] = %q, want %q", k, got[k], v)
		}
	}
}

// Nil-safe like every other Frontmatter method (TestNilFrontmatterSafe
// companion).
func TestPairsNilSafe(t *testing.T) {
	var f *Frontmatter
	if got := f.Pairs(); got != nil {
		t.Errorf("Pairs() on nil = %v, want nil", got)
	}
}
```

```go
// internal/vault/moc_write_test.go — append at end of file

// CONTRACT(#96): RenameMOCLink replaces every [[old]] with [[new]] in the
// MOC body only; frontmatter untouched (ported from triage_llm.py
// update_moc_entry_title, tests/test_triage_llm.py:45-104).
func TestRenameMOCLinkReplacesInBody(t *testing.T) {
	content := "---\ntype: moc\n---\n# MOC Music\n\n- [[Old Song]] — a tune\n- [[Other]] — unrelated\n"
	got, changed := RenameMOCLink(content, "Old Song", "New Song")
	if !changed {
		t.Fatal("expected a change")
	}
	want := "---\ntype: moc\n---\n# MOC Music\n\n- [[New Song]] — a tune\n- [[Other]] — unrelated\n"
	if got != want {
		t.Errorf("RenameMOCLink =\n%q\nwant\n%q", got, want)
	}
}

// CONTRACT(#96): the frontmatter block is never touched even if it happens
// to contain the literal wikilink text.
func TestRenameMOCLinkNeverTouchesFrontmatter(t *testing.T) {
	content := "---\nmoc: [[Old Song]]\n---\nbody has [[Old Song]] here\n"
	got, changed := RenameMOCLink(content, "Old Song", "New Song")
	if !changed {
		t.Fatal("expected a change")
	}
	if !strings.Contains(got, "moc: [[Old Song]]") {
		t.Errorf("frontmatter was modified: %q", got)
	}
	if !strings.Contains(got, "body has [[New Song]] here") {
		t.Errorf("body was not renamed: %q", got)
	}
}

// CONTRACT(#96): old==new is a no-op (mirrors v1's early return).
func TestRenameMOCLinkNoOpSameTitle(t *testing.T) {
	content := "# MOC Music\n\n- [[Same]] — x\n"
	got, changed := RenameMOCLink(content, "Same", "Same")
	if changed || got != content {
		t.Errorf("expected no-op, got changed=%v content=%q", changed, got)
	}
}

// CONTRACT(#96): no matching entry -> no-op, changed=false (the note may
// not actually be linked from this MOC — caller reports a warning, never
// an error, per row #94).
func TestRenameMOCLinkNoMatch(t *testing.T) {
	content := "# MOC Music\n\n- [[Unrelated]] — x\n"
	got, changed := RenameMOCLink(content, "Missing", "New")
	if changed || got != content {
		t.Errorf("expected no-op, got changed=%v content=%q", changed, got)
	}
}

// A note with no frontmatter block still renames correctly.
func TestRenameMOCLinkNoFrontmatter(t *testing.T) {
	content := "# MOC Music\n\n- [[Old]] — x\n"
	got, changed := RenameMOCLink(content, "Old", "New")
	if !changed {
		t.Fatal("expected a change")
	}
	if got != "# MOC Music\n\n- [[New]] — x\n" {
		t.Errorf("got %q", got)
	}
}
```

Add `"strings"` to `moc_write_test.go`'s import block if not already present (it currently imports `os`/`path/filepath`/`testing` only — add `strings`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vault/... -run 'TestPairs|TestRenameMOCLink' -v`
Expected: FAIL — `Pairs` and `RenameMOCLink` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/vault/frontmatter.go — append at end of file

// Pairs returns every top-level (zero-indent) declaring key with its raw
// lenient-view value (quotes stripped, same rule as Get), in no particular
// order. A block-style value's own continuation lines (e.g. `tags:`
// followed by indented `- a` items) contribute no value for that key — the
// declaring line's raw suffix after the colon is used as-is (typically
// empty for a pure block list), matching Get/rawValue's behavior on that
// same key. Read/display only: this generalizes the already-ported
// lenient view (row #85) to every key at once for interpolating a note's
// current frontmatter into the phase 3 LLM prompt (internal/triage);
// never used for writes, which stay via Set/Delete's patch-in-place
// semantics (design spec's lossless-frontmatter contract).
func (f *Frontmatter) Pairs() map[string]string {
	if f == nil {
		return nil
	}
	out := make(map[string]string)
	for _, line := range f.lines {
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		val := strings.TrimSpace(v)
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		out[key] = val
	}
	return out
}
```

```go
// internal/vault/moc_write.go — append at end of file

// RenameMOCLink replaces every "[[oldTitle]]" wikilink in content's BODY
// (never its frontmatter block) with "[[newTitle]]". Pure text transform,
// same shape as AppendMOCEntry — the caller re-reads, re-hashes, and
// writes via WriteNoteAtomic. Returns the (possibly unchanged) content and
// whether a rename was made. Ports triage_llm.py update_moc_entry_title
// (row #96): intentionally narrow and mechanical — it only fixes entry
// text after a triage rename, never reorders/dedupes/reorganizes a MOC
// (that's `ov mocs cleanup`, phase 4, LLM-assisted and human-approved).
func RenameMOCLink(content, oldTitle, newTitle string) (string, bool) {
	if oldTitle == newTitle {
		return content, false
	}
	fm, body := ParseNote(content)
	target := "[[" + oldTitle + "]]"
	if !strings.Contains(body, target) {
		return content, false
	}
	newBody := strings.ReplaceAll(body, target, "[["+newTitle+"]]")
	if fm == nil {
		return newBody, true
	}
	return fm.Render() + newBody, true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vault/... -v`
Expected: PASS, full package green (no regressions in existing frontmatter/moc_write tests).

- [ ] **Step 5: Commit**

```bash
git add internal/vault/frontmatter.go internal/vault/frontmatter_test.go internal/vault/moc_write.go internal/vault/moc_write_test.go
git commit -m "Add Frontmatter.Pairs and vault.RenameMOCLink for phase 3 triage"
```

---

### Task 2: `internal/llmtest` + `internal/llm` — subprocess transport, failure classification, health check

The hardened transport at the center of this phase: shlex-style argv split (never a shell), absolute-path `argv[0]` resolution, scratch-CWD + process-group execution, context-driven timeout/cancel with whole-group kill, a bounded semaphore, stderr-sniffed failure classification, and the `ExtractHTMLBlock` decoder contract (consumed starting phase 4/5). `internal/llmtest` is the shared TestMain re-exec stub-LLM responder this task's own integration tests need first, and that `cmd/ov` (Task 7) and `internal/web` (Task 8) reuse.

**Files:**
- Create: `internal/llmtest/stub.go`
- Create: `internal/llm/transport.go`
- Create: `internal/llm/transport_test.go`
- Create: `internal/llm/classify.go`
- Create: `internal/llm/classify_test.go`
- Modify: `internal/llm/decode.go` (add `ExtractHTMLBlock`)
- Modify: `internal/llm/decode_test.go` (add `TestExtractHTMLBlock`)
- Create: `internal/llm/main_test.go` (`TestMain` re-exec wiring for `internal/llm`'s own transport tests)
- Modify: `go.mod` / `go.sum` (add `github.com/sergi/go-diff v1.4.0` — pulled in transitively by nothing yet this task, but pin it now per Global Constraints so Task 4 doesn't need a separate `go get`)
- Modify: `examples/ov.config.example` (document the tool-disabling settings-profile recommendation, row #143)

**Interfaces:**
- Consumes: nothing from other tasks (foundational)
- Produces:
  - `package llmtest` (new package, `github.com/arosenkranz/obsidian-vault-tools/internal/llmtest`)
  - `const llmtest.StubEnv, llmtest.ResponseEnv, llmtest.ExitCodeEnv, llmtest.StderrEnv, llmtest.SleepEnv string`
  - `func llmtest.MaybeRunStub() bool`
  - `func llmtest.SelfCmd() (string, error)`
  - `type llm.Config struct{ Cmd, Model string }`
  - `var llm.ErrEmptyCmd, llm.ErrBinaryNotFound, llm.ErrTimeout, llm.ErrAuth error`
  - `func llm.Classify(runErr error, stderr string) error`
  - `type llm.Runner struct{...}`
  - `func llm.NewRunner(cmd, model string) *llm.Runner`
  - `func (r *llm.Runner) Run(ctx context.Context, prompt string) (string, error)`
  - `func (r *llm.Runner) HealthCheck(ctx context.Context) error`
  - `func llm.ExtractHTMLBlock(text string) string`

- [ ] **Step 1: Write `internal/llmtest/stub.go` (not a test file — it must be importable from other packages' `_test.go` files)**

```go
// internal/llmtest/stub.go
//
// Package llmtest provides a TestMain re-exec stub-LLM responder shared by
// internal/llm, cmd/ov, and internal/web's temp-vault integration tests
// (design spec §Testing strategy tier 3: "OV_LLM_CMD pointed at a stub
// responder binary (TestMain re-exec)" — exercises the REAL internal/llm
// subprocess transport deterministically instead of mocking exec.Cmd
// away). A test sets OV_LLM_CMD to the currently-running test binary's own
// absolute path (SelfCmd) plus the env vars below (t.Setenv — child
// processes spawned via exec.Command with a nil Env inherit the parent's
// environment, so these propagate automatically); the test binary's own
// TestMain calls MaybeRunStub before m.Run() so a re-exec'd invocation
// short-circuits into stub-responder mode instead of running the real test
// suite.
package llmtest

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	// StubEnv, when "1", switches this binary into stub-LLM-responder mode.
	StubEnv = "OV_TEST_STUB_LLM"
	// ResponseEnv is written verbatim to stdout on a zero-exit-code run.
	ResponseEnv = "OV_TEST_STUB_RESPONSE"
	// ExitCodeEnv overrides the stub's exit code (default 0).
	ExitCodeEnv = "OV_TEST_STUB_EXIT_CODE"
	// StderrEnv is written verbatim to stderr before exit.
	StderrEnv = "OV_TEST_STUB_STDERR"
	// SleepEnv, in milliseconds, delays the stub before it drains stdin and
	// responds — used to exercise job-timeout / process-group-kill tests.
	SleepEnv = "OV_TEST_STUB_SLEEP_MS"
)

// MaybeRunStub checks StubEnv and, if set, drains stdin (mirrors a real LLM
// CLI's stdin-prompt contract), optionally sleeps, writes StderrEnv to
// stderr if set, then exits with ExitCodeEnv (default 0), writing
// ResponseEnv to stdout first only on a zero exit code. Call from TestMain
// BEFORE m.Run(); it never returns when the stub path is taken (it calls
// os.Exit), so callers do not need to branch on the return value except to
// satisfy the compiler in unreachable code after the call.
func MaybeRunStub() bool {
	if os.Getenv(StubEnv) != "1" {
		return false
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	if ms := os.Getenv(SleepEnv); ms != "" {
		if n, err := strconv.Atoi(ms); err == nil {
			time.Sleep(time.Duration(n) * time.Millisecond)
		}
	}
	if stderr := os.Getenv(StderrEnv); stderr != "" {
		fmt.Fprint(os.Stderr, stderr)
	}
	code := 0
	if c := os.Getenv(ExitCodeEnv); c != "" {
		if n, err := strconv.Atoi(c); err == nil {
			code = n
		}
	}
	if code == 0 {
		fmt.Fprint(os.Stdout, os.Getenv(ResponseEnv))
	}
	os.Exit(code)
	return true // unreachable
}

// SelfCmd returns the absolute path of the currently-running test binary —
// the value to put in OV_LLM_CMD (or config's llm_cmd) so llm.Run re-execs
// this same binary in stub mode. os.Args[0] is already the compiled test
// binary's path under `go test`; filepath.Abs is defensive in case a
// caller ever runs tests from a relative-path working directory.
func SelfCmd() (string, error) {
	return filepath.Abs(os.Args[0])
}
```

- [ ] **Step 2: Write `internal/llm/main_test.go` (TestMain for this package's own stub-based tests)**

```go
// internal/llm/main_test.go
package llm

import (
	"os"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llmtest"
)

func TestMain(m *testing.M) {
	if llmtest.MaybeRunStub() {
		return // unreachable: MaybeRunStub calls os.Exit
	}
	os.Exit(m.Run())
}
```

- [ ] **Step 3: Write the failing tests**

```go
// internal/llm/transport_test.go
package llm

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llmtest"
)

func stubCmd(t *testing.T) string {
	t.Helper()
	self, err := llmtest.SelfCmd()
	if err != nil {
		t.Fatal(err)
	}
	return self
}

// CONTRACT(#89): OV_LLM_CMD is shlex-split into argv, never a shell; the
// prompt is delivered on stdin; --model is appended when set.
func TestRunSuccessDeliversPromptAndReturnsStdout(t *testing.T) {
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.ResponseEnv, `{"to": "ok"}`)
	r := NewRunner(stubCmd(t), "")
	got, err := r.Run(context.Background(), "hello prompt")
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"to": "ok"}` {
		t.Errorf("Run() = %q", got)
	}
}

// CONTRACT(#90): missing binary produces a clean, typed error, not a panic
// or an opaque exec error.
func TestRunMissingBinary(t *testing.T) {
	r := NewRunner("this-binary-does-not-exist-anywhere-12345", "")
	_, err := r.Run(context.Background(), "prompt")
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Errorf("err = %v, want ErrBinaryNotFound", err)
	}
}

// DECIDE(new in v2, row #89 companion): an empty OV_LLM_CMD is a clean
// error, not an empty-argv exec.Command panic.
func TestRunEmptyCmd(t *testing.T) {
	r := NewRunner("   ", "")
	_, err := r.Run(context.Background(), "prompt")
	if !errors.Is(err, ErrEmptyCmd) {
		t.Errorf("err = %v, want ErrEmptyCmd", err)
	}
}

// CONTRACT(#90): a non-zero exit is classified with the exit code and
// stderr attached.
func TestRunNonZeroExit(t *testing.T) {
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.ExitCodeEnv, "7")
	t.Setenv(llmtest.StderrEnv, "boom")
	r := NewRunner(stubCmd(t), "")
	_, err := r.Run(context.Background(), "prompt")
	if err == nil || !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "7") {
		t.Errorf("err = %v, want exit code 7 and stderr 'boom'", err)
	}
}

// DECIDE(new in v2, row #147): stderr auth/login markers classify as
// ErrAuth regardless of exit code text.
func TestRunAuthFailureClassified(t *testing.T) {
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.ExitCodeEnv, "1")
	t.Setenv(llmtest.StderrEnv, "Error: not logged in. Please run `claude login`.")
	r := NewRunner(stubCmd(t), "")
	_, err := r.Run(context.Background(), "prompt")
	if !errors.Is(err, ErrAuth) {
		t.Errorf("err = %v, want ErrAuth", err)
	}
}

// BUG(fixed)(#145): a job whose deadline fires kills the WHOLE process
// group, not just the direct child — proven here by a stub that sleeps
// past the deadline; the call must return promptly (bounded by
// WaitDelay), not hang for the stub's full sleep duration.
func TestRunTimeoutKillsPromptly(t *testing.T) {
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.SleepEnv, "5000") // 5s, far longer than the deadline below
	t.Setenv(llmtest.ResponseEnv, "should never be seen")
	r := NewRunner(stubCmd(t), "")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := r.Run(ctx, "prompt")
	elapsed := time.Since(start)
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("err = %v, want ErrTimeout", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("Run took %v after a 200ms deadline — process group was not killed promptly", elapsed)
	}
}

// DECIDE(new in v2, row #146): a third concurrent Run blocks until a slot
// frees rather than spawning unbounded subprocesses.
func TestRunSemaphoreBoundsConcurrency(t *testing.T) {
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.SleepEnv, "300")
	t.Setenv(llmtest.ResponseEnv, "ok")
	r := NewRunner(stubCmd(t), "")

	const n = 4
	start := time.Now()
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			_, _ = r.Run(context.Background(), "prompt")
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
	elapsed := time.Since(start)
	// 4 calls through a semaphore(2), each taking >=300ms, must take at
	// least two full "waves" — a generous floor that would fail if the
	// semaphore did not bound concurrency at all (all 4 running at once
	// would finish in ~300ms total).
	if elapsed < 500*time.Millisecond {
		t.Errorf("elapsed = %v, want >= 500ms (semaphore(2) should force at least 2 waves)", elapsed)
	}
}

// CONTRACT(#143): the LLM subprocess never runs with the vault (or any
// caller-chosen directory) as its CWD — it must be an empty scratch
// directory. Proven via the test-only lastRunDir seam, which runs the
// real transport once and reports the scratch directory it used.
func TestRunUsesScratchCWDNeverCallerDir(t *testing.T) {
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.ResponseEnv, "ok")
	r := NewRunner(stubCmd(t), "")
	cwd, err := r.lastRunDir(context.Background(), "prompt")
	if err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	if cwd == wd {
		t.Errorf("subprocess CWD = %q, must not equal the test process's own CWD %q", cwd, wd)
	}
	if !strings.Contains(cwd, "ov2-llm-") {
		t.Errorf("subprocess CWD = %q, want an ov2-llm-* scratch dir", cwd)
	}
}
```

```go
// internal/llm/classify_test.go
package llm

import (
	"errors"
	"os/exec"
	"testing"
)

// DECIDE(new in v2, row #147): auth/login markers in stderr always
// classify as ErrAuth, independent of the underlying run error.
func TestClassifyAuthMarkers(t *testing.T) {
	cases := []string{
		"Error: not logged in",
		"please run `claude login` to continue",
		"Authentication required",
		"401 Unauthorized",
	}
	for _, stderr := range cases {
		err := Classify(errors.New("exit status 1"), stderr)
		if !errors.Is(err, ErrAuth) {
			t.Errorf("Classify(_, %q) = %v, want ErrAuth", stderr, err)
		}
	}
}

// DECIDE(new in v2, row #147): a plain *exec.ExitError without an auth
// marker classifies as a generic exit-code error, not ErrAuth.
func TestClassifyGenericExitError(t *testing.T) {
	cmd := exec.Command("false")
	runErr := cmd.Run()
	classified := Classify(runErr, "some unrelated stderr")
	if errors.Is(classified, ErrAuth) {
		t.Errorf("Classify unexpectedly matched ErrAuth: %v", classified)
	}
	if classified == nil {
		t.Fatal("expected a non-nil classified error")
	}
}
```

(`false` is present on both macOS and Linux CI images used by this repo; if unavailable in a given environment, `exec.Command("sh", "-c", "exit 1")` is an equally valid substitute — use whichever the implementer confirms runs here.)

```go
// internal/llm/decode_test.go — append at end of file

// CONTRACT(#74): ExtractHTMLBlock returns the <html>...</html> block when
// present (case-insensitive, dotall), else the raw response trimmed.
// Consumed starting phase 4/5 (publish/render); defined now per design
// spec so the decoder contract doesn't need revisiting later.
func TestExtractHTMLBlock(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain block", "<html><body>hi</body></html>", "<html><body>hi</body></html>"},
		{"with prose around it", "Sure!\n<HTML>\n<body>hi</body>\n</HTML>\ndone.", "<HTML>\n<body>hi</body>\n</HTML>"},
		{"no html block", "just some markdown text", "just some markdown text"},
		{"leading/trailing whitespace, no block", "  raw text  \n", "raw text"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ExtractHTMLBlock(c.in); got != c.want {
				t.Errorf("ExtractHTMLBlock(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/llm/... -v`
Expected: FAIL to compile — `Runner`, `NewRunner`, `Classify`, `ErrAuth`, `ErrBinaryNotFound`, `ErrEmptyCmd`, `ErrTimeout`, `ExtractHTMLBlock` undefined.

- [ ] **Step 5: Write the implementation**

```go
// internal/llm/classify.go
package llm

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var (
	// ErrEmptyCmd is returned when the configured LLM command is empty or
	// whitespace-only.
	ErrEmptyCmd = errors.New("OV_LLM_CMD is empty")
	// ErrBinaryNotFound is returned when argv[0] cannot be resolved via
	// exec.LookPath (row #90: "missing binary -> clean error").
	ErrBinaryNotFound = errors.New("llm binary not found in PATH")
	// ErrTimeout is returned when the call's context deadline fired before
	// the subprocess exited.
	ErrTimeout = errors.New("llm call timed out")
	// ErrAuth is returned when stderr indicates an expired/missing login
	// session, so frontends can render a specific, actionable message
	// (row #147) instead of a generic failure.
	ErrAuth = errors.New("llm auth expired — run `claude login` on the Mac")
)

// authMarkers are case-insensitive stderr substrings from `claude --print`
// and similar CLIs indicating an auth/login problem rather than a generic
// failure (row #147).
var authMarkers = []string{
	"not logged in",
	"please run",
	"claude login",
	"authentication",
	"unauthorized",
	"401",
}

// Classify turns a failed subprocess run (any error from exec.Cmd.Run, or
// context.DeadlineExceeded) plus its captured stderr into a typed error a
// frontend can render specifically. v1 raised one generic RuntimeError
// carrying stderr text (triage_llm.py:271-274); the web UI needs to tell
// "auth expired" apart from every other failure.
func Classify(runErr error, stderr string) error {
	lower := strings.ToLower(stderr)
	for _, m := range authMarkers {
		if strings.Contains(lower, m) {
			return fmt.Errorf("%w: %s", ErrAuth, strings.TrimSpace(stderr))
		}
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return fmt.Errorf("llm exited %d: %s", exitErr.ExitCode(), strings.TrimSpace(stderr))
	}
	if runErr != nil {
		return runErr
	}
	return fmt.Errorf("llm call failed: %s", strings.TrimSpace(stderr))
}
```

```go
// internal/llm/transport.go
package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Config is the narrow subset of ov config the llm package needs — kept
// separate from internal/config.Config (design spec's "stateless verbs"
// principle, same pattern as capture.CaptureConfig / triage.Config).
type Config struct {
	Cmd   string // OV_LLM_CMD, e.g. "claude --print"
	Model string // optional; appended as "--model <Model>" when non-empty
}

// maxConcurrentRuns bounds simultaneous LLM subprocesses (design spec
// §LLM subsystem "Job model": semaphore of 2; row #146).
const maxConcurrentRuns = 2

// Runner executes the configured LLM command over subprocess transport,
// bounding concurrent subprocesses to a fixed semaphore. One Runner is
// constructed per process (cmd/ov's triage command, internal/web's
// server) and reused across every call so the semaphore is shared.
type Runner struct {
	cmd   string
	model string
	sem   chan struct{}
}

// NewRunner builds a Runner around the resolved OV_LLM_CMD/OV_MODEL
// values (internal/config.Config.LLMCmd/Model, already precedence-merged
// by config.Load — Runner never reads the environment itself, avoiding
// the row #82-class bug of two independent config readers disagreeing).
func NewRunner(cmd, model string) *Runner {
	return &Runner{cmd: cmd, model: model, sem: make(chan struct{}, maxConcurrentRuns)}
}

// Run invokes the configured LLM command with prompt on stdin and returns
// its stdout. argv is built via shlexSplit — never a shell (row #89, also
// the mechanism that retires publish's `eval`, row #72, in a later
// phase). argv[0] is resolved to an absolute path via exec.LookPath before
// every invocation (row #144). The subprocess's CWD is a freshly created,
// empty scratch directory — never the vault (row #143, prompt injection
// posture). It runs in its own process group; on ctx cancellation or
// deadline the WHOLE group is killed, not just the direct child, because
// `claude` spawns a node subprocess tree a direct-child-only kill would
// orphan (row #145). Concurrent calls are bounded by the semaphore
// (row #146). Exit code + stderr are classified via Classify (row #147).
func (r *Runner) Run(ctx context.Context, prompt string) (string, error) {
	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-r.sem }()
	return runOnce(ctx, r.cmd, r.model, prompt)
}

// lastRunDir is a test-only seam: it runs the real transport once and
// returns the scratch directory used, so tests can assert the subprocess
// never ran with the vault (or the test process's own CWD) as its
// working directory, without parsing stdout for a marker.
func (r *Runner) lastRunDir(ctx context.Context, prompt string) (string, error) {
	scratch, err := os.MkdirTemp("", "ov2-llm-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	if _, err := runInDir(ctx, r.cmd, r.model, prompt, scratch); err != nil {
		return "", err
	}
	return scratch, nil
}

func runOnce(ctx context.Context, cmdStr, model, prompt string) (string, error) {
	scratch, err := os.MkdirTemp("", "ov2-llm-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	return runInDir(ctx, cmdStr, model, prompt, scratch)
}

func runInDir(ctx context.Context, cmdStr, model, prompt, dir string) (string, error) {
	argv, err := shlexSplit(cmdStr)
	if err != nil {
		return "", err
	}
	if len(argv) == 0 {
		return "", ErrEmptyCmd
	}
	if model != "" {
		argv = append(argv, "--model", model)
	}
	absPath, err := exec.LookPath(argv[0])
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrBinaryNotFound, argv[0])
	}

	cmd := exec.CommandContext(ctx, absPath, argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // whole process group, row #145
	}

	runErr := cmd.Run()
	if runErr != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%w: %s", ErrTimeout, ctx.Err())
		}
		return "", Classify(runErr, stderr.String())
	}
	return stdout.String(), nil
}

// shlexSplit is a minimal shell-word splitter: whitespace-separated
// tokens, with single- and double-quoted spans preserved as one token
// (quotes stripped, no further interpretation — no glob/variable
// expansion, this is NOT a shell). Mirrors python shlex.split's behavior
// for the simple `claude --print` / `pi --print -nc -nt --mode json`
// style commands OV_LLM_CMD actually holds (row #89).
func shlexSplit(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	var inQuote rune
	hasToken := false
	for _, r := range s {
		switch {
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			inQuote = r
			hasToken = true
		case r == ' ' || r == '\t' || r == '\n':
			if hasToken {
				out = append(out, cur.String())
				cur.Reset()
				hasToken = false
			}
		default:
			cur.WriteRune(r)
			hasToken = true
		}
	}
	if inQuote != 0 {
		return nil, errors.New("unterminated quote in OV_LLM_CMD")
	}
	if hasToken {
		out = append(out, cur.String())
	}
	return out, nil
}

// HealthCheck attempts a minimal, short-timeout LLM invocation purely to
// surface configuration/auth problems before a user starts a real 120s
// triage call (row #149). Returns nil only if the subprocess actually ran
// and exited 0.
func (r *Runner) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := r.Run(ctx, "Reply with the single word: ok")
	return err
}
```

```go
// internal/llm/decode.go — add at end of existing file (do not modify
// ExtractJSON or jsonFenceRe, which shipped in phase 0)

var htmlBlockRe = regexp.MustCompile(`(?is)<html.*?>.*?</html>`)

// ExtractHTMLBlock ports vault.sh publish's HTML cleanup (row #74): when
// the LLM response contains an <html>...</html> block (case-insensitive,
// dotall), return exactly that span, trimmed; otherwise return the raw
// response, trimmed. Consumed starting phase 4/5 (publish/render); the
// contract is defined now per design spec so it doesn't need revisiting.
func ExtractHTMLBlock(text string) string {
	if m := htmlBlockRe.FindString(text); m != "" {
		return strings.TrimSpace(m)
	}
	return strings.TrimSpace(text)
}
```

Add `github.com/sergi/go-diff v1.4.0` to `go.mod`'s `require` block now (it has no direct consumer until Task 4, but is pinned per Global Constraints):

```bash
go get github.com/sergi/go-diff@v1.4.0
go mod tidy
```

Update `examples/ov.config.example` — append a documented settings-profile recommendation (row #143: this is a docs-only defense, never a code-injected flag, since `OV_LLM_CMD` must stay provider-agnostic):

```
# SECURITY: triage --llm interpolates note bodies and previously-fetched
# URL titles into the LLM prompt. Those are attacker-influenced (a
# malicious clipped page or a crafted inbox note). ov2 always runs the LLM
# subprocess with its CWD set to an empty scratch directory (never your
# vault) — but tool access is provider-specific and ov2 cannot inject
# provider-specific flags into OV_LLM_CMD without breaking it for other
# providers. If your provider supports disabling tool/file access for a
# single invocation, point OV_LLM_CMD at a wrapper or settings profile
# that does so. For Claude Code, consider a dedicated settings file with
# tool permissions denied and reference it via --settings, or a shell
# wrapper script that passes the equivalent flag for your installed
# version — check `claude --help` for the current flag name.
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/llm/... -v -race`
Expected: PASS, all new tests green, phase 0's `TestExtractJSON*` still green (unchanged).

- [ ] **Step 7: Commit**

```bash
git add internal/llmtest internal/llm go.mod go.sum examples/ov.config.example
git commit -m "Add hardened LLM subprocess transport, failure classification, health check, ExtractHTMLBlock"
```

---

### Task 3: `internal/llm` — generic job manager

The async submit/poll/result table shared by the web layer's htmx polling and available to the TUI (design spec §LLM subsystem "Job model", row #148). Kept generic (`JobManager[T]`) so it can back both a raw-string health-check-style job and a `triage.Proposal` job without `internal/llm` importing `internal/triage`.

**Files:**
- Create: `internal/llm/job.go`
- Create: `internal/llm/job_test.go`

**Interfaces:**
- Consumes: nothing from Task 2 directly (independent of `Runner`; a job's `fn` closure is supplied by the caller, e.g. wrapping `triage.Propose`)
- Produces:
  - `type llm.Status string` with `llm.StatusPending`, `llm.StatusDone`, `llm.StatusFailed`
  - `type llm.Job[T any] struct{ ID string; Status Status; Result T; Err error }`
  - `type llm.JobManager[T any] struct{...}`
  - `func llm.NewJobManager[T any]() *llm.JobManager[T]`
  - `func (jm *llm.JobManager[T]) Submit(ctx context.Context, fn func(context.Context) (T, error)) string`
  - `func (jm *llm.JobManager[T]) Get(id string) (llm.Job[T], bool)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/llm/job_test.go
package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

// DECIDE(new in v2, row #148): Submit returns immediately with a job id;
// the function runs asynchronously.
func TestJobManagerSubmitReturnsImmediately(t *testing.T) {
	jm := NewJobManager[string]()
	started := make(chan struct{})
	release := make(chan struct{})
	id := jm.Submit(context.Background(), func(ctx context.Context) (string, error) {
		close(started)
		<-release
		return "done", nil
	})
	if id == "" {
		t.Fatal("expected a non-empty job id")
	}
	<-started // proves the function actually started running
	job, ok := jm.Get(id)
	if !ok {
		t.Fatal("expected the job to be trackable immediately after Submit")
	}
	if job.Status != StatusPending {
		t.Errorf("Status = %v, want StatusPending", job.Status)
	}
	close(release)
}

// DECIDE(new in v2, row #148): once the function returns, status flips to
// Done and Result is populated — "submit -> job id; status polled; result
// swapped in".
func TestJobManagerPollUntilDone(t *testing.T) {
	jm := NewJobManager[string]()
	id := jm.Submit(context.Background(), func(ctx context.Context) (string, error) {
		return "the-result", nil
	})
	deadline := time.Now().Add(2 * time.Second)
	var job Job[string]
	for time.Now().Before(deadline) {
		j, ok := jm.Get(id)
		if !ok {
			t.Fatal("job disappeared")
		}
		job = j
		if job.Status != StatusPending {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if job.Status != StatusDone {
		t.Fatalf("Status = %v, want StatusDone", job.Status)
	}
	if job.Result != "the-result" {
		t.Errorf("Result = %q", job.Result)
	}
	if job.Err != nil {
		t.Errorf("Err = %v, want nil", job.Err)
	}
}

// A failed job function surfaces StatusFailed and the error, never panics
// or leaves the job stuck pending.
func TestJobManagerPollUntilFailed(t *testing.T) {
	jm := NewJobManager[string]()
	wantErr := errors.New("boom")
	id := jm.Submit(context.Background(), func(ctx context.Context) (string, error) {
		return "", wantErr
	})
	deadline := time.Now().Add(2 * time.Second)
	var job Job[string]
	for time.Now().Before(deadline) {
		j, _ := jm.Get(id)
		job = j
		if job.Status != StatusPending {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if job.Status != StatusFailed {
		t.Fatalf("Status = %v, want StatusFailed", job.Status)
	}
	if !errors.Is(job.Err, wantErr) {
		t.Errorf("Err = %v, want %v", job.Err, wantErr)
	}
}

// Get on an unknown id reports ok=false, never a zero-value success.
func TestJobManagerGetUnknownID(t *testing.T) {
	jm := NewJobManager[string]()
	if _, ok := jm.Get("nonexistent"); ok {
		t.Error("expected ok=false for an unknown job id")
	}
}

// Two concurrent jobs get distinct ids and don't clobber each other's
// state (basic concurrency-safety smoke test; run with -race).
func TestJobManagerConcurrentJobsIndependent(t *testing.T) {
	jm := NewJobManager[int]()
	id1 := jm.Submit(context.Background(), func(ctx context.Context) (int, error) { return 1, nil })
	id2 := jm.Submit(context.Background(), func(ctx context.Context) (int, error) { return 2, nil })
	if id1 == id2 {
		t.Fatal("expected distinct job ids")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j1, _ := jm.Get(id1)
		j2, _ := jm.Get(id2)
		if j1.Status == StatusDone && j2.Status == StatusDone {
			if j1.Result != 1 || j2.Result != 2 {
				t.Errorf("results crossed: j1=%d j2=%d", j1.Result, j2.Result)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("jobs did not both complete in time")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/llm/... -run TestJobManager -v`
Expected: FAIL to compile — `Status`, `Job`, `JobManager`, `NewJobManager` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/llm/job.go
package llm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// Status is a Job's lifecycle state.
type Status string

const (
	StatusPending Status = "pending"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// Job is a snapshot of one asynchronous unit of work. Get returns a copy,
// safe to read after the call without holding any lock.
type Job[T any] struct {
	ID     string
	Status Status
	Result T
	Err    error
}

// JobManager is a generic, goroutine-based submit/poll/result table
// (design spec §LLM subsystem "Job model", row #148): LLM calls take up
// to 120s (triage) / 180s (moc cleanup, phase 4) — a synchronous HTTP
// handler dies on mobile Safari over Tailscale. Submit runs fn in a new
// goroutine and returns immediately with a job id; Get polls the current
// status. No callbacks, no UI types — generic over the result type so it
// backs both a raw LLM string job and a typed triage.Proposal job without
// internal/llm importing internal/triage (design spec's "stateless verbs"
// invariant).
type JobManager[T any] struct {
	mu   sync.Mutex
	jobs map[string]*Job[T]
}

func NewJobManager[T any]() *JobManager[T] {
	return &JobManager[T]{jobs: make(map[string]*Job[T])}
}

// Submit runs fn(ctx) in a new goroutine under a fresh job id, tracks its
// status, and returns the id immediately. ctx governs fn's own
// cancellation/deadline (e.g. a 120s triage timeout) — it is intentionally
// NOT tied to any HTTP request context, since a job must keep running
// after the request that started it returns (the whole point of the
// job model).
func (jm *JobManager[T]) Submit(ctx context.Context, fn func(context.Context) (T, error)) string {
	id := newJobID()
	job := &Job[T]{ID: id, Status: StatusPending}
	jm.mu.Lock()
	jm.jobs[id] = job
	jm.mu.Unlock()

	go func() {
		result, err := fn(ctx)
		jm.mu.Lock()
		defer jm.mu.Unlock()
		job.Result = result
		job.Err = err
		if err != nil {
			job.Status = StatusFailed
		} else {
			job.Status = StatusDone
		}
	}()

	return id
}

// Get returns a copy of the job's current state, or ok=false if id is
// unknown.
func (jm *JobManager[T]) Get(id string) (Job[T], bool) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	j, ok := jm.jobs[id]
	if !ok {
		return Job[T]{}, false
	}
	return *j, true
}

func newJobID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/llm/... -v -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/llm/job.go internal/llm/job_test.go
git commit -m "Add generic llm.JobManager for async submit/poll/result"
```

---

### Task 4: `internal/triage` — Proposal type, prompt assembly, `Propose`, `Validate`, diff

The read-half of the triage core: the AGENTS.md §7 schema, the ported prompt template, the LLM call + JSON decode into a typed `Proposal`, and the safety-gate `Validate` function that closes the v1 `body_patch`/`links_to_add` hole. `Apply` (the write-half) is Task 5 — kept separate because it is the security-sensitive mutation path and warrants its own focused review.

**Files:**
- Create: `internal/triage/proposal.go`
- Create: `internal/triage/config.go`
- Create: `internal/triage/prompt.go`
- Create: `internal/triage/prompt_test.go`
- Create: `internal/triage/propose.go`
- Create: `internal/triage/propose_test.go`
- Create: `internal/triage/validate.go`
- Create: `internal/triage/validate_test.go`
- Create: `internal/triage/diff.go`
- Create: `internal/triage/diff_test.go`
- Create: `internal/triage/testdata/prompt_golden.txt` (golden file, Step 3b)

**Interfaces:**
- Consumes: `vault.Note`, `vault.Frontmatter`, `vault.ParseNote`, `vault.ContainPath`, `vault.DiscoverFolders`, `vault.ReadNote` (phase 0/1/2, `internal/vault`); `llm.ExtractJSON` (phase 0, `internal/llm`)
- Produces:
  - `package triage` (new package, `github.com/arosenkranz/obsidian-vault-tools/internal/triage`)
  - `type triage.Proposal struct{ From, To, NewTitle string; FrontmatterPatch map[string]any; BodyPatch *string; LinksToAdd []string; Rationale, Confidence string }` — JSON tags `from`/`to`/`new_title`/`frontmatter_patch`/`body_patch`/`links_to_add`/`rationale`/`confidence`, matching AGENTS.md §7 exactly.
  - `type triage.Config struct{ VaultDir, Inbox, Projects, Areas, Resources, Archive string }`
  - `func (c triage.Config) ParaRoots() []string`
  - `type triage.Runner interface{ Run(ctx context.Context, prompt string) (string, error) }` (matches `*llm.Runner`'s method set; tests inject a fake)
  - `func triage.BuildPrompt(folders []string, fromPath, agentsMD string, fm *vault.Frontmatter, body string) string`
  - `func triage.Propose(ctx context.Context, cfg Config, note vault.Note, agentsMD string, runner Runner) (Proposal, error)`
  - `var triage.ErrBodyPatchRejected, triage.ErrLinksToAddRejected, triage.ErrMissingTo, triage.ErrTargetNotParaRoot error`
  - `func triage.Validate(cfg Config, p Proposal) error`
  - `type triage.DiffLine struct{ Op byte; Text string }`
  - `func triage.Diff(old, new string) []DiffLine`

- [ ] **Step 1: Write the failing tests**

```go
// internal/triage/validate_test.go
package triage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	vaultDir := t.TempDir()
	for _, d := range []string{"00-Inbox", "01-Projects", "02-Areas", "03-Resources", "04-Archive", "99-Meta"} {
		if err := os.MkdirAll(filepath.Join(vaultDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return Config{
		VaultDir:  vaultDir,
		Inbox:     "00-Inbox",
		Projects:  "01-Projects",
		Areas:     "02-Areas",
		Resources: "03-Resources",
		Archive:   "04-Archive",
	}
}

func validProposal() Proposal {
	return Proposal{
		From:             "00-Inbox/2026-05-14 0830 thought.md",
		To:               "02-Areas/Local LLM Notes.md",
		NewTitle:         "Local LLM Notes",
		FrontmatterPatch: map[string]any{"type": "learning"},
		BodyPatch:        nil,
		LinksToAdd:       nil,
		Rationale:        "fits areas/learning",
		Confidence:       "high",
	}
}

// CONTRACT(#97): a well-formed proposal targeting a configured PARA root
// passes.
func TestValidateAccepts(t *testing.T) {
	cfg := testConfig(t)
	if err := Validate(cfg, validProposal()); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

// BUG(fixed)(#5): the headline safety fix — a non-null body_patch is
// REJECTED, never silently applied. v1's prompt forbade this but
// apply_proposal honored it anyway and the approval display never showed
// it.
func TestValidateRejectsBodyPatch(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	patch := "rewritten body content"
	p.BodyPatch = &patch
	err := Validate(cfg, p)
	if !errors.Is(err, ErrBodyPatchRejected) {
		t.Errorf("Validate() = %v, want ErrBodyPatchRejected", err)
	}
}

// BUG(fixed)(#5): a non-empty links_to_add is REJECTED.
func TestValidateRejectsLinksToAdd(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	p.LinksToAdd = []string{"[[MOC Local LLM]]"}
	err := Validate(cfg, p)
	if !errors.Is(err, ErrLinksToAddRejected) {
		t.Errorf("Validate() = %v, want ErrLinksToAddRejected", err)
	}
}

// CONTRACT(#97): the PARA-root gate is a SEMANTIC check layered on top of
// vault.ContainPath's pure filesystem containment — a path can stay
// inside the vault (99-Meta is a real, contained folder) yet still fail
// this check because it isn't a configured PARA root or the inbox.
func TestValidateRejectsNonParaRootTarget(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	p.To = "99-Meta/evil.md"
	err := Validate(cfg, p)
	if !errors.Is(err, ErrTargetNotParaRoot) {
		t.Errorf("Validate() = %v, want ErrTargetNotParaRoot", err)
	}
}

// BUG(fixed)(#6, #130): a target escaping the vault entirely (traversal)
// is rejected via vault.ContainPath, surfaced through Validate.
func TestValidateRejectsEscapingTarget(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	p.To = "../../etc/passwd"
	if err := Validate(cfg, p); err == nil {
		t.Fatal("expected rejection of an escaping target")
	}
}

// CONTRACT(#97): the inbox itself is an allowed target first-component
// (a proposal can leave a note in the inbox, e.g. after only a
// frontmatter-only tidy — mirrors v1's PARA_ROOTS + [inbox_name]).
func TestValidateAllowsInboxTarget(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	p.To = "00-Inbox/Still Undecided.md"
	if err := Validate(cfg, p); err != nil {
		t.Errorf("Validate() = %v, want nil (inbox is an allowed root)", err)
	}
}

func TestValidateRejectsMissingTo(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	p.To = ""
	if err := Validate(cfg, p); !errors.Is(err, ErrMissingTo) {
		t.Errorf("Validate() = %v, want ErrMissingTo", err)
	}
}
```

```go
// internal/triage/prompt_test.go
package triage

import (
	"os"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// Golden file (design spec §Testing strategy tier 2: "both LLM prompt
// assemblies (prompt drift = silent behavior change)"). Update the golden
// file deliberately (never silently) when the prompt text intentionally
// changes: `UPDATE_GOLDEN=1 go test ./internal/triage/... -run TestBuildPromptGolden`.
func TestBuildPromptGolden(t *testing.T) {
	content := "---\ntype: inbox\ncreated: 2026-05-14\nsource: cli\n---\nSome raw capture text.\n"
	fm, body := vault.ParseNote(content)
	folders := []string{"01-Projects", "02-Areas", "02-Areas/Learning", "03-Resources"}
	got := BuildPrompt(folders, "00-Inbox/2026-05-14 0830 thought.md", "=== fake AGENTS.md contents ===\n", fm, body)

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

// CONTRACT(#91): folders are rendered one per line with a "  - " prefix,
// matching v1's PROMPT_TEMPLATE folder rendering.
func TestBuildPromptFoldersFormatted(t *testing.T) {
	fm, body := vault.ParseNote("no frontmatter here")
	got := BuildPrompt([]string{"01-Projects", "02-Areas"}, "00-Inbox/x.md", "AGENTS", fm, body)
	if !stringsContains(got, "  - 01-Projects\n  - 02-Areas") {
		t.Errorf("prompt did not contain formatted folder list:\n%s", got)
	}
}

// CONTRACT: an empty body renders as the literal placeholder v1 used.
func TestBuildPromptEmptyBodyPlaceholder(t *testing.T) {
	fm, body := vault.ParseNote("---\ntype: inbox\n---\n   \n")
	got := BuildPrompt(nil, "00-Inbox/x.md", "AGENTS", fm, body)
	if !stringsContains(got, "(empty body)") {
		t.Errorf("expected the empty-body placeholder, got:\n%s", got)
	}
}

func stringsContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return len(needle) == 0
}
```

```go
// internal/triage/propose_test.go
package triage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
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

func writeTestNote(t *testing.T, dir, name, content string) vault.Note {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return vault.Note{Path: p, Rel: "00-Inbox/" + name, Name: name[:len(name)-3]}
}

// CONTRACT(#89, #92): Propose builds a prompt, calls the runner, and
// decodes the JSON response into a typed Proposal via the phase 0
// llm.ExtractJSON 3-tier fallback.
func TestProposeDecodesResponse(t *testing.T) {
	cfg := testConfig(t)
	note := writeTestNote(t, filepath.Join(cfg.VaultDir, "00-Inbox"), "2026-05-14 0830 thought.md",
		"---\ntype: inbox\ncreated: 2026-05-14\n---\nSome idea.\n")
	runner := &fakeRunner{response: `{"from":"00-Inbox/2026-05-14 0830 thought.md","to":"02-Areas/Idea.md","new_title":"Idea","frontmatter_patch":{"type":"note"},"body_patch":null,"links_to_add":[],"rationale":"fits","confidence":"high"}`}
	p, err := Propose(context.Background(), cfg, note, "AGENTS.md contents", runner)
	if err != nil {
		t.Fatal(err)
	}
	if p.To != "02-Areas/Idea.md" || p.NewTitle != "Idea" || p.Confidence != "high" {
		t.Errorf("Proposal = %+v", p)
	}
	if p.BodyPatch != nil {
		t.Errorf("BodyPatch = %v, want nil", p.BodyPatch)
	}
}

// CONTRACT(#92): a fenced-JSON response still decodes (ExtractJSON tier 2).
func TestProposeDecodesFencedResponse(t *testing.T) {
	cfg := testConfig(t)
	note := writeTestNote(t, filepath.Join(cfg.VaultDir, "00-Inbox"), "x.md", "---\ntype: inbox\n---\nbody\n")
	runner := &fakeRunner{response: "Here you go:\n```json\n{\"to\":\"02-Areas/X.md\",\"new_title\":\"X\",\"confidence\":\"low\",\"rationale\":\"r\"}\n```\n"}
	p, err := Propose(context.Background(), cfg, note, "AGENTS", runner)
	if err != nil {
		t.Fatal(err)
	}
	if p.To != "02-Areas/X.md" {
		t.Errorf("To = %q", p.To)
	}
}

// A runner failure (e.g. llm.ErrAuth) propagates unchanged so the caller
// (cmd/ov, internal/web) can classify it.
func TestProposePropagatesRunnerError(t *testing.T) {
	cfg := testConfig(t)
	note := writeTestNote(t, filepath.Join(cfg.VaultDir, "00-Inbox"), "x.md", "---\ntype: inbox\n---\nbody\n")
	wantErr := errors.New("llm auth expired")
	runner := &fakeRunner{err: wantErr}
	_, err := Propose(context.Background(), cfg, note, "AGENTS", runner)
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

// A response with no parseable JSON object propagates a decode error, not
// a zero-value Proposal masquerading as success.
func TestProposeDecodeFailure(t *testing.T) {
	cfg := testConfig(t)
	note := writeTestNote(t, filepath.Join(cfg.VaultDir, "00-Inbox"), "x.md", "---\ntype: inbox\n---\nbody\n")
	runner := &fakeRunner{response: "sorry, I can't help with that"}
	_, err := Propose(context.Background(), cfg, note, "AGENTS", runner)
	if err == nil {
		t.Fatal("expected a decode error")
	}
}
```

```go
// internal/triage/diff_test.go
package triage

import "testing"

// CONTRACT(#151, new in v2): line-level diff, one DiffLine per line,
// tagged '+'/'-'/' '.
func TestDiffAddedAndRemovedLines(t *testing.T) {
	old := "line one\nline two\nline three\n"
	updated := "line one\nline TWO changed\nline three\nline four\n"
	lines := Diff(old, updated)

	var got []string
	for _, l := range lines {
		got = append(got, string(l.Op)+l.Text)
	}
	// Every original line must be accounted for as context or removed, and
	// every new line as context or added — assert presence rather than
	// exact ordering/grouping, which is an implementation detail of the
	// underlying line-diff algorithm.
	mustContain := []string{" line one", "-line two", "+line TWO changed", " line three", "+line four"}
	for _, want := range mustContain {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Diff() missing expected line %q; got %v", want, got)
		}
	}
}

// Identical input produces only context lines, no +/- lines.
func TestDiffIdenticalInputNoChanges(t *testing.T) {
	content := "same\ncontent\n"
	lines := Diff(content, content)
	for _, l := range lines {
		if l.Op != ' ' {
			t.Errorf("unexpected change on identical input: %+v", l)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/triage/... -v`
Expected: FAIL to compile — package `internal/triage` does not exist yet.

- [ ] **Step 3: Write the implementation**

```go
// internal/triage/proposal.go
package triage

// Proposal matches AGENTS.md §7 exactly. JSON tags are the wire contract
// the LLM prompt asks for and internal/llm.ExtractJSON decodes into.
type Proposal struct {
	From             string         `json:"from"`
	To               string         `json:"to"`
	NewTitle         string         `json:"new_title"`
	FrontmatterPatch map[string]any `json:"frontmatter_patch"`
	// BodyPatch is a pointer so JSON `null` and an absent key both decode
	// to nil — Validate rejects any non-nil value regardless of which
	// form the LLM used (row #5).
	BodyPatch  *string  `json:"body_patch"`
	LinksToAdd []string `json:"links_to_add"`
	Rationale  string   `json:"rationale"`
	Confidence string   `json:"confidence"`
}
```

```go
// internal/triage/config.go
package triage

// Config is the narrow subset of ov config the triage package needs —
// kept separate from internal/config.Config (design spec's "stateless
// verbs" principle, same pattern as capture.CaptureConfig / llm.Config).
type Config struct {
	VaultDir  string
	Inbox     string
	Projects  string
	Areas     string
	Resources string
	Archive   string
}

// ParaRoots returns the four configured PARA root folder names.
func (c Config) ParaRoots() []string {
	return []string{c.Projects, c.Areas, c.Resources, c.Archive}
}
```

```go
// internal/triage/prompt.go
package triage

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

const promptTemplate = `You are a triage assistant for a PARA-organized Obsidian vault. Your job is to propose where ONE inbox note should be filed.

Read the AGENTS.md contract below, then produce exactly one JSON object matching the schema in §7.

Output rules:
- Output ONLY the JSON object. No prose. No markdown fences. No commentary before or after.
- This is v1 of triage. Set "links_to_add" to []. Do NOT propose wikilinks.
- Set "body_patch" to null. Do NOT rewrite the body.
- "to" must be a path under one of these existing folders. You may include a new filename inside an existing folder, but do NOT invent new folders:
%s
- "from" must equal: %s
- "new_title" must follow the naming conventions in §3.
- "frontmatter_patch" should upgrade the note from inbox: set type, status, tags. Do NOT include created/modified — the script handles those.
- "confidence" must be one of: high, medium, low.
- "rationale" is one or two sentences.

=== AGENTS.md ===
%s

=== NOTE TO TRIAGE ===
path: %s
current_frontmatter: %s
body:
---BEGIN BODY---
%s
---END BODY---

Return the JSON object now.`

// BuildPrompt assembles the triage prompt for one note, mirroring
// triage_llm.py's PROMPT_TEMPLATE (row #91). current_frontmatter is
// rendered from Frontmatter.Pairs() (the lenient raw-value view, row #85)
// — informational text for the LLM to read, never parsed back, so it does
// not need to match Python's json.dumps(dict) typing exactly (a list-typed
// value like `tags: [a, b]` renders as the string "[a, b]" rather than a
// JSON array — acceptable since this field is prompt context, not a
// machine-parsed response).
func BuildPrompt(folders []string, fromPath, agentsMD string, fm *vault.Frontmatter, body string) string {
	folderLines := make([]string, len(folders))
	for i, f := range folders {
		folderLines[i] = "  - " + f
	}
	fmJSON, _ := json.Marshal(fm.Pairs())
	b := strings.TrimSpace(body)
	if b == "" {
		b = "(empty body)"
	}
	return fmt.Sprintf(promptTemplate, strings.Join(folderLines, "\n"), fromPath, agentsMD, fromPath, string(fmJSON), b)
}
```

- [ ] **Step 3b: Generate the golden file, then verify it**

```bash
UPDATE_GOLDEN=1 go test ./internal/triage/... -run TestBuildPromptGolden -v
git status internal/triage/testdata/prompt_golden.txt   # confirm the file was created
go test ./internal/triage/... -run TestBuildPromptGolden -v   # re-run WITHOUT UPDATE_GOLDEN — must pass
```

```go
// internal/triage/propose.go
package triage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// Runner is the subset of *llm.Runner's method set Propose needs — an
// interface so tests inject a fake without spawning a real subprocess
// (mirrors capture.TitleFetcher's injection pattern). *llm.Runner
// satisfies this interface as-is.
type Runner interface {
	Run(ctx context.Context, prompt string) (string, error)
}

// Propose builds the AGENTS.md §7 triage prompt for note (reading its
// current content fresh from disk), asks the LLM for a filing proposal,
// and decodes the response into a typed Proposal via
// llm.ExtractJSON's 3-tier fallback (row #92). agentsMD is the vault's
// AGENTS.md content, read once by the caller and reused across notes
// (the caller also owns the exit-2-on-missing-AGENTS.md precondition,
// row #104). folders come from vault.DiscoverFolders (depth<=2, the same
// folder list used for phase-1's LLM-facing discovery, row #87).
func Propose(ctx context.Context, cfg Config, note vault.Note, agentsMD string, runner Runner) (Proposal, error) {
	content, _, err := vault.ReadNote(note.Path)
	if err != nil {
		return Proposal{}, err
	}
	fm, body := vault.ParseNote(content)
	folders := vault.DiscoverFolders(cfg.VaultDir, cfg.ParaRoots())
	prompt := BuildPrompt(folders, note.Rel, agentsMD, fm, body)

	raw, err := runner.Run(ctx, prompt)
	if err != nil {
		return Proposal{}, err
	}
	obj, err := llm.ExtractJSON(raw)
	if err != nil {
		return Proposal{}, err
	}
	buf, err := json.Marshal(obj)
	if err != nil {
		return Proposal{}, fmt.Errorf("triage: re-marshaling decoded JSON: %w", err)
	}
	var p Proposal
	if err := json.Unmarshal(buf, &p); err != nil {
		return Proposal{}, fmt.Errorf("triage: proposal did not match the expected schema: %w", err)
	}
	return p, nil
}
```

```go
// internal/triage/validate.go
package triage

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

var (
	// ErrBodyPatchRejected is the headline v2 safety fix (row #5): v1's
	// prompt forbade a non-null body_patch, but apply_proposal honored one
	// anyway and the approval display never showed it. v2 rejects it in
	// code, unconditionally.
	ErrBodyPatchRejected = errors.New("triage: body_patch must be null (row #5 enforcement)")
	// ErrLinksToAddRejected is the companion half of row #5.
	ErrLinksToAddRejected = errors.New("triage: links_to_add must be empty (row #5 enforcement)")
	ErrMissingTo          = errors.New("triage: proposal missing 'to'")
	// ErrTargetNotParaRoot is the PARA-root gate (row #97): the target's
	// first vault-relative path component must be a configured PARA root
	// or the inbox, layered on top of vault.ContainPath's pure filesystem
	// containment.
	ErrTargetNotParaRoot = errors.New("triage: target is not inside a configured PARA root or the inbox")
)

// Validate rejects a proposal that violates a hard safety gate — enforced
// in code, never trusted from the LLM's own output (design spec §Safety
// item 1, §LLM subsystem "Prompt injection posture"). Every gate here is
// re-run by Apply against fresh disk content at write time; Validate
// itself never touches the filesystem for writes, only resolves p.To
// through vault.ContainPath for the containment check.
func Validate(cfg Config, p Proposal) error {
	if p.BodyPatch != nil {
		return ErrBodyPatchRejected
	}
	if len(p.LinksToAdd) > 0 {
		return ErrLinksToAddRejected
	}
	if strings.TrimSpace(p.To) == "" {
		return ErrMissingTo
	}
	targetAbs, err := vault.ContainPath(cfg.VaultDir, p.To)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(cfg.VaultDir, targetAbs)
	if err != nil {
		return err
	}
	first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	allowed := append(append([]string{}, cfg.ParaRoots()...), cfg.Inbox)
	for _, a := range allowed {
		if first == a {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrTargetNotParaRoot, p.To)
}
```

```go
// internal/triage/diff.go
package triage

import (
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// DiffLine is one line of a unified, line-level diff: Op is '+' (added),
// '-' (removed), or ' ' (context); Text is the line content without a
// trailing newline.
type DiffLine struct {
	Op   byte
	Text string
}

// Diff renders a unified, line-level diff between old and new note
// content (the note before this proposal's frontmatter/move and after).
// Presentation-free — internal/tui colorizes for the terminal,
// internal/web escapes into an HTML diff card (design spec: phase 3 needs
// a diff view for triage approval in both TUI and web; row #151).
func Diff(old, new string) []DiffLine {
	dmp := diffmatchpatch.New()
	a, b, lines := dmp.DiffLinesToChars(old, new)
	diffs := dmp.DiffMain(a, b, false)
	diffs = dmp.DiffCharsToLines(diffs, lines)

	var out []DiffLine
	for _, d := range diffs {
		text := strings.TrimSuffix(d.Text, "\n")
		if text == "" {
			continue
		}
		var op byte = ' '
		switch d.Type {
		case diffmatchpatch.DiffInsert:
			op = '+'
		case diffmatchpatch.DiffDelete:
			op = '-'
		}
		for _, line := range strings.Split(text, "\n") {
			out = append(out, DiffLine{Op: op, Text: line})
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/triage/... -v -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/triage go.mod go.sum
git commit -m "Add internal/triage: Proposal schema, prompt assembly, Propose, Validate, Diff"
```

---

### Task 5: `internal/triage` — `Apply` (frontmatter merge, write, MOC rename sync, dry-run)

The write half: the AGENTS.md §7 field rules (row #100), target filename resolution (row #98), existing-target refusal (row #99, mechanically enforced by `WriteNoteAtomic`'s `ErrExists`), and best-effort MOC rename sync (rows #94-96) built on Task 1's `vault.RenameMOCLink`. `Apply` calls `Validate` itself first (defense in depth) and supports a `dryRun` flag so the CLI and web diff-confirm screens preview the exact write `Apply` would perform.

**Files:**
- Create: `internal/triage/apply.go`
- Create: `internal/triage/apply_test.go`
- Create: `internal/triage/mocsync.go`
- Create: `internal/triage/mocsync_test.go`

**Interfaces:**
- Consumes: `Proposal`, `Config`, `Validate`, `ErrBodyPatchRejected`, `ErrTargetNotParaRoot` (Task 4); `vault.Note`, `vault.ReadNote`, `vault.ParseNote`, `vault.ContainPath`, `vault.Slugify`, `vault.WriteNoteAtomic`, `vault.ErrExists`, `vault.FindMOCByName`, `vault.RenameMOCLink`, `vault.NewFrontmatter` (phase 0-2, Task 1)
- Produces:
  - `type triage.Result struct{ Target string; Content string; MOCSynced bool; MOCWarning string }`
  - `func triage.Apply(cfg Config, note vault.Note, p Proposal, now time.Time, dryRun bool) (Result, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/triage/apply_test.go
package triage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

func writeInboxNote(t *testing.T, cfg Config, name, content string) vault.Note {
	t.Helper()
	p := filepath.Join(cfg.VaultDir, cfg.Inbox, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return vault.Note{Path: p, Rel: cfg.Inbox + "/" + name, Name: name[:len(name)-3]}
}

var fixedNow = time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

// CONTRACT(#98, #100): a real apply moves the note, upgrades frontmatter
// (type: inbox dropped, patch applied except created/modified which stay
// script-owned), and writes the new heading/body unchanged (body_patch is
// nil, enforced by Validate inside Apply).
func TestApplyMovesAndMergesFrontmatter(t *testing.T) {
	cfg := testConfig(t)
	note := writeInboxNote(t, cfg, "2026-05-14 0830 thought.md",
		"---\ntype: inbox\ncreated: 2026-05-14\nsource: cli\n---\n# thought\n\nSome idea.\n")
	p := Proposal{
		To:               "02-Areas/Local LLM Notes.md",
		NewTitle:         "Local LLM Notes",
		FrontmatterPatch: map[string]any{"type": "learning", "status": "active"},
		Confidence:       "high",
		Rationale:        "fits",
	}
	res, err := Apply(cfg, note, p, fixedNow, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Target != "02-Areas/Local LLM Notes.md" {
		t.Errorf("Target = %q", res.Target)
	}
	if _, err := os.Stat(note.Path); !os.IsNotExist(err) {
		t.Error("source note should be removed after a real apply")
	}
	got, err := os.ReadFile(filepath.Join(cfg.VaultDir, "02-Areas", "Local LLM Notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(got)
	if stringsContains(content, "type: inbox") {
		t.Error("type: inbox should have been dropped")
	}
	if !stringsContains(content, "type: learning") || !stringsContains(content, "status: active") {
		t.Errorf("frontmatter patch not applied:\n%s", content)
	}
	if !stringsContains(content, "created: 2026-05-14") {
		t.Errorf("created should be preserved from the original note:\n%s", content)
	}
	if !stringsContains(content, "modified: 2026-07-15") {
		t.Errorf("modified should be today (fixedNow):\n%s", content)
	}
	if !stringsContains(content, "Some idea.") {
		t.Errorf("body should be unchanged:\n%s", content)
	}
}

// CONTRACT: --dry-run performs every resolution/merge step and returns
// the would-be Result (including rendered Content for diff display)
// without touching the filesystem.
func TestApplyDryRunWritesNothing(t *testing.T) {
	cfg := testConfig(t)
	note := writeInboxNote(t, cfg, "x.md", "---\ntype: inbox\ncreated: 2026-05-14\n---\nbody\n")
	p := Proposal{To: "02-Areas/X.md", NewTitle: "X", FrontmatterPatch: map[string]any{"type": "note"}}
	res, err := Apply(cfg, note, p, fixedNow, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Target != "02-Areas/X.md" {
		t.Errorf("Target = %q", res.Target)
	}
	if res.Content == "" || !stringsContains(res.Content, "type: note") {
		t.Errorf("dry-run Content not populated correctly: %q", res.Content)
	}
	if _, err := os.Stat(note.Path); err != nil {
		t.Error("dry-run must not remove the source note")
	}
	if _, err := os.Stat(filepath.Join(cfg.VaultDir, "02-Areas", "X.md")); !os.IsNotExist(err) {
		t.Error("dry-run must not write the target note")
	}
}

// BUG(fixed)(#5): Apply refuses a body_patch even if a caller forgot to
// call Validate first — the gate lives inside Apply too (defense in
// depth). Neither the source nor a target file is touched.
func TestApplyRefusesBodyPatchWithoutExternalValidate(t *testing.T) {
	cfg := testConfig(t)
	note := writeInboxNote(t, cfg, "x.md", "---\ntype: inbox\n---\noriginal body\n")
	patch := "INJECTED CONTENT"
	p := Proposal{To: "02-Areas/X.md", NewTitle: "X", BodyPatch: &patch}
	_, err := Apply(cfg, note, p, fixedNow, false)
	if !errors.Is(err, ErrBodyPatchRejected) {
		t.Fatalf("err = %v, want ErrBodyPatchRejected", err)
	}
	if _, statErr := os.Stat(note.Path); statErr != nil {
		t.Error("source note must remain untouched on rejection")
	}
	if _, statErr := os.Stat(filepath.Join(cfg.VaultDir, "02-Areas", "X.md")); !os.IsNotExist(statErr) {
		t.Error("target must never be written when body_patch is rejected")
	}
	if content, _ := os.ReadFile(note.Path); stringsContains(string(content), "INJECTED CONTENT") {
		t.Error("injected content must never reach disk")
	}
}

// CONTRACT(#99): an existing target file is refused, never overwritten —
// mechanically enforced by WriteNoteAtomic's ErrExists (write_test.go
// already covers the primitive; this proves Apply routes through it).
func TestApplyRefusesExistingTarget(t *testing.T) {
	cfg := testConfig(t)
	note := writeInboxNote(t, cfg, "x.md", "---\ntype: inbox\n---\nbody\n")
	if err := os.WriteFile(filepath.Join(cfg.VaultDir, "02-Areas", "X.md"), []byte("already here"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Proposal{To: "02-Areas/X.md", NewTitle: "X"}
	_, err := Apply(cfg, note, p, fixedNow, false)
	if !errors.Is(err, vault.ErrExists) {
		t.Fatalf("err = %v, want vault.ErrExists", err)
	}
	if _, statErr := os.Stat(note.Path); statErr != nil {
		t.Error("source note must remain when the target already exists")
	}
}

// CONTRACT(#97): Apply itself also enforces the PARA-root gate (not just
// Validate) — a proposal that somehow bypassed an external Validate call
// is still refused.
func TestApplyRefusesNonParaRootTarget(t *testing.T) {
	cfg := testConfig(t)
	note := writeInboxNote(t, cfg, "x.md", "---\ntype: inbox\n---\nbody\n")
	p := Proposal{To: "99-Meta/evil.md", NewTitle: "evil"}
	_, err := Apply(cfg, note, p, fixedNow, false)
	if !errors.Is(err, ErrTargetNotParaRoot) {
		t.Fatalf("err = %v, want ErrTargetNotParaRoot", err)
	}
}

// CONTRACT(#98): a "to" with no filename (directory-style, trailing "/")
// derives its filename from the slugified new_title.
func TestApplyDirectoryTargetDerivesFilename(t *testing.T) {
	cfg := testConfig(t)
	note := writeInboxNote(t, cfg, "x.md", "---\ntype: inbox\n---\nbody\n")
	p := Proposal{To: "02-Areas/", NewTitle: "My New Title"}
	res, err := Apply(cfg, note, p, fixedNow, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Target != "02-Areas/My New Title.md" {
		t.Errorf("Target = %q", res.Target)
	}
}

// CONTRACT(#94-96): when the title changes and the note was linked from a
// MOC at capture time (moc: frontmatter), Apply syncs that MOC's
// [[old]] -> [[new]] entry, best-effort.
func TestApplySyncsMOCLinkOnRename(t *testing.T) {
	cfg := testConfig(t)
	mocPath := filepath.Join(cfg.VaultDir, cfg.Resources, "MOC Music.md")
	if err := os.WriteFile(mocPath, []byte("# MOC Music\n\n- [[Old Thought]] — a tune\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	note := writeInboxNote(t, cfg, "Old Thought.md", "---\ntype: inbox\nmoc: [[MOC Music]]\n---\nbody\n")
	p := Proposal{To: "02-Areas/", NewTitle: "New Thought"}
	res, err := Apply(cfg, note, p, fixedNow, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.MOCSynced {
		t.Error("expected MOCSynced=true")
	}
	mocContent, err := os.ReadFile(mocPath)
	if err != nil {
		t.Fatal(err)
	}
	if !stringsContains(string(mocContent), "[[New Thought]]") || stringsContains(string(mocContent), "[[Old Thought]]") {
		t.Errorf("MOC not synced: %s", mocContent)
	}
}

// CONTRACT(#94): a missing MOC file is a warning, never an abort — the
// completed move is not rolled back.
func TestApplyMOCSyncMissingMOCWarnsNeverAborts(t *testing.T) {
	cfg := testConfig(t)
	note := writeInboxNote(t, cfg, "Old Thought.md", "---\ntype: inbox\nmoc: [[MOC Nonexistent]]\n---\nbody\n")
	p := Proposal{To: "02-Areas/", NewTitle: "New Thought"}
	res, err := Apply(cfg, note, p, fixedNow, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.MOCWarning == "" {
		t.Error("expected a MOCWarning")
	}
	if _, statErr := os.Stat(filepath.Join(cfg.VaultDir, "02-Areas", "New Thought.md")); statErr != nil {
		t.Error("the move must have completed despite the MOC sync failure")
	}
}

// A title that does not change never touches any MOC (row #94: "runs only
// when the title changed").
func TestApplyNoMOCSyncWhenTitleUnchanged(t *testing.T) {
	cfg := testConfig(t)
	mocPath := filepath.Join(cfg.VaultDir, cfg.Resources, "MOC Music.md")
	original := "# MOC Music\n\n- [[Same Title]] — x\n"
	if err := os.WriteFile(mocPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	note := writeInboxNote(t, cfg, "Same Title.md", "---\ntype: inbox\nmoc: [[MOC Music]]\n---\nbody\n")
	p := Proposal{To: "02-Areas/", NewTitle: "Same Title"}
	res, err := Apply(cfg, note, p, fixedNow, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.MOCSynced {
		t.Error("expected no MOC sync when the title did not change")
	}
	got, _ := os.ReadFile(mocPath)
	if string(got) != original {
		t.Errorf("MOC was modified despite an unchanged title: %s", got)
	}
}
```

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/triage/... -run 'TestApply|TestExtractMOCName' -v`
Expected: FAIL to compile — `Apply`, `Result`, `extractMOCName` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/triage/mocsync.go
package triage

import (
	"fmt"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// extractMOCName pulls a bare MOC name (e.g. "MOC Music") out of a note's
// moc: frontmatter field, however it happens to have been parsed. Mirrors
// triage_llm.py extract_moc_name_from_frontmatter (row #4's documented
// quirk): `moc: [[MOC Music]]` round-trips through Frontmatter.GetList's
// bracket-list heuristic as a one-item list whose element is the string
// "[MOC Music]" (the outer brackets are consumed as the list delimiter,
// the inner pair survives). This unwraps that quirk as well as a plain
// scalar value.
func extractMOCName(fm *vault.Frontmatter) string {
	if list, ok := fm.GetList("moc"); ok {
		if len(list) == 0 {
			return ""
		}
		return strings.Trim(strings.TrimSpace(list[0]), "[]")
	}
	if raw, ok := fm.Get("moc"); ok {
		return strings.Trim(strings.TrimSpace(raw), "[]")
	}
	return ""
}

// syncMOCLink is best-effort: if note was linked from a MOC at capture
// time and triage just renamed it, update that MOC's entry so the link
// keeps resolving instead of silently breaking. Never returns an error —
// any failure is reported as a warning string, and the caller (Apply) has
// already completed the file move by the time this runs, which must never
// be rolled back for a MOC-sync failure (row #94).
func syncMOCLink(cfg Config, fm *vault.Frontmatter, oldTitle, newTitle string) (synced bool, warning string) {
	if oldTitle == newTitle {
		return false, ""
	}
	mocName := extractMOCName(fm)
	if mocName == "" {
		return false, ""
	}
	moc, err := vault.FindMOCByName(cfg.VaultDir, cfg.Resources, mocName)
	if err != nil {
		return false, fmt.Sprintf("note was linked from %q but that MOC file wasn't found — link not updated, check manually", mocName)
	}
	content, hash, err := vault.ReadNote(moc.Path)
	if err != nil {
		return false, fmt.Sprintf("failed to read MOC %q: %v", mocName, err)
	}
	newContent, changed := vault.RenameMOCLink(content, oldTitle, newTitle)
	if !changed {
		return false, fmt.Sprintf("note was linked from %q but no matching [[%s]] entry was found there — check manually", mocName, oldTitle)
	}
	if err := vault.WriteNoteAtomic(moc.Path, []byte(newContent), hash); err != nil {
		return false, fmt.Sprintf("failed to update MOC %q link after rename: %v", mocName, err)
	}
	return true, ""
}
```

```go
// internal/triage/apply.go
package triage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// Result reports what Apply did (or, under dryRun, would do).
type Result struct {
	// Target is the vault-relative path the note was (or would be)
	// written to.
	Target string
	// Content is the full rendered note content Apply wrote (or would
	// write) at Target — populated in both real and dry-run modes so
	// callers can render a diff against the note's original content
	// (design spec: diff-confirm before any apply, row #151).
	Content string
	// MOCSynced reports whether a MOC entry's wikilink title was renamed
	// (rows #94-96). Never set under dryRun (no filesystem write occurs).
	MOCSynced bool
	// MOCWarning is set when a title change should have synced a MOC
	// entry but couldn't (missing MOC, missing entry, or a write
	// failure) — never fatal (row #94).
	MOCWarning string
}

// Apply merges the proposal's frontmatter_patch into the note per
// AGENTS.md §7 field rules (row #100), resolves and writes the target
// (rows #97-99), removes the source, and best-effort syncs the source
// MOC's wikilink title (rows #94-96). Apply calls Validate itself as its
// first step — defense in depth: it refuses unsafe input even if a
// caller forgot to call Validate first (design spec §Safety item 1).
// Content is always read fresh from note.Path (never a snapshot Propose
// captured earlier) so a concurrent Obsidian Sync edit is reflected,
// per design spec's conditional-write principle. dryRun performs every
// resolution/merge step and returns the would-be Result without touching
// the filesystem — the CLI's --dry-run and both frontends' diff-confirm
// screens call Apply(..., dryRun=true) to preview exactly what the real
// apply (dryRun=false) would do, using the identical code path.
func Apply(cfg Config, note vault.Note, p Proposal, now time.Time, dryRun bool) (Result, error) {
	if err := Validate(cfg, p); err != nil {
		return Result{}, err
	}

	content, _, err := vault.ReadNote(note.Path)
	if err != nil {
		return Result{}, err
	}
	fm, body := vault.ParseNote(content)
	oldTitle := strings.TrimSuffix(filepath.Base(note.Path), ".md")

	targetAbs, err := vault.ContainPath(cfg.VaultDir, p.To)
	if err != nil {
		return Result{}, err
	}

	newTitle := strings.TrimSpace(p.NewTitle)
	if newTitle == "" {
		newTitle = oldTitle
	} else {
		newTitle = vault.Slugify(newTitle, 80)
	}

	// Directory-style target (trailing "/" or an existing directory) gets
	// a filename derived from the slugified new_title (row #98).
	isDirTarget := strings.HasSuffix(p.To, "/")
	if !isDirTarget {
		if info, statErr := os.Stat(targetAbs); statErr == nil && info.IsDir() {
			isDirTarget = true
		}
	}
	if isDirTarget {
		targetAbs = filepath.Join(targetAbs, newTitle+".md")
	} else if filepath.Ext(targetAbs) == "" {
		targetAbs += ".md"
	}

	rel, err := filepath.Rel(cfg.VaultDir, targetAbs)
	if err != nil {
		return Result{}, err
	}
	relSlash := filepath.ToSlash(rel)

	newFM := mergeFrontmatter(fm, p.FrontmatterPatch, now)
	// body_patch is guaranteed nil by Validate above — body stays as-is.
	newContent := newFM.Render() + strings.TrimPrefix(body, "\n")

	res := Result{Target: relSlash, Content: newContent}

	if dryRun {
		return res, nil
	}

	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
		return Result{}, err
	}
	if err := vault.WriteNoteAtomic(targetAbs, []byte(newContent), ""); err != nil {
		return Result{}, err
	}
	if err := os.Remove(note.Path); err != nil {
		return Result{}, fmt.Errorf("note written to %s but the source could not be removed: %w", relSlash, err)
	}

	synced, warning := syncMOCLink(cfg, fm, oldTitle, newTitle)
	res.MOCSynced = synced
	res.MOCWarning = warning

	return res, nil
}

// mergeFrontmatter applies AGENTS.md §7's frontmatter merge rule (row
// #100): type: inbox is dropped; the patch is applied to every key except
// created/modified (script-owned); created is preserved if already
// present, else set to now; modified is always set to now. Patch keys are
// applied in sorted order so multiple newly-introduced keys append to the
// frontmatter block deterministically (Go map iteration order is
// randomized; the frontmatter_patch field itself decodes from JSON into
// an unordered map[string]any).
func mergeFrontmatter(fm *vault.Frontmatter, patch map[string]any, now time.Time) *vault.Frontmatter {
	if fm == nil {
		fm = vault.NewFrontmatter()
	}
	if v, ok := fm.Get("type"); ok && v == "inbox" {
		fm.Delete("type")
	}
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "created" || k == "modified" {
			continue
		}
		fm.Set(k, renderPatchValue(patch[k]))
	}
	if _, ok := fm.Get("created"); !ok {
		fm.Set("created", now.Format("2006-01-02"))
	}
	fm.Set("modified", now.Format("2006-01-02"))
	return fm
}

// renderPatchValue renders one frontmatter_patch JSON value into the raw
// scalar/flow-list text Frontmatter.Set expects, mirroring
// triage_llm.py's _fm_line list handling.
func renderPatchValue(v any) string {
	if list, ok := v.([]any); ok {
		strs := make([]string, 0, len(list))
		for _, e := range list {
			strs = append(strs, fmt.Sprint(e))
		}
		return "[" + strings.Join(strs, ", ") + "]"
	}
	return fmt.Sprint(v)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/triage/... -v -race`
Expected: PASS, full package green.

- [ ] **Step 5: Commit**

```bash
git add internal/triage/apply.go internal/triage/apply_test.go internal/triage/mocsync.go internal/triage/mocsync_test.go
git commit -m "Add triage.Apply: frontmatter merge, write, MOC rename sync, dry-run"
```

---

### Task 6: `internal/tui` — diff rendering

A small, presentation-only addition: colorize `triage.DiffLine`s for the terminal, consumed by Task 7's CLI approval loop.

**Files:**
- Create: `internal/tui/diff.go`
- Create: `internal/tui/diff_test.go`

**Interfaces:**
- Consumes: `triage.DiffLine` (Task 4, `internal/triage`)
- Produces: `func tui.RenderDiff(lines []triage.DiffLine) string`

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/diff_test.go
package tui

import (
	"strings"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/triage"
)

// CONTRACT(#151): each diff line is rendered with a leading marker
// matching its Op, so a plain-text terminal (color stripped) still shows
// unambiguous +/- markers.
func TestRenderDiffMarkers(t *testing.T) {
	lines := []triage.DiffLine{
		{Op: ' ', Text: "unchanged"},
		{Op: '+', Text: "added"},
		{Op: '-', Text: "removed"},
	}
	got := RenderDiff(lines)
	for _, want := range []string{"unchanged", "+ added", "- removed"} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderDiff() missing %q; got:\n%s", want, got)
		}
	}
}

func TestRenderDiffEmpty(t *testing.T) {
	if got := RenderDiff(nil); got != "" {
		t.Errorf("RenderDiff(nil) = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/... -run TestRenderDiff -v`
Expected: FAIL to compile — `RenderDiff` undefined.

- [ ] **Step 3: Write the implementation**

```go
// internal/tui/diff.go
package tui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/arosenkranz/obsidian-vault-tools/internal/triage"
)

var (
	diffAddStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))  // green
	diffDelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // red
	diffCtxStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // dim
)

// RenderDiff colorizes triage.DiffLines for terminal display: green "+ "
// lines, red "- " lines, dim unmarked context lines. Presentation-only —
// internal/triage owns the diff computation (design spec: shared diff
// data, two presentations, row #151).
func RenderDiff(lines []triage.DiffLine) string {
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch l.Op {
		case '+':
			b.WriteString(diffAddStyle.Render("+ " + l.Text))
		case '-':
			b.WriteString(diffDelStyle.Render("- " + l.Text))
		default:
			b.WriteString(diffCtxStyle.Render("  " + l.Text))
		}
	}
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/... -v`
Expected: PASS, full package green.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/diff.go internal/tui/diff_test.go
git commit -m "Add tui.RenderDiff for triage approval's terminal diff view"
```

---

### Task 7: `cmd/ov triage --llm` — interactive approval loop, `--dry-run`, `--limit`, stub-LLM integration tests

Extends the EXISTING `ov2 triage` command (row #79: one binary, `--llm` is a flag on the single triage command) with the LLM path: exit-2 preconditions (row #104), the a/e/s/d/r/q approval loop (row #102) with a diff+confirm view before any apply, `--dry-run`, and `--limit`. Shares the container command and `resolveConfig` with the existing manual path. `cmd/ov/main.go` currently maps every returned error to exit code 1 unconditionally (verified: no existing exit-code-by-error-type mechanism) — this task adds the minimal recognition needed for row #104's exit-2 divergence.

**Files:**
- Modify: `cmd/ov/triage.go` (add `--llm`/`--dry-run`/`--limit` flags, `runLLMTriage`, `llmTriageDeps`, `errExitCode2`, `editProposalFields`)
- Modify: `cmd/ov/triage_test.go` (add LLM-mode tests using a fake `triage.Runner`)
- Modify: `cmd/ov/main.go` (recognize `errExitCode2` and exit 2)
- Create: `cmd/ov/triage_llm_integration_test.go` (stub-subprocess integration tests, design spec §Testing strategy tier 3)
- Create: `cmd/ov/main_test.go` (`TestMain` re-exec wiring)

**Interfaces:**
- Consumes: `resolveConfig` (phase 1, `cmd/ov/common.go`); `vault.ListInbox`, `vault.ReadNote` (phase 0/1); `triage.Config`, `triage.Proposal`, `triage.Runner`, `triage.Propose`, `triage.Validate`, `triage.Apply`, `triage.Result`, `triage.Diff` (Task 4/5); `llm.NewRunner`, `llm.ErrAuth`, `llm.ErrBinaryNotFound`, `llm.ErrTimeout` (Task 2); `tui.RenderDiff`, `tui.Confirm` (Task 6, phase 2); `llmtest.SelfCmd`, `llmtest.MaybeRunStub` and its env consts (Task 2)
- Produces:
  - `func newTriageCmd() *cobra.Command` — gains flags `--llm`, `--dry-run`, `--limit N` alongside the existing `--vault`
  - `var errExitCode2 error`
  - `type llmTriageDeps struct{ runner triage.Runner; confirm func(string) (bool, error); agentsMD string }`
  - `func runLLMTriage(cfg *config.Config, in *bufio.Reader, out, errw io.Writer, deps llmTriageDeps, dryRun bool, limit int) error`

- [ ] **Step 1: Write `cmd/ov/main_test.go`**

```go
// cmd/ov/main_test.go
package main

import (
	"os"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llmtest"
)

func TestMain(m *testing.M) {
	if llmtest.MaybeRunStub() {
		return // unreachable: MaybeRunStub calls os.Exit
	}
	os.Exit(m.Run())
}
```

- [ ] **Step 2: Write the failing unit tests (fake `triage.Runner`, no subprocess)**

```go
// cmd/ov/triage_test.go — append at end of existing file; add "context",
// "errors" (if not already imported), and
// "github.com/arosenkranz/obsidian-vault-tools/internal/llm" to the
// import block.

// fakeTriageRunner is a triage.Runner that returns a canned response,
// used by the fast unit tests below (no subprocess). Integration tests in
// triage_llm_integration_test.go use the real stub-subprocess path
// instead.
type fakeTriageRunner struct {
	response string
	err      error
}

func (f *fakeTriageRunner) Run(ctx context.Context, prompt string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

func writeLLMTestFixture(t *testing.T, vaultDir, noteName string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(vaultDir, "AGENTS.md"), []byte("AGENTS.md contract text"), 0o644); err != nil {
		t.Fatal(err)
	}
	addNote(t, vaultDir, "00-Inbox/"+noteName, "---\ntype: inbox\ncreated: 2026-05-14\n---\nSome idea.\n", 0)
}

// CONTRACT(#104): --llm returns an error whose exit code is 2 when
// AGENTS.md is missing at the vault root (checked before any note is
// processed).
func TestLLMTriageMissingAgentsMDExits2(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/x.md", "---\ntype: inbox\n---\nbody\n", 0)
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf bytes.Buffer
	err = runLLMTriage(cfg, bufio.NewReader(strings.NewReader("")), io.Discard, &errBuf, llmTriageDeps{runner: &fakeTriageRunner{}}, false, 0)
	if !errors.Is(err, errExitCode2) {
		t.Errorf("err = %v, want errExitCode2", err)
	}
}

// CONTRACT(#104): the same exit-2 precondition applies to a missing inbox
// directory.
func TestLLMTriageMissingInboxExits2(t *testing.T) {
	vaultDir := t.TempDir()
	clearOVEnv(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "AGENTS.md"), []byte("contract"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("vault_dir = \""+vaultDir+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OV_CONFIG", cfgPath)
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	err = runLLMTriage(cfg, bufio.NewReader(strings.NewReader("")), io.Discard, io.Discard, llmTriageDeps{runner: &fakeTriageRunner{}}, false, 0)
	if !errors.Is(err, errExitCode2) {
		t.Errorf("err = %v, want errExitCode2", err)
	}
}

// CONTRACT(#104): --limit N caps the number of notes processed.
func TestLLMTriageLimit(t *testing.T) {
	vaultDir := newVaultFixture(t)
	writeLLMTestFixture(t, vaultDir, "2026-05-14 0800 First.md")
	addNote(t, vaultDir, "00-Inbox/2026-05-14 0801 Second.md", "---\ntype: inbox\n---\nSecond.\n", 0)
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeTriageRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"low","rationale":"r"}`}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, confirm: func(string) (bool, error) { return true, nil }, agentsMD: "AGENTS"}
	if err := runLLMTriage(cfg, bufio.NewReader(strings.NewReader("s\ns\n")), io.Discard, &errBuf, deps, true, 1); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errBuf.String(), "Second") {
		t.Errorf("expected only the first note processed under --limit 1:\n%s", errBuf.String())
	}
}

// CONTRACT(#104): --dry-run shows every proposal and moves nothing.
func TestLLMTriageDryRunWritesNothing(t *testing.T) {
	vaultDir := newVaultFixture(t)
	writeLLMTestFixture(t, vaultDir, "2026-05-14 0800 First.md")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeTriageRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"low","rationale":"r"}`}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "AGENTS"}
	if err := runLLMTriage(cfg, bufio.NewReader(strings.NewReader("")), io.Discard, &errBuf, deps, true, 0); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-05-14 0800 First.md")); statErr != nil {
		t.Error("dry-run must leave the source note in place")
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "02-Areas", "X.md")); !os.IsNotExist(statErr) {
		t.Error("dry-run must not write a target note")
	}
	if !strings.Contains(errBuf.String(), "02-Areas/X.md") {
		t.Errorf("dry-run output should show the proposed target:\n%s", errBuf.String())
	}
}

// CONTRACT(#102): 'a' approves and applies the proposal.
func TestLLMTriageApprove(t *testing.T) {
	vaultDir := newVaultFixture(t)
	writeLLMTestFixture(t, vaultDir, "2026-05-14 0800 First.md")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeTriageRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"high","rationale":"r"}`}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "AGENTS"}
	if err := runLLMTriage(cfg, bufio.NewReader(strings.NewReader("a\n")), io.Discard, &errBuf, deps, false, 0); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "02-Areas", "X.md")); statErr != nil {
		t.Error("approved proposal should have been written")
	}
}

// BUG(fixed)(#5): even in the interactive loop, a body_patch-carrying
// proposal is rejected before any write, and the rejection is visible to
// the user (not silently skipped).
func TestLLMTriageRejectsBodyPatchProposal(t *testing.T) {
	vaultDir := newVaultFixture(t)
	writeLLMTestFixture(t, vaultDir, "2026-05-14 0800 First.md")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeTriageRunner{response: `{"to":"02-Areas/X.md","new_title":"X","body_patch":"INJECTED","confidence":"high","rationale":"r"}`}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "AGENTS"}
	_ = runLLMTriage(cfg, bufio.NewReader(strings.NewReader("")), io.Discard, &errBuf, deps, false, 0)
	if !strings.Contains(errBuf.String(), "body_patch") {
		t.Errorf("rejection reason should be visible in output:\n%s", errBuf.String())
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "02-Areas", "X.md")); !os.IsNotExist(statErr) {
		t.Error("a rejected proposal must never be written")
	}
	if content, _ := os.ReadFile(filepath.Join(vaultDir, "00-Inbox", "2026-05-14 0800 First.md")); strings.Contains(string(content), "INJECTED") {
		t.Error("injected content must never reach disk")
	}
}

// CONTRACT(#102): 'q' quits immediately.
func TestLLMTriageQuit(t *testing.T) {
	vaultDir := newVaultFixture(t)
	writeLLMTestFixture(t, vaultDir, "2026-05-14 0800 First.md")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeTriageRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"high","rationale":"r"}`}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "AGENTS"}
	if err := runLLMTriage(cfg, bufio.NewReader(strings.NewReader("q\n")), io.Discard, &errBuf, deps, false, 0); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-05-14 0800 First.md")); statErr != nil {
		t.Error("quitting must leave the pending note untouched")
	}
}

// DECIDE(new in v2, row #147): an auth-classified runner error surfaces a
// specific, actionable message, not a generic failure string.
func TestLLMTriageAuthFailureMessage(t *testing.T) {
	vaultDir := newVaultFixture(t)
	writeLLMTestFixture(t, vaultDir, "2026-05-14 0800 First.md")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeTriageRunner{err: fmt.Errorf("%w: not logged in", llm.ErrAuth)}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "AGENTS"}
	_ = runLLMTriage(cfg, bufio.NewReader(strings.NewReader("q\n")), io.Discard, &errBuf, deps, false, 0)
	if !strings.Contains(errBuf.String(), "auth expired") {
		t.Errorf("expected an auth-specific message, got:\n%s", errBuf.String())
	}
}
```

Add `"context"`, `"fmt"` (if not already present — `triage.go`'s existing test file already imports `os`/`path/filepath`/`strings`/`testing`/`bufio`/`bytes`), and `"github.com/arosenkranz/obsidian-vault-tools/internal/llm"` to `triage_test.go`'s import block.

- [ ] **Step 3: Write the failing stub-subprocess integration tests**

```go
// cmd/ov/triage_llm_integration_test.go
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
	"github.com/arosenkranz/obsidian-vault-tools/internal/llmtest"
)

// Temp-vault integration tests (design spec §Testing strategy tier 3):
// OV_LLM_CMD points at this same test binary, re-executed in stub mode via
// internal/llmtest — proving the REAL internal/llm subprocess transport,
// not a fake.

func newLLMIntegrationRunner(t *testing.T, response string, exitCode int, stderr string) *llm.Runner {
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

// CONTRACT: triage approve → move + MOC rename sync, exercised through the
// REAL subprocess transport end to end.
func TestLLMTriageIntegrationApproveMovesAndSyncsMOC(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "AGENTS.md"), []byte("AGENTS.md contract"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"), []byte("# MOC Music\n\n- [[First]] — a tune\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	addNote(t, vaultDir, "00-Inbox/First.md", "---\ntype: inbox\nmoc: [[MOC Music]]\ncreated: 2026-05-14\n---\nbody\n", 0)

	runner := newLLMIntegrationRunner(t, `{"to":"02-Areas/Second.md","new_title":"Second","frontmatter_patch":{"type":"note"},"confidence":"high","rationale":"r"}`, 0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "AGENTS.md contract"}
	if err := runLLMTriage(cfg, bufio.NewReader(strings.NewReader("a\n")), os.Stdout, &errBuf, deps, false, 0); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "02-Areas", "Second.md")); statErr != nil {
		t.Fatal("expected the note to be moved")
	}
	mocContent, err := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mocContent), "[[Second]]") {
		t.Errorf("MOC not synced via the real transport: %s", mocContent)
	}
}

// BUG(fixed)(#5): body_patch rejection through the real transport — the
// note never touches disk with injected content even when the LLM
// actually returns one.
func TestLLMTriageIntegrationRejectsBodyPatchFromRealTransport(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "AGENTS.md"), []byte("contract"), 0o644); err != nil {
		t.Fatal(err)
	}
	addNote(t, vaultDir, "00-Inbox/First.md", "---\ntype: inbox\n---\noriginal\n", 0)
	runner := newLLMIntegrationRunner(t, `{"to":"02-Areas/X.md","new_title":"X","body_patch":"INJECTED VIA REAL LLM","confidence":"high","rationale":"r"}`, 0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "contract"}
	_ = runLLMTriage(cfg, bufio.NewReader(strings.NewReader("")), os.Stdout, &errBuf, deps, false, 0)
	if content, _ := os.ReadFile(filepath.Join(vaultDir, "00-Inbox", "First.md")); strings.Contains(string(content), "INJECTED") {
		t.Fatal("injected content reached disk")
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "02-Areas", "X.md")); !os.IsNotExist(statErr) {
		t.Fatal("target should never have been written")
	}
}

// CONTRACT(#97): PARA-root rejection through the real transport.
func TestLLMTriageIntegrationRejectsNonParaRootFromRealTransport(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "AGENTS.md"), []byte("contract"), 0o644); err != nil {
		t.Fatal(err)
	}
	addNote(t, vaultDir, "00-Inbox/First.md", "---\ntype: inbox\n---\nbody\n", 0)
	runner := newLLMIntegrationRunner(t, `{"to":"99-Meta/evil.md","new_title":"evil","confidence":"high","rationale":"r"}`, 0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "contract"}
	_ = runLLMTriage(cfg, bufio.NewReader(strings.NewReader("")), os.Stdout, &errBuf, deps, false, 0)
	if !strings.Contains(errBuf.String(), "PARA") && !strings.Contains(errBuf.String(), "target is not inside") {
		t.Errorf("expected a PARA-root rejection message, got:\n%s", errBuf.String())
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "99-Meta", "evil.md")); !os.IsNotExist(statErr) {
		t.Fatal("target should never have been written")
	}
}

// BUG(fixed)(#145): job timeout kills the whole process group — no
// orphaned child process survives (proven at the internal/llm layer by
// TestRunTimeoutKillsPromptly). This test proves the CLI-facing runner,
// constructed exactly as cmd/ov constructs it in production, surfaces
// llm.ErrTimeout through the real subprocess transport.
func TestLLMTriageIntegrationTimeoutClassified(t *testing.T) {
	self, err := llmtest.SelfCmd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.SleepEnv, "3000")
	runner := llm.NewRunner(self, "")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = runner.Run(ctx, "prompt")
	if !errors.Is(err, llm.ErrTimeout) {
		t.Errorf("err = %v, want llm.ErrTimeout", err)
	}
}

// DECIDE(new in v2, row #149): the health-check endpoint's underlying
// mechanism (a minimal real invocation) succeeds through the real
// transport when the stub responds normally.
func TestLLMTriageIntegrationHealthCheckSucceeds(t *testing.T) {
	runner := newLLMIntegrationRunner(t, "ok", 0, "")
	if err := runner.HealthCheck(context.Background()); err != nil {
		t.Errorf("HealthCheck() = %v, want nil", err)
	}
}

// DECIDE(new in v2, row #147): the health check surfaces an auth failure
// through the real transport too.
func TestLLMTriageIntegrationHealthCheckAuthFailure(t *testing.T) {
	runner := newLLMIntegrationRunner(t, "", 1, "Error: not logged in")
	err := runner.HealthCheck(context.Background())
	if !errors.Is(err, llm.ErrAuth) {
		t.Errorf("HealthCheck() = %v, want llm.ErrAuth", err)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./cmd/ov/... -run 'TestLLMTriage' -v`
Expected: FAIL to compile — `runLLMTriage`, `llmTriageDeps`, `errExitCode2` undefined.

- [ ] **Step 5: Modify `cmd/ov/main.go` to recognize `errExitCode2`**

```go
// cmd/ov/main.go
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ov2:", err)
		if errors.Is(err, errExitCode2) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
```

- [ ] **Step 6: Implement `errExitCode2`, `llmTriageDeps`, `editProposalFields`, `runLLMTriage` in `cmd/ov/triage.go`**

Add to `cmd/ov/triage.go`'s import block: `"context"`, `"time"`, `"github.com/arosenkranz/obsidian-vault-tools/internal/llm"`, `"github.com/arosenkranz/obsidian-vault-tools/internal/triage"`.

```go
// cmd/ov/triage.go — add near the top, after the existing triageDeps type

// errExitCode2 is a sentinel wrapped into the error returned by
// runLLMTriage's two AGENTS.md-precondition checks. main.go recognizes it
// and exits the process with code 2 instead of the default 1 (row #104:
// this triage --llm mode intentionally diverges from the exit-1
// convention used elsewhere in ov2, mirroring v1 triage_llm.py's own exit
// code for the binary it replaces — manual, non-llm triage keeps its
// existing phase-2 exit-0-on-missing-inbox behavior unchanged, an
// unrelated code path).
var errExitCode2 = errors.New("triage --llm precondition failed")

// llmTriageDeps injects the LLM runner and the tty-dependent confirm
// collaborator so runLLMTriage is fully testable without a real tty or
// subprocess. agentsMD is the vault's AGENTS.md content, read once by the
// caller (newTriageCmd's RunE) and reused across every note in the run.
type llmTriageDeps struct {
	runner   triage.Runner
	confirm  func(string) (bool, error)
	agentsMD string
}

const llmTriageTimeout = 120 * time.Second

// runLLMTriage is the testable core of `ov2 triage --llm`: for each inbox
// note (up to limit, 0 = unlimited), call triage.Propose, render a diff
// via triage.Apply(..., dryRun=true) + tui.RenderDiff, and either show it
// (dryRun) or drive the a/e/s/d/r/q approval loop (row #102). Missing
// AGENTS.md or a missing inbox directory both return an error wrapping
// errExitCode2 (row #104) before any note is processed.
func runLLMTriage(cfg *config.Config, in *bufio.Reader, out, errw io.Writer, deps llmTriageDeps, dryRun bool, limit int) error {
	agentsPath := filepath.Join(cfg.VaultDir, "AGENTS.md")
	if _, err := os.Stat(agentsPath); err != nil {
		return fmt.Errorf("%w: AGENTS.md not found at vault root", errExitCode2)
	}
	inboxPath := filepath.Join(cfg.VaultDir, cfg.Inbox)
	if info, err := os.Stat(inboxPath); err != nil || !info.IsDir() {
		return fmt.Errorf("%w: %s not found", errExitCode2, cfg.Inbox)
	}

	notes, err := vault.ListInbox(cfg.VaultDir, cfg.Inbox)
	if err != nil {
		return err
	}
	if limit > 0 && len(notes) > limit {
		notes = notes[:limit]
	}
	if len(notes) == 0 {
		fmt.Fprintln(errw, "✓ Inbox is empty")
		return nil
	}

	tcfg := triage.Config{
		VaultDir:  cfg.VaultDir,
		Inbox:     cfg.Inbox,
		Projects:  cfg.Projects,
		Areas:     cfg.Areas,
		Resources: cfg.Resources,
		Archive:   cfg.Archive,
	}

	for _, n := range notes {
		if _, err := os.Stat(n.Path); err != nil {
			continue // vanished mid-loop (walk resilience)
		}
		content, _, err := vault.ReadNote(n.Path)
		if err != nil {
			fmt.Fprintf(errw, "error reading %s: %v\n", n.Name, err)
			continue
		}

		var proposal triage.Proposal
		haveProposal := false
	inner:
		for {
			if !haveProposal {
				fmt.Fprintf(errw, "\n📥 %s\n", n.Name)
				fmt.Fprintln(errw, "  thinking…")
				ctx, cancel := context.WithTimeout(context.Background(), llmTriageTimeout)
				p, err := triage.Propose(ctx, tcfg, n, deps.agentsMD, deps.runner)
				cancel()
				if err != nil {
					fmt.Fprintf(errw, "  LLM call failed: %v\n", err)
					break inner
				}
				proposal = p
				haveProposal = true
			}

			preview, applyErr := triage.Apply(tcfg, n, proposal, time.Now(), true)
			fmt.Fprintf(errw, "\n🤖 LLM proposal → %s\n", proposal.To)
			fmt.Fprintf(errw, "  title:       %s\n", proposal.NewTitle)
			fmt.Fprintf(errw, "  confidence:  %s\n", proposal.Confidence)
			fmt.Fprintf(errw, "  rationale:   %s\n", proposal.Rationale)
			if applyErr != nil {
				fmt.Fprintf(errw, "  rejected: %v\n", applyErr)
				break inner
			}
			fmt.Fprintln(errw, tui.RenderDiff(triage.Diff(content, preview.Content)))

			if dryRun {
				break inner
			}

			fmt.Fprint(errw, "  [a]pprove  [e]dit  [s]kip  [d]elete  [r]e-ask  [q]uit ?  ")
			line, readErr := in.ReadString('\n')
			choice := strings.TrimSpace(line)
			if readErr != nil && line == "" {
				fmt.Fprintln(errw, "\ninterrupted")
				return nil
			}
			if choice == "" {
				choice = "s"
			}
			switch choice[0] {
			case 'a':
				res, err := triage.Apply(tcfg, n, proposal, time.Now(), false)
				if err != nil {
					fmt.Fprintf(errw, "  apply failed: %v\n", err)
				} else {
					fmt.Fprintf(errw, "  ✓ filed → %s\n", res.Target)
					if res.MOCWarning != "" {
						fmt.Fprintf(errw, "  ⚠ %s\n", res.MOCWarning)
					} else if res.MOCSynced {
						fmt.Fprintln(errw, "  ✓ MOC link updated")
					}
				}
				break inner
			case 'e':
				proposal = editProposalFields(in, errw, proposal)
				continue inner
			case 's':
				fmt.Fprintln(errw, "  → skipped")
				break inner
			case 'd':
				ok, cerr := deps.confirm(fmt.Sprintf("Delete %q?", n.Name))
				if cerr != nil || !ok {
					fmt.Fprintln(errw, "  → cancelled")
					break inner
				}
				if err := os.Remove(n.Path); err != nil {
					fmt.Fprintf(errw, "  delete failed: %v\n", err)
				} else {
					fmt.Fprintln(errw, "  → deleted")
				}
				break inner
			case 'r':
				haveProposal = false
				continue inner
			case 'q':
				fmt.Fprintln(errw, "  → quit")
				return nil
			default:
				fmt.Fprintln(errw, "  → invalid choice, skipped")
				break inner
			}
		}
	}
	fmt.Fprintln(errw, "\nTriage complete")
	return nil
}

// editProposalFields is the v2 simplification of v1's edit_proposal (row
// #103 DECIDE: the phase 3 TUI redesign supersedes v1's exact prompts; the
// same field-by-field affordance is kept). Only "to" and "new_title" are
// editable — frontmatter_patch editing is deliberately left to a future
// polish pass (not part of this phase's acceptance criteria); an empty
// input line keeps the current value.
func editProposalFields(in *bufio.Reader, errw io.Writer, p triage.Proposal) triage.Proposal {
	fmt.Fprint(errw, "  to ["+p.To+"]: ")
	if line, _ := in.ReadString('\n'); strings.TrimSpace(line) != "" {
		p.To = strings.TrimSpace(line)
	}
	fmt.Fprint(errw, "  new_title ["+p.NewTitle+"]: ")
	if line, _ := in.ReadString('\n'); strings.TrimSpace(line) != "" {
		p.NewTitle = strings.TrimSpace(line)
	}
	return p
}
```

- [ ] **Step 7: Wire the `--llm`/`--dry-run`/`--limit` flags into `newTriageCmd`**

```go
// cmd/ov/triage.go — replace the existing newTriageCmd function

func newTriageCmd() *cobra.Command {
	var vaultFlag string
	var llmMode, dryRun bool
	var limit int
	cmd := &cobra.Command{
		Use:   "triage",
		Short: "Interactively process inbox notes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			if llmMode {
				agentsMD, err := os.ReadFile(filepath.Join(cfg.VaultDir, "AGENTS.md"))
				if err != nil {
					return fmt.Errorf("%w: AGENTS.md not found at vault root", errExitCode2)
				}
				runner := llm.NewRunner(cfg.LLMCmd, cfg.Model)
				deps := llmTriageDeps{runner: runner, confirm: tui.Confirm, agentsMD: string(agentsMD)}
				return runLLMTriage(cfg, bufio.NewReader(cmd.InOrStdin()), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps, dryRun, limit)
			}
			if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stdout.Fd()) {
				return errors.New("ov2 triage requires an interactive terminal")
			}
			deps := triageDeps{pickFolder: tui.RunFolderPicker, confirm: tui.Confirm}
			return runTriage(cfg, bufio.NewReader(cmd.InOrStdin()), cmd.ErrOrStderr(), deps)
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	cmd.Flags().BoolVar(&llmMode, "llm", false, "use LLM-assisted triage instead of manual folder picking")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show every LLM proposal (with diff); write nothing (--llm only)")
	cmd.Flags().IntVar(&limit, "limit", 0, "process at most N notes (--llm only; 0 = unlimited)")
	return cmd
}
```

Note: `agentsPath`/`os.Stat(agentsPath)` inside `runLLMTriage` is a **second**, redundant-looking check of the same precondition `newTriageCmd`'s `RunE` already performed via `os.ReadFile`. This redundancy is intentional, not a mistake to remove: `runLLMTriage` is directly unit-tested (Step 2's tests call it without going through `RunE` at all), so its own precondition check is what those tests actually exercise; `RunE`'s check exists only to fail fast with the `agentsMD` content already in hand before constructing a real `llm.Runner`. Do not delete either check.

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./cmd/ov/... -v -race`
Expected: PASS, full package green — every existing phase 0-2 `cmd/ov` test plus every new one in this task.

- [ ] **Step 9: Commit**

```bash
git add cmd/ov/triage.go cmd/ov/triage_test.go cmd/ov/triage_llm_integration_test.go cmd/ov/main.go cmd/ov/main_test.go
git commit -m "Add ov2 triage --llm: approval loop, --dry-run, --limit, exit-2 preconditions"
```

---

### Task 8: `internal/web` — triage propose/status/approve/skip, health check

The web v1 triage surface (design spec §Web layer, row #150): `POST /triage/{note}/propose` submits an `llm.JobManager[triage.Proposal]` job and returns 202 + a pending partial; `GET /triage/{note}/status` is htmx's `hx-trigger="every 2s"` poll target, rendering pending/error/proposal-with-diff depending on job status; `POST /triage/{note}/approve` and `/skip` finalize. `GET /triage-health` dry-runs the LLM (row #149). Reuses phase 2's `internal/web` server, hygiene middleware, and bind guard unchanged — only `routes()`, `Config`, `Server`, and the embedded templates grow.

**Files:**
- Modify: `internal/web/server.go` (`Config` gains `Projects`/`Areas`/`Archive`/`AgentsMD`; `Server` gains `runner`/`jobs`; `New` gains a `runner` parameter; `routes()` gains 5 routes; template `Funcs` for diff rendering)
- Create: `internal/web/jobs.go` (`triageJobs`)
- Create: `internal/web/triage.go` (handlers)
- Create: `internal/web/triage_test.go`
- Create: `internal/web/main_test.go` (`TestMain` re-exec wiring, same pattern as Task 2/7)
- Modify: `internal/web/handlers.go` (`inboxViewNote` gains `NoteEscaped`; `handleInbox` populates it)
- Modify: `internal/web/handlers_test.go` (every existing `New(...)` call site gains a runner argument)
- Modify: `internal/web/fixture_test.go` (`newTestVault`'s `Config` gains the new PARA-root fields; add a shared `fakeLLMRunner`)
- Modify: `internal/web/assets/inbox.html` (per-note triage button)
- Create: `internal/web/assets/triage-button.html`
- Create: `internal/web/assets/triage-pending.html`
- Create: `internal/web/assets/triage-error.html`
- Create: `internal/web/assets/triage-proposal.html`

**Interfaces:**
- Consumes: `triage.Config`, `triage.Proposal`, `triage.Propose`, `triage.Apply`, `triage.Diff`, `triage.DiffLine`, `triage.Runner` (Task 4/5); `llm.JobManager[T]`, `llm.NewJobManager`, `llm.Job[T]`, `llm.Status`, `llm.StatusPending/Done/Failed`, `llm.ErrAuth` (Task 2/3); `vault.Note`, `vault.ReadNote` (phase 0/1)
- Produces:
  - `func New(cfg Config, fetcher capture.TitleFetcher, runner llmRunner, nowFn func() time.Time) *Server` — signature change, 3rd positional param inserted
  - `type llmRunner interface{ triage.Runner; HealthCheck(ctx context.Context) error }`
  - `type triageJobs struct{...}`, `func newTriageJobs() *triageJobs`

- [ ] **Step 1: Write `internal/web/main_test.go`**

```go
// internal/web/main_test.go
package web

import (
	"os"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llmtest"
)

func TestMain(m *testing.M) {
	if llmtest.MaybeRunStub() {
		return
	}
	os.Exit(m.Run())
}
```

- [ ] **Step 2: Update `internal/web/fixture_test.go`'s fixture and add a shared fake runner**

```go
// internal/web/fixture_test.go — replace newTestVault's Config construction
// and add a fake runner at the end of the file

func newTestVault(t *testing.T) (vaultDir string, cfg Config) {
	t.Helper()
	vaultDir = t.TempDir()
	for _, d := range []string{"00-Inbox", "01-Projects", "02-Areas", "03-Resources", "04-Archive", "99-Meta"} {
		if err := os.MkdirAll(filepath.Join(vaultDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "AGENTS.md"), []byte("test AGENTS.md contract"), 0o644); err != nil {
		t.Fatal(err)
	}
	return vaultDir, Config{
		VaultDir: vaultDir, Inbox: "00-Inbox", Resources: "03-Resources",
		Projects: "01-Projects", Areas: "02-Areas", Archive: "04-Archive",
		AgentsMD: "test AGENTS.md contract", Bind: "127.0.0.1:4173",
	}
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

// fakeLLMRunner is an llmRunner used by every test in this package that
// doesn't specifically exercise the real stub-subprocess path (Task 2's
// pattern: fast fake for unit tests, real internal/llmtest stub for
// integration tests proving the actual transport).
type fakeLLMRunner struct {
	response  string
	err       error
	healthErr error
}

func (f *fakeLLMRunner) Run(ctx context.Context, prompt string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

func (f *fakeLLMRunner) HealthCheck(ctx context.Context) error {
	return f.healthErr
}
```

Add `"os"` and `"path/filepath"` imports if not already present (they already are, per the existing file).

- [ ] **Step 3: Update every existing `New(...)` call site in `handlers_test.go` and `bind_test.go` (if any) to pass a runner argument**

Every existing call of the form `New(cfg, someFetcher, nowFnOrNil)` in `internal/web/handlers_test.go` becomes `New(cfg, someFetcher, &fakeLLMRunner{}, nowFnOrNil)` — a zero-value-friendly `&fakeLLMRunner{}` (empty response, no error) is a safe default for every test that doesn't specifically exercise triage. Search the file for every `New(` call (there are 6, per `TestHandleInboxEmpty`, `TestHandleInboxListsNotes`, the 3 capture-submit tests, and the 3 hygiene tests share a helper or repeat the pattern — update all of them) and insert `&fakeLLMRunner{}` as the third argument.

- [ ] **Step 4: Write the failing tests**

```go
// internal/web/triage_test.go
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

func triagePOST(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Host = srv.cfg.Bind
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func triageGET(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = srv.cfg.Bind
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func writeInboxTestNote(t *testing.T, vaultDir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(vaultDir, "00-Inbox", name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// DECIDE(new in v2, row #150): propose returns 202 with a pending
// partial carrying the poll target.
func TestHandleTriageProposeReturns202(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	writeInboxTestNote(t, vaultDir, "First.md", "---\ntype: inbox\n---\nbody\n")
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"high","rationale":"r"}`}, nil)
	rec := triagePOST(t, srv, "/triage/"+url.PathEscape("First.md")+"/propose")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "status") {
		t.Errorf("expected the pending partial to carry a status poll target: %s", rec.Body.String())
	}
}

// DECIDE(new in v2, row #150): polling status eventually returns the
// proposal (with a diff) once the job completes.
func TestHandleTriageStatusEventuallyDone(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	writeInboxTestNote(t, vaultDir, "First.md", "---\ntype: inbox\n---\nbody\n")
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"high","rationale":"r"}`}, nil)
	triagePOST(t, srv, "/triage/First.md/propose")

	deadline := time.Now().Add(2 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		rec := triageGET(t, srv, "/triage/First.md/status")
		body = rec.Body.String()
		if strings.Contains(body, "02-Areas/X.md") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("status never showed the proposal; last body: %s", body)
}

// CONTRACT(#151): the proposal partial contains a diff view.
func TestHandleTriageStatusIncludesDiff(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	writeInboxTestNote(t, vaultDir, "First.md", "---\ntype: inbox\ncreated: 2026-05-14\n---\noriginal body\n")
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{response: `{"to":"02-Areas/X.md","new_title":"X","frontmatter_patch":{"type":"note"},"confidence":"high","rationale":"r"}`}, nil)
	triagePOST(t, srv, "/triage/First.md/propose")
	deadline := time.Now().Add(2 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		rec := triageGET(t, srv, "/triage/First.md/status")
		body = rec.Body.String()
		if strings.Contains(body, "type: note") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(body, "type: note") {
		t.Fatalf("expected the diff to show the frontmatter change; got: %s", body)
	}
}

// CONTRACT: approve applies the proposal and writes the note.
func TestHandleTriageApproveWritesNote(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	writeInboxTestNote(t, vaultDir, "First.md", "---\ntype: inbox\n---\nbody\n")
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"high","rationale":"r"}`}, nil)
	triagePOST(t, srv, "/triage/First.md/propose")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec := triageGET(t, srv, "/triage/First.md/status")
		if strings.Contains(rec.Body.String(), "02-Areas/X.md") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	rec := triagePOST(t, srv, "/triage/First.md/approve")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(vaultDir, "02-Areas", "X.md")); err != nil {
		t.Errorf("note not written: %v", err)
	}
}

// BUG(fixed)(#5): the web approve path enforces the same body_patch
// rejection as the CLI — never trust the LLM's own output for a gate.
func TestHandleTriageApproveRejectsBodyPatch(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	writeInboxTestNote(t, vaultDir, "First.md", "---\ntype: inbox\n---\noriginal\n")
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{response: `{"to":"02-Areas/X.md","new_title":"X","body_patch":"INJECTED","confidence":"high","rationale":"r"}`}, nil)
	triagePOST(t, srv, "/triage/First.md/propose")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec := triageGET(t, srv, "/triage/First.md/status")
		if strings.Contains(rec.Body.String(), "02-Areas") || strings.Contains(rec.Body.String(), "error") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	rec := triagePOST(t, srv, "/triage/First.md/approve")
	if _, err := os.Stat(filepath.Join(vaultDir, "02-Areas", "X.md")); !os.IsNotExist(err) {
		t.Fatal("body_patch-carrying proposal must never be written")
	}
	if content, _ := os.ReadFile(filepath.Join(vaultDir, "00-Inbox", "First.md")); strings.Contains(string(content), "INJECTED") {
		t.Fatal("injected content must never reach disk")
	}
	_ = rec
}

// DECIDE(new in v2, row #150): skip discards the pending job for that
// note without writing anything.
func TestHandleTriageSkipClearsJob(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	writeInboxTestNote(t, vaultDir, "First.md", "---\ntype: inbox\n---\nbody\n")
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"high","rationale":"r"}`}, nil)
	triagePOST(t, srv, "/triage/First.md/propose")
	rec := triagePOST(t, srv, "/triage/First.md/skip")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	statusRec := triageGET(t, srv, "/triage/First.md/status")
	if statusRec.Code != http.StatusNotFound {
		t.Errorf("expected 404 after skip cleared the job, got %d", statusRec.Code)
	}
}

// DECIDE(new in v2, row #147): an auth-classified failure renders the
// specific message, not a generic error.
func TestHandleTriageStatusAuthFailure(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	writeInboxTestNote(t, vaultDir, "First.md", "---\ntype: inbox\n---\nbody\n")
	authErr := fmt.Errorf("%w: not logged in", llm.ErrAuth)
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{err: authErr}, nil)
	triagePOST(t, srv, "/triage/First.md/propose")
	deadline := time.Now().Add(2 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		rec := triageGET(t, srv, "/triage/First.md/status")
		body = rec.Body.String()
		if strings.Contains(body, "auth expired") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected an auth-specific message, got: %s", body)
}

// BUG(fixed)(row #140-class): a {note} path parameter containing a
// traversal sequence is rejected before it ever reaches vault.ReadNote.
func TestHandleTriageProposeRejectsTraversalNote(t *testing.T) {
	_, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{}, nil)
	rec := triagePOST(t, srv, "/triage/"+url.PathEscape("../../etc/passwd")+"/propose")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a traversal note param", rec.Code)
	}
}

// DECIDE(new in v2, row #149): the health endpoint reports ok on success.
func TestHandleTriageHealthOK(t *testing.T) {
	_, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{}, nil)
	rec := triageGET(t, srv, "/triage-health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// DECIDE(new in v2, row #149, #147): the health endpoint reports the
// auth-specific message and a 503, not a 500.
func TestHandleTriageHealthAuthFailure(t *testing.T) {
	_, cfg := newTestVault(t)
	authErr := fmt.Errorf("%w: not logged in", llm.ErrAuth)
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{healthErr: authErr}, nil)
	rec := triageGET(t, srv, "/triage-health")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "auth expired") {
		t.Errorf("body = %q", rec.Body.String())
	}
}
```

Add `"fmt"` and `"github.com/arosenkranz/obsidian-vault-tools/internal/llm"` to the import block.

- [ ] **Step 5: Run tests to verify they fail**

Run: `go test ./internal/web/... -v`
Expected: FAIL to compile — `New`'s signature mismatch across every existing call site, `llmRunner`, `triageJobs`, handler functions undefined.

- [ ] **Step 6: Write `internal/web/jobs.go`**

```go
// internal/web/jobs.go
package web

import (
	"context"
	"sync"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
	"github.com/arosenkranz/obsidian-vault-tools/internal/triage"
)

// triageJobs tracks in-flight and completed triage proposal jobs, keyed by
// the note's vault-relative filename (design spec's "in-process map keyed
// by note path" — Core interaction contract, row #150). Regenerated on
// restart; fine for one user (design spec).
type triageJobs struct {
	mgr    *llm.JobManager[triage.Proposal]
	mu     sync.Mutex
	byNote map[string]string // note filename -> current job id
}

func newTriageJobs() *triageJobs {
	return &triageJobs{mgr: llm.NewJobManager[triage.Proposal](), byNote: make(map[string]string)}
}

// submit starts a new proposal job for note and records it as the
// current job for that note (overwriting any prior job id — e.g. a
// re-propose after a skip).
func (j *triageJobs) submit(ctx context.Context, note string, fn func(context.Context) (triage.Proposal, error)) string {
	id := j.mgr.Submit(ctx, fn)
	j.mu.Lock()
	j.byNote[note] = id
	j.mu.Unlock()
	return id
}

// current returns the current job (and whether one exists) for note.
func (j *triageJobs) current(note string) (llm.Job[triage.Proposal], bool) {
	j.mu.Lock()
	id, ok := j.byNote[note]
	j.mu.Unlock()
	if !ok {
		return llm.Job[triage.Proposal]{}, false
	}
	return j.mgr.Get(id)
}

// clear discards the tracked job for note (after approve/skip).
func (j *triageJobs) clear(note string) {
	j.mu.Lock()
	delete(j.byNote, note)
	j.mu.Unlock()
}
```

- [ ] **Step 7: Write `internal/web/triage.go`**

```go
// internal/web/triage.go
package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
	"github.com/arosenkranz/obsidian-vault-tools/internal/triage"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

const webTriageTimeout = 120 * time.Second

// llmRunner is the subset of *llm.Runner the web server needs:
// triage.Runner's Run method plus HealthCheck (row #149). Tests inject a
// fake satisfying both.
type llmRunner interface {
	triage.Runner
	HealthCheck(ctx context.Context) error
}

// noteParam validates the {note} URL path value: it must be a bare
// filename with no path separator — defense in depth against a crafted
// path segment ever reaching vault.ReadNote (mirrors row #140's
// FindMOCByName traversal defense). net/http's ServeMux decodes
// percent-encoded path segments before PathValue returns them, so this
// check runs against the fully-decoded value.
func noteParam(r *http.Request) (string, error) {
	note := r.PathValue("note")
	if note == "" || strings.ContainsAny(note, "/\\") {
		return "", errors.New("invalid note")
	}
	return note, nil
}

func (s *Server) triageConfig() triage.Config {
	return triage.Config{
		VaultDir:  s.cfg.VaultDir,
		Inbox:     s.cfg.Inbox,
		Projects:  s.cfg.Projects,
		Areas:     s.cfg.Areas,
		Resources: s.cfg.Resources,
		Archive:   s.cfg.Archive,
	}
}

func (s *Server) noteFor(note string) vault.Note {
	p := filepath.Join(s.cfg.VaultDir, s.cfg.Inbox, note)
	return vault.Note{Path: p, Rel: s.cfg.Inbox + "/" + note, Name: strings.TrimSuffix(note, ".md")}
}

func (s *Server) handleTriagePropose(w http.ResponseWriter, r *http.Request) {
	note, err := noteParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	n := s.noteFor(note)
	tcfg := s.triageConfig()
	agentsMD := s.cfg.AgentsMD
	runner := s.runner
	jobID := s.jobs.submit(context.Background(), note, func(ctx context.Context) (triage.Proposal, error) {
		ctx, cancel := context.WithTimeout(ctx, webTriageTimeout)
		defer cancel()
		return triage.Propose(ctx, tcfg, n, agentsMD, runner)
	})
	w.WriteHeader(http.StatusAccepted)
	s.renderTriagePending(w, note, jobID)
}

func (s *Server) handleTriageStatus(w http.ResponseWriter, r *http.Request) {
	note, err := noteParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	job, ok := s.jobs.current(note)
	if !ok {
		http.Error(w, "no pending triage job for this note", http.StatusNotFound)
		return
	}
	switch job.Status {
	case llm.StatusPending:
		s.renderTriagePending(w, note, job.ID)
	case llm.StatusFailed:
		s.renderTriageError(w, note, job.Err)
	default: // llm.StatusDone
		s.renderTriageProposal(w, note, job.Result)
	}
}

func (s *Server) handleTriageApprove(w http.ResponseWriter, r *http.Request) {
	note, err := noteParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	job, ok := s.jobs.current(note)
	if !ok || job.Status != llm.StatusDone {
		http.Error(w, "no completed proposal to approve", http.StatusConflict)
		return
	}
	res, applyErr := triage.Apply(s.triageConfig(), s.noteFor(note), job.Result, s.now(), false)
	s.jobs.clear(note)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if applyErr != nil {
		fmt.Fprintf(w, `<div class="error">Apply failed: %s</div>`, template.HTMLEscapeString(applyErr.Error()))
		return
	}
	fmt.Fprintf(w, `<div class="success">Filed &#8594; %s</div>`, template.HTMLEscapeString(res.Target))
}

func (s *Server) handleTriageSkip(w http.ResponseWriter, r *http.Request) {
	note, err := noteParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.jobs.clear(note)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if tplErr := s.tmpl.ExecuteTemplate(w, "triage-button.html", map[string]any{"NoteEscaped": url.PathEscape(note)}); tplErr != nil {
		http.Error(w, tplErr.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleTriageHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	err := s.runner.HealthCheck(ctx)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err != nil {
		if errors.Is(err, llm.ErrAuth) {
			http.Error(w, "LLM auth expired — run `claude login` on the Mac", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "LLM health check failed: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	fmt.Fprintln(w, "ok")
}

func (s *Server) renderTriagePending(w http.ResponseWriter, note, jobID string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.tmpl.ExecuteTemplate(w, "triage-pending.html", map[string]any{
		"NoteEscaped": url.PathEscape(note), "JobID": jobID,
	})
}

func (s *Server) renderTriageError(w http.ResponseWriter, note string, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	msg := err.Error()
	if errors.Is(err, llm.ErrAuth) {
		msg = "LLM auth expired — run `claude login` on the Mac"
	}
	s.tmpl.ExecuteTemplate(w, "triage-error.html", map[string]any{
		"NoteEscaped": url.PathEscape(note), "Error": msg,
	})
}

func (s *Server) renderTriageProposal(w http.ResponseWriter, note string, p triage.Proposal) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	n := s.noteFor(note)
	content, _, _ := vault.ReadNote(n.Path)
	preview, err := triage.Apply(s.triageConfig(), n, p, s.now(), true)
	if err != nil {
		s.renderTriageError(w, note, err)
		return
	}
	s.tmpl.ExecuteTemplate(w, "triage-proposal.html", map[string]any{
		"NoteEscaped": url.PathEscape(note),
		"Proposal":    p,
		"Diff":        triage.Diff(content, preview.Content),
		"Target":      preview.Target,
	})
}

func diffClass(op byte) string {
	switch op {
	case '+':
		return "diff-add"
	case '-':
		return "diff-del"
	default:
		return "diff-ctx"
	}
}

func diffMarker(op byte) string {
	return string(op)
}
```

- [ ] **Step 8: Modify `internal/web/server.go`**

```go
// internal/web/server.go — full replacement
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
// stays a thin frontend over the capture/vault/triage core verbs (design
// spec's "stateless verbs" principle).
type Config struct {
	VaultDir  string
	Inbox     string
	Resources string
	Bind      string // the configured bind address, for Host-header validation
	Projects  string
	Areas     string
	Archive   string
	AgentsMD  string // the vault's AGENTS.md content, read once at server construction
}

type Server struct {
	cfg     Config
	mux     *http.ServeMux
	fetcher capture.TitleFetcher
	tmpl    *template.Template
	now     func() time.Time
	runner  llmRunner
	jobs    *triageJobs
}

// New builds a Server around an already-constructed listener seam (the
// caller owns bind-guard decisions and listener construction — design spec
// §Web layer "Listener seam"). fetcher and runner are injected so tests
// never hit the network or spawn a real subprocess; nowFn defaults to
// time.Now when nil.
func New(cfg Config, fetcher capture.TitleFetcher, runner llmRunner, nowFn func() time.Time) *Server {
	if nowFn == nil {
		nowFn = time.Now
	}
	tmpl := template.Must(template.New("web").Funcs(template.FuncMap{
		"diffClass":  diffClass,
		"diffMarker": diffMarker,
	}).ParseFS(assetsFS, "assets/*.html"))
	s := &Server{cfg: cfg, fetcher: fetcher, runner: runner, tmpl: tmpl, now: nowFn, jobs: newTriageJobs()}
	s.mux = http.NewServeMux()
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleInbox)
	s.mux.HandleFunc("GET /capture", s.handleCaptureForm)
	s.mux.HandleFunc("POST /capture", s.handleCaptureSubmit)
	s.mux.HandleFunc("GET /assets/htmx.min.js", s.handleHTMX)
	s.mux.HandleFunc("POST /triage/{note}/propose", s.handleTriagePropose)
	s.mux.HandleFunc("GET /triage/{note}/status", s.handleTriageStatus)
	s.mux.HandleFunc("POST /triage/{note}/approve", s.handleTriageApprove)
	s.mux.HandleFunc("POST /triage/{note}/skip", s.handleTriageSkip)
	s.mux.HandleFunc("GET /triage-health", s.handleTriageHealth)
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

- [ ] **Step 9: Modify `internal/web/handlers.go`'s `inboxViewNote` and `handleInbox`**

```go
// internal/web/handlers.go — replace inboxViewNote and handleInbox; add
// "net/url" and "path/filepath" to the import block

type inboxViewNote struct {
	Name        string
	Age         int
	NoteEscaped string // url.PathEscape(filename with .md) — for the per-note triage button's URLs
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
		views = append(views, inboxViewNote{
			Name:        n.Name,
			Age:         vault.AgeDays(now, n.ModTime),
			NoteEscaped: url.PathEscape(filepath.Base(n.Path)),
		})
	}
	s.renderInbox(w, views)
}
```

(Leave `renderInbox`, `handleCaptureForm`, `handleCaptureSubmit`, and `handleHTMX` unchanged.)

- [ ] **Step 10: Write the new templates**

```html
{{define "triage-button.html"}}
<div id="triage-{{.NoteEscaped}}">
  <button hx-post="/triage/{{.NoteEscaped}}/propose" hx-target="#triage-{{.NoteEscaped}}" hx-swap="outerHTML">Triage</button>
</div>
{{end}}
```

```html
{{define "triage-pending.html"}}
<div id="triage-{{.NoteEscaped}}" hx-get="/triage/{{.NoteEscaped}}/status" hx-trigger="every 2s" hx-swap="outerHTML">
  <em>Thinking…</em>
</div>
{{end}}
```

```html
{{define "triage-error.html"}}
<div id="triage-{{.NoteEscaped}}" class="error">
  LLM call failed: {{.Error}}
  <button hx-post="/triage/{{.NoteEscaped}}/propose" hx-target="#triage-{{.NoteEscaped}}" hx-swap="outerHTML">Retry</button>
</div>
{{end}}
```

```html
{{define "triage-proposal.html"}}
<div id="triage-{{.NoteEscaped}}" class="triage-proposal">
  <p>&#8594; <strong>{{.Target}}</strong></p>
  <p>confidence: {{.Proposal.Confidence}} — {{.Proposal.Rationale}}</p>
  <pre class="diff">{{range .Diff}}<span class="{{diffClass .Op}}">{{diffMarker .Op}} {{.Text}}</span>
{{end}}</pre>
  <button hx-post="/triage/{{.NoteEscaped}}/approve" hx-target="#triage-{{.NoteEscaped}}" hx-swap="outerHTML">Approve</button>
  <button hx-post="/triage/{{.NoteEscaped}}/skip" hx-target="#triage-{{.NoteEscaped}}" hx-swap="outerHTML">Skip</button>
</div>
{{end}}
```

Update `internal/web/assets/inbox.html`'s note-listing line to include the per-note triage button (the sub-template receives `.` — the `inboxViewNote` — directly, since `triage-button.html` only reads `.NoteEscaped`):

```html
{{define "inbox.html"}}
<!doctype html>
<html>
<head><title>ov2 — Inbox</title><script src="/assets/htmx.min.js"></script></head>
<body>
<h1>Inbox</h1>
<p><a href="/capture">Capture a note</a></p>
{{if .Notes}}
<ul>
{{range .Notes}}<li>{{.Name}} — {{.Age}}d old {{template "triage-button.html" .}}</li>{{end}}
</ul>
{{else}}
<p>Inbox is empty.</p>
{{end}}
</body>
</html>
{{end}}
```

- [ ] **Step 11: Run tests to verify they pass**

Run: `go test ./internal/web/... -v -race`
Expected: PASS, full package green — every phase 2 test plus every new triage test.

- [ ] **Step 12: Commit**

```bash
git add internal/web
git commit -m "Add web triage propose/status/approve/skip and LLM health-check endpoint"
```

---

### Task 9: `cmd/ov serve` — wire the LLM runner and AGENTS.md into `web.Config`

The last integration point: `ov2 serve` constructs the `llm.Runner` from resolved config and reads `AGENTS.md` once at startup, extending phase 2's `newServeCmd` with the fields Task 8's `web.Config`/`web.New` now require.

**Files:**
- Modify: `cmd/ov/serve.go`
- Modify: `cmd/ov/serve_test.go`

**Interfaces:**
- Consumes: `web.Config` (Task 8, gains `Projects`/`Areas`/`Archive`/`AgentsMD`); `web.New` (Task 8, gains a `runner` parameter); `llm.NewRunner` (Task 2)
- Produces: no new exported symbols — `newServeCmd`'s `RunE` gains the AGENTS.md read + `llm.NewRunner` construction

- [ ] **Step 1: Read `cmd/ov/serve_test.go` before editing, to preserve its existing assertions**

The implementer MUST read the current `cmd/ov/serve_test.go` in full first (it is not reproduced here — phase 2 Task 8 wrote it and it currently asserts on `TestServeStartsAndStopsOnLoopback`-style lifecycle behavior unrelated to triage). This task only adds an `AGENTS.md` file to any vault fixture the existing tests construct (via `newVaultFixture`, which already creates the standard PARA dirs — `AGENTS.md` is the one new precondition `newServeCmd`'s `RunE` now requires, mirroring Task 7's `--llm` precondition applied here at server-startup time instead of per-triage-command time). If an existing serve test's fixture does not yet include an `AGENTS.md` file, add `os.WriteFile(filepath.Join(vaultDir, "AGENTS.md"), []byte("test contract"), 0o644)` to that test's setup — do not change any other existing assertion.

- [ ] **Step 2: Modify `cmd/ov/serve.go`**

```go
// cmd/ov/serve.go — full replacement
package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/arosenkranz/obsidian-vault-tools/internal/capture"
	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
	"github.com/arosenkranz/obsidian-vault-tools/internal/web"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var vaultFlag, bindFlag string
	var allowNonlocal bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the ov2 web server (capture form + inbox + LLM triage)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			errw := cmd.ErrOrStderr()
			agentsMD, err := os.ReadFile(filepath.Join(cfg.VaultDir, "AGENTS.md"))
			if err != nil {
				return fmt.Errorf("AGENTS.md not found at vault root: %w", err)
			}
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
			runner := llm.NewRunner(cfg.LLMCmd, cfg.Model)
			srv := web.New(web.Config{
				VaultDir:  cfg.VaultDir,
				Inbox:     cfg.Inbox,
				Resources: cfg.Resources,
				Projects:  cfg.Projects,
				Areas:     cfg.Areas,
				Archive:   cfg.Archive,
				AgentsMD:  string(agentsMD),
				Bind:      ln.Addr().String(), // the resolved address (bindFlag may end in ":0"), so Host-header validation matches what clients actually dial
			}, capture.NewHTTPTitleFetcher(), runner, nil)
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

Add `"path/filepath"` to the import block (needed for `filepath.Join`).

- [ ] **Step 3: Run tests to verify they pass**

Run: `go test ./cmd/ov/... -run TestServe -v`
Expected: PASS. If any existing serve test's fixture lacked `AGENTS.md`, Step 1's fixture update resolves the new failure; no other test logic changes.

- [ ] **Step 4: Full-repo build and test sweep**

Run:
```bash
go build ./...
go vet ./...
go test ./... -race
```
Expected: PASS across all 10 packages (`internal/config`, `internal/vault`, `internal/capture`, `internal/tui`, `internal/llm`, `internal/llmtest`, `internal/triage`, `internal/web`, `cmd/ov`, plus the module root if it has any root-level tests), zero build/vet errors, `-race` clean.

- [ ] **Step 5: Build the real binary and smoke-test `--dry-run` against a scratch vault (manual, not a unit test)**

```bash
make build
```

Then, in a scratch directory (NOT the real vault — this step just proves the binary starts, resolves config, and reaches the LLM precondition checks; it is not the design spec's exit-criterion dry-run diff, which is Task 9's own final self-review step below):

```bash
mkdir -p /tmp/ov2-smoke/00-Inbox /tmp/ov2-smoke/01-Projects /tmp/ov2-smoke/02-Areas /tmp/ov2-smoke/03-Resources /tmp/ov2-smoke/04-Archive /tmp/ov2-smoke/99-Meta
echo "# AGENTS.md smoke test" > /tmp/ov2-smoke/AGENTS.md
echo "---
type: inbox
created: 2026-07-15
---
Smoke test note." > "/tmp/ov2-smoke/00-Inbox/2026-07-15 0900 Smoke Test.md"
OV_LLM_CMD="claude --print" ./dist/ov2 triage --llm --vault /tmp/ov2-smoke --dry-run
```

Expected: either a real proposal + diff is printed (if `claude` is installed and authenticated on this machine), or a clean, typed error (`llm binary not found in PATH` / an auth-classified message) — never a panic, a stack trace, or a hang past 120s. Report which outcome occurred in the task report; either is acceptable evidence this task's wiring is live.

- [ ] **Step 6: Commit**

```bash
git add cmd/ov/serve.go cmd/ov/serve_test.go
git commit -m "Wire llm.Runner and AGENTS.md into ov2 serve's web.Config"
```

---

## Self-Review

**1. Spec coverage (phase table row: "LLM triage: transport+decoders+job model; Propose/Validate/Apply; tui approval; web triage propose/approve; `--dry-run`"):**
- Transport (`Run`, shlex-split, never a shell, argv[0] absolute resolution) → Task 2, rows #89, #144.
- Two decoders (`ExtractJSON` already shipped phase 0; `ExtractHTMLBlock` defined now, phase 4/5 consumer) → Task 2, row #74.
- Job model (submit/poll/result, process-group kill, semaphore(2), health check) → Task 2 (transport-level kill/semaphore/health), Task 3 (generic `JobManager[T]`), rows #145, #146, #148, #149.
- Failure classification (auth vs generic, typed errors) → Task 2, row #147.
- `Propose`/`Validate`/`Apply` → Task 4 (`Propose`, `Validate`) and Task 5 (`Apply`), rows #91, #92, #94-101, #5, #97.
- The `body_patch`/`links_to_add` enforcement hole (design spec §Safety item 1, the phase's headline fix) → `Validate` (Task 4) rejects in code; `Apply` (Task 5) re-enforces via its own `Validate` call — covered by dedicated tests in both `internal/triage` and `cmd/ov`'s interactive-loop and stub-subprocess integration tests, and `internal/web`'s approve-path test.
- TUI approval loop (a/e/s/d/r/q, diff+confirm before apply) → Task 7, row #102; diff rendering → Task 6, row #151.
- Web triage propose/approve (202+job id, htmx poll, diff card, approve/skip) → Task 8, row #150.
- `--dry-run` (shows every proposal with diff, applies nothing) → `triage.Apply`'s `dryRun` parameter (Task 5), consumed by Task 7's CLI loop and available identically to Task 8's web preview path.
- `--limit N`, `--vault` override → Task 7 (`--vault` already existed on the shared command; `--limit`/`--dry-run` added this phase).
- Exit 2 on missing AGENTS.md/inbox → Task 7 (`errExitCode2`, `cmd/ov/main.go`), row #104; `ov2 serve`'s own AGENTS.md precondition → Task 9 (a related but distinct startup-time check, not a triage-command exit code — `serve` fails with the ordinary exit 1 path since it isn't triage-specific).
- Prompt injection posture (scratch CWD, tools-disabled documentation, server-side-only gates) → Task 2 (scratch CWD is code-enforced, row #143), `examples/ov.config.example` documentation (Task 2), gates enforced exclusively in `internal/triage` (Tasks 4/5), never trusted from LLM output anywhere in Tasks 7/8's frontend code.
- Stub-LLM TestMain re-exec pattern (design spec §Testing strategy tier 3) → `internal/llmtest` (Task 2), consumed by `internal/llm`'s own tests (Task 2), `cmd/ov`'s integration tests (Task 7), and available to `internal/web` (Task 8's `main_test.go`, though Task 8's own tests use the fast `fakeLLMRunner` for its httptest-driven flow — the design spec's requirement is that the transport itself is proven via the real subprocess somewhere in the phase, which Task 2 and Task 7 satisfy; Task 8 additionally has the `internal/llmtest` wiring available should a future phase need a genuine subprocess-backed web test).
- go-diff dependency budget check → confirmed against design spec §Architecture's dep list (`go-diff` explicitly named alongside `goldmark`/`bubbles`/`glamour` as approved-but-not-yet-added); added this phase, pinned `v1.4.0`, Global Constraints documents the decision to add it now rather than defer to phase 6.

**2. Placeholder scan:** No `TBD`/`TODO`/"add appropriate error handling"/"similar to Task N" placeholders — every step shows complete code. The one explicit forward-reference is `ExtractHTMLBlock`, which is deliberately unconsumed until phase 4/5 per the design spec's own instruction ("define the contract now") — its test (`TestExtractHTMLBlock`) exercises it directly, so it is not untested dead code.

**3. Type consistency:** `triage.Proposal{From, To, NewTitle string; FrontmatterPatch map[string]any; BodyPatch *string; LinksToAdd []string; Rationale, Confidence string}` — defined once in Task 4, consumed identically by Task 5 (`Apply`), Task 7 (`cmd/ov`), Task 8 (`internal/web`). `triage.Config{VaultDir, Inbox, Projects, Areas, Resources, Archive string}` with `ParaRoots()` — defined Task 4, consumed identically by Task 5, 7, 8. `triage.Result{Target, Content string; MOCSynced bool; MOCWarning string}` — defined Task 5, consumed identically by Task 7 and Task 8's approve handler. `triage.Runner interface{ Run(ctx, prompt) (string, error) }` — defined Task 4, satisfied by `*llm.Runner` (Task 2) without either package importing the other's concrete type; `internal/web`'s `llmRunner` interface (Task 8) embeds `triage.Runner` and adds `HealthCheck`, also satisfied by `*llm.Runner` directly. `llm.JobManager[T]`/`llm.Job[T]`/`llm.Status` — defined Task 3, instantiated as `JobManager[triage.Proposal]` by Task 8's `triageJobs` without `internal/llm` ever importing `internal/triage` (generic-over-T is what makes this possible — verified no import cycle). `triage.DiffLine{Op byte; Text string}` and `triage.Diff(old, new string) []DiffLine` — defined Task 4, consumed identically by Task 6 (`tui.RenderDiff`), Task 7 (CLI loop), and Task 8 (`triage-proposal.html` via the `diffClass`/`diffMarker` template funcs). `web.New(cfg Config, fetcher capture.TitleFetcher, runner llmRunner, nowFn func() time.Time) *Server` — signature changed once in Task 8 (inserting `runner` as the 3rd parameter), and Task 9 is the only other call site (`cmd/ov/serve.go`), updated in lockstep within the same plan — no stale call site survives past Task 9.

**4. Cross-task safety-gate consistency check (specific to this phase's headline risk):** `Validate` is called from exactly two places — inside `Apply` (Task 5, unconditionally, first statement) and directly by test code proving the standalone contract (Task 4). Neither `cmd/ov` (Task 7) nor `internal/web` (Task 8) calls `Validate` directly; both call `Apply`, which enforces it internally. This means there is exactly one authoritative enforcement point for the `body_patch`/`links_to_add`/PARA-root gates, and no frontend can accidentally bypass it by forgetting a `Validate` call — the design spec's "defense in depth" requirement is structurally guaranteed, not just documented.

**5. Exit-criterion verification (design spec: "`ov2 triage --llm` replaces bash in daily use; dry-run diffed vs python on vault copy; stub-LLM e2e green"):**
- "stub-LLM e2e green" → satisfied mechanically by Task 2's and Task 7's `internal/llmtest`-based integration tests, run as part of every task's own test suite and the final `go test ./... -race` sweep (Task 9 Step 4).
- "dry-run diffed vs python on vault copy" is explicitly a **manual verification step**, not a unit test (per the task instructions: "this is a manual verification step at the end, not a unit test — plan for it explicitly as a self-review/exit-criterion step, not skipped"). It is NOT delegated to a task subagent — the controller (this session) performs it after all 9 tasks are merged to the phase branch, before requesting the final PR, using these steps:
  1. Copy the real vault to a scratch location: `rsync -a --exclude .obsidian "$OV_VAULT_DIR"/ /tmp/ov2-parity-check/`.
  2. Run the python oracle: `python3 bin/triage_llm.py --vault /tmp/ov2-parity-check --dry-run > /tmp/py-dry-run.txt 2>&1` (requires a working `OV_LLM_CMD`/`claude` on this machine — if unavailable, this step is documented as blocked-on-environment, not silently skipped, and reported to the user).
  3. Re-copy the vault fresh (dry-run must not have mutated it, but isolate anyway): `rsync -a --exclude .obsidian "$OV_VAULT_DIR"/ /tmp/ov2-parity-check-2/`.
  4. Run the Go binary: `./dist/ov2 triage --llm --vault /tmp/ov2-parity-check-2 --dry-run > /tmp/go-dry-run.txt 2>&1`.
  5. Diff the proposal SET (target folder + new_title per note — not exact prose, since rationale/confidence phrasing legitimately differs between prompt-template wordings and LLM sampling variance): confirm every note gets a proposal, confirm no proposal targets a non-PARA-root folder in either output, and spot-check that `to`/`new_title` agree or are both defensibly reasonable for at least 80% of notes (LLM outputs are not byte-deterministic across two separate invocations even with an identical prompt — full agreement is not the bar; structural soundness and no regressions is).
  6. Record the outcome (pass / partial with explained divergences / blocked-on-environment) in the progress ledger before finishing the branch.

**6. Global Constraints re-check against the finished task list:** every constraint in this plan's Global Constraints section maps to at least one task's explicit implementation (body_patch/links_to_add rejection → Task 4/5; PARA-root gate → Task 4/5; prompt injection posture → Task 2/4; transport hardening → Task 2; failure classification → Task 2; job model → Task 3/8; exit codes → Task 7; dry-run → Task 5/7/8; stub-LLM tests → Task 2/7; go-diff pin → Task 2/4). No constraint is orphaned without an owning task.
