# Architecture

## Two sync mechanisms, two responsibilities

| Layer | Synced by | Canonical source |
|---|---|---|
| **Tool** (`bin/`, Makefile, etc.) | git | this repo |
| **Templates snapshot** (`templates/`) | git | this repo (snapshot — vault wins for live values) |
| **Per-machine config** (`~/.config/ov/config`) | not synced | each machine |
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

`bin/vault.sh`, `bin/triage_llm.py`, and `bin/moc_cleanup.py`:

1. Read `~/.config/ov/config` (or `$OV_CONFIG`).
2. Apply env-var overrides.
3. Apply CLI-flag overrides.

None of the scripts have hardcoded paths. The repo can live anywhere; the vault can live anywhere; PARA folders can be renamed.

## LLM-call abstraction

`bin/triage_llm.py` shells out to whatever `OV_LLM_CMD` points at. The contract is minimal:

- prompt arrives on stdin
- response (a single JSON object matching the AGENTS.md §7 schema) on stdout
- non-zero exit code = failure

`bin/moc_cleanup.py` (`ov mocs cleanup`) reuses the same `load_config`/
`call_llm`/`extract_json` plumbing from `triage_llm.py` rather than
duplicating it, so both tools stay consistent as `OV_LLM_CMD` evolves. Its
response schema is different (`new_content`/`duplicates_flagged`/`summary`,
see its own prompt in `bin/moc_cleanup.py`), and unlike triage it is never
invoked autonomously — every write requires an explicit diff + `y`
confirmation from the human running the command.

This is met by both `claude --print` and `pi --print -nc -nt --mode json`. Other LLM CLIs that accept stdin and return text on stdout will work too.

The script tries direct JSON parse first, then fenced-block extraction, then last-resort `{...}` extraction — so it tolerates LLMs that wrap output in markdown.

## Hard rules / safety

The triage script is **suggest-only**:
- Every move is approved per-note.
- Bodies are never auto-rewritten (`body_patch` forced to null in the v1 prompt).
- Wikilinks to existing notes are not proposed (`links_to_add` forced to `[]` in v1).
- Targets are sanity-checked: must be inside the vault, must be inside one of the configured PARA roots.
- If a note carries a `moc:` frontmatter field (set by `ov capture --moc`) and triage renames it, the script does a best-effort update of that MOC's `[[old title]]` entry to `[[new title]]` so the link keeps resolving. This is a mechanical string substitution only (see `update_moc_entry_title` in `bin/triage_llm.py`) — it never reorders or otherwise edits the MOC, and any failure is reported but does not roll back the already-completed file move.

Future v2 changes (append-to-existing, body cleanup, wikilinks) will require explicit AGENTS.md schema updates — that file is the contract.

## What if I want to publish this publicly?

Currently private. Before flipping public, scrub:

1. `LICENSE` already clean (MIT).
2. `templates/AGENTS.md` and `templates/99-Meta/Vault Guide.md` mention specific project names and "Datadog". Replace with placeholders or remove.
3. `templates/99-Meta/Workflow Guide.md`, `Naming Conventions Guide.md` mention "Datadog" — same scrub.
4. Add a CONTRIBUTING.md if you want contributions.
5. Mention LLM CLI requirements clearly in README.

`bin/vault.sh`, `bin/triage_llm.py`, and `bin/moc_cleanup.py` are already generic — they have no personal information.
