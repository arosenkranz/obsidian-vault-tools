# Config reference

The `ov` CLI reads per-machine configuration from `~/.config/ov/config` (override via `$OV_CONFIG`). It's a bash-style file: `KEY=VALUE`, comments with `#`. Bash sources it directly; Python reads it with a regex.

## Resolution order

For each setting, most specific wins:

1. CLI flag (e.g. `--vault`, `--model`, `--llm-cmd`)
2. Environment variable (e.g. `OV_VAULT_DIR`)
3. Config file
4. Built-in default

## Keys

| Key | Required | Default | Description |
|---|---|---|---|
| `OV_VAULT_DIR` | **yes** | — | Absolute path to the Obsidian vault root |
| `OV_INBOX` | no | `00-Inbox` | Inbox folder name (relative to vault) |
| `OV_PROJECTS` | no | `01-Projects` | Projects folder name |
| `OV_AREAS` | no | `02-Areas` | Areas folder name |
| `OV_RESOURCES` | no | `03-Resources` | Resources folder name |
| `OV_ARCHIVE` | no | `04-Archive` | Archive folder name |
| `OV_META` | no | `99-Meta` | Meta folder name (templates, dashboards) |
| `OV_LLM_CMD` | no | `claude --print` | Command piped triage prompts; must accept stdin and emit text on stdout |
| `OV_MODEL` | no | (empty) | Model passed via `--model` to `OV_LLM_CMD`. Empty = LLM default |

## Examples

### Default — Claude Code

```bash
OV_VAULT_DIR="$HOME/Documents/main-vault"
OV_LLM_CMD="claude --print"
```

### `pi` for triage (recommended if you have it)

`pi --print` plus three flags that matter:
- `-nc` — disable AGENTS.md auto-discovery (we already pass it in the prompt; otherwise it's loaded twice)
- `-nt` — disable bundled tools (triage just needs a text response, not bash/edit/write)
- `--mode json` — native JSON output (cleaner parsing)

```bash
OV_LLM_CMD="pi --print -nc -nt --mode json"
OV_MODEL="anthropic/claude-sonnet-4-5"   # or whatever
```

### Renamed PARA folders

```bash
OV_PROJECTS="Projects"
OV_AREAS="Areas"
OV_RESOURCES="Resources"
OV_ARCHIVE="Archive"
```

(Display strings inside `ov triage` — the manual interactive flow — are not yet configurable; only paths are.)

## Per-machine variations

Common cases:

- **Vault path differs**: just `OV_VAULT_DIR`.
- **Different LLM available** (e.g. claude on one machine, pi on another): set `OV_LLM_CMD` per machine.
- **Different model preference** (e.g. opus on workstation, sonnet on laptop): set `OV_MODEL` per machine.

The config file is intentionally outside the repo — the tool's git history stays generic, the per-machine settings stay local.
