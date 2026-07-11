# ov v2 — Go rewrite design

**Status:** approved design, pre-implementation
**Date:** 2026-07-11
**Reviewed by:** trevelyan (adversarial, 24 findings), m (architecture/planning, 15 findings) — both SHIP-WITH-CHANGES, all required changes incorporated below.

## Summary

Rewrite `obsidian-vault-tools` (bash `vault.sh` ~1,200 ln + python `triage_llm.py`/`moc_cleanup.py` ~1,050 ln) as a single static Go binary with two frontends over one core: a flags-first CLI (bubbletea TUI only where interaction demands it) and an embedded web app (`ov serve`, Go templates + htmx) for away-from-desk capture and triage. Clean-room v2: the **vault format** is the compatibility contract, not command behavior — with one frozen exception (see §Contract). Ships as `ov2` per phase into daily use; renamed to `ov` at full parity.

## Motivation (confirmed pain points)

1. bash at its ceiling — vault.sh hard to extend or test (zero test coverage)
2. Install/portability friction — symlink + python + config dance per machine
3. Clunky interactive UX — prompt-based flows, gum/fzf external deps
4. No mobile/away-from-desk access — capture and triage need a terminal

## Locked decisions

| Decision | Choice | Rationale |
|---|---|---|
| Language | Go, single static binary | kills #2; one language; Charm ecosystem; user fluent |
| Approach | clean-room v2 | porting clunky UX faithfully preserves the clunk |
| Frontends | CLI + web over stateless core verbs | "all of it eventually" in web; mobile capture |
| Web stack | Go templates + htmx, go:embed, stdlib ServeMux | no node toolchain; single-binary embed |
| CLI UX | flags-first; bubbletea for triage approval, pickers, diff-confirm | scripting/agent-safe; TUI where it earns it |
| LLM | subprocess contract preserved (`OV_LLM_CMD`, stdin prompt → stdout) | zero new auth surface; per-machine provider swap |
| Deployment | `ov serve` localhost on the Mac; remote later = Tailscale/tsnet | Obsidian Sync is E2E-encrypted, no server API — no homelab replica exists |
| Repo | same repo; Go replaces `bin/`; `templates/`, `docs/` stay | history preserved |
| Config | TOML at `~/.config/ov/config.toml`; env names stay `OV_*` | `ov config migrate` prints TOML from old file |
| Cutover | `ov2` in daily use per phase; single rename at parity | both reviewers: all-or-nothing = stall pattern; no shims, one deletion |
| Web v1 scope | capture form, inbox, triage propose/approve+diff | mobile-relevant flows; rest CLI-only until v1.1 (resequencing, not scope cut) |
| `ov render` | port (goldmark) | user call |

## Compatibility contract

1. **The vault**: PARA folders (names configurable), frontmatter conventions, MOC wikilink structure, AGENTS.md §7 proposal schema.
2. **Frozen CLI subset** (AGENTS.md §9–10 mandates exact invocations; LLM agents depend on them): `ov capture` flag surface (`--title`, `--tags`, `--source`, `--moc`), stdin body semantics (heredoc), `--source` values, exit codes. Any change to this subset updates AGENTS.md in the same commit. Everything else is clean-room.
3. **Safety rules carried over as requirements** (intent, with holes closed — see §Safety): suggest-only triage, per-note human approval, diff+confirm before any LLM-driven write, MOC link-rename sync, target containment inside vault + PARA roots.

## Architecture

Single Go module. Direct deps: cobra; bubbletea+bubbles+lipgloss (one unit); go-toml; go-diff; goldmark (render). Stdlib ServeMux (Go 1.22+ method+wildcard routing) — no chi, no viper, no yaml, no SSH lib.

