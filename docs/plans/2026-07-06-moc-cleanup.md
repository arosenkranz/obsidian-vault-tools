# MOC LLM Cleanup Implementation Plan

**Goal:** Add `ov mocs cleanup <name>` / `ov mocs cleanup --all`, an LLM-assisted
reorganizer for Maps of Content (MOC) files that fixes garbled auto-captured
entries (slugified-URL titles, flat unsorted bullets) by proposing a
reorganized version of the file and requiring explicit diff approval before
writing anything.

**Architecture:** A new `bin/moc_cleanup.py` script, structurally mirroring
`bin/triage_llm.py` (config loading, `call_llm()`, `extract_json()`, ANSI
color helpers — reused via import, not copy-pasted). Unlike triage's
per-note JSON-patch model, this operates on one whole MOC file at a time:
send the full file content to the LLM inside a constrained prompt, parse the
JSON response containing the full proposed file body, show a unified diff,
and only write on explicit user confirmation. `bin/vault.sh` gets a thin
`moc_cleanup` wrapper function and a `mocs cleanup` case branch that shells
out to the Python script, following the exact pattern already used for
`ov triage --llm` and `ov render`.

**Tech Stack:** Bash (`bin/vault.sh`), Python 3 stdlib only (`argparse`,
`json`, `re`, `subprocess`, `difflib`, `pathlib`) — no new dependencies.
Tests via `pytest` (already available in this environment) against the pure
logic in `moc_cleanup.py` (prompt building, response parsing/validation, diff
generation) — the parts that don't require a live LLM call or a real vault.

---

## Guardrails this plan must satisfy (from vault's `templates/AGENTS.md`)

- §6: "MOCs... are hand-curated indexes, not auto-generated." "Do not edit
  MOCs without approval."
- §8 hard rule: "❌ Edit MOCs [without approval]." "The user's default mode
  is suggest-only."

Consequences baked into every task below:
- The script **never writes to disk** unless the user explicitly confirms
  (`y` at the diff prompt, or a future non-interactive `--yes` flag the user
  must pass deliberately — not built in v1, see Task 8 notes).
- The LLM is instructed (and the code double-checks) that it may only:
  reorganize/re-file existing bullets under better subheadings, fix obviously
  garbled titles (e.g. slugified URLs), and flag (not silently merge)
  probable duplicates.
- The LLM is instructed it may **not**: delete any existing bullet/link,
  invent new links or URLs, alter frontmatter, or rewrite free-text prose
  sections (e.g. `## Notes`, `## Overview`) beyond reformatting whitespace.
- A structural safety check (Task 4) rejects any LLM proposal that drops
  wikilinks present in the original file, before the diff is even shown —
  belt-and-suspenders on top of the prompt instructions.

---

## File Structure

- **Create:** `bin/moc_cleanup.py` — the whole feature. Single file, mirrors
  `bin/triage_llm.py`'s size/shape (~350-450 lines). Not split further:
  this is a small, cohesive CLI tool and the codebase's existing convention
  (`triage_llm.py`, `render_html.py`) is one script per LLM-assisted
  operation.
- **Modify:** `bin/vault.sh` — add a `moc_cleanup()` bash wrapper function
  (near the other `moc_*` functions, after `moc_add()`) and wire it into the
  `mocs)` case dispatcher and help text.
- **Create:** `tests/test_moc_cleanup.py` — pytest tests for the pure-logic
  functions in `moc_cleanup.py` (no live LLM calls, no real vault needed).
- **Modify:** `Makefile` — add a `test` target that runs pytest, so `make
  check` and `make test` both exist as documented entry points.
- **Modify:** `templates/AGENTS.md` — add one line to §6 documenting that
  `ov mocs cleanup` exists as the deliberate, approval-gated way to reorganize
  a MOC (keeps the "don't edit MOCs" rule accurate — this is the sanctioned
  exception, invoked by the human, not autonomously).

---

## Task 1: Project scaffolding — new test file, confirm pytest runs

**Files:**
- Create: `tests/test_moc_cleanup.py`

- [ ] **Step 1: Create the tests directory and a trivial smoke test**

```python
"""Tests for bin/moc_cleanup.py — pure-logic pieces only (no live LLM calls,
no real vault). Run with: python3 -m pytest tests/test_moc_cleanup.py -v
"""

import sys
from pathlib import Path

# bin/ is not a package; import moc_cleanup by adding bin/ to sys.path.
sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "bin"))


def test_smoke():
    assert 1 + 1 == 2
```

