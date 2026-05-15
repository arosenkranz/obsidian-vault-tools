#!/usr/bin/env python3
"""
LLM-assisted inbox triage for the PARA Obsidian vault.

Suggest-only: every move is approved interactively. Calls `claude --print`
for each inbox note and asks for a JSON proposal matching the schema in
AGENTS.md §7.

Usage:
    triage_llm.py [--limit N] [--dry-run] [--model MODEL] [--vault PATH]

Invoked indirectly via:  ov triage --llm
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import re
import shlex
import subprocess
import sys
from pathlib import Path

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

CONFIG_FILE_DEFAULT = Path("~/.config/ov/config").expanduser()

# Recognized keys (mirrors examples/ov.config.example). Values are read in
# this order of precedence:
#   1. CLI flag
#   2. Environment variable
#   3. Config file
#   4. Built-in default
CONFIG_KEYS = (
    "OV_VAULT_DIR",
    "OV_INBOX",
    "OV_PROJECTS",
    "OV_AREAS",
    "OV_RESOURCES",
    "OV_ARCHIVE",
    "OV_META",
    "OV_LLM_CMD",
    "OV_MODEL",
)

CONFIG_DEFAULTS = {
    "OV_INBOX": "00-Inbox",
    "OV_PROJECTS": "01-Projects",
    "OV_AREAS": "02-Areas",
    "OV_RESOURCES": "03-Resources",
    "OV_ARCHIVE": "04-Archive",
    "OV_META": "99-Meta",
    "OV_LLM_CMD": "claude --print",
    "OV_MODEL": "",
}

_CONFIG_LINE_RE = re.compile(r'^\s*(OV_[A-Z_]+)\s*=\s*"?([^"#]*?)"?\s*(?:#.*)?$')


def load_config(explicit_file: Path | None = None) -> dict[str, str]:
    """Read OV_* values from config file then env, with defaults applied."""
    cfg: dict[str, str] = {}
    config_file = explicit_file or Path(
        os.environ.get("OV_CONFIG", str(CONFIG_FILE_DEFAULT))
    ).expanduser()
    if config_file.is_file():
        for line in config_file.read_text().splitlines():
            m = _CONFIG_LINE_RE.match(line)
            if not m:
                continue
            key, val = m.group(1), m.group(2).strip()
            if key in CONFIG_KEYS:
                cfg[key] = os.path.expandvars(val)
    # Env wins over file
    for key in CONFIG_KEYS:
        if os.environ.get(key):
            cfg[key] = os.environ[key]
    # Defaults fill remaining
    for key, val in CONFIG_DEFAULTS.items():
        cfg.setdefault(key, val)
    return cfg

# ANSI colors
C_RESET = "\033[0m"
C_DIM = "\033[2m"
C_BOLD = "\033[1m"
C_RED = "\033[31m"
C_GREEN = "\033[32m"
C_YELLOW = "\033[33m"
C_BLUE = "\033[34m"
C_PURPLE = "\033[35m"
C_CYAN = "\033[36m"


def color(s: str, c: str) -> str:
    return f"{c}{s}{C_RESET}"


# ---------------------------------------------------------------------------
# Frontmatter parsing
# ---------------------------------------------------------------------------

FM_RE = re.compile(r"^---\n(.*?)\n---\n?", re.DOTALL)


def split_frontmatter(text: str) -> tuple[dict, str]:
    """Return (frontmatter_dict, body). Frontmatter is parsed leniently as
    a flat key:value YAML subset — enough for our use case."""
    m = FM_RE.match(text)
    if not m:
        return {}, text
    raw_fm = m.group(1)
    body = text[m.end():]
    fm: dict = {}
    for line in raw_fm.splitlines():
        if not line.strip() or line.startswith("#"):
            continue
        if ":" not in line:
            continue
        k, _, v = line.partition(":")
        k = k.strip()
        v = v.strip()
        # strip surrounding quotes
        if (v.startswith('"') and v.endswith('"')) or (v.startswith("'") and v.endswith("'")):
            v = v[1:-1]
        # YAML list shorthand: [a, b, c]
        if v.startswith("[") and v.endswith("]"):
            inner = v[1:-1].strip()
            v = [x.strip() for x in inner.split(",") if x.strip()] if inner else []
        fm[k] = v
    return fm, body


def render_frontmatter(fm: dict) -> str:
    """Render a small frontmatter dict back to YAML."""
    if not fm:
        return ""
    lines = ["---"]
    # Stable order for the common keys, then anything else
    preferred = ["type", "created", "modified", "tags", "status", "source", "url", "project", "area"]
    seen = set()
    for k in preferred:
        if k in fm:
            lines.append(_fm_line(k, fm[k]))
            seen.add(k)
    for k, v in fm.items():
        if k in seen:
            continue
        lines.append(_fm_line(k, v))
    lines.append("---")
    return "\n".join(lines) + "\n"


def _fm_line(k: str, v) -> str:
    if isinstance(v, list):
        return f"{k}: [{', '.join(str(x) for x in v)}]"
    return f"{k}: {v}"


# ---------------------------------------------------------------------------
# Folder discovery
# ---------------------------------------------------------------------------

def discover_folders(vault: Path, para_roots: list[str]) -> list[str]:
    """Return PARA folder paths (relative to vault) up to depth 2."""
    out: list[str] = []
    for root in para_roots:
        root_path = vault / root
        if not root_path.is_dir():
            continue
        out.append(root)
        for p in sorted(root_path.iterdir()):
            if p.is_dir():
                out.append(f"{root}/{p.name}")
                # one more level for Work/, etc.
                for sub in sorted(p.iterdir()):
                    if sub.is_dir():
                        out.append(f"{root}/{p.name}/{sub.name}")
    return out


# ---------------------------------------------------------------------------
# Slugify (mirrors slugify_title in vault.sh)
# ---------------------------------------------------------------------------

FORBIDDEN_FN = re.compile(r'[\\/:*?"<>|@&#]+')


def slugify_title(s: str) -> str:
    if not s:
        return "Untitled"
    s = re.sub(r"^\s*#+\s*", "", s)  # strip leading markdown heading markers
    s = FORBIDDEN_FN.sub(" ", s)
    s = re.sub(r"\s+", " ", s).strip()
    if len(s) > 80:
        # Truncate at word boundary (filed notes get a slightly larger budget than inbox)
        s = s[:80].rsplit(" ", 1)[0]
    return s or "Untitled"


# ---------------------------------------------------------------------------
# LLM call
# ---------------------------------------------------------------------------

# Filled at runtime from config
PARA_ROOTS: list[str] = []


PROMPT_TEMPLATE = """You are a triage assistant for a PARA-organized Obsidian vault. Your job is to propose where ONE inbox note should be filed.

