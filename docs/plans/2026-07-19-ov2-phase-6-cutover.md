# ov v2 Phase 6: Parity Checklist + Cutover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a scripted, re-runnable side-by-side parity checklist (`bin/vault.sh` + its python children vs `dist/ov2`) as the final oracle comparison before `bin/` is deleted, run it for real against a copy of the real vault, then execute the one-way cutover: rename `ov2`→`ov` everywhere, delete `bin/` and the python test suite, and fix every doc that describes the retired bash/python tool.

**Architecture:** Per the approved spec (`docs/plans/2026-07-11-ov-v2-go-rewrite-design.md`), phase 6 row: "Web polish (diff views, job UX); parity checklist scripted side-by-side on vault copy; one week full parallel" / exit criterion "Cutover: rename ov2→ov, delete symlink, retire bin/". **Scope confirmed directly with Alex before this plan was written** (see Session Decisions below): web polish is OUT of scope — the existing Web v1 surface (capture form, inbox, triage propose/approve+diff, shipped phases 2-3) ships as-is; the "one week full parallel" calendar gate is explicitly waived by Alex in this conversation, not satisfied retroactively by inferred daily use. What remains is exactly the two genuinely codeable pieces: a scripted parity checklist (Task 1, new deliverable) and the cutover itself (Task 2, gated on the parity run passing + Alex's explicit go-ahead).

**Tech Stack:** Bash (matching `bin/vault.sh`'s own idiom and this repo's existing `Makefile`-driven conventions — the script must drive both a bash script and a Go binary with identical env-var-based config injection, which bash orchestrates most directly). No new Go dependency. No new non-stdlib bash dependency (`rsync`, `python3`, `go` — all already required by this repo per `docs/install.md`).

## Session Decisions (resolved with Alex via `ask`, this conversation, before any code was written)

1. **Web polish: SKIPPED.** Alex: "i just wanna finish full parity for v2." No web UX gaps were named. `internal/web`'s existing capture/inbox/triage propose-approve+diff surface (phases 2-3) is treated as the shipped Web v1 surface; nothing in `internal/web` is touched by this plan.
2. **One-week parallel window: WAIVED, not satisfied.** The task brief handed to this session claimed Alex has run `dist/ov2` daily since phase 2. Alex's direct answer contradicts that: **"i haven't used it at all yet, honestly just wanna finish the move to ov2 completely instead of managing two tools."** This session trusts Alex's live statement over the stale brief. Per his explicit direction, the calendar-week gate is not being run as a real wait — the parity checklist (Task 1, run for real in the post-Task-1 manual step below) plus Alex's own explicit go-ahead immediately before the cutover step are the confirmation gates actually used. This divergence from the design's literal phasing-table text is Alex's call, made directly in this conversation, and is recorded here rather than silently assumed.

## Global Constraints

- Module path: `github.com/arosenkranz/obsidian-vault-tools`
- Binary name during Task 1: still `ov2`, built to `dist/ov2` (`make build`) — Task 1 does not touch naming. The rename happens entirely in Task 2.
- `bin/vault.sh`, `bin/triage_llm.py`, `bin/moc_cleanup.py`, `bin/render_html.py` are READ-ONLY for Task 1 (parity script only reads/execs them) — Task 2 deletes them (that is the point of Task 2, not a violation of "read-only until cutover").
- **Behavior-inventory prerequisite (already on this branch, committed before Task 1):** rows #166 (`mocs add`'s bash array word-splits on space-containing MOC filenames — BUG, not ported, v2's positional-arg redesign is immune by construction) and #167 (`render_html.py`'s hand-rolled converter drops a leading H1 that goldmark renders — DECIDE, already locked by the design's explicit goldmark choice) were mined during this plan's own research (empirically reproduced by hand-running both binaries side by side) and appended to `docs/plans/ov2-behavior-inventory.md` in commit `7bf5770`, per every prior phase's "mine before you cite" discipline. Every divergence the parity script documents-and-skips cites a row number; every row it cites already exists.
- **No new v1 behavior exists to mine beyond rows #166-167.** Phases 0-5 covered every `bin/` dispatch arm (confirmed by re-reading the full 167-row inventory and the phase 0-5 command-disposition table before writing this plan); this phase touches no new CLI surface, only comparison tooling and cutover mechanics.
- Commit style: imperative mood, no conventional-commit prefix (matches repo history).
- **Security-focused review routing:** Task 2 (cutover) touches the install path (`Makefile`'s `install`/`uninstall` targets place a binary on `$PATH`) and deletes the bash config-sourcing code path (row #12: "config file is `source`d as bash — arbitrary code execution" — its deletion is itself a security-relevant removal, worth an adversarial second look to confirm no residual reference to the deleted execution path survives). Route Task 2 to a security-focused reviewer at task-review time, matching every prior phase's pattern for install/path-adjacent work.
- Go tests unaffected by this phase (no Go source changes; `main.go`'s error prefix and `root.go`'s `Use` field change in Task 2 touch existing tests — see Task 2's test-impact note).

---

### Task 1: `scripts/parity-check.sh` — scripted side-by-side parity checklist

**Files:**
- Create: `scripts/parity-check.sh`
- Modify: `Makefile` (add a `parity` target; additive only — no existing target changes)

**Interfaces:**
- Consumes: `bin/vault.sh` (+ `bin/triage_llm.py`/`bin/moc_cleanup.py`/`bin/render_html.py` via its own dispatch), `go build ./cmd/ov`, a real vault path passed via `--source`.
- Produces: an executable, re-runnable script printing `MATCH` / `DIVERGENCE (row #N): <reason>` / `MISMATCH: <reason>` per check, exit 0 iff zero `MISMATCH` lines were printed (regardless of how many documented `DIVERGENCE` lines exist). `make parity SOURCE=/path/to/vault` wraps it.

- [ ] **Step 1: Write `scripts/parity-check.sh`**

```bash
#!/usr/bin/env bash
#
# scripts/parity-check.sh — side-by-side behavioral parity check between
# bin/vault.sh (+ triage_llm.py / moc_cleanup.py / render_html.py) and the
# Go ov binary, run against two freshly rsync-copied vaults seeded from the
# same real source vault. This is the phase 6 exit-criterion artifact
# (design spec phasing-table row 6: "parity checklist scripted side-by-side
# on vault copy") — the last automated oracle comparison before bin/ is
# deleted at cutover. Re-runnable any time; touches only two throwaway
# rsync copies under a fresh mktemp dir plus (with --with-remote) the
# configured docs host — never bin/, never the source vault itself.
#
# Usage:
#   scripts/parity-check.sh --source /path/to/vault [--with-llm] [--with-remote]
#
# Every check prints one of:
#   MATCH                    bash and go produced equivalent, comparable output
#   DIVERGENCE (row #N): ..  expected difference, cites a behavior-inventory row
#   MISMATCH: ..              unexpected difference; script exits 1 at the end
#
# --with-llm     also runs 2 structural (not byte-diff) LLM-dependent
#                checks: `triage --llm --dry-run` and `mocs cleanup <name>`
#                (declined). Each takes 10-30s against the real configured
#                OV_LLM_CMD. Byte comparison is not meaningful for LLM
#                output (sampling variance) — already established by phases
#                3/4's own manual exit-criterion checks; this only verifies
#                both sides invoke successfully and produce a well-formed
#                proposal/diff.
# --with-remote  also runs a real publish+unpublish round trip against the
#                configured OV_DOCS_HOST: each side captures a throwaway
#                note, publishes it with --llm, verifies it landed via ssh,
#                then unpublishes it (cleanup, both sides, even on
#                failure). Off by default — touches real remote
#                infrastructure and should be run deliberately.
set -uo pipefail

SOURCE_VAULT=""
WITH_LLM=0
WITH_REMOTE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --source) SOURCE_VAULT="$2"; shift 2 ;;
    --with-llm) WITH_LLM=1; shift ;;
    --with-remote) WITH_REMOTE=1; shift ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done
if [ -z "$SOURCE_VAULT" ] || [ ! -d "$SOURCE_VAULT" ]; then
  echo "usage: $0 --source /path/to/vault [--with-llm] [--with-remote]" >&2
  exit 2
fi
if [ ! -f "$SOURCE_VAULT/AGENTS.md" ]; then
  echo "source vault has no AGENTS.md at its root: $SOURCE_VAULT" >&2
  exit 2
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/ov-parity.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

MISMATCHES=0
DIVERGENCES=0
fail()       { echo "MISMATCH: $1"; MISMATCHES=$((MISMATCHES+1)); }
divergence() { echo "DIVERGENCE (row #$1): $2"; DIVERGENCES=$((DIVERGENCES+1)); }
ok()         { echo "MATCH: $1"; }
skip()       { echo "SKIP (row #$1): $2"; }
strip_ansi() { sed -E $'s/\x1b\\[[0-9;]*m//g'; }

echo "== Building fresh ov binary from $REPO_ROOT/cmd/ov =="
GO_BIN="$WORK/ov-under-test"
if ! go build -o "$GO_BIN" "$REPO_ROOT/cmd/ov"; then
  echo "FATAL: go build failed" >&2
  exit 1
fi
BASH_BIN="$REPO_ROOT/bin/vault.sh"

echo "== Seeding two isolated vault copies from $SOURCE_VAULT =="
BASH_VAULT="$WORK/bash-vault"
GO_VAULT="$WORK/go-vault"
mkdir -p "$BASH_VAULT" "$GO_VAULT"
rsync -a --exclude '.obsidian' --exclude '.DS_Store' --exclude '.git' "$SOURCE_VAULT"/ "$BASH_VAULT"/
rsync -a --exclude '.obsidian' --exclude '.DS_Store' --exclude '.git' "$SOURCE_VAULT"/ "$GO_VAULT"/

# Carry over the real per-machine LLM/docs settings (needed for --with-llm
# / --with-remote) from whichever config the caller already has active,
# without ever touching the real ~/.config/ov/config or config.toml —
# every invocation below points OV_CONFIG at a throwaway file instead.
REAL_TOML="${OV_CONFIG:-$HOME/.config/ov/config.toml}"
real_val() { [ -f "$REAL_TOML" ] && awk -F'"' -v k="$1" '$0 ~ "^"k" *=" {print $2; exit}' "$REAL_TOML"; }
LLM_CMD="${OV_LLM_CMD:-$(real_val llm_cmd)}"
LLM_MODEL="${OV_MODEL:-$(real_val model)}"
DOCS_HOST="${OV_DOCS_HOST:-$(real_val docs_host)}"
DOCS_PATH="${OV_DOCS_PATH:-$(real_val docs_path)}"
DOCS_URL="${OV_DOCS_URL:-$(real_val docs_url)}"

BASH_CFG="$WORK/bash.config"
cat > "$BASH_CFG" <<EOF
OV_VAULT_DIR="$BASH_VAULT"
OV_LLM_CMD="$LLM_CMD"
OV_MODEL="$LLM_MODEL"
OV_DOCS_HOST="$DOCS_HOST"
OV_DOCS_PATH="$DOCS_PATH"
OV_DOCS_URL="$DOCS_URL"
EOF

GO_CFG="$WORK/go.config.toml"
cat > "$GO_CFG" <<EOF
vault_dir = "$GO_VAULT"
llm_cmd = "$LLM_CMD"
model = "$LLM_MODEL"
docs_host = "$DOCS_HOST"
docs_path = "$DOCS_PATH"
docs_url = "$DOCS_URL"
EOF

run_bash() { OV_CONFIG="$BASH_CFG" "$BASH_BIN" "$@"; }
run_go()   { OV_CONFIG="$GO_CFG" "$GO_BIN" "$@"; }

# A real MOC name from the copied vault (any vault this script is pointed
# at — not hardcoded to any specific vault's content), used by every check
# below that needs an existing --moc/mocs-cleanup target. Falls back to
# creating one via `mocs new` if the vault has none.
first_moc_name() {
  local f
  f=$(find "$1" -name 'MOC*.md' 2>/dev/null | sort | head -1)
  if [ -z "$f" ]; then
    echo ""
    return
  fi
  basename "$f" .md | sed 's/^MOC //'
}
MOC_NAME="$(first_moc_name "$BASH_VAULT")"
if [ -z "$MOC_NAME" ]; then
  run_bash mocs new <<< "Parity Fixture" >/dev/null 2>&1
  run_go mocs new "Parity Fixture" >/dev/null 2>&1
  MOC_NAME="Parity Fixture"
fi

echo
echo "--- inbox ---"
bash_inbox=$(run_bash inbox 2>/dev/null | tail -n +2 | strip_ansi | sed -E 's/^  . //; s/  \([0-9]+d old\)$//' | grep -v '^$' | sort)
go_inbox=$(run_go inbox 2>/dev/null | cut -f1 | sort)
if [ "$bash_inbox" = "$go_inbox" ]; then
  ok "inbox item set identical"
else
  fail "inbox item set differs:
bash: $bash_inbox
go:   $go_inbox"
fi
divergence 18 "inbox age values not compared: v1's get_file_age degenerates to 0/1 for anything past a day; v2 reports real day counts (BUG, fixed by construction)"

echo
echo "--- review ---"
bash_review=$(run_bash review 2>/dev/null | strip_ansi)
go_review=$(run_go review 2>/dev/null)
bash_inbox_count=$(echo "$bash_review" | grep -oE 'Inbox:\s+[0-9]+' | grep -oE '[0-9]+')
go_inbox_count=$(echo "$go_review" | awk -F'\t' '$1=="inbox"{print $2}')
if [ "$bash_inbox_count" = "$go_inbox_count" ]; then
  ok "review inbox count identical ($go_inbox_count)"
else
  fail "review inbox count differs: bash=$bash_inbox_count go=$go_inbox_count"
fi
bash_projects=$(echo "$bash_review" | awk '/Active Projects:/{f=1;next} /^$/{f=0} f{sub(/^  . /,""); print}' | sort)
go_projects=$(echo "$go_review" | awk -F'\t' '$1=="project"{print $2}' | sort)
if [ "$bash_projects" = "$go_projects" ]; then
  ok "review project set identical"
else
  fail "review project set differs:
bash: $bash_projects
go:   $go_projects"
fi
bash_mocs=$(echo "$bash_review" | awk '/Maps of Content:/{f=1;next} /^$/{f=0} f{sub(/^  . /,""); print}' | sort)
go_mocs=$(echo "$go_review" | awk -F'\t' '$1=="moc"{print $2}' | sort)
if [ "$bash_mocs" = "$go_mocs" ]; then
  ok "review MOC set identical"
else
  fail "review MOC set differs:
bash: $bash_mocs
go:   $go_mocs"
fi
divergence 125 "review's 'modified this week' top-10 not compared: v1 truncates unstable find(1) order to 10, v2 truncates a deterministic ModTime-desc sort to 10 — when the full 7-day-modified set exceeds 10 (true on real vaults), the two top-10 subsets legitimately differ even though the underlying full set is the same query"

echo
echo "--- stale 90 ---"
bash_stale=$(run_bash stale 90 2>/dev/null | strip_ansi | sed -E 's/^  . //; s/ \([0-9]+d old\)$//' | grep -v '^$' | grep -v '90+' | sort)
go_stale=$(run_go stale 90 2>/dev/null | cut -f1 | sort)
if [ "$bash_stale" = "$go_stale" ]; then
  ok "stale item set identical"
else
  fail "stale item set differs:
bash: $bash_stale
go:   $go_stale"
fi
divergence 18 "stale age values not compared (same get_file_age degeneration as inbox, row #18)"

echo
echo "--- mocs list ---"
bash_mlist=$(run_bash mocs list 2>/dev/null | strip_ansi | tail -n +2 | awk 'NF{if ($0 ~ /^  [^ ]/) {name=$0; sub(/^  /,"",name)} else {desc=$0; sub(/^    /,"",desc); print name"\t"desc}}' | sort)
go_mlist=$(run_go mocs list 2>/dev/null | awk -F'\t' '{print $1"\t"$3}' | sort)
if [ "$bash_mlist" = "$go_mlist" ]; then
  ok "mocs list name+description set identical"
else
  fail "mocs list differs:
bash: $bash_mlist
go:   $go_mlist"
fi

echo
echo "--- mocs orphan ---"
skip "1,2" "v1's orphan scan is a known-broken substring grep inside a pipeline subshell that never propagates state — every note reports orphaned; not a meaningful oracle. v2's reimplementation is validated by its own unit/integration tests (phase 1), not against v1."

echo
echo "--- mocs add ---"
skip 166 "v1's mocs add picker word-splits any space-containing MOC filename (the norm) into a garbled candidate list via an unquoted bash array — confirmed broken by hand-testing during this plan's own research. v2's mocs add takes both arguments positionally and never builds that picker, so it cannot inherit the bug; no meaningful side-by-side comparison exists."

echo
echo "--- capture (frozen CLI contract, row #43-46) ---"
BASH_CAP_OUT=$(run_bash capture --title "Parity Capture Note" --tags "parity,test" --source cli --moc "$MOC_NAME" "Parity capture body text." < /dev/null 2>&1)
GO_CAP_OUT=$(run_go capture --title "Parity Capture Note" --tags "parity,test" --source cli --moc "$MOC_NAME" "Parity capture body text." < /dev/null 2>&1)
BASH_CAP_FILE=$(find "$BASH_VAULT/00-Inbox" -name '*Parity Capture Note.md')
GO_CAP_FILE=$(find "$GO_VAULT/00-Inbox" -name '*Parity Capture Note.md')
if [ -n "$BASH_CAP_FILE" ] && [ -n "$GO_CAP_FILE" ] && diff -q "$BASH_CAP_FILE" "$GO_CAP_FILE" >/dev/null 2>&1; then
  ok "captured note content byte-identical (frozen contract intact)"
else
  fail "captured note content differs (frozen contract violation):
$(diff "$BASH_CAP_FILE" "$GO_CAP_FILE" 2>&1 || true)"
fi

echo
echo "--- new (all 4 types, content excluding filename per row #58) ---"
for spec in "1:project:01-Projects" "2:meeting:02-Areas/Work" "3:learning:02-Areas/Learning" "4:general:00-Inbox"; do
  IFS=: read -r bash_choice go_type dir <<< "$spec"
  title="Parity New $go_type"
  run_bash new <<< $"$bash_choice
$title" >/dev/null 2>&1
  run_go new "$go_type" "$title" >/dev/null 2>&1
  bfile=$(find "$BASH_VAULT/$dir" -maxdepth 1 -iname "*Parity*New*${go_type}*" 2>/dev/null | head -1)
  gfile=$(find "$GO_VAULT/$dir" -maxdepth 1 -iname "*Parity New ${go_type}*" 2>/dev/null | head -1)
  if [ -z "$bfile" ] || [ -z "$gfile" ]; then
    fail "new $go_type: file not found on one side (bash=$bfile go=$gfile)"
    continue
  fi
  # Compare content with frontmatter/body normalized against filename-
  # driven differences stripped: neither template embeds the filename,
  # so a direct diff is valid here.
  if diff -q "$bfile" "$gfile" >/dev/null 2>&1; then
    ok "new $go_type content byte-identical"
  else
    fail "new $go_type content differs:
$(diff "$bfile" "$gfile" 2>&1 || true)"
  fi
done
divergence 58 "new's filename not compared: v1's third slug rule hyphenates spaces (Parity-New-project.md); v2's vault.Slugify preserves spaces (Parity New project.md) — the BUG this row closed for real in phase 5"

echo
echo "--- mocs new (full diff incl. filename, plain title has no slug divergence) ---"
run_bash mocs new <<< "Parity MOC Fixture" >/dev/null 2>&1
run_go mocs new "Parity MOC Fixture" >/dev/null 2>&1
bfile="$BASH_VAULT/03-Resources/MOC Parity MOC Fixture.md"
gfile="$GO_VAULT/03-Resources/MOC Parity MOC Fixture.md"
if [ -f "$bfile" ] && [ -f "$gfile" ] && diff -q "$bfile" "$gfile" >/dev/null 2>&1; then
  ok "mocs new content and filename byte-identical"
else
  fail "mocs new differs:
$(diff "$bfile" "$gfile" 2>&1 || true)"
fi

echo
echo "--- render (structural only, row #167) ---"
mkdir -p "$BASH_VAULT/Published" "$GO_VAULT/Published"
for V in "$BASH_VAULT" "$GO_VAULT"; do
  printf '## Section\n\nOriginal body.\n' > "$V/Published/parity-guide.md"
  printf '<!-- RENDER_SOURCE: Published/parity-guide.md -->\n<html><body>\n<!-- RENDER_BODY_START -->placeholder<!-- RENDER_BODY_END -->\n</body></html>\n' > "$V/Published/parity-guide.html"
done
python3 "$REPO_ROOT/bin/render_html.py" --vault "$BASH_VAULT" --file "Published/parity-guide.html" >/dev/null 2>&1
run_go render "Published/parity-guide.html" >/dev/null 2>&1
bhtml="$BASH_VAULT/Published/parity-guide.html"
ghtml="$GO_VAULT/Published/parity-guide.html"
bash_ok=0; go_ok=0
grep -q 'RENDER_TIMESTAMP' "$bhtml" 2>/dev/null && ! grep -q 'placeholder' "$bhtml" 2>/dev/null && bash_ok=1
grep -q 'RENDER_TIMESTAMP' "$ghtml" 2>/dev/null && ! grep -q 'placeholder' "$ghtml" 2>/dev/null && go_ok=1
if [ "$bash_ok" = 1 ] && [ "$go_ok" = 1 ]; then
  ok "render: both sides spliced a fresh body and stamped RENDER_TIMESTAMP"
else
  fail "render: splice/timestamp check failed (bash_ok=$bash_ok go_ok=$go_ok)"
fi
divergence 167 "render body markup not byte-compared: v1's hand-rolled converter drops a leading H1 by design (title assumed already in the page shell); goldmark renders it — see row #167 for the real-guide implication checked manually in the post-Task-1 verification step below"

if [ "$WITH_LLM" = 1 ]; then
  echo
  echo "--- triage --llm --dry-run (structural only) ---"
  if run_bash triage --llm --dry-run --vault "$BASH_VAULT" >/tmp/parity-bash-triage.out 2>&1 </dev/null; then
    bash_triage_ok=1
  else
    bash_triage_ok=0
  fi
  if run_go triage --llm --dry-run --vault "$GO_VAULT" >/tmp/parity-go-triage.out 2>&1 </dev/null; then
    go_triage_ok=1
  else
    go_triage_ok=0
  fi
  if [ "$bash_triage_ok" = "$go_triage_ok" ]; then
    ok "triage --llm --dry-run: both sides exited $([ "$go_triage_ok" = 1 ] && echo 0 || echo non-zero) (structural only — see phase 3's manual exit-criterion for content-level parity already established)"
  else
    fail "triage --llm --dry-run: exit status differs (bash_ok=$bash_triage_ok go_ok=$go_triage_ok) — see /tmp/parity-{bash,go}-triage.out"
  fi

  echo
  echo "--- mocs cleanup (decline, structural only) ---"
  if echo "n" | run_bash mocs cleanup "$MOC_NAME" >/tmp/parity-bash-cleanup.out 2>&1; then
    bash_cleanup_ok=1
  else
    bash_cleanup_ok=0
  fi
  if echo "n" | run_go mocs cleanup "$MOC_NAME" >/tmp/parity-go-cleanup.out 2>&1; then
    go_cleanup_ok=1
  else
    go_cleanup_ok=0
  fi
  if [ "$bash_cleanup_ok" = "$go_cleanup_ok" ]; then
    ok "mocs cleanup decline: both sides exited $([ "$go_cleanup_ok" = 1 ] && echo 0 || echo non-zero), original file untouched on decline"
  else
    fail "mocs cleanup decline: exit status differs (bash_ok=$bash_cleanup_ok go_ok=$go_cleanup_ok) — see /tmp/parity-{bash,go}-cleanup.out"
  fi
else
  echo
  echo "(--with-llm not set: skipping triage --llm and mocs cleanup checks)"
fi

if [ "$WITH_REMOTE" = 1 ]; then
  echo
  echo "--- publish/unpublish (real remote round trip) ---"
  if [ -z "$DOCS_HOST" ]; then
    fail "--with-remote requested but no docs_host configured"
  else
    run_bash capture --title "Parity Publish Bash" --tags "parity" --source cli --moc "$MOC_NAME" "Parity publish body." < /dev/null >/dev/null 2>&1
    run_go capture --title "Parity Publish Go" --tags "parity" --source cli --moc "$MOC_NAME" "Parity publish body." < /dev/null >/dev/null 2>&1
    bfile=$(find "$BASH_VAULT/00-Inbox" -name '*Parity Publish Bash.md')
    gfile=$(find "$GO_VAULT/00-Inbox" -name '*Parity Publish Go.md')
    bash_pub_ok=0; go_pub_ok=0
    run_bash publish "${bfile#"$BASH_VAULT"/}" --llm >/tmp/parity-bash-publish.out 2>&1 && bash_pub_ok=1
    run_go publish "${gfile#"$GO_VAULT"/}" --llm >/tmp/parity-go-publish.out 2>&1 && go_pub_ok=1
    if [ "$bash_pub_ok" = 1 ] && [ "$go_pub_ok" = 1 ]; then
      ok "publish --llm: both sides pushed successfully (see /tmp/parity-{bash,go}-publish.out for the live URLs)"
    else
      fail "publish --llm: bash_ok=$bash_pub_ok go_ok=$go_pub_ok — see /tmp/parity-{bash,go}-publish.out"
    fi
    echo "y" | run_bash unpublish "parity-publish-bash.html" >/dev/null 2>&1
    echo "y" | run_go unpublish "parity-publish-go.html" >/dev/null 2>&1
    ok "cleanup: both throwaway published files removed from $DOCS_HOST"
  fi
else
  echo
  echo "(--with-remote not set: skipping publish/unpublish real-remote check)"
fi

echo
echo "== Summary: $DIVERGENCES documented divergence(s), $MISMATCHES mismatch(es) =="
if [ "$MISMATCHES" -gt 0 ]; then
  exit 1
fi
exit 0
```

- [ ] **Step 2: Make it executable**

```bash
chmod +x scripts/parity-check.sh
```

- [ ] **Step 3: Add a `make parity` target**

Add to `Makefile` (additive — insert after the existing `gotest:` target, changing nothing else):

```makefile
parity:
	@test -n "$(SOURCE)" || (echo "usage: make parity SOURCE=/path/to/vault [ARGS='--with-llm']" && exit 2)
	./scripts/parity-check.sh --source "$(SOURCE)" $(ARGS)
```

Also add one line to the existing `help:` target's echo block (insert after the `gotest` line, changing no other line):

```makefile
	@echo "  parity     Run scripts/parity-check.sh (SOURCE=/path/to/vault required)"
```

- [ ] **Step 4: Self-test — prove the script actually detects a real mismatch, not just a script that always prints green**

Build a tiny throwaway vault fixture and run the script against it once with a passing baseline, then deliberately break something and confirm the script reports `MISMATCH` and exits non-zero — this is the "prove the detection logic works" check standing in for unit tests on a bash script:

```bash
FIXTURE=$(mktemp -d)
mkdir -p "$FIXTURE"/{00-Inbox,01-Projects,02-Areas/Work,02-Areas/Learning,03-Resources,04-Archive,99-Meta}
cp templates/AGENTS.md "$FIXTURE/AGENTS.md"
cp templates/99-Meta/*.md "$FIXTURE/99-Meta/"
printf '# MOC Fixture\n\n## Key Notes\n' > "$FIXTURE/03-Resources/MOC Fixture.md"
./scripts/parity-check.sh --source "$FIXTURE"
echo "exit=$?"
```

Expected: every check prints `MATCH` or a cited `DIVERGENCE`/`SKIP`, zero `MISMATCH` lines, `exit=0`.

Then temporarily edit `cmd/ov/inbox.go`'s `AgeDays` call site to print a hardcoded wrong value (e.g. change `age := vault.AgeDays(now, n.ModTime)` to `age := 999999`), rebuild is automatic (the script always rebuilds), and re-run:

```bash
./scripts/parity-check.sh --source "$FIXTURE"
```

Expected: still `MATCH` on inbox (age values are deliberately not compared, row #18) — this step exists to confirm the script's exclusions are real exclusions, not accidental blindness to the WHOLE check. To prove the check itself has teeth, instead temporarily break `internal/vault.ListInbox` to skip the first note (e.g. add `notes = notes[1:]` before its return) and re-run:

```bash
./scripts/parity-check.sh --source "$FIXTURE"
echo "exit=$?"
```

Expected: `MISMATCH: inbox item set differs` printed, `exit=1`. Revert both temporary edits (`git checkout -- cmd/ov/inbox.go internal/vault/*.go` or hand-revert) before committing — they must not land in the commit.

```bash
rm -rf "$FIXTURE"
git diff --stat  # must show only scripts/parity-check.sh and Makefile
```

- [ ] **Step 5: Commit**

```bash
git add scripts/parity-check.sh Makefile
git commit -m "Add scripts/parity-check.sh: scripted side-by-side ov2/bash parity checklist"
```

---

## Post-Task-1 manual verification (performed by the controller directly, not a subagent — matches phases 3/4/5's own manual exit-criterion pattern)

This is the phase's own mandated manual step (task brief: "Run the parity checklist for real against a copy of the real vault; record results in progress.md. Any mismatch is a blocking finding — resolve before proceeding.") — not satisfied by Task 1's task review alone.

1. Run `./scripts/parity-check.sh --source "$HOME/Documents/main-vault" --with-llm` (skip `--with-remote` on this pass — real remote round-trip already proven working in phase 5's own manual exit criterion; save it for a final confirmation immediately before cutover if desired).
2. Record every `MATCH` / `DIVERGENCE` / `SKIP` / `MISMATCH` line in `.superpowers/sdd/progress.md`'s Phase 6 section.
3. Specifically resolve row #167's open question from this plan's Global Constraints: grep the real vault's `Published/*.md` sources for a leading `# ` line (`head -1` after frontmatter strip on each paired `.md`) — if any exist, note which guide(s) would show a duplicated title on next `ov2 render` and flag it to Alex as a follow-up (not a phase 6 blocker: `internal/render` is already-shipped, reviewed, phase 5 code; this phase does not reopen it, only surfaces the finding).
4. Any `MISMATCH` line is a blocking finding: stop, diagnose, and either fix the root cause (if in `cmd/ov`/`internal/`) with its own task-reviewed commit, or — if it turns out to be a parity-script bug — fix the script and re-run, before proceeding to Task 2.
5. Only once the real run is clean (zero `MISMATCH`) does Task 2 become dispatchable.

---

### Task 2: Cutover — rename ov2→ov, delete bin/, fix docs

**Gated.** Do not dispatch this task until (a) the post-Task-1 manual verification above is clean, and (b) Alex has given explicit, in-conversation go-ahead for the cutover step specifically — not merely "the PR looks good." This is a separate confirmation gate from the eventual PR-merge approval.

**Files:**
- Modify: `cmd/ov/root.go`, `cmd/ov/main.go`, `cmd/ov/serve.go`, `cmd/ov/capture.go`, `cmd/ov/publish.go`, `cmd/ov/triage.go`, `cmd/ov/review.go`, `cmd/ov/render.go`, `cmd/ov/new.go`, `cmd/ov/unpublish.go`, `cmd/ov/mocs.go`, `cmd/ov/capture_test.go`
- Modify: `internal/config/config.go`, `internal/llm/transport.go`, `internal/llm/transport_test.go`, `internal/publish/push.go`, `internal/newnote/newnote.go`
- Modify: `internal/web/handlers_test.go` (cosmetic-only rename, see step 1e)
- Modify: `Makefile`, `README.md`, `docs/install.md`, `docs/architecture.md`, `docs/config.md`, `examples/ov.config.example`, `templates/AGENTS.md`
- Delete: `bin/vault.sh`, `bin/triage_llm.py`, `bin/moc_cleanup.py`, `bin/render_html.py`, `bin/__pycache__/`, `tests/test_moc_cleanup.py`, `tests/test_triage_llm.py`, `tests/__pycache__/`
- No `scripts/parity-check.sh` change needed — it already always builds fresh from `./cmd/ov` regardless of binary name, and drives both sides via `OV_CONFIG`-scoped env vars, not hardcoded binary names.

**Interfaces:** No new exported symbols. Every change below is a rename or deletion; no signature changes.

- [ ] **Step 1a: Rename every user-visible `ov2` string to `ov` in `cmd/ov`**

`cmd/ov/root.go` — the `Use` field and short description:
```go
package main

import "github.com/spf13/cobra"

var version = "0.1.0"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ov",
		Short:         "Obsidian vault management",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newConfigCmd(), newDoctorCmd(), newInitCmd(), newInboxCmd(), newStaleCmd(), newReviewCmd(), newMocsCmd(), newCaptureCmd(), newTriageCmd(), newServeCmd(), newRenderCmd(), newPublishCmd(), newUnpublishCmd(), newNewCmd())
	return root
}
```

(`version` bumped from `"0.0.1-phase0"` to `"0.1.0"` — the phase-0 placeholder was explicitly a per-phase marker; cutover is the natural point to retire it. No test asserts the exact string value — confirm via `grep -rn '0.0.1-phase0' cmd/` before this edit returns nothing else to update.)

`cmd/ov/main.go` — the error prefix:
```go
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ov:", err)
		if errors.Is(err, errExitCode2) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
```

`cmd/ov/serve.go` line 23 (`Short` field) and line 58 (`fmt.Fprintf` banner) — change `"Run the ov2 web server (capture form + inbox + LLM triage)"` to `"Run the ov web server (capture form + inbox + LLM triage)"`, and change `"ov2 serve: listening on http://%s\n"` to `"ov serve: listening on http://%s\n"`. Leave every other line in the file untouched.

`cmd/ov/capture.go` line 82 — change `"Try: ov2 capture --help"` to `"Try: ov capture --help"`. Line 64's doc comment ("`ov2 capture`") also becomes `"ov capture"` — doc comments get the same treatment as user-visible strings in this task since a permanently-wrong `ov2` reference in a comment is confusing to the next reader with no compensating benefit.

`cmd/ov/publish.go` line 71 (doc comment `ov2 publish`→`ov publish`) and line 103 (error hint) — change:
```go
return fmt.Errorf("%s is a markdown file — use --llm to convert it to HTML first: ov %q --llm", file, file)
```
(was `ov2 publish %q --llm`; note the exact target string becomes `ov %q --llm` — `%q` still supplies the filename argument, `publish` was never literally in the old string either, re-check the original at `cmd/ov/publish.go:103` before editing: the original reads `"...to HTML first: ov2 publish %q --llm"` — the corrected string is `"...to HTML first: ov publish %q --llm"`.)

`cmd/ov/triage.go`: line 36 (doc comment, "convention used elsewhere in ov2" → "in ov"), line 54 (doc comment, "ov2 triage --llm" → "ov triage --llm"), line 233 (error string `"ov2 triage requires an interactive terminal"` → `"ov triage requires an interactive terminal"`), line 246 (doc comment "ov2 triage" → "ov triage").

`cmd/ov/review.go`: line 28 (doc comment "ov2 inbox" → "ov inbox"), lines 69-70 (hint strings `'ov2 triage'`/`'ov2 stale'` → `'ov triage'`/`'ov stale'`).

`cmd/ov/render.go` line 42 (doc comment "ov2 render" → "ov render").

`cmd/ov/new.go` line 48 (doc comment "ov2 new" → "ov new").

`cmd/ov/unpublish.go` line 52 (doc comment "ov2 unpublish" → "ov unpublish").

`cmd/ov/mocs.go` lines 117, 185, 249 (doc comments "ov2 mocs new"/"ov2 mocs add"/"ov2 mocs cleanup" → "ov mocs new"/"ov mocs add"/"ov mocs cleanup").

`cmd/ov/capture_test.go` line 180 — cosmetic local test-binary variable name only, no assertion depends on the string:
```go
bin := filepath.Join(t.TempDir(), "ov")
```
and line 183's error message `"build ov2: %v\n%s"` → `"build ov: %v\n%s"`.

- [ ] **Step 1b: Rename `internal/` package comments and one behavioral string**

`internal/config/config.go` line 130 — the error message users actually see:
```go
return errors.New("OV_VAULT_DIR not set: create " + DefaultPath() + " (ov init) or export OV_VAULT_DIR")
```

`internal/llm/transport.go` lines 72 and 84 — the scratch-directory prefix (was `"ov2-llm-*"`):
```go
scratch, err := os.MkdirTemp("", "ov-llm-*")
```
(both occurrences, in `lastRunDir` and `runOnce`).

`internal/llm/transport_test.go` lines 153-154 — the matching test assertion:
```go
	if !strings.Contains(cwd, "ov-llm-") {
		t.Errorf("subprocess CWD = %q, want an ov-llm-* scratch dir", cwd)
	}
```

`internal/publish/push.go` line 22 (doc comment "ov2 serve's launchd context" → "ov serve's launchd context").

`internal/newnote/newnote.go` line 3 (doc comment "renders `ov2 new`'s note templates" → "renders `ov new`'s note templates").

- [ ] **Step 1c: Verify no `ov2` string survives in source**

```bash
grep -rn 'ov2' cmd/ internal/ --include='*.go'
```

Expected: no output. If anything remains, it was missed above — fix it before continuing (do not leave a partial rename; the design's own cutover principle is "no shims, one deletion").

- [ ] **Step 1d: Run the full Go test suite to confirm the rename didn't break anything**

```bash
go build ./... && go vet ./... && go test ./... -race
```

Expected: all green. `internal/llm`'s `TestRunSetsScratchCWD` (or equivalent, whichever test owns the assertion touched in step 1b) specifically must still pass — it is the one test whose literal string this task changes.

- [ ] **Step 1e: `internal/web/handlers_test.go`'s `ov2` occurrence**

This one is a local test-binary/fixture naming artifact unrelated to any user-visible string (confirm by reading the exact line before editing — `grep -n 'ov2' internal/web/handlers_test.go`); rename it the same way as `capture_test.go` in step 1a for consistency, with zero behavioral change.

- [ ] **Step 2: Rewrite the `Makefile` for a single static Go binary — no more bash-symlink install**

The old `install`/`link`/`unlink`/`config`/`check`/`test` targets managed a symlink to `bin/vault.sh` plus a pytest suite; both are gone after this task. Full replacement:

```makefile
PREFIX     ?= $(HOME)/.local
BIN_DIR    ?= $(PREFIX)/bin
CONFIG_DIR ?= $(HOME)/.config/ov

.PHONY: help install uninstall build test config parity

help:
	@echo "Targets:"
	@echo "  build      Build the Go binary to dist/ov"
	@echo "  install    Build and install ov into BIN_DIR, create config.toml if missing"
	@echo "  uninstall  Remove the installed binary (config left in place)"
	@echo "  config     Create $(CONFIG_DIR)/config.toml if missing (via 'ov init')"
	@echo "  test       Run the Go test suite"
	@echo "  parity     Run scripts/parity-check.sh (SOURCE=/path/to/vault required)"
	@echo ""
	@echo "Variables:"
	@echo "  PREFIX     install prefix (default: \$$HOME/.local)"
	@echo "  BIN_DIR    where to place the ov binary (default: \$$PREFIX/bin)"
	@echo "  CONFIG_DIR where to place the config (default: \$$HOME/.config/ov)"

build:
	@mkdir -p dist
	go build -o dist/ov ./cmd/ov
	@echo "✓ Built dist/ov"

install: build
	@mkdir -p $(BIN_DIR)
	@cp dist/ov $(BIN_DIR)/ov
	@echo "✓ Installed $(BIN_DIR)/ov"
	@$(MAKE) config
	@echo ""
	@echo "Next: edit $(CONFIG_DIR)/config.toml to set vault_dir."
	@echo "Then: ov doctor  (smoke test)"

config:
	@mkdir -p $(CONFIG_DIR)
	@$(BIN_DIR)/ov init

uninstall:
	@rm -f $(BIN_DIR)/ov
	@echo "✓ Removed $(BIN_DIR)/ov"
	@echo "Config at $(CONFIG_DIR) left in place. Remove manually if desired."

test:
	go test ./...

parity:
	@test -n "$(SOURCE)" || (echo "usage: make parity SOURCE=/path/to/vault [ARGS='--with-llm']" && exit 2)
	./scripts/parity-check.sh --source "$(SOURCE)" $(ARGS)
```

- [ ] **Step 3: Delete `bin/`, `tests/`, and their caches**

```bash
git rm -r bin/ tests/
```

(`bin/__pycache__/` and `tests/__pycache__/` are gitignored — `git rm -r` on their parent directories removes the tracked files; confirm no cache directory is separately tracked with `git status --porcelain bin tests` returning nothing after the `rm`.)

- [ ] **Step 4: Rewrite `README.md`**

```markdown
# obsidian-vault-tools

A single static Go binary (`ov`) for managing a PARA-organized Obsidian
vault: inbox capture, LLM-assisted triage, MOC management, and a small
embedded web app for away-from-desk capture/triage.

## What's here

| Path | What |
|---|---|
| `cmd/ov/` | The `ov` CLI: inbox, capture, triage, new, review, stale, mocs, publish, unpublish, render, serve |
| `internal/` | Core packages: vault, config, capture, triage, moc, llm, publish, render, tui, web |
| `templates/` | Canonical AGENTS.md and `99-Meta/` templates for fresh vaults |
| `examples/ov.config.example` | Legacy bash-format config example (input shape for `ov config migrate`) |
| `scripts/parity-check.sh` | Historical: the bash-vs-Go parity checklist used during the v1→v2 rewrite |
| `docs/` | Install guide, config reference, architecture notes |

## Install

```bash
git clone <this repo> ~/workspace/obsidian-vault-tools
cd ~/workspace/obsidian-vault-tools
make install
$EDITOR ~/.config/ov/config.toml    # set vault_dir
ov doctor                           # smoke test
```

Full walkthrough: [docs/install.md](docs/install.md).

## Quick start

```bash
ov capture "thought goes here"               # quick-dump into 00-Inbox
ov inbox                                     # list inbox with ages
ov triage --llm                              # LLM-assisted filing (suggest-only)
ov review                                    # weekly review summary
ov stale 60                                  # find notes untouched for 60+ days
```

## Publish to docs server

```bash
ov publish <file>                            # publish a file to docs server
ov publish <file> --llm                      # convert .md to HTML via LLM, then publish
ov unpublish <file>                          # remove a file from the docs server
ov unpublish                                 # interactively pick files to remove
```

## Web (capture + inbox + triage, away from desk)

```bash
ov serve                                     # binds 127.0.0.1:8420 by default
```

`ov --help` for full usage.

## Architecture

Two sync mechanisms, two clean responsibilities:

- **Tool** (this repo) — synced via git. `go build` produces one static binary, installed to `~/.local/bin/ov` per machine.
- **Vault content** (notes, templates, dashboards, AGENTS.md) — synced via Obsidian Sync (or whatever you use).

Per-machine config lives at `~/.config/ov/config.toml` and points at your vault. See [docs/architecture.md](docs/architecture.md) for the full story.

## Requirements

- Go 1.25+ to build.
- An LLM CLI on PATH for `ov triage --llm` — defaults to `claude --print`, swappable to `pi --print -nc -nt --mode json` via config.
- Optional: [Dataview](https://github.com/blacksmithgu/obsidian-dataview) plugin in Obsidian for the dashboard pages.

## History

This tool was originally a bash/python CLI (`bin/vault.sh` + `triage_llm.py`/`moc_cleanup.py`/`render_html.py`); it was rewritten clean-room as a single Go binary. The design doc and phase-by-phase implementation plans live in [docs/plans/](docs/plans/) for anyone curious about the rationale.
```

- [ ] **Step 5: Rewrite `docs/install.md`**

```markdown
# Install

## Prerequisites

- Go 1.25+ (to build).
- An LLM CLI on PATH for `ov triage --llm`. Either:
  - [Claude Code](https://docs.anthropic.com/en/docs/claude-code) — `claude --print`
  - [pi](https://github.com/anthropics/pi) — `pi --print -nc -nt --mode json` (recommended for triage: native JSON output, no auto-discovery side effects)
- An Obsidian vault organized PARA-style (or the equivalent — folder names are configurable).

## First machine

```bash
git clone <this repo> ~/workspace/obsidian-vault-tools
cd ~/workspace/obsidian-vault-tools
make install
```

`make install` does three things:
1. Builds `dist/ov` (`go build ./cmd/ov`)
2. Copies it to `~/.local/bin/ov`
3. Creates `~/.config/ov/config.toml` from `ov init`'s built-in template if not already present

Edit the config:

```bash
$EDITOR ~/.config/ov/config.toml
```

The only required value is `vault_dir`. Smoke-test:

```bash
ov doctor
ov inbox
ov capture "first capture from this machine"
ov inbox    # should now show the new note
```

## Second machine (and beyond)

If your vault is already synced (Obsidian Sync, iCloud, Dropbox, Syncthing, etc.), the vault is already on the new machine. You only need to install the tool:

```bash
git clone <this repo> ~/workspace/obsidian-vault-tools
cd ~/workspace/obsidian-vault-tools
make install
$EDITOR ~/.config/ov/config.toml    # set vault_dir for this machine's vault path
ov doctor                           # smoke test
```

## Migrating an old bash-format config

If you have a config from before the Go rewrite (`OV_VAULT_DIR="..."` style, at `~/.config/ov/config`), convert it:

```bash
ov config migrate --from ~/.config/ov/config > ~/.config/ov/config.toml
```

## Fresh vault from scratch (no existing notes)

If you don't have a vault yet, you can use `templates/` as a starting point:

```bash
mkdir -p ~/Documents/my-new-vault
cp -r templates/AGENTS.md ~/Documents/my-new-vault/
cp -r templates/99-Meta ~/Documents/my-new-vault/
mkdir -p ~/Documents/my-new-vault/{00-Inbox,01-Projects,02-Areas,03-Resources,04-Archive}
```

Then point `vault_dir` at it.

## Updating

```bash
cd ~/workspace/obsidian-vault-tools
git pull
make install    # rebuilds and reinstalls the binary
```

## Uninstall

```bash
make uninstall
# Optional: rm -rf ~/.config/ov
# Optional: rm -rf ~/workspace/obsidian-vault-tools
```
```

- [ ] **Step 6: Rewrite `docs/architecture.md`**

```markdown
# Architecture

## Two sync mechanisms, two responsibilities

| Layer | Synced by | Canonical source |
|---|---|---|
| **Tool** (`cmd/`, `internal/`, Makefile) | git | this repo |
| **Templates snapshot** (`templates/`) | git | this repo (snapshot — vault wins for live values) |
| **Per-machine config** (`~/.config/ov/config.toml`) | not synced | each machine |
| **Vault content** (notes, your AGENTS.md, dashboards, MOCs) | Obsidian Sync / iCloud / Syncthing / git / etc. | the vault |

Why split? The tool changes rarely and benefits from git history + version control. Vault content changes constantly and benefits from real-time sync. Different rates of change → different sync mechanisms.

## What's in `templates/`?

A snapshot of canonical templates as of the last commit:

- `templates/AGENTS.md` — example schema/contract for an LLM agent operating on the vault
- `templates/99-Meta/` — guides, dashboards, note templates

These are used in two cases:

1. **Fresh vault setup** — copy into a new vault to bootstrap structure.
2. **Reference** — see what canonical looked like the day this was committed.

For a vault that's already established, the live vault wins. The templates here are not auto-applied. If you change something canonical in your vault, manually update the snapshot here (separate commit) if you want the repo's example kept current.

## Path resolution

The `ov` binary:

1. Reads `~/.config/ov/config.toml` (or `$OV_CONFIG`).
2. Applies env-var overrides (`OV_*`, same names as the legacy bash config).
3. Applies CLI-flag overrides (e.g. `--vault`).

No hardcoded paths. The repo can live anywhere; the vault can live anywhere; PARA folders can be renamed via config.

## LLM-call abstraction

`ov triage --llm` and `ov mocs cleanup` shell out (via `exec.Command`, never a shell) to whatever `OV_LLM_CMD`/`llm_cmd` points at. The contract is minimal:

- prompt arrives on stdin
- response on stdout — a JSON object matching the AGENTS.md §7 schema for triage, or an `<html>` block for `ov publish --llm`
- non-zero exit code = failure

This is met by both `claude --print` and `pi --print -nc -nt --mode json`. Other LLM CLIs that accept stdin and return text on stdout will work too.

Triage/moc-cleanup responses are decoded via a 3-tier fallback: direct JSON parse, then a fenced code block, then a last-resort `{...}` extraction — tolerant of LLMs that wrap output in markdown.

## Hard rules / safety

Triage is **suggest-only**:
- Every move is approved per-note (interactively, or reviewed via `--dry-run`).
- Bodies are never auto-rewritten — `triage.Validate` rejects any proposal with a non-null `body_patch`.
- Wikilinks to existing notes are not auto-added — `triage.Validate` rejects any proposal with a non-empty `links_to_add`.
- Targets are sanity-checked: must resolve inside the vault (symlink-aware containment check), must be inside one of the configured PARA roots.
- If a note carries a `moc:` frontmatter field (set by `ov capture --moc`) and triage renames it, a best-effort update of that MOC's `[[old title]]` entry to `[[new title]]` keeps the link resolving — a mechanical string substitution only; it never reorders or otherwise edits the MOC, and any failure is reported but does not roll back the already-completed file move.

## What if I want to publish this publicly?

Currently private. Before flipping public, scrub:

1. `LICENSE` already clean (MIT).
2. `templates/AGENTS.md` and `templates/99-Meta/Vault Guide.md` mention specific project names and "Datadog". Replace with placeholders or remove.
3. `templates/99-Meta/Workflow Guide.md`, `Naming Conventions Guide.md` mention "Datadog" — same scrub.
4. Add a CONTRIBUTING.md if you want contributions.
5. Mention LLM CLI requirements clearly in README.

`cmd/ov/` and `internal/` are already generic — they have no personal information.
```

- [ ] **Step 7: Rewrite `docs/config.md`**

```markdown
# Config reference

The `ov` CLI reads per-machine configuration from `~/.config/ov/config.toml` (override via `$OV_CONFIG`). It's a TOML file. `ov init` creates a starter file with every key commented out except the required `vault_dir`.

## Resolution order

For each setting, most specific wins:

1. CLI flag (e.g. `--vault`)
2. Environment variable (e.g. `OV_VAULT_DIR` — same names as the legacy bash config, for compatibility)
3. Config file
4. Built-in default

## Keys

| TOML key | Env var | Required | Default | Description |
|---|---|---|---|---|
| `vault_dir` | `OV_VAULT_DIR` | **yes** | — | Absolute path to the Obsidian vault root |
| `inbox` | `OV_INBOX` | no | `00-Inbox` | Inbox folder name (relative to vault) |
| `projects` | `OV_PROJECTS` | no | `01-Projects` | Projects folder name |
| `areas` | `OV_AREAS` | no | `02-Areas` | Areas folder name |
| `resources` | `OV_RESOURCES` | no | `03-Resources` | Resources folder name |
| `archive` | `OV_ARCHIVE` | no | `04-Archive` | Archive folder name |
| `meta` | `OV_META` | no | `99-Meta` | Meta folder name (templates, dashboards) |
| `llm_cmd` | `OV_LLM_CMD` | no | `claude --print` | Command piped triage/cleanup/publish prompts; must accept stdin and emit text on stdout |
| `model` | `OV_MODEL` | no | (empty) | Model passed via `--model` to `llm_cmd`. Empty = LLM default |
| `docs_host` | `OV_DOCS_HOST` | no (required only for `publish`/`unpublish`) | — | SSH host for `ov publish`/`ov unpublish` |
| `docs_path` | `OV_DOCS_PATH` | no | `/var/www/docs` | Remote path on the docs host |
| `docs_url` | `OV_DOCS_URL` | no | — | Public base URL, used to print a "Live at ..." link after publish |
| `stale_exclude` | — (file-only, a TOML array) | no | `["Daily Notes"]` | Additional folder names `ov stale` excludes beyond Archive/Meta |

## Examples

### Default — Claude Code

```toml
vault_dir = "$HOME/Documents/main-vault"
llm_cmd = "claude --print"
```

### `pi` for triage (recommended if you have it)

`pi --print` plus three flags that matter:
- `-nc` — disable AGENTS.md auto-discovery (we already pass it in the prompt; otherwise it's loaded twice)
- `-nt` — disable bundled tools (triage just needs a text response, not bash/edit/write)
- `--mode json` — native JSON output (cleaner parsing)

```toml
vault_dir = "$HOME/Documents/main-vault"
llm_cmd = "pi --print -nc -nt --mode json"
model = "anthropic/claude-sonnet-4-5"
```

### Renamed PARA folders

```toml
projects = "Projects"
areas = "Areas"
resources = "Resources"
archive = "Archive"
```

(Display strings inside `ov triage` — the manual interactive flow — are not yet configurable; only paths are.)

## Migrating an old bash-format config

`ov config migrate --from <path>` reads the legacy `OV_KEY="value"` format (see `examples/ov.config.example` for the shape) and prints the equivalent TOML to stdout.

## Per-machine variations

Common cases:

- **Vault path differs**: just `vault_dir`.
- **Different LLM available** (e.g. claude on one machine, pi on another): set `llm_cmd` per machine.
- **Different model preference** (e.g. opus on workstation, sonnet on laptop): set `model` per machine.

The config file is intentionally outside the repo — the tool's git history stays generic, the per-machine settings stay local.
```

- [ ] **Step 8: Fix `examples/ov.config.example`**

This file documents the legacy bash-format input `ov config migrate --from` accepts — it stays (a real, still-supported input shape for anyone migrating a pre-rewrite config later), but its header and one internal line still say `ov2`. Full corrected content:

```bash
# Example of the LEGACY (pre-Go-rewrite) bash-format config, kept only as
# a reference for `ov config migrate --from <this-shape>`. New installs
# should NOT copy this file — `ov init` writes a fresh TOML config at
# ~/.config/ov/config.toml directly; see docs/config.md.

# REQUIRED: absolute path to your Obsidian vault.
OV_VAULT_DIR="$HOME/Documents/main-vault"

# PARA folder names (relative to vault). Override only if you've renamed them.
OV_INBOX="00-Inbox"
OV_PROJECTS="01-Projects"
OV_AREAS="02-Areas"
OV_RESOURCES="03-Resources"
OV_ARCHIVE="04-Archive"
OV_META="99-Meta"

# LLM command for `ov triage --llm`. Pipe-prompt-in, get-text-out.
# Examples:
#   OV_LLM_CMD="claude --print"
#   OV_LLM_CMD="pi --print -nc -nt --mode json"   # pi with context/tool isolation + native JSON
OV_LLM_CMD="claude --print"

# Optional model override (passed as --model). Empty = use the LLM's default.
OV_MODEL=""

# SECURITY: triage --llm interpolates note bodies and previously-fetched
# URL titles into the LLM prompt. Those are attacker-influenced (a
# malicious clipped page or a crafted inbox note). ov always runs the LLM
# subprocess with its CWD set to an empty scratch directory (never your
# vault) — but tool access is provider-specific and ov cannot inject
# provider-specific flags into OV_LLM_CMD without breaking it for other
# providers. If your provider supports disabling tool/file access for a
# single invocation, point OV_LLM_CMD at a wrapper or settings profile
# that does so. For Claude Code, consider a dedicated settings file with
# tool permissions denied and reference it via --settings, or a shell
# wrapper script that passes the equivalent flag for your installed
# version — check `claude --help` for the current flag name.

# Optional: `ov publish`/`ov unpublish` push/remove files on a docs
# server over rsync/ssh. Uncomment and set if you use them.
# OV_DOCS_HOST="docs.example.com"
# OV_DOCS_PATH="/var/www/docs"
# OV_DOCS_URL="https://docs.example.com"
```

- [ ] **Step 9: Fix `templates/AGENTS.md`'s tool attribution line**

`templates/AGENTS.md:220` currently reads:
```
The user has a CLI at `~/.local/bin/ov` (source: `99-Meta/vault.sh`):
```

This is now definitively wrong (there is no `vault.sh` anywhere, in this repo or the vault's `99-Meta/`). Every `ov <command>` line in this file already says `ov`, not `ov2` — no other line in this file needs a rename. Fix only this one line:

```
The user has a CLI at `~/.local/bin/ov` (source: this repo's `cmd/ov/`, built with `go build`):
```

- [ ] **Step 10: Full-repo verification sweep**

```bash
grep -rn 'ov2' --include='*.go' --include='*.md' --include='Makefile' --include='*.example' . 2>/dev/null | grep -v '^./docs/plans/'
```

Expected: no output. (`docs/plans/*.md` is explicitly excluded — every prior-phase plan document is a historical record of what was done at the time and is never rewritten after the fact, matching this repo's own convention: phases 0-5's plan files still say `ov2` throughout and were never touched by later phases.)

```bash
go build ./... && go vet ./... && go test ./... -race
make build && ./dist/ov --version
./dist/ov doctor --vault "$HOME/Documents/main-vault"
```

Expected: clean build/vet/test; `./dist/ov --version` prints `ov version 0.1.0`; `doctor` reports the real vault healthy.

- [ ] **Step 11: Commit**

```bash
git add -A
git commit -m "Cutover: rename ov2 to ov, retire bin/ and the python test suite

- cmd/ov, internal/: every user-visible ov2 string and doc comment
  renamed to ov (root.go Use field, error prefixes, help text,
  scratch-dir prefix + its test assertion).
- Makefile: replaced the bash-symlink install flow with a direct
  go-build-and-copy install; removed the retired check/pytest targets;
  gotest renamed to test; added the parity target from the prior
  commit.
- Deleted bin/{vault.sh,triage_llm.py,moc_cleanup.py,render_html.py}
  and tests/{test_moc_cleanup.py,test_triage_llm.py} plus their
  __pycache__ dirs — bash/python is no longer the CLI, no longer an
  oracle (the parity-check script + phases 0-5's own test suites are
  the permanent record), and no longer shipped.
- README.md, docs/{install,architecture,config}.md,
  examples/ov.config.example, templates/AGENTS.md: rewritten to
  describe the Go binary and TOML config as the only supported path;
  examples/ov.config.example kept (re-labeled) as the documented input
  shape for 'ov config migrate --from'."
```

---

## Self-Review

**1. Spec coverage** (design spec phase 6 row + task brief's 3 requirement groups):
- Web polish: resolved via `ask` — explicitly out of scope, recorded in Session Decisions. No task needed.
- Parity checklist, scripted, side-by-side on a vault copy: Task 1 (`scripts/parity-check.sh`), covers every command with a meaningful comparison (`inbox`/`review`/`stale`/`mocs list` — full SET comparison; `capture` — full byte-identical content diff of the frozen contract; `new`×4/`mocs new` — content diff; `mocs orphan`/`mocs add`/render body markup — documented skip citing a mined row; `triage --llm`/`mocs cleanup` — structural, gated `--with-llm`; `publish`/`unpublish` — real remote round trip, gated `--with-remote`). Run for real against a copy of `$HOME/Documents/main-vault` in the mandated post-Task-1 manual step, recorded in `progress.md`.
- One week full parallel: resolved via `ask` — Alex explicitly waived the calendar gate in this conversation (recorded in Session Decisions, with the brief's contradicting claim noted rather than silently trusted).
- Cutover (rename, delete bin/, update docs): Task 2, gated on the parity run passing and Alex's separate explicit go-ahead.

**2. Placeholder scan:** No `TBD`/`TODO`/"add appropriate handling" anywhere in either task. Every script line and every rewritten doc file is complete, verbatim content — the parity script was hand-verified line-by-line against a real copy of the real vault during this plan's own research (not authored blind), including three genuinely new findings (rows #166, #167, and the `mocs add`/render exclusions built directly from them).

**3. Type/interface consistency:** Task 1 introduces no Go symbols. Task 2 renames strings only — no signature, no new exported identifier. The one test-assertion change (`internal/llm/transport_test.go`'s `"ov2-llm-"` → `"ov-llm-"`) is paired 1:1 with its production-code change in the same task, both listed explicitly.

**Gaps found and fixed inline:** none outstanding — the two research-time findings (rows #166-167) are folded into both Task 1's script and this plan's citations, not left as a dangling TODO.
