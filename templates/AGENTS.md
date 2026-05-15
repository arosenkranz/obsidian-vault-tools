# AGENTS.md

This file tells any LLM agent (Claude Code, Codex, Pi, Cursor, etc.) how to work with this Obsidian vault. It is the contract. If you are an agent operating on this vault, read this file first and follow it.

The deeper conventions live in `99-Meta/`:
- `Vault Guide.md` — full structure and rationale
- `Naming Conventions Guide.md` — file/folder naming rules
- `Workflow Guide.md` — the human-side workflow
- `Vault Workflow Plan.md` — the active plan we're executing against

This file is the **short, operational** version. When in doubt, defer to the longer guides.

---

## 1. Vault layers

Three layers, like Karpathy's gist:

| Layer | Path | Who writes |
|---|---|---|
| **Raw inbox** | `00-Inbox/` | Me, via any capture surface |
| **Wiki (PARA)** | `01-Projects/`, `02-Areas/`, `03-Resources/`, `04-Archive/` | Me, with LLM as filing/linting assistant |
| **Schema** | `99-Meta/`, this file | Me + LLM co-evolve |

`Daily Notes/`, `Sessions/`, `Confluence/` exist but are not part of the active workflow. Don't file new notes there.

---

## 2. PARA semantics — where does a note belong?

When deciding a target folder, apply this decision tree **in order**:

1. **Is there a goal with a finish line and active work happening?** → `01-Projects/`
   - Examples: `alexrosenkranz.dev`, `Gargantuan FM`, `HiTech Stereo Site`, `Home Improvement`
   - If unsure whether a project is still active, ask. Don't auto-archive.

2. **Is this an ongoing responsibility with no end date that I maintain?** → `02-Areas/`
   - `02-Areas/Work/` — anything Datadog-related (work projects, meeting notes, internal docs)
   - `02-Areas/Learning/` — courses, cheatsheets, skill-building (Terraform, Neovim, etc.)
   - `02-Areas/Personal/` — travel, personal life, health

3. **Is this reference material I look up but don't maintain?** → `03-Resources/`
   - Recipes, music recommendations, homelab notes, reading lists, MOCs that index resources.

4. **Is this completed, abandoned, or no longer relevant?** → `04-Archive/`
   - Default for stale projects. Never delete; archive instead unless the user explicitly asks.

5. **Can't decide?** → leave in `00-Inbox/` and flag the ambiguity in your proposal's `rationale` field.

### Heuristics for the hard cases
- A "thing I'm currently learning" with no specific deadline → `02-Areas/Learning/`, not Projects.
- A reference doc *for* a project (e.g. an API cheatsheet used by `alexrosenkranz.dev`) → file under the project folder, not Resources, if it's project-specific.
- Meeting notes always go to `02-Areas/Work/` unless the meeting is specifically about a personal project.
- Clipped articles (`type: clipping` in frontmatter) → most go to `03-Resources/`. If the article is being read *for* a specific project, file under the project.

---

## 3. Naming conventions (operational summary)

Full rules in `99-Meta/Naming Conventions Guide.md`. The short version:

| Type | Format | Example |
|---|---|---|
| Inbox capture | `YYYY-MM-DD HHMM Title Case.md` | `2026-05-14 0830 Idea For Triage UI.md` |
| Daily/meeting | `YYYY-MM-DD Description.md` | `2026-05-14 Sprint Planning.md` |
| Evergreen | `Title Case.md` | `Terraform Cheatsheet.md` |
| Project doc | `Project - Detail.md` | `Storedog - API Changes.md` |
| Session log | `YYYY-MM-DD-kebab-case.md` | `2026-05-14-vault-cli-extension.md` |

Rules:
- Title Case, spaces between words, `.md` extension.
- No `@`, `#`, `&`, `/`, `\`, `:`, `_` in filenames.
- Standard tech abbreviations OK: `API`, `URL`, `AWS`, `CLI`.
- Sessions are the only exception that uses kebab-case lowercase.

When proposing a rename: preserve user-meaningful proper nouns exactly (`Datadog`, `GitHub`, `Obsidian`).

---

## 4. Frontmatter contract

Every note **should** have YAML frontmatter. Inbox captures get a minimal version; filed notes get the full version.

### Minimum (inbox)
```yaml
---
type: inbox
created: 2026-05-14
source: cli            # cli | web | llm | manual
---
```

### Full (filed notes)
```yaml
---
type: note             # see type vocabulary below
created: 2026-05-14
modified: 2026-05-14
tags: [topic1, topic2]
status: active         # see status vocabulary below
---
```

**`type` vocabulary** (use the closest match; don't invent new ones without flagging):
- `note` — generic evergreen note
- `project` — a project note in `01-Projects/`
- `meeting` — meeting notes
- `learning` — course/cheatsheet/skill-building
- `work` — work-context note that isn't a meeting (Datadog scripts, internal docs)
- `journal` — dated personal/reflective entry
- `reference` — reference material in `03-Resources/`
- `clipping` — web-clipped article (set `source: web`, `url: ...`)
- `moc` — Map of Content / index page
- `guide` — long-form how-to
- `plan` — plan / spec / design doc
- `dashboard` — Dataview-driven overview page
- `inbox` — unprocessed inbox capture (only valid in `00-Inbox/`)

**`status` vocabulary**:
- `active` — currently being worked or maintained
- `in-progress` — project is actively being executed (more specific than `active`)
- `backlog` — queued, not started
- `research` — still gathering info, not committed to execution
- `drafting` — writing/editing in flight (typically blog posts)
- `published` — shipped/published (typically blog posts)
- `paused` — explicitly on hold
- `done` — finished, not yet archived
- `reference` — reference material, no active work expected

Optional fields:
- `source: web` + `url: https://...` for clipped articles
- `project: [[Project Name]]` if the note belongs to a project
- `area: Work | Learning | Personal` for area-scoped notes