- [ ] **Step 2: Run it to confirm pytest works in this environment**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && python3 -m pytest tests/test_moc_cleanup.py -v`

Expected: `1 passed`

- [ ] **Step 3: Commit**

```bash
git add tests/test_moc_cleanup.py
git commit -m "test: scaffold moc_cleanup test file"
```

---

## Task 2: `moc_cleanup.py` skeleton — config loading, CLI args, reused helpers

**Files:**
- Create: `bin/moc_cleanup.py`

Reuse `load_config`, `color`, `C_*` constants, `call_llm`, `extract_json`
from `triage_llm.py` by importing them (same directory, both live in `bin/`)
rather than duplicating — keeps config-loading and LLM-invocation behavior
identical across both tools with one source of truth.

- [ ] **Step 1: Write the skeleton script**

```python
#!/usr/bin/env python3
"""
LLM-assisted MOC (Map of Content) reorganizer for the PARA Obsidian vault.

Suggest-only: shows a full unified diff of the proposed reorganization and
requires explicit y/n approval before writing anything. Never invoked
autonomously — always a deliberate `ov mocs cleanup` call by the human.

Usage:
    moc_cleanup.py <name> [--vault PATH] [--model MODEL] [--llm-cmd CMD]
    moc_cleanup.py --all [--vault PATH]

Invoked indirectly via:  ov mocs cleanup <name> | ov mocs cleanup --all
"""

from __future__ import annotations

import argparse
import difflib
import os
import sys
from pathlib import Path

# triage_llm.py lives next to this script; reuse its config/LLM plumbing
# instead of duplicating it.
sys.path.insert(0, str(Path(__file__).resolve().parent))
from triage_llm import (  # noqa: E402
    C_BOLD, C_CYAN, C_DIM, C_GREEN, C_RED, C_YELLOW,
    call_llm, color, extract_json, load_config,
)


def find_moc_files(vault: Path, name: str | None) -> list[Path]:
    """Resolve `name` to a single MOC file, or list all MOC*.md files in the
    vault if name is None (the --all case). Matches vault.sh's
    find_moc_by_name(): accepts 'Music' or 'MOC Music' for a single lookup."""
    all_mocs = sorted(vault.rglob("MOC*.md"))
    if name is None:
        return all_mocs

    candidates = [f"MOC {name}.md", f"{name}.md" if name.startswith("MOC") else None]
    candidates = [c for c in candidates if c]
    for moc in all_mocs:
        if moc.name in candidates:
            return [moc]
    return []


def main() -> int:
    parser = argparse.ArgumentParser(description="LLM-assisted MOC cleanup")
    parser.add_argument("name", nargs="?", default=None,
                        help="MOC name, e.g. 'Music' or 'MOC Music' (omit with --all)")
    parser.add_argument("--all", action="store_true", help="process every MOC in the vault")
    parser.add_argument("--vault", type=Path, default=None, help="vault root (overrides OV_VAULT_DIR)")
    parser.add_argument("--model", type=str, default=None, help="override LLM model")
    parser.add_argument("--llm-cmd", type=str, default=None, help="override OV_LLM_CMD")
    parser.add_argument("--config", type=Path, default=None, help="path to config file")
    args = parser.parse_args()

    if not args.all and not args.name:
        print(color("error: pass a MOC name or --all", C_RED), file=sys.stderr)
        return 2
    if args.all and args.name:
        print(color("error: pass a MOC name or --all, not both", C_RED), file=sys.stderr)
        return 2

    cfg = load_config(args.config)
    vault_str = str(args.vault) if args.vault else cfg.get("OV_VAULT_DIR")
    if not vault_str:
        print(color("error: vault not set; use --vault or set OV_VAULT_DIR in config", C_RED),
              file=sys.stderr)
        return 2
    vault: Path = Path(os.path.expandvars(vault_str)).expanduser().resolve()

    targets = find_moc_files(vault, None if args.all else args.name)
    if not targets:
        print(color(f"error: no MOC found matching '{args.name}'", C_RED), file=sys.stderr)
        return 2

    print(color(f"🧹 MOC cleanup — {len(targets)} file(s)", C_CYAN + C_BOLD))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        print()
        print(color("interrupted", C_YELLOW))
        sys.exit(130)
```

- [ ] **Step 2: Make it executable and syntax-check**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && chmod +x bin/moc_cleanup.py && python3 -m py_compile bin/moc_cleanup.py && echo OK`

Expected: `OK`

- [ ] **Step 3: Manual smoke test against the real vault (read-only so far — it only lists targets, writes nothing)**

Run:
```bash
cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools
python3 bin/moc_cleanup.py "Music"
python3 bin/moc_cleanup.py --all
python3 bin/moc_cleanup.py "Nonexistent MOC"
```

