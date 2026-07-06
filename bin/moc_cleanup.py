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
import re
import sys
from pathlib import Path

# triage_llm.py lives next to this script; reuse its config/LLM plumbing
# instead of duplicating it.
sys.path.insert(0, str(Path(__file__).resolve().parent))
from triage_llm import (  # noqa: E402
    C_BOLD, C_CYAN, C_DIM, C_GREEN, C_RED, C_YELLOW,
    call_llm, color, extract_json, load_config,
)


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
