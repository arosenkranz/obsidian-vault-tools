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
