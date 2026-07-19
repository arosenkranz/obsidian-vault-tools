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
