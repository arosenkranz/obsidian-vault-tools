# obsidian-vault-tools

A small CLI (`ov`) for managing a PARA-organized Obsidian vault, plus an LLM-assisted inbox triage script.

## What's here

| Path | What |
|---|---|
| `bin/vault.sh` | The `ov` CLI: inbox, capture, triage, new, review, stale, mocs |
| `bin/triage_llm.py` | LLM-assisted inbox triage (`ov triage --llm`) |
| `templates/` | Canonical AGENTS.md and `99-Meta/` templates for fresh vaults |
| `examples/ov.config.example` | Example per-machine config |
| `docs/` | Install guide, config reference, architecture notes |

## Install

```bash
git clone <this repo> ~/workspace/obsidian-vault-tools
cd ~/workspace/obsidian-vault-tools
make install
$EDITOR ~/.config/ov/config    # set OV_VAULT_DIR
ov inbox                       # smoke test
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

`ov help` for full usage.

## Architecture

Two sync mechanisms, two clean responsibilities:

- **Tool** (this repo) — synced via git. Symlinked to `~/.local/bin/ov` per machine.
- **Vault content** (notes, templates, dashboards, AGENTS.md) — synced via Obsidian Sync (or whatever you use).

Per-machine config lives at `~/.config/ov/config` and points at your vault. See [docs/architecture.md](docs/architecture.md) for the full story.

## Requirements

- macOS or Linux (bash, Python 3.9+)
- An LLM CLI on PATH for `ov triage --llm` — defaults to `claude --print`, swappable to `pi --print -nc -nt --mode json` via config.
- Optional: [Dataview](https://github.com/blacksmithgu/obsidian-dataview) plugin in Obsidian for the dashboard pages.