Read the AGENTS.md contract below, then produce exactly one JSON object matching the schema in §7.

Output rules:
- Output ONLY the JSON object. No prose. No markdown fences. No commentary before or after.
- This is v1 of triage. Set "links_to_add" to []. Do NOT propose wikilinks.
- Set "body_patch" to null. Do NOT rewrite the body.
- "to" must be a path under one of these existing folders. You may include a new filename inside an existing folder, but do NOT invent new folders:
{folders}
- "from" must equal: {from_path}
- "new_title" must follow the naming conventions in §3.
- "frontmatter_patch" should upgrade the note from inbox: set type, status, tags. Do NOT include created/modified — the script handles those.
- "confidence" must be one of: high, medium, low.
- "rationale" is one or two sentences.

=== AGENTS.md ===
{agents_md}

=== NOTE TO TRIAGE ===
path: {from_path}
current_frontmatter: {current_fm}
body:
---BEGIN BODY---
{body}
---END BODY---

Return the JSON object now."""


def call_llm(
    prompt: str,
    llm_cmd: str,
    model: str | None = None,
    timeout: int = 120,
) -> str:
    """Invoke the configured LLM command with the prompt on stdin. Returns raw stdout.

    `llm_cmd` is a shell-style string like 'claude --print' or
    'pi --print -nc -nt --mode json'. Model is appended via --model if given."""
    cmd = shlex.split(llm_cmd)
    if not cmd:
        raise SystemExit(color("error: OV_LLM_CMD is empty", C_RED))
    if model:
        cmd += ["--model", model]
    try:
        result = subprocess.run(
            cmd,
            input=prompt,
            text=True,
            capture_output=True,
            timeout=timeout,
        )
    except FileNotFoundError:
        raise SystemExit(color(f"error: `{cmd[0]}` not found in PATH", C_RED))
    except subprocess.TimeoutExpired:
        raise RuntimeError(f"{cmd[0]} timed out after {timeout}s")
    if result.returncode != 0:
        raise RuntimeError(
            f"{cmd[0]} exited {result.returncode}: {result.stderr.strip()}"
        )
    return result.stdout


JSON_FENCE_RE = re.compile(r"```(?:json)?\s*(.*?)```", re.DOTALL)


def extract_json(text: str) -> dict:
    """Best-effort JSON extraction. Handles fenced and unfenced output."""
    text = text.strip()
    # 1) Direct parse
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        pass
    # 2) Fenced block
    m = JSON_FENCE_RE.search(text)
    if m:
        try:
            return json.loads(m.group(1).strip())
        except json.JSONDecodeError:
            pass
    # 3) First { ... last }
    start = text.find("{")
    end = text.rfind("}")
    if start != -1 and end > start:
        try:
            return json.loads(text[start : end + 1])
        except json.JSONDecodeError as e:
            raise ValueError(f"could not parse JSON from claude response: {e}\n--- raw ---\n{text}")
    raise ValueError(f"no JSON object found in claude response:\n--- raw ---\n{text}")


# ---------------------------------------------------------------------------
# Display
# ---------------------------------------------------------------------------

def show_note(path: Path, fm: dict, body: str) -> None:
    print()
    print(color("─" * 72, C_DIM))
    print(color(f"📥 {path.name}", C_PURPLE + C_BOLD))
    print(color("─" * 72, C_DIM))
    if fm:
        print(color("frontmatter:", C_DIM), fm)
    body_preview = body.strip()
    if len(body_preview) > 1200:
        body_preview = body_preview[:1200] + color("\n  …(truncated)…", C_DIM)
    print(color("body:", C_DIM))
    for line in body_preview.splitlines():
        print(color("  │ ", C_DIM) + line)


def show_proposal(p: dict) -> None:
    conf = p.get("confidence", "?")
    conf_color = {"high": C_GREEN, "medium": C_YELLOW, "low": C_RED}.get(conf, C_DIM)
    print()
    print(color("🤖 LLM proposal", C_CYAN + C_BOLD))
    print(f"  → {color(p.get('to', '?'), C_GREEN)}")
    print(f"  title:       {p.get('new_title', '?')}")
    print(f"  frontmatter: {p.get('frontmatter_patch', {})}")
    print(f"  confidence:  {color(conf, conf_color)}")
    print(f"  rationale:   {color(p.get('rationale', ''), C_DIM)}")


# ---------------------------------------------------------------------------
# Apply
# ---------------------------------------------------------------------------

def apply_proposal(
    vault: Path,
    src: Path,
    body: str,
    fm: dict,
    proposal: dict,
    inbox_name: str = "00-Inbox",
) -> Path:
    """Write the new file, delete the original. Returns target path."""
    to = proposal.get("to")
    if not to:
        raise ValueError("proposal missing 'to'")

    target = vault / to
    # Sanity: ensure target is inside vault and inside a PARA root
    target = target.resolve()
    if not str(target).startswith(str(vault.resolve())):
        raise ValueError(f"target escapes vault: {target}")
    rel_first = target.relative_to(vault).parts[0]
    if rel_first not in PARA_ROOTS + [inbox_name]:
        raise ValueError(f"target not in a PARA root: {target}")

    # If "to" is a directory, append a filename derived from new_title
    new_title = proposal.get("new_title") or src.stem
    new_title = slugify_title(new_title)
    if target.is_dir() or to.endswith("/"):
        target = target / f"{new_title}.md"
    elif not target.suffix:
        target = target.with_suffix(".md")

    # Refuse to overwrite an existing file
    if target.exists():
        raise FileExistsError(f"target already exists: {target} — pick a different name")

    target.parent.mkdir(parents=True, exist_ok=True)

    # Merge frontmatter
    new_fm = dict(fm)
    # Drop inbox-only fields
    new_fm.pop("type", None) if new_fm.get("type") == "inbox" else None
    patch = proposal.get("frontmatter_patch") or {}
    for k, v in patch.items():
        if k in ("created", "modified"):
            continue  # we own these
        new_fm[k] = v
    if "created" not in new_fm:
        new_fm["created"] = dt.date.today().isoformat()
    new_fm["modified"] = dt.date.today().isoformat()

    # Body: respect body_patch if non-null (we tell the LLM not to set it in v1)
    new_body = body
    if proposal.get("body_patch"):
        new_body = proposal["body_patch"]

    target.write_text(render_frontmatter(new_fm) + "\n" + new_body.lstrip("\n"))
    src.unlink()
    return target


# ---------------------------------------------------------------------------
# Interactive loop
# ---------------------------------------------------------------------------

def prompt_choice(prompt: str, choices: str) -> str:
    while True:
        print(color(prompt, C_BOLD), end=" ")
        try:
            ans = input().strip().lower()
        except EOFError:
            return "q"
        if not ans:
            return ""
        if ans[0] in choices:
            return ans[0]
        print(color(f"  invalid choice; expected one of [{choices}]", C_YELLOW))


def edit_proposal(folders: list[str], p: dict) -> dict:
    """Field-by-field edit (v1 — keep simple)."""
    print(color("Edit fields. Press Enter to keep current value.", C_DIM))
    print(color("Available folders:", C_DIM))
    for i, f in enumerate(folders, 1):
        print(color(f"  {i:>2}. {f}", C_DIM))

    cur_to = p.get("to", "")
    print(f"to [{cur_to}]: ", end="")
    new_to = input().strip()
    if new_to:
        # accept either an index or a path
        if new_to.isdigit() and 1 <= int(new_to) <= len(folders):
            new_to = folders[int(new_to) - 1]
        p["to"] = new_to

    cur_title = p.get("new_title", "")
    print(f"new_title [{cur_title}]: ", end="")
    new_title = input().strip()
    if new_title:
        p["new_title"] = new_title

    fm_patch = p.get("frontmatter_patch") or {}
    cur_tags = ",".join(fm_patch.get("tags", [])) if isinstance(fm_patch.get("tags"), list) else fm_patch.get("tags", "")
    print(f"tags (comma-sep) [{cur_tags}]: ", end="")
    new_tags = input().strip()
    if new_tags:
        fm_patch["tags"] = [t.strip() for t in new_tags.split(",") if t.strip()]
        p["frontmatter_patch"] = fm_patch

    cur_status = fm_patch.get("status", "")
    print(f"status [{cur_status}]: ", end="")
    new_status = input().strip()
    if new_status:
        fm_patch["status"] = new_status
        p["frontmatter_patch"] = fm_patch

    return p


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main() -> int:
    parser = argparse.ArgumentParser(description="LLM-assisted inbox triage")
    parser.add_argument("--vault", type=Path, default=None,
                        help="vault root (overrides OV_VAULT_DIR)")
    parser.add_argument("--limit", type=int, default=0, help="process at most N notes")
    parser.add_argument("--dry-run", action="store_true", help="print proposals; do not move files")
    parser.add_argument("--model", type=str, default=None, help="override LLM model")
    parser.add_argument("--llm-cmd", type=str, default=None,
                        help="override OV_LLM_CMD (e.g. 'pi --print -nc -nt --mode json')")
    parser.add_argument("--config", type=Path, default=None, help="path to config file")
    args = parser.parse_args()

    cfg = load_config(args.config)

    # Resolve vault dir: --vault > config > error
    vault_str = str(args.vault) if args.vault else cfg.get("OV_VAULT_DIR")
    if not vault_str:
        print(color("error: vault not set; use --vault or set OV_VAULT_DIR in config", C_RED),
              file=sys.stderr)
        return 2
    vault: Path = Path(os.path.expandvars(vault_str)).expanduser().resolve()

    # Populate module-level PARA_ROOTS used by apply_proposal()
    global PARA_ROOTS
    PARA_ROOTS = [
        cfg["OV_PROJECTS"],
        cfg["OV_AREAS"],
        cfg["OV_RESOURCES"],
        cfg["OV_ARCHIVE"],
    ]

    llm_cmd = args.llm_cmd or cfg.get("OV_LLM_CMD") or "claude --print"
    model = args.model or cfg.get("OV_MODEL") or None
    inbox = vault / cfg["OV_INBOX"]
    agents_md_path = vault / "AGENTS.md"

    if not agents_md_path.is_file():
        print(color("error: AGENTS.md not found at vault root", C_RED), file=sys.stderr)
        return 2
    if not inbox.is_dir():
        print(color("error: 00-Inbox/ not found", C_RED), file=sys.stderr)
        return 2

    agents_md = agents_md_path.read_text()
    folders = discover_folders(vault, [cfg["OV_PROJECTS"], cfg["OV_AREAS"],
                                       cfg["OV_RESOURCES"], cfg["OV_ARCHIVE"]])

    inbox_files = sorted(p for p in inbox.glob("*.md") if p.is_file())
    if args.limit:
        inbox_files = inbox_files[: args.limit]

    if not inbox_files:
        print(color("✓ Inbox is empty", C_GREEN))
        return 0

    print(color(f"🔄 LLM triage — {len(inbox_files)} note(s)", C_CYAN + C_BOLD))
    if args.dry_run:
        print(color("(dry-run: no files will be moved)", C_DIM))

    counts = {"approved": 0, "edited": 0, "skipped": 0, "deleted": 0, "errored": 0}

    for src in inbox_files:
        try:
            text = src.read_text()
        except Exception as e:
            print(color(f"error reading {src}: {e}", C_RED))
            counts["errored"] += 1
            continue
        fm, body = split_frontmatter(text)
        show_note(src, fm, body)

        proposal: dict | None = None
        while True:
            if proposal is None:
                prompt = PROMPT_TEMPLATE.format(
                    folders="\n".join(f"  - {f}" for f in folders),
                    from_path=str(src.relative_to(vault)),
                    agents_md=agents_md,
                    current_fm=json.dumps(fm),
                    body=body.strip() or "(empty body)",
                )
                print(color("  thinking…", C_DIM))
                try:
                    raw = call_llm(prompt, llm_cmd=llm_cmd, model=model)
                    proposal = extract_json(raw)
                except Exception as e:
                    print(color(f"  LLM call failed: {e}", C_RED))
                    counts["errored"] += 1
                    break

            show_proposal(proposal)

            if args.dry_run:
                break

            choice = prompt_choice(
                "  [a]pprove  [e]dit  [s]kip  [d]elete  [r]e-ask  [q]uit ?",
                "aesdrq",
            )
            if choice == "a":
                try:
                    target = apply_proposal(vault, src, body, fm, proposal,
                                            inbox_name=cfg["OV_INBOX"])
                    print(color(f"  ✓ filed → {target.relative_to(vault)}", C_GREEN))
                    counts["approved"] += 1
                except Exception as e:
                    print(color(f"  apply failed: {e}", C_RED))
                    counts["errored"] += 1
                break
            elif choice == "e":
                proposal = edit_proposal(folders, proposal)
                counts["edited"] += 1
                continue
            elif choice == "s" or choice == "":
                print(color("  → skipped", C_YELLOW))
                counts["skipped"] += 1
                break
            elif choice == "d":
                confirm = prompt_choice("  really delete? [y/n]", "yn")
                if confirm == "y":
                    src.unlink()
                    print(color("  → deleted", C_RED))
                    counts["deleted"] += 1
                else:
                    print(color("  → cancelled", C_DIM))
                break
            elif choice == "r":
                proposal = None  # force re-call
                continue
            elif choice == "q":
                print(color("  → quit", C_YELLOW))
                summary(counts)
                return 0

    summary(counts)
    return 0


def summary(counts: dict) -> None:
    print()
    print(color("─" * 72, C_DIM))
    print(color("triage summary", C_BOLD))
    for k, v in counts.items():
        print(f"  {k:<10} {v}")


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        print()
        print(color("interrupted", C_YELLOW))
        sys.exit(130)