Expected: first two print `🧹 MOC cleanup — 1 file(s)` and `🧹 MOC cleanup — 3 file(s)` respectively (adjust count to however many `MOC*.md` files exist in your vault); the third prints an error to stderr and exits 2.

- [ ] **Step 4: Commit**

```bash
git add bin/moc_cleanup.py
git commit -m "feat: scaffold moc_cleanup.py CLI (arg parsing, MOC file resolution)"
```

---

## Task 3: Prompt construction — write the failing test first

**Files:**
- Modify: `bin/moc_cleanup.py`
- Modify: `tests/test_moc_cleanup.py`

The prompt is the enforcement point for the "reorganize + fix titles only"
scope decision. It must state the allowed/forbidden operations explicitly
and demand a specific JSON shape back.

- [ ] **Step 1: Write the failing test**

Add to `tests/test_moc_cleanup.py`:

```python
from moc_cleanup import build_prompt


def test_build_prompt_includes_full_moc_content():
    moc_text = "---\ntype: moc\n---\n# MOC Test\n\n## Resources\n\n- [[foo]] — bar\n"
    prompt = build_prompt(moc_text, moc_name="MOC Test")
    assert moc_text in prompt


def test_build_prompt_states_forbidden_operations():
    prompt = build_prompt("content", moc_name="MOC Test")
    assert "must not delete" in prompt.lower() or "never delete" in prompt.lower()
    assert "frontmatter" in prompt.lower()


def test_build_prompt_requests_json_shape():
    prompt = build_prompt("content", moc_name="MOC Test")
    assert '"new_content"' in prompt
    assert '"duplicates_flagged"' in prompt
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && python3 -m pytest tests/test_moc_cleanup.py -v`

Expected: `FAIL` — `ImportError: cannot import name 'build_prompt'`

- [ ] **Step 3: Implement `build_prompt`**

Add to `bin/moc_cleanup.py`, above `main()`:

```python
PROMPT_TEMPLATE = """\
You are reorganizing a single Obsidian "Map of Content" (MOC) file. This is
a hand-curated index of links, and your job is ONLY to tidy its structure —
not to change what it indexes.

MOC name: {moc_name}

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

{{
  "new_content": "<the complete reorganized file content, including the\\nunchanged frontmatter block, as a single string with literal \\n newlines>",
  "duplicates_flagged": ["<human-readable description of duplicate pair 1>", "..."],
  "summary": "<one or two sentences describing what you changed and why>"
}}

Here is the current file content:

---
{moc_content}
---
"""


def build_prompt(moc_text: str, moc_name: str) -> str:
    return PROMPT_TEMPLATE.format(moc_name=moc_name, moc_content=moc_text)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && python3 -m pytest tests/test_moc_cleanup.py -v`

Expected: `3 passed`

- [ ] **Step 5: Commit**

```bash
git add bin/moc_cleanup.py tests/test_moc_cleanup.py
git commit -m "feat: add MOC cleanup prompt builder with explicit allow/forbid rules"
```

---

## Task 4: Response validation — the safety net (write failing tests first)

This is the most important task in the plan: a structural check that runs
**before** the diff is ever shown, rejecting any LLM proposal that would
lose content — independent of whether the LLM followed the prompt's rules.

**Files:**
- Modify: `bin/moc_cleanup.py`
- Modify: `tests/test_moc_cleanup.py`

- [ ] **Step 1: Write the failing tests**

Add to `tests/test_moc_cleanup.py`:

