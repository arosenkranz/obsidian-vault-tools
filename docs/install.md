# Install

## Prerequisites

- bash, Python 3.9+
- An LLM CLI on PATH for `ov triage --llm`. Either:
  - [Claude Code](https://docs.anthropic.com/en/docs/claude-code) — `claude --print`
  - [pi](https://github.com/anthropics/pi) — `pi --print -nc -nt --mode json` (recommended for triage: native JSON output, no auto-discovery side effects)
- An Obsidian vault organized PARA-style (or the equivalent — folder names are configurable).

## First machine

```bash
git clone <this repo> ~/workspace/obsidian-vault-tools
cd ~/workspace/obsidian-vault-tools
make install
```

`make install` does two things:
1. Symlinks `bin/vault.sh` → `~/.local/bin/ov`
2. Copies `examples/ov.config.example` → `~/.config/ov/config` (only if not present)

Edit the config:

```bash
$EDITOR ~/.config/ov/config
```

The only required value is `OV_VAULT_DIR`. Smoke-test:

```bash
ov inbox
ov capture "first capture from this machine"
ov inbox    # should now show the new note
```

## Second machine (and beyond)

If your vault is already synced (Obsidian Sync, iCloud, Dropbox, Syncthing, etc.), the vault is already on the new machine. You only need to install the tool:

```bash
git clone <this repo> ~/workspace/obsidian-vault-tools
cd ~/workspace/obsidian-vault-tools
make install
$EDITOR ~/.config/ov/config    # set OV_VAULT_DIR for this machine's vault path
ov inbox                       # smoke test
```

## Fresh vault from scratch (no existing notes)

If you don't have a vault yet, you can use `templates/` as a starting point:

```bash
mkdir -p ~/Documents/my-new-vault
cp -r templates/AGENTS.md ~/Documents/my-new-vault/
cp -r templates/99-Meta ~/Documents/my-new-vault/
mkdir -p ~/Documents/my-new-vault/{00-Inbox,01-Projects,02-Areas,03-Resources,04-Archive}
```

Then point `OV_VAULT_DIR` at it.

## Updating

```bash
cd ~/workspace/obsidian-vault-tools
git pull
# No need to re-run `make install` unless the Makefile or config example changed.
```

## Uninstall

```bash
make uninstall
# Optional: rm -rf ~/.config/ov
# Optional: rm -rf ~/workspace/obsidian-vault-tools
```