When filing a note from inbox → wiki, you must:
1. Upgrade `type` from `inbox` to the appropriate type.
2. Set `modified` to today.
3. Add `tags` (see §5).
4. Set `status` (default `active` for projects/areas, `reference` for resources).

---

## 5. Tag taxonomy

Keep it short. These are the only tags worth using:

| Tag | Meaning |
|---|---|
| `#meeting` | Meeting notes |
| `#idea` | Raw idea, not yet developed |
| `#todo` | Has action items |
| `#learning` | Educational content |
| `#resource` | Reference material |
| `#active` | Current focus |
| `#review` | Needs follow-up |
| `#meta` | About the vault itself |
| `#guide` | Long-form how-to |
| `#plan` | Plan/spec document |

Topical tags (e.g. `#terraform`, `#datadog`, `#music`) are fine when they aid search, but don't invent new ones speculatively. If a topic deserves a tag, it probably deserves an MOC instead — see §6.

---

## 6. Linking and MOCs

- Use `[[wikilinks]]` to connect related notes. Prefer linking over tagging when the relationship is between two specific notes.
- MOCs (Maps of Content) live in `03-Resources/` prefixed `MOC` (e.g. `MOC Music.md`, `MOC Programming & DevOps.md`). They are hand-curated indexes, not auto-generated.
- When filing a note, look for an obvious MOC to link from. Propose the link in your triage output but **do not** edit MOCs without approval.
- Don't create new MOCs as part of a triage. That's a separate, deliberate action via `ov mocs new`.

---

## 7. Triage proposal schema

When proposing how to file a note from `00-Inbox/`, return JSON matching this shape:

```json
{
  "from": "00-Inbox/2026-05-14 0830 thought.md",
  "to": "02-Areas/Learning/Local LLM Notes.md",
  "new_title": "Local LLM Notes",
  "frontmatter_patch": {
    "type": "learning",
    "tags": ["learning", "llm"],
    "status": "active"
  },
  "body_patch": null,
  "links_to_add": ["[[MOC Local LLM & Homelab]]"],
  "rationale": "Note describes ongoing LLM experimentation; fits Areas/Learning. Existing MOC indexes this domain.",
  "confidence": "high"
}
```

Field rules:
- `to` is the **full target path** including the new filename. If the target already exists, set `to` to the existing path and treat `body_patch` as content to **append**, not replace.
- `body_patch` is `null` if the body should stay as-is. Otherwise it is the full new body (post-frontmatter). Never silently rewrite content — if you want to clean up the body, propose it explicitly.
- `links_to_add` are wikilinks the agent recommends adding to the body (typically at the bottom under a `## Related` section). The user decides whether to apply them.
- `confidence`: `high` (clear PARA fit, existing MOC, unambiguous), `medium` (defensible but other folders possible), `low` (genuinely unsure — explain in rationale).
- Always include `rationale`. One or two sentences.

---

## 8. Hard rules — never do these without explicit user approval

- ❌ Move or rename files in `01-Projects/`, `02-Areas/`, `03-Resources/` (only inbox → wiki moves are auto-approvable, and only when the user has approved that specific note).
- ❌ Edit MOCs.
- ❌ Delete files. Archive instead.
- ❌ Rewrite note bodies wholesale. Propose, don't apply.
- ❌ Create new top-level folders.
- ❌ Touch `.obsidian/`, `Sessions/`, `Confluence/`, or anything in `04-Archive/`.
- ❌ Add new tags outside the taxonomy in §5 without flagging them as proposals.

When uncertain, propose and ask. The user's default mode is **suggest-only**.

---

## 9. Tools available to agents

The user has a CLI at `~/.local/bin/ov` (source: `99-Meta/vault.sh`):

```
ov inbox           # list inbox with ages
ov triage          # interactive (manual) triage
ov new             # new note from template
ov review          # weekly review summary
ov stale [days]    # find untouched notes
ov mocs <sub>      # MOC management
ov capture ...     # quick capture (see §10)
```

Prefer using `ov` over raw `mv`/`mkdir`/`cat` so behaviors stay consistent.

---

## 10. Capture conventions (`ov capture`)

`ov capture` is the canonical entry point for inbox dumps. Agents calling it should pass content via stdin and set `--source llm`:

```bash
ov capture --title "Insight on triage UX" --tags "idea,llm" --source llm <<'EOF'
<markdown body of the takeaway>
EOF
```

If `--title` is omitted, `ov capture` derives one from the first non-empty line. The agent should still pass a title when it has a clear one — the heuristic is a fallback, not the goal.

---

## 11. When to update this file

Update `AGENTS.md` when:
- The PARA semantics shift (e.g. a new top-level folder).
- The frontmatter contract changes.
- A new agent tool is added that other agents should know about.
- A hard rule from §8 changes.

Do not update for one-off preferences or temporary experiments — those belong in `99-Meta/Vault Workflow Plan.md` until they stabilize.