```python
from moc_cleanup import validate_proposal, ValidationError

ORIGINAL = (
    "---\ntype: moc\n---\n"
    "# MOC Test\n\n"
    "## Resources\n\n"
    "- [[Foo Note]] — https://example.com/foo\n"
    "- [[Bar Note]] — https://example.com/bar\n"
)


def test_validate_accepts_reordering_that_keeps_all_links():
    reordered = (
        "---\ntype: moc\n---\n"
        "# MOC Test\n\n"
        "## Resources\n\n"
        "### Example.com\n"
        "- [[Bar Note]] — https://example.com/bar\n"
        "- [[Foo Note]] — https://example.com/foo\n"
    )
    # Should not raise
    validate_proposal(ORIGINAL, reordered)


def test_validate_rejects_dropped_wikilink():
    missing_link = (
        "---\ntype: moc\n---\n"
        "# MOC Test\n\n"
        "## Resources\n\n"
        "- [[Foo Note]] — https://example.com/foo\n"
    )
    try:
        validate_proposal(ORIGINAL, missing_link)
        assert False, "expected ValidationError"
    except ValidationError as e:
        assert "Bar Note" in str(e)


def test_validate_rejects_frontmatter_change():
    changed_fm = (
        "---\ntype: moc\nextra: field\n---\n"
        "# MOC Test\n\n"
        "## Resources\n\n"
        "- [[Foo Note]] — https://example.com/foo\n"
        "- [[Bar Note]] — https://example.com/bar\n"
    )
    try:
        validate_proposal(ORIGINAL, changed_fm)
        assert False, "expected ValidationError"
    except ValidationError as e:
        assert "frontmatter" in str(e).lower()


def test_validate_rejects_dropped_url():
    # Title changed AND its URL vanished entirely — url count check catches
    # cases the wikilink check might miss (e.g. plain URLs in prose).
    missing_url = (
        "---\ntype: moc\n---\n"
        "# MOC Test\n\n"
        "## Resources\n\n"
        "- [[Foo Note]] — https://example.com/foo\n"
        "- [[Bar Note]] — some text with no url\n"
    )
    try:
        validate_proposal(ORIGINAL, missing_url)
        assert False, "expected ValidationError"
    except ValidationError as e:
        assert "url" in str(e).lower() or "link" in str(e).lower()
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && python3 -m pytest tests/test_moc_cleanup.py -v`

Expected: `FAIL` — `ImportError: cannot import name 'validate_proposal'`

- [ ] **Step 3: Implement `validate_proposal` and `ValidationError`**

Add to `bin/moc_cleanup.py`, above `build_prompt`:

```python
import re


WIKILINK_RE = re.compile(r"\[\[([^\]|]+)")
URL_RE = re.compile(r"https?://\S+")
FRONTMATTER_RE = re.compile(r"^---\n(.*?)\n---\n?", re.DOTALL)


class ValidationError(Exception):
    """Raised when an LLM's proposed MOC content fails a structural safety
    check. Callers must treat this as 'reject the proposal', never as
    'partially apply it'."""


def _frontmatter_block(text: str) -> str | None:
    m = FRONTMATTER_RE.match(text)
    return m.group(0) if m else None


def validate_proposal(original: str, proposed: str) -> None:
    """Structural safety net, independent of prompt compliance. Raises
    ValidationError on any violation; returns None (silently) if the
    proposal is safe to show as a diff for human approval.

    This does NOT guarantee the reorganization is *good* — only that it
    didn't lose frontmatter, wikilinks, or URLs present in the original.
    """
    orig_fm = _frontmatter_block(original)
    new_fm = _frontmatter_block(proposed)
    if orig_fm != new_fm:
        raise ValidationError(
            "proposal changes the frontmatter block, which is forbidden"
        )

    orig_links = set(WIKILINK_RE.findall(original))
    new_links = set(WIKILINK_RE.findall(proposed))
    dropped_links = orig_links - new_links
    if dropped_links:
        raise ValidationError(
            f"proposal drops {len(dropped_links)} wikilink(s) present in the "
            f"original: {', '.join(sorted(dropped_links))}"
        )

    orig_urls = set(URL_RE.findall(original))
    new_urls = set(URL_RE.findall(proposed))
    dropped_urls = orig_urls - new_urls
    if dropped_urls:
        raise ValidationError(
            f"proposal drops {len(dropped_urls)} URL(s) present in the "
            f"original: {', '.join(sorted(dropped_urls))}"
        )
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && python3 -m pytest tests/test_moc_cleanup.py -v`

Expected: `7 passed`

- [ ] **Step 5: Commit**

```bash
git add bin/moc_cleanup.py tests/test_moc_cleanup.py
git commit -m "feat: add structural validation to reject MOC proposals that drop content"
```

---

## Task 5: Diff rendering — write failing tests first

**Files:**
- Modify: `bin/moc_cleanup.py`
- Modify: `tests/test_moc_cleanup.py`

- [ ] **Step 1: Write the failing tests**

Add to `tests/test_moc_cleanup.py`:

```python
from moc_cleanup import render_diff


def test_render_diff_shows_added_and_removed_lines():
    old = "line1\nline2\nline3\n"
    new = "line1\nline2-changed\nline3\n"
    out = render_diff(old, new, filename="MOC Test.md")
    assert "-line2\n" in out or "-line2" in out
    assert "+line2-changed" in out


def test_render_diff_empty_when_identical():
    same = "line1\nline2\n"
    out = render_diff(same, same, filename="MOC Test.md")
    assert out.strip() == "" or "no changes" in out.lower()
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && python3 -m pytest tests/test_moc_cleanup.py -v`

Expected: `FAIL` — `ImportError: cannot import name 'render_diff'`