```
cmd/ov/            cobra commands — thin: parse flags → config.Load → core verb → render via tui/plain
internal/config/   Config struct; TOML load; flag>env>file>default merged exactly once;
                   ~/$HOME expansion for path values; full key inventory incl. OV_DOCS_HOST/PATH/URL;
                   subcommands: ov init, ov doctor, ov config migrate
internal/vault/    pure FS domain — no network, no LLM, no terminal:
                   Note{Path, FM, Body}; lossless Frontmatter; slugify(maxLen);
                   WriteNoteAtomic; conditional writes; filename policy; containment;
                   PARA discovery; queries: ListInbox, Stale(days), Orphans, MOCs;
                   mechanical MOC ops (entry add, wikilink count, link rename)
internal/llm/      transport: Run(ctx, prompt) — argv-exec via shlex-style split, never a shell;
                   decoders: ExtractJSON (direct → fenced → {...}), ExtractHTMLBlock;
                   job table; process-group kill; semaphore(2); failure classification; health check
internal/capture/  title derivation; bare-URL detection; TitleFetcher interface (http impl injected);
                   filename stamping + collision suffix; MOC entry append
internal/triage/   Propose(ctx, note) → Proposal; Validate(p); Apply(vault, note, p) → Result;
                   AGENTS.md §7 Proposal type; PARA-root gate; body_patch/links_to_add rejection;
                   MOC rename sync
internal/moc/      LLM cleanup workflow only: ProposeCleanup, structural validator, Apply
                   (mechanical ops live in vault; split named in doc comments)
internal/publish/  LLM→HTML convert; rsync/ssh push/remove/list-remote via subprocess
internal/render/   goldmark md→HTML; RENDER_SOURCE marker splicing (port of render_html.py logic)
internal/tui/      bubbletea models: folder picker, MOC picker, triage approval loop, diff-confirm
internal/web/      server.go (accepts injected net.Listener); routes.go; handlers.go (thin);
                   jobs.go (in-process job map); assets/ via go:embed
```

**Core interaction contract — stateless verbs.** Core packages expose `Propose`/`Validate`/`Apply`-shaped functions; no callbacks, no loops, no UI types. Each frontend owns its loop and state: the bubbletea model drives the CLI approval flow; the web server holds pending proposals in an in-process map keyed by note path (regenerated on restart — fine for one user). This is the invariant that makes two-frontends-one-core real; any core function that wants a callback is a design error.

## Core contracts (internal/vault)

**Lossless frontmatter (review blocker).** Parse leniently for reading (flat `key: value` view), but never re-serialize the whole block from the parsed dict. Mutations patch known keys in place in the original text; unrecognized lines (nested maps, block lists, multiline strings, comments, dataview fields) pass through byte-for-byte. Acceptance: parse + no-op patch is byte-identical across a corpus of real vault notes. No yaml dependency — a YAML lib would canonicalize formatting and create sync diff noise.

**Atomic + conditional writes.** The real concurrent writer is Obsidian Sync; no architecture fixes that, so:
- Atomicity: temp file created in the **target directory** (never os.TempDir — cross-device rename), dot-prefixed `.ov-tmp-*` (Obsidian ignores dotfiles, so partial files never sync), write, fsync file, rename, best-effort dir fsync. One `WriteNoteAtomic` used by every write path including MOC entry edits.
- Conditionality: every read returns `(content, hash)`; every write re-reads and compares immediately before rename; mismatch → "changed on disk, refresh" error, never a silent clobber. Matters because LLM flows hold snapshots across 30–180s calls plus human approval time — under `ov serve` the stale-snapshot window is the common case. LLM proposals re-validate against fresh content at apply time (the moc validator is already re-runnable by design).

**Filename policy.** NFC-normalize titles; enforce uniqueness case-insensitively against a real directory listing, not `exists()` (APFS is case-insensitive; Obsidian Sync peers are not — case-twin filenames produce conflicted-copy spam). Policy documented in AGENTS.md. Table-tested with case-variant and NFD inputs.

**Containment.** Current check is `startswith(vault_path)` — accepts sibling `/path/vault-evil`. v2: `filepath.EvalSymlinks` both sides, `filepath.Rel(vault, target)`, reject absolute or `..`-prefixed results. Table-tested with sibling dir, traversal, symlinked subdir.

