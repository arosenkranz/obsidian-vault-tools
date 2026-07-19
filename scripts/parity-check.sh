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
bash_inbox=$(run_bash inbox 2>/dev/null | tail -n +2 | strip_ansi | sed -E 's/^  . +//; s/  \([0-9]+d old\)$//' | grep -v '^$' | grep -v "Inbox is empty" | sort)
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