- [ ] **Step 3: Implement `render_diff`**

Add to `bin/moc_cleanup.py`, above `main()`:

```python
def render_diff(old: str, new: str, filename: str) -> str:
    """Unified diff, colorized like `git diff` (+green/-red), or a plain
    'no changes' message if the proposal is identical to the original."""
    diff_lines = list(difflib.unified_diff(
        old.splitlines(keepends=True),
        new.splitlines(keepends=True),
        fromfile=f"{filename} (current)",
        tofile=f"{filename} (proposed)",
    ))
    if not diff_lines:
        return color("(no changes proposed)", C_DIM)

    out = []
    for line in diff_lines:
        if line.startswith("+++") or line.startswith("---"):
            out.append(color(line.rstrip("\n"), C_BOLD))
        elif line.startswith("+"):
            out.append(color(line.rstrip("\n"), C_GREEN))
        elif line.startswith("-"):
            out.append(color(line.rstrip("\n"), C_RED))
        elif line.startswith("@@"):
            out.append(color(line.rstrip("\n"), C_CYAN))
        else:
            out.append(line.rstrip("\n"))
    return "\n".join(out)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && python3 -m pytest tests/test_moc_cleanup.py -v`

Expected: `9 passed`

- [ ] **Step 5: Commit**

```bash
git add bin/moc_cleanup.py tests/test_moc_cleanup.py
git commit -m "feat: add colorized unified diff rendering for MOC cleanup proposals"
```

---

## Task 6: Response parsing helper — write failing tests first

Wraps `extract_json` (reused from `triage_llm.py`) with the specific field
checks this feature needs (`new_content` must be present and a string).

**Files:**
- Modify: `bin/moc_cleanup.py`
- Modify: `tests/test_moc_cleanup.py`

- [ ] **Step 1: Write the failing tests**

Add to `tests/test_moc_cleanup.py`:

```python
from moc_cleanup import parse_llm_response


def test_parse_llm_response_extracts_new_content():
    raw = '{"new_content": "hello", "duplicates_flagged": [], "summary": "ok"}'
    result = parse_llm_response(raw)
    assert result["new_content"] == "hello"
    assert result["duplicates_flagged"] == []


def test_parse_llm_response_handles_fenced_json():
    raw = '```json\n{"new_content": "hi", "duplicates_flagged": [], "summary": "x"}\n```'
    result = parse_llm_response(raw)
    assert result["new_content"] == "hi"


def test_parse_llm_response_rejects_missing_new_content():
    raw = '{"duplicates_flagged": [], "summary": "x"}'
    try:
        parse_llm_response(raw)
        assert False, "expected ValueError"
    except ValueError as e:
        assert "new_content" in str(e)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && python3 -m pytest tests/test_moc_cleanup.py -v`

Expected: `FAIL` — `ImportError: cannot import name 'parse_llm_response'`

- [ ] **Step 3: Implement `parse_llm_response`**

Add to `bin/moc_cleanup.py`, above `main()`:

```python
def parse_llm_response(raw: str) -> dict:
    """Parse and validate the LLM's JSON response. Raises ValueError if the
    required 'new_content' field is missing or not a string."""
    data = extract_json(raw)
    if "new_content" not in data or not isinstance(data["new_content"], str):
        raise ValueError(
            "LLM response missing required string field 'new_content':\n"
            f"{raw}"
        )
    data.setdefault("duplicates_flagged", [])
    data.setdefault("summary", "")
    return data
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && python3 -m pytest tests/test_moc_cleanup.py -v`

Expected: `12 passed`

- [ ] **Step 5: Commit**

```bash
git add bin/moc_cleanup.py tests/test_moc_cleanup.py
git commit -m "feat: add LLM response parsing with required-field validation"
```

---

## Task 7: Wire it all together in `main()` — the approve/discard loop

**Files:**
- Modify: `bin/moc_cleanup.py`

This is the orchestration step: for each target MOC, read it, build the
prompt, call the LLM, validate, show the diff, prompt y/n, write on yes.
Not unit-testable without mocking the subprocess call, so this task is
verified manually against a throwaway test vault (Step 3), matching how the
earlier bash fixes in this session were verified.

- [ ] **Step 1: Replace the `main()` body's placeholder loop**

Replace the line `print(color(f"🧹 MOC cleanup — {len(targets)} file(s)", C_CYAN + C_BOLD))`
and the `return 0` after it in `bin/moc_cleanup.py` with:

