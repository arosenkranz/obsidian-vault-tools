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
