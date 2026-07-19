# Config reference

The `ov` CLI reads per-machine configuration from `~/.config/ov/config.toml` (override via `$OV_CONFIG`). It's a TOML file. `ov init` creates a starter file with every key commented out except the required `vault_dir`.

## Resolution order

For each setting, most specific wins:

1. CLI flag (e.g. `--vault`)
2. Environment variable (e.g. `OV_VAULT_DIR` — same names as the legacy bash config, for compatibility)
3. Config file
4. Built-in default

## Keys

| TOML key | Env var | Required | Default | Description |
|---|---|---|---|---|
| `vault_dir` | `OV_VAULT_DIR` | **yes** | — | Absolute path to the Obsidian vault root |
| `inbox` | `OV_INBOX` | no | `00-Inbox` | Inbox folder name (relative to vault) |
| `projects` | `OV_PROJECTS` | no | `01-Projects` | Projects folder name |
| `areas` | `OV_AREAS` | no | `02-Areas` | Areas folder name |
| `resources` | `OV_RESOURCES` | no | `03-Resources` | Resources folder name |
| `archive` | `OV_ARCHIVE` | no | `04-Archive` | Archive folder name |
| `meta` | `OV_META` | no | `99-Meta` | Meta folder name (templates, dashboards) |
| `llm_cmd` | `OV_LLM_CMD` | no | `claude --print` | Command piped triage/cleanup/publish prompts; must accept stdin and emit text on stdout |
| `model` | `OV_MODEL` | no | (empty) | Model passed via `--model` to `llm_cmd`. Empty = LLM default |
| `docs_host` | `OV_DOCS_HOST` | no (required only for `publish`/`unpublish`) | — | SSH host for `ov publish`/`ov unpublish` |
| `docs_path` | `OV_DOCS_PATH` | no | `/var/www/docs` | Remote path on the docs host |
| `docs_url` | `OV_DOCS_URL` | no | — | Public base URL, used to print a "Live at ..." link after publish |
| `stale_exclude` | — (file-only, a TOML array) | no | `["Daily Notes"]` | Additional folder names `ov stale` excludes beyond Archive/Meta |

## Examples

### Default — Claude Code

```toml
vault_dir = "$HOME/Documents/main-vault"
llm_cmd = "claude --print"
```

### `pi` for triage (recommended if you have it)

`pi --print` plus three flags that matter:
- `-nc` — disable AGENTS.md auto-discovery (we already pass it in the prompt; otherwise it's loaded twice)
- `-nt` — disable bundled tools (triage just needs a text response, not bash/edit/write)
- `--mode json` — native JSON output (cleaner parsing)

```toml
vault_dir = "$HOME/Documents/main-vault"
llm_cmd = "pi --print -nc -nt --mode json"
model = "anthropic/claude-sonnet-4-5"
```

### Renamed PARA folders

```toml
projects = "Projects"
areas = "Areas"
resources = "Resources"
archive = "Archive"
```

(Display strings inside `ov triage` — the manual interactive flow — are not yet configurable; only paths are.)

## Migrating an old bash-format config

`ov config migrate --from <path>` reads the legacy `OV_KEY="value"` format (see `examples/ov.config.example` for the shape) and prints the equivalent TOML to stdout.

## Per-machine variations

Common cases:

- **Vault path differs**: just `vault_dir`.
- **Different LLM available** (e.g. claude on one machine, pi on another): set `llm_cmd` per machine.
- **Different model preference** (e.g. opus on workstation, sonnet on laptop): set `model` per machine.

The config file is intentionally outside the repo — the tool's git history stays generic, the per-machine settings stay local.