```python
    llm_cmd = args.llm_cmd or cfg.get("OV_LLM_CMD") or "claude --print"
    model = args.model or cfg.get("OV_MODEL") or None

    print(color(f"🧹 MOC cleanup — {len(targets)} file(s)", C_CYAN + C_BOLD))

    counts = {"applied": 0, "skipped": 0, "unchanged": 0, "rejected": 0, "errored": 0}

    for moc_path in targets:
        print()
        print(color("─" * 72, C_DIM))
        print(color(f"📄 {moc_path.name}", C_BOLD))
        print(color("─" * 72, C_DIM))

        try:
            original = moc_path.read_text()
        except Exception as e:
            print(color(f"  error reading file: {e}", C_RED))
            counts["errored"] += 1
            continue

        prompt = build_prompt(original, moc_name=moc_path.stem)
        print(color("  thinking…", C_DIM))
        try:
            raw = call_llm(prompt, llm_cmd=llm_cmd, model=model, timeout=180)
            proposal = parse_llm_response(raw)
        except Exception as e:
            print(color(f"  LLM call failed: {e}", C_RED))
            counts["errored"] += 1
            continue

        new_content = proposal["new_content"]

        try:
            validate_proposal(original, new_content)
        except ValidationError as e:
            print(color(f"  ✗ rejected proposal: {e}", C_RED))
            counts["rejected"] += 1
            continue

        if new_content == original:
            print(color("  ✓ already well-organized, no changes proposed", C_GREEN))
            counts["unchanged"] += 1
            continue

        if proposal["summary"]:
            print(color(f"  summary: {proposal['summary']}", C_DIM))
        if proposal["duplicates_flagged"]:
            print(color("  ⚠ possible duplicates (not merged, review manually):", C_YELLOW))
            for dup in proposal["duplicates_flagged"]:
                print(color(f"    - {dup}", C_YELLOW))

        print()
        print(render_diff(original, new_content, filename=moc_path.name))
        print()
        print(color("Apply this reorganization? [y/N] ", C_BOLD), end="")
        try:
            answer = input().strip().lower()
        except EOFError:
            answer = "n"

        if answer == "y":
            moc_path.write_text(new_content)
            print(color(f"  ✓ applied → {moc_path}", C_GREEN))
            counts["applied"] += 1
        else:
            print(color("  → skipped, no changes written", C_YELLOW))
            counts["skipped"] += 1

    print()
    print(color("─" * 72, C_DIM))
    print(color("cleanup summary", C_BOLD))
    for k, v in counts.items():
        print(f"  {k:<10} {v}")
    return 0
```

- [ ] **Step 2: Syntax-check**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && python3 -m py_compile bin/moc_cleanup.py && echo OK`

Expected: `OK`

- [ ] **Step 3: Manual end-to-end test against a throwaway vault (never touches your real vault)**

```bash
cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools
mkdir -p /tmp/moc-cleanup-test/03-Resources
cat > "/tmp/moc-cleanup-test/03-Resources/MOC Test.md" <<'EOF'
---
type: moc
---
# MOC Test

## 🔗 Resources

- [[https example com some-garbled-slug]] — https://example.com/some-garbled-slug
- [[Clean Existing Entry]] — https://example.com/clean

### Aquarium Drunkard
- [[Old Entry]] — https://aquariumdrunkard.com/old
EOF

cat > /tmp/moc-cleanup-test-config <<'EOF'
OV_VAULT_DIR="/tmp/moc-cleanup-test"
OV_LLM_CMD="claude --print"
EOF

OV_CONFIG=/tmp/moc-cleanup-test-config python3 bin/moc_cleanup.py "Test"
```

Expected: prints the file header, "thinking…", then a colorized diff showing
the garbled entry moved/retitled, prompts `Apply this reorganization? [y/N]`.
Type `n` first to confirm nothing is written (`diff /tmp/moc-cleanup-test/03-Resources/"MOC Test.md" <(cat ...)` — file unchanged), then re-run and type `y` to
confirm it writes and the file now contains the reorganized content with all
original links/URLs still present.

Cleanup: `rm -rf /tmp/moc-cleanup-test /tmp/moc-cleanup-test-config`

- [ ] **Step 4: Commit**

```bash
git add bin/moc_cleanup.py
git commit -m "feat: wire up MOC cleanup approve/discard loop in main()"
```

---

## Task 8: Wire into `bin/vault.sh` — `ov mocs cleanup`

**Files:**
- Modify: `bin/vault.sh:925` area (after `moc_add()`) — add `moc_cleanup()`
- Modify: `bin/vault.sh` — `mocs)` case dispatcher (~line 1173) and help text
  (~lines 82-87)

- [ ] **Step 1: Add the `moc_cleanup` bash wrapper**

Add after the `moc_add()` function definition in `bin/vault.sh`:

```bash
moc_cleanup() {
    local target="$1"
    if [ -z "$target" ]; then
        echo -e "${RED}Usage: ov mocs cleanup <name>${NC}"
        echo -e "       ov mocs cleanup --all"
        return 1
    fi
    if [ "$target" = "--all" ]; then
        python3 "$SCRIPT_DIR/moc_cleanup.py" --all --vault "$VAULT_DIR"
    else
        python3 "$SCRIPT_DIR/moc_cleanup.py" "$target" --vault "$VAULT_DIR"
    fi
}
```

- [ ] **Step 2: Wire into the `mocs)` case dispatcher**

Find this block (around line 1173):

```bash
        mocs)
            case "${2:-list}" in
                list)
                    moc_list
                    ;;
                new)
                    moc_new
                    ;;
                orphan)
                    moc_orphan
                    ;;
                add)
                    moc_add
                    ;;
                update)
                    echo "MOC update functionality coming soon"
                    ;;