**Walk resilience.** Scans tolerate files vanishing mid-walk (sync pull): WalkDir error callback skips ENOENT; unreadable files are skip-and-report, never fatal in serve paths.

**No index (explicit non-goal).** 10³–10⁴ notes: per-request scans are fine; an index is a cache-invalidation liability against a concurrent syncer. All reads go through vault query functions so an mtime-keyed cache can slot in behind the same signatures if serve latency ever demands it.

## LLM subsystem

**Transport.** `Run(ctx, prompt)`: shlex-style argv split of `OV_LLM_CMD` (+ `--model` when `OV_MODEL` set), prompt on stdin, stdout captured, argv[0] resolved to absolute path at startup (launchd won't have nvm PATH). Never through a shell — this also retires `publish --llm`'s current `eval "$llm_cmd"`.

**Two decoders, not one.** `ExtractJSON` (direct parse → fenced block → last-resort `{…}`) for triage/moc; `ExtractHTMLBlock` for publish/render conversion. The HTML contract is defined alongside the JSON one — it was previously implicit in sed.

**Job model (designed now, not retrofitted).** LLM calls take up to 120s (triage) / 180s (moc cleanup); a synchronous handler dies on mobile Safari over Tailscale. In-process job table: submit → job id; status polled; result swapped in. Used by web (htmx poll) and available to TUI. Process-**group** kill on timeout/cancel (`Setpgid` + `kill(-pgid)`) — `claude` spawns a node tree that `CommandContext` alone orphans. Bounded semaphore of 2 concurrent LLM subprocesses.

**Failure classification.** Exit code + stderr sniffed for auth/login markers → typed errors. Web UI renders "LLM auth expired — run `claude login` on the Mac", not a 500. Health endpoint dry-runs the LLM. Documented: `ov serve` runs in a login session (launchd daemon context can lose keychain access).

**Prompt injection posture.** `claude --print` is not a pure function — it runs with the user's global tool permissions in its CWD. Note bodies and fetched URL titles are attacker-influenced and are interpolated into prompts. Defenses: invoke with tools disabled (flags/settings profile documented in the config example); subprocess CWD = empty scratch dir, never the vault; every proposal gate enforced server-side in `internal/triage` (see §Safety); human approval remains mandatory.

## Safety (holes closed relative to v1)

1. **body_patch enforcement hole (found in review):** today the prompt forbids `body_patch`, but `apply_proposal` honors a non-null one and the approval display never shows it — an LLM ignoring instructions can rewrite a note body invisibly through an approval that displayed only the move. v2: `triage.Validate` **rejects** proposals with non-null `body_patch` or non-empty `links_to_add` while the schema says v1. Enforcement in code, not prompt; in core, not frontend.
2. Suggest-only, per-note approval, diff+confirm: unchanged as requirements; all gates server-side.
3. MOC rename sync: best-effort, never aborts a completed move, body-only substitution, warns on missing MOC/entry (current semantics preserved deliberately).
4. SSRF: TitleFetcher keeps the 5s cap and UA, adds post-DNS IP check refusing loopback/private/link-local/CGNAT targets (server-side fetch of attacker-chosen URLs is otherwise a probe primitive).

## Web layer

- Server-rendered Go templates + htmx; assets embedded (`internal/web/assets` — deliberately not named `templates/` to avoid colliding with the repo's vault templates).
- Triage flow: `POST /triage/{note}/propose` → 202 + job id → htmx `hx-trigger="every 2s"` poll → proposal card with diff (go-diff, server-rendered like moc_cleanup's difflib output) → approve/skip POSTs.
- **Localhost hygiene (v1, ~30 lines, distinct from auth):** Host-header validation against configured bind (kills DNS rebinding); state-changing routes require same-origin/absent Origin AND `HX-Request` header (kills drive-by form CSRF — CORS does not stop cross-origin form POSTs to 127.0.0.1). Any local process can still hit the API; accepted for v1, documented.
- **Bind guard:** default 127.0.0.1; refuse non-loopback binds unless the address belongs to a Tailscale interface (100.64.0.0/10) or an explicit long-named override flag is set. The 0.0.0.0-on-café-wifi path must be hard to take by accident.
- **Listener seam:** `web.New(listener net.Listener, …)` — remote access later is tsnet (in-process Tailscale, no OS port), slotting into the same constructor with zero handler changes.
- Web v1 surface: capture form (title-fetch as explicit opt-in checkbox), inbox list, triage propose/approve. Everything else CLI-only until v1.1.

## CLI / TUI

- Flags-first: every interactive flow fully drivable by flags/args alone (acceptance criterion, testable). No gum, no fzf — bubbletea replaces both (directly serves pain #2).
- **tty discipline (load-bearing, from AGENTS.md heredoc usage):** piped stdin carries the capture body and must never fight the TUI — TUI input from /dev/tty, output to tty/stderr; stdout carries machine-readable results only; no tty at all (cron/agent) → non-interactive fallback or clean error. CI smoke test: `echo body | ov2 capture --title x` with stdout piped.
- MOC entry placement: the personal emoji-heading preference chain leaves the tool; v2 rule is append under `## 🔗 Recent Additions` (create if missing). `obsidian://open?vault=…` name comes from config (currently hardcoded `main-vault` — bug, fixed not ported).
- Stale exclusions (`Daily Notes`) move to config.

## Command disposition (every vault.sh dispatch arm)

| Command | Disposition |
|---|---|
| `capture` | Port — flag surface frozen (contract §2) |
| `inbox` | Port |
| `triage` / `triage --llm` | Port; clean-room UX; hardened validator |
| `new` | Port; template `{{title}}` substitution; vault name from config |
| `review`, `stale` | Port; exclusions configurable |
| `mocs list/new/add` | Port; MOC placement rule simplified (above) |
| `mocs orphan` | **Reimplement — broken today**: `found_in_moc` set in a `\| while read` subshell never propagates (every note reports orphaned); matching is substring grep, not link-aware. v2: parse `[[wikilink]]` targets, compare against note basenames |
| `mocs cleanup` | Port; validator table tests land before workflow |
| `publish` / `publish --llm` / `unpublish` | Port; argv-exec (no `eval`); HTML decoder contract explicit |
| `render` | Port via goldmark + marker splicing |
| `help` | Regenerated by cobra |

## Testing strategy

Three tiers; mined rows classified **CONTRACT / BUG / DECIDE** (the orphan bug proves bug-for-bug fidelity is the wrong goal; the slugify 60-vs-80 char bash/python divergence gets unified deliberately).

1. **Table tests as the clean-room spec.** Port existing pytest suites 1:1 first (already-mined corpus), then: frontmatter parse corpus (quotes, bracket lists, `moc: [[…]]` wikilink quirk, comments, no-colon lines, empty); slugify (heading-marker strip, forbidden set incl. `@&#`, word-boundary truncation, Untitled fallback, case preserved, NFC/case-variant inputs); ExtractJSON tiers; bare-URL detection + Cloudflare-interstitial rejection; MOC name resolution; `update_moc_entry_title`; moc validator (dropped URL / dropped bare wikilink / retitled URL-anchored link OK / frontmatter mutation); config precedence; containment (vault-evil sibling, traversal, symlink).
2. **Golden files** where bytes are the contract: rendered frontmatter key order (`type,created,modified,tags,status,source,url,project,area`); captured note under injected clock; both LLM prompt assemblies (prompt drift = silent behavior change).
3. **Temp-vault integration harness.** Fixture builder (PARA dirs, AGENTS.md, MOCs, inbox notes); `OV_LLM_CMD` pointed at a stub responder binary (TestMain re-exec) returning canned JSON/HTML — exercises the real transport deterministically. Covers: capture→file content; triage approve→move+MOC sync; collision suffix; containment and PARA-root rejection; body_patch rejection; cleanup validator rejection; atomicity (kill mid-write, original intact); conditional-write conflict. Web handlers via httptest against the same temp vault and stub. Inject clock and TitleFetcher; never inject the filesystem; no mocks of core.

Bash/python stays runnable as an oracle until cutover: same fixture vault, diff the results.

## Phasing

Binary ships as `ov2`; each phase ends with that command's **real daily traffic** on ov2. One rename (`ov2`→`ov`, delete symlink, retire `bin/`) at parity. Bash remains untouched and authoritative for not-yet-ported commands throughout.

| Phase | Content | Exit criterion |
|---|---|---|
| 0 | Module scaffold; internal/config; vault core (lossless FM, slugify, atomic+conditional writes); behavior-inventory pass (CONTRACT/BUG/DECIDE); mined table corpus | `go test ./...` green over corpus; `ov2 doctor` validates real config against live vault; zero write paths exist |
| 1 | Read-only: inbox, review, stale, mocs list, mocs orphan (fixed) | daily reads on ov2 against real vault; zero corruption risk by construction |
| 2 | capture (+URL title, MOC entry, collision) ; manual triage; tui pickers; **web capture form + inbox (`ov2 serve`)** | daily capture (CLI+phone via Tailscale-to-Mac) on ov2; piped-stdin verified; temp-vault tests for every write path |
| 3 | LLM triage: transport+decoders+job model; Propose/Validate/Apply; tui approval; web triage propose/approve; `--dry-run` | `ov2 triage --llm` replaces bash in daily use; dry-run diffed vs python on vault copy; stub-LLM e2e green |
| 4 | MOC suite: mocs new/add; cleanup with ported validator | validator decisions identical to python on recorded proposal corpus |
| 5 | publish/unpublish; render (goldmark) | every bash command has a Go equivalent; CLI surface complete |
| 6 | Web polish (diff views, job UX); parity checklist scripted side-by-side on vault copy; one week full parallel | **Cutover**: rename ov2→ov, delete symlink, retire bin/ |

Note phase 2 pulls web capture forward (reviewer point: mobile capture is the #1 unmet pain, needs only the lowest-risk write path — create-new-file, no read-modify-write).

## Risks

| Risk | Mitigation |
|---|---|
| ov-serve writes race Obsidian Sync | atomic dot-temp writes + conditional (hash) writes + apply-time re-validation; conflicts surface as visible refresh errors, never silent clobbers |
| Clean-room silently drops edge behavior | mining pass with CONTRACT/BUG/DECIDE classification; pytest corpus ported first; bash/python kept as oracle until cutover |
| LLM subprocess under long-running server | job model, process-group kill, semaphore(2), auth-failure classification, health endpoint, login-session requirement documented |
| Prompt injection via note content / URL titles | tools-disabled invocation, scratch CWD, server-side gates, mandatory human approval |
| localhost web surface | Host/Origin+HX-Request middleware, loopback bind guard, listener seam for tsnet |
| Rewrite stall | per-phase daily-use exit criteria; mobile capture lands in phase 2, not phase 6 |

## Non-goals (v1)

- No vault index/cache (query-function seam reserved)
- No streaming LLM output
- No auth system (Tailscale is the remote-access boundary; hygiene middleware is not auth)
- No direct LLM API integration
- No homelab deployment (Obsidian Sync E2E encryption forecloses it)
- No web UI beyond capture/inbox/triage until v1.1

## Provenance

Interview decisions 2026-07-11; adversarial review by trevelyan (F1 concurrency, F2 filename policy, F6 frontmatter blocker, F7 AGENTS.md carve-out, F8 disposition table, F13 cutover resequencing, F14–F17 LLM hardening, F19–F21 web hygiene, F23 orphan bug) and m (F2 transport/decoder split, F3 config package, F4 stateless verbs, F5 lossless FM, F6 body_patch hole, F7 job model, F8 tty discipline, F9 bug/contract mining, F10 publish package, F12 atomic mechanics, F13 no-index, F14 web v1 pin, F15 dep budget, package layout, phase plan, test strategy).
