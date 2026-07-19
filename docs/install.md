# Install

## Prerequisites

- Go 1.25+ (to build).
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

`make install` does three things:
1. Builds `dist/ov` (`go build ./cmd/ov`)
2. Copies it to `~/.local/bin/ov`
3. Creates `~/.config/ov/config.toml` from `ov init`'s built-in template if not already present

Edit the config:

```bash
$EDITOR ~/.config/ov/config.toml
```

The only required value is `vault_dir`. Smoke-test:

```bash
ov doctor
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
$EDITOR ~/.config/ov/config.toml    # set vault_dir for this machine's vault path
ov doctor                           # smoke test
```

## Migrating an old bash-format config

If you have a config from before the Go rewrite (`OV_VAULT_DIR="..."` style, at `~/.config/ov/config`), convert it:

```bash
ov config migrate --from ~/.config/ov/config > ~/.config/ov/config.toml
```

## Fresh vault from scratch (no existing notes)

If you don't have a vault yet, you can use `templates/` as a starting point:

```bash
mkdir -p ~/Documents/my-new-vault
cp -r templates/AGENTS.md ~/Documents/my-new-vault/
cp -r templates/99-Meta ~/Documents/my-new-vault/
mkdir -p ~/Documents/my-new-vault/{00-Inbox,01-Projects,02-Areas,03-Resources,04-Archive}
```

Then point `vault_dir` at it.

## Updating

```bash
cd ~/workspace/obsidian-vault-tools
git pull
make install    # rebuilds and reinstalls the binary
```

## Uninstall

```bash
make uninstall
# Optional: rm -rf ~/.config/ov
# Optional: rm -rf ~/workspace/obsidian-vault-tools
```