```

Add a `cleanup)` branch right after `add)`'s block, leaving `update)` as-is:

```bash
                add)
                    moc_add
                    ;;
                cleanup)
                    shift 2
                    moc_cleanup "$1"
                    ;;
                update)
                    echo "MOC update functionality coming soon"
                    ;;
```

- [ ] **Step 3: Update the help text**

Find (around line 82-87):

```
MOC SUBCOMMANDS:
    mocs list           List all MOCs with descriptions
    mocs new            Create new MOC from template
    mocs orphan         Find notes not linked from any MOC
    mocs add            Add note to MOC interactively
    mocs update         Update MOCs in directory
```

Replace with:

```
MOC SUBCOMMANDS:
    mocs list           List all MOCs with descriptions
    mocs new            Create new MOC from template
    mocs orphan         Find notes not linked from any MOC
    mocs add            Add note to MOC interactively
    mocs cleanup <name> LLM-reorganize one MOC (shows diff, asks to confirm)
    mocs cleanup --all  LLM-reorganize every MOC in the vault (one at a time)
    mocs update         Update MOCs in directory
```

- [ ] **Step 4: Syntax-check**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && bash -n bin/vault.sh && echo OK`

Expected: `OK`

- [ ] **Step 5: Manual test — missing-name error path**

Run: `bin/vault.sh mocs cleanup`

Expected: prints usage error to terminal (red text), exits non-zero (check with `echo $?`).

- [ ] **Step 6: Manual test — real invocation on the throwaway vault from Task 7**

```bash
mkdir -p /tmp/moc-cleanup-test/03-Resources
cat > "/tmp/moc-cleanup-test/03-Resources/MOC Test.md" <<'EOF'
---
type: moc
---
# MOC Test

## 🔗 Resources

- [[https example com some-garbled-slug]] — https://example.com/some-garbled-slug
EOF
cat > /tmp/moc-cleanup-test-config <<'EOF'
OV_VAULT_DIR="/tmp/moc-cleanup-test"
OV_LLM_CMD="claude --print"
EOF
cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools
OV_CONFIG=/tmp/moc-cleanup-test-config bin/vault.sh mocs cleanup "Test"
rm -rf /tmp/moc-cleanup-test /tmp/moc-cleanup-test-config
```

Expected: same diff-and-confirm flow as Task 7 Step 3, now reached through
the real `ov` CLI entrypoint.

- [ ] **Step 7: Commit**

```bash
git add bin/vault.sh
git commit -m "feat: wire ov mocs cleanup into vault.sh CLI"
```

---

## Task 9: Add `make test` target

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Add a `test` target and register it in `.PHONY`**

Find the `.PHONY` line:

```makefile
.PHONY: help install uninstall link unlink config check
```

Replace with:

```makefile
.PHONY: help install uninstall link unlink config check test
```

Find the `check:` target block and add a `test:` target immediately after it:

```makefile
test:
	python3 -m pytest tests/ -v
```

- [ ] **Step 2: Update the `help` target's listed targets**

Find the line `@echo "  check      Syntax-check both scripts"` inside the
`help:` target and add a line after it:

```makefile
	@echo "  check      Syntax-check both scripts"
	@echo "  test       Run the pytest suite (tests/)"
```

- [ ] **Step 3: Run it**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && make test`

Expected: `12 passed` (all tests from Tasks 1, 3, 4, 5, 6)

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "build: add make test target for the pytest suite"
```

---

## Task 10: Document the sanctioned MOC-editing exception in `templates/AGENTS.md`

**Files:**
- Modify: `templates/AGENTS.md`

The vault's own agent rules say "do not edit MOCs without approval." This
feature is the deliberate, human-invoked, diff-gated exception — document it
so the rule stays accurate for future readers (including future LLM triage
sessions reading this same file).

- [ ] **Step 1: Update §6**

Find:

```
- MOCs (Maps of Content) live in `03-Resources/` prefixed `MOC` (e.g. `MOC Music.md`, `MOC Programming & DevOps.md`). They are hand-curated indexes, not auto-generated.
- When filing a note, look for an obvious MOC to link from. Propose the link in your triage output but **do not** edit MOCs without approval.
- Don't create new MOCs as part of a triage. That's a separate, deliberate action via `ov mocs new`.
```

Replace with:

```
- MOCs (Maps of Content) live in `03-Resources/` prefixed `MOC` (e.g. `MOC Music.md`, `MOC Programming & DevOps.md`). They are hand-curated indexes, not auto-generated.
- When filing a note, look for an obvious MOC to link from. Propose the link in your triage output but **do not** edit MOCs without approval.
- Don't create new MOCs as part of a triage. That's a separate, deliberate action via `ov mocs new`.
- The one sanctioned exception to "don't edit MOCs": `ov mocs cleanup <name>` (or `--all`) runs an LLM reorganization pass that shows a full diff and requires explicit `y` confirmation before writing anything. It only reorganizes/re-titles existing entries — it cannot delete links or touch frontmatter (enforced by a structural check, not just prompt instructions). This is always a deliberate command the human runs themselves; never invoke it as part of an autonomous triage.
```

- [ ] **Step 2: Verify no other section needs updating**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && grep -n "do not.*edit MOC\|❌ Edit MOC" templates/AGENTS.md`

Expected: shows §8's "❌ Edit MOCs." line unchanged — that hard rule is
correct as-is; it governs autonomous agent behavior, and `ov mocs cleanup`
is explicitly human-invoked, not autonomous, so no edit needed there.

- [ ] **Step 3: Commit**

```bash
git add templates/AGENTS.md
git commit -m "docs: document ov mocs cleanup as the sanctioned MOC-editing exception"
```

---

## Task 11: Update `README.md` and `docs/` if they enumerate commands

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Check whether README enumerates `mocs` subcommands**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && grep -n "mocs" README.md`

- [ ] **Step 2: If a command table/list exists, add a row/line for `mocs cleanup`**

(Exact diff depends on Step 1's output — if README only has the one-line
summary `ov mocs <sub>      # MOC management` seen earlier in this session,
no change is needed there; that line already covers it generically. Only
edit README if it has a *detailed* per-subcommand list, mirroring the style
of whatever entries already exist for `mocs list`/`mocs new`/etc.)

- [ ] **Step 3: Commit (only if Step 2 made changes)**

```bash
git add README.md
git commit -m "docs: mention ov mocs cleanup in README"
```

---

## Task 12: Final full-suite verification and push

**Files:** none (verification only)

- [ ] **Step 1: Run the full test suite**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && make test`

Expected: all tests pass (12+ depending on Task 11 additions).

- [ ] **Step 2: Run the syntax check**

Run: `cd /Users/alex.rosenkranz/workspace/obsidian-vault-tools && make check`

Expected: no errors from either `bash -n bin/vault.sh` or the Python
compile check (confirm `make check`'s exact commands first with `cat
Makefile` if unsure — adjust this step's expectation to match).

- [ ] **Step 3: Manually re-run the Task 8 Step 6 end-to-end test one more time**

Confirms nothing regressed after Tasks 9-11's non-code changes.

- [ ] **Step 4: Review `git log` for this feature's commits**

Run: `git log --oneline -14`

Expected: one commit per task above, in order, each with a clear message.

- [ ] **Step 5: Push**

Run: `git push origin main`

---

## Explicitly out of scope for this plan (YAGNI)

- **No `--yes`/non-interactive flag.** Every application of a proposal
  requires a live `y` at the prompt. If batch/non-interactive cleanup is
  wanted later, that's a deliberate follow-up decision (it changes the
  safety posture) — not bundled here.
- **No automatic backup/undo file.** The repo is expected to live inside a
  git-tracked or Obsidian-synced vault; `git diff`/`git checkout` on the MOC
  file is the undo mechanism. Not reinventing that.
- **No per-section (as opposed to per-file) approval.** Considered and
  explicitly rejected in favor of whole-file diff + single confirm (see the
  design decision at the top of this session).
- **No dedup auto-merge.** Duplicates are flagged only, per the "reorganize
  + fix titles only" scope — never silently merged.
