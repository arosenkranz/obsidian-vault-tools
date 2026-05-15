---
type: guide
created: 2026-05-09
tags: [meta, guide, para]
---

# 🧠 My Second Brain — Vault Guide

> A reference for the restructured Obsidian vault — how it works, why it's organized this way, and how to keep it that way.

---

## Table of Contents

- [[#What Changed]]
- [[#The PARA System]]
- [[#Full Folder Structure]]
- [[#Your Routine]]
- [[#Capturing Notes]]
- [[#Templates]]
- [[#vault CLI]]
- [[#Tags]]
- [[#Golden Rules]]
- [[#FAQ]]

---

## What Changed

The vault was reorganized from a scattered mix of folders into a clean **PARA system**.

| Before | After |
|--------|-------|
| Flat folders with no clear system | 6 top-level folders with clear intent |
| Separate `Reference` and `References` folders | Merged into a single `03-Resources/` |
| `Datadog/` at root level, mixed with personal | Lives inside `02-Areas/Work/` |
| 41 session logs in `Sessions/`, unused | Archived in `04-Archive/` |
| `Ideas and Journal` mixed together | Inbox is a staging area, not a permanent home |
| Learning, Reading, Resources as separate silos | Learning under Areas, Resources is reference-only |
| No automation, 100% manual | `vault` CLI for triage, review, and new notes |
| Daily notes going to wrong folder | Dedicated `Daily Notes/` folder, auto-configured |

---

## The PARA System

PARA is a 4-bucket system. Every note belongs to exactly one category. The number prefixes keep folders sorted in priority order.

### 01-Projects — *Things with a goal and a deadline*

Active work that will eventually be **done**. If there's no finish line, it's not a project.

**Your projects:** `alexrosenkranz.dev`, `Gargantuan FM`, `HiTech Stereo Site`, `Home Improvement`

### 02-Areas — *Ongoing responsibilities with no end date*

Things you **maintain** over time. No finish line — just a standard to keep up.

**Your areas:** `Work` (Datadog), `Learning`, `Personal`

### 03-Resources — *Reference material on topics you care about*

Not tied to any project or responsibility. You look things up here, you don't maintain them.

**Your resources:** Recipes, Music, LLM Homelab Stack, Self-host & Homelab, Reading lists

### 04-Archive — *Completed or inactive items*

Everything from the other three categories that's done or no longer relevant. Out of sight, not deleted.

**Your archive:** 41 Claude session logs, old work feedback

> **The key insight:** A project becomes an area when it loses its deadline. An area becomes a resource when you're no longer responsible for it. A resource becomes an archive when you no longer care about it.

---

## Full Folder Structure

```
main-vault/
├── 00-Inbox/                        ← Everything starts here. Unprocessed.
│
├── 01-Projects/                     ← Active work with a goal & end date
│   ├── alexrosenkranz.dev/
│   │   └── posts/
│   ├── Gargantuan FM.md
│   ├── HiTech Stereo Site.md
│   ├── Home Improvement/
│   ├── hockeybot-pickem-command/
│   ├── Home Assistant Projects.md
│   ├── Ava Remote.md
│   └── Project Ideas.md
│
├── 02-Areas/                        ← Ongoing responsibilities, no end date
│   ├── Work/                        ← Previously "Datadog/" at root
│   │   ├── Active/
│   │   ├── Documentation/
│   │   ├── Scripts/
│   │   ├── Knowledge/
│   │   ├── General/
│   │   ├── Archive/
│   │   └── Brag Document.md
│   ├── Learning/                    ← Courses, cheatsheets, skill notes
│   │   ├── Terraform/
│   │   ├── DevOps for Devs.md
│   │   ├── Neovim.md
│   │   └── Ubuntu Server Fundamentals.md
│   └── Personal/                    ← Travel, personal life notes
│       ├── Greece Itinerary.md
│       └── Montreal Trip.md
│
├── 03-Resources/                    ← Reference material you look up, not maintain
│   ├── Local LLM Homelab Stack.md
│   ├── Recipes.md
│   ├── Drink Recipes.md
│   ├── Music.md
│   └── Self-host and Homelab.md
│
├── 04-Archive/                      ← Done or inactive. 41 session logs live here.
│
├── Daily Notes/                     ← One note per day (Obsidian auto-creates)
│
└── 99-Meta/                         ← Templates, this guide, vault.sh script
    ├── Vault Guide.md               ← You are here
    ├── vault-guide.html
    ├── vault.sh
    ├── Daily Note Template.md
    ├── Project Template.md
    ├── Meeting Note Template.md
    └── Learning Note Template.md
```

---

## Your Routine

### Daily (~3 minutes)
1. Open today's note — `Cmd+Shift+D` in Obsidian
2. Write one sentence for **Today's Focus**
3. Dump freely into **Inbox Dump** throughout the day
4. End of day: glance at **Tomorrow** section

### Weekly — Friday (~15 minutes)
1. Run `ov review` in terminal
2. Run `ov triage` to clear the inbox
3. Check active projects — anything stuck or done?
4. Move finished projects to `04-Archive/`

### Monthly (~20 minutes)
1. Run `ov stale` — prune notes no one touches
2. Any Projects completed? Move to Archive
3. Any Resources you never read? Delete them
4. Update your **Brag Document** in `02-Areas/Work/`

---

## Capturing Notes

**Most important rule:** When in doubt, put it in `00-Inbox`. Never let "where does this go?" stop you from capturing something.

### Decision Tree

```
Is this tied to a specific goal with an end date?
  → Yes  →  01-Projects/
  → No   →  Is it an ongoing responsibility?
               → Yes  →  02-Areas/ (Work, Learning, or Personal)
               → No   →  Is it a topic you want to reference later?
                            → Yes      →  03-Resources/
                            → Not sure →  00-Inbox/  ← always fine
```

> ⚠️ **Inbox is not a permanent home.** Notes in `00-Inbox/` are unprocessed. Run `vault triage` weekly to move them out. The `vault inbox` command flags anything older than 7 days.

---

## Templates

Templates live in `99-Meta/`. In Obsidian use **Cmd+P → "Insert template"** to apply one.

### Daily Note Template
Auto-created when you open Today's Note (`Cmd+Shift+D`).

```markdown
# Friday, May 9 2026

## 🎯 Today's Focus
> One sentence: what does a successful day look like?

## 📥 Inbox Dump
> Capture anything quickly — process later

## 🗓 Meetings
### Meeting name
- With:
- Notes:
- Action items:

## 💡 Ideas & Thoughts
## 📚 Learned Today
## ⏭ Tomorrow
```

### Project Note Template
Use for anything in `01-Projects/`. Create via `vault new` or from template.

```markdown
## 🎯 Goal
> What does "done" look like?

## 📋 Tasks
- [ ] ...

## 📝 Notes & Updates
### YYYY-MM-DD
-

## ✅ Done
```

### Other Templates
- `Meeting Note Template.md` — structured meeting notes
- `Learning Note Template.md` — courses and study notes
- `Session Note Template.md` — Claude coding session logs

---

## ov CLI

The `ov` command is available in any terminal window (short for "Obsidian Vault"). It's the main way to automate your workflow without opening Obsidian.

### `ov inbox`
List every note in `00-Inbox/` with its age. Items older than 7 days are flagged.

```
▸ Inbox contents
  ⚠  2025-11-16 Montreal Recap  (90d old)
  •  Cool Articles  (5d old)
```

### `ov triage`
Interactively process your inbox. Steps through each note and asks where it belongs.

```
📄 Cool Articles
   [1] 01-Projects
   [2] 02-Areas/Work
   [3] 02-Areas/Learning
   [4] 02-Areas/Personal
   [5] 03-Resources
   [6] 04-Archive
   [s] Skip
   [d] Delete
```

### `ov new`
Create a new note from template. Prompts for type and title, creates the file in the right folder, opens it in Obsidian.

### `ov review`
Weekly review summary — notes modified this week, inbox count, active projects, **and MOCs**.

### `ov stale [days]`
Find notes outside Archive not touched in 90+ days (default). Good for monthly pruning.

```bash
ov stale 60   # find notes untouched for 60+ days
```

### `ov mocs`
Manage your Maps of Content.

| Command | What it does |
|---|---|
| `ov mocs list` | List all MOCs with descriptions |
| `ov mocs new` | Create a new MOC with template |
| `ov mocs orphan` | Find notes not linked from any MOC |
| `ov mocs add` | Add a note to a MOC interactively |
| `ov mocs update` | Update all MOCs in a directory |

**Example:**
```bash
ov mocs orphan   # find scattered notes
ov mocs add      # link a note to an MOC
```

**Tip:** Run `ov mocs orphan` during weekly review to discover notes that should be linked from a MOC.

> 🔧 **Script location:** `~/Documents/main-vault/99-Meta/vault.sh`
> Symlinked to `~/.local/bin/vault`. Make sure `~/.local/bin` is in your `$PATH` (already added to `~/.zshrc`).

---

## MOCs (Maps of Content)

**MOC = Map of Content** — an index note that links to all related notes on a topic.

### Why MOCs?

| Problem with folders | How MOCs fix it |
|---|---|
| Decision paralysis — "Where does this go?" | Put it anywhere, link from MOC |
| Deep nesting (can't find notes) | One MOC to rule them all |
| Notes can't be in two places | A note links to multiple MOCs |
| Forgotten notes | MOC is your "table of contents" |

### Your MOCs

| MOC | What it covers |
|---|---|
| [[MOC Local LLM & Homelab]] | Personal AI stack, services, models |
| [[MOC Programming & DevOps]] | Dev tools, Neovim, Terraform, learning |
| [[MOC Datadog]] | Work notes, courses, scripts, automation |
| [[MOC Music]] | Album lists, articles, reading |

### When to Add More MOCs

✅ **Good candidates:**
- Topics with 5+ scattered notes (AI, programming, homelab)
- Knowledge you want to explore over time
- Things spanning multiple folders

❌ **Skip MOCs for:**
- Single notes or 1-2 related items
- Truly separate topics (Work vs Personal)
- One-off projects

---

## Tags

Tags complement folders for cross-cutting concerns — things you'd want to find *across* multiple folders at once.

| Tag | Use it for |
|-----|-----------|
| `#daily` | Auto-added by daily note template |
| `#project` | Active project notes |
| `#idea` | Half-formed thoughts to develop |
| `#meeting` | Meeting notes anywhere in vault |
| `#reference` | Notes you look up, not maintain |
| `#learning` | Study notes, courses, cheatsheets |
| `#todo` | Actionable tasks across any note |
| `#homelab` | Self-hosting, local LLM, infra |
| `#work` | Datadog / work content |

> 💡 **Use tags sparingly.** A note doesn't need tags if it's already in the right folder. Only tag when you'd want to find something across multiple folders at once.

---

## Golden Rules

1. **Inbox first, organize later.** Never let "where does this go?" block you. Put everything in Inbox and triage on Friday.

2. **Archive, don't delete.** When a project finishes or a note gets stale, move it to `04-Archive/`. You'll be glad it's there someday.

3. **One project = one folder or note.** If a project has more than ~5 files, give it its own subfolder in `01-Projects/`.

4. **The daily note is your anchor.** Open it every morning. Everything flows through it — meetings, ideas, tasks, learnings.

5. **Run `ov review` every Friday.** 15 minutes keeps the system clean indefinitely. Skip it for a month and the inbox fills up fast.

---

## FAQ

**Where do meeting notes go?**
Into your daily note if it's a quick sync. If it's a substantial meeting with its own action items, create a separate note in `02-Areas/Work/` using the Meeting template, then link it from your daily note.

**What if something fits in both Projects and Areas?**
Ask: *does it have a finish line?* If yes → Projects. If it's ongoing with no clear done state → Areas. When in doubt, put it in Projects — it's easy to downgrade to an Area later.

**Should I use subfolders inside Resources?**
Only if you have 10+ notes on a topic. A flat list is easier to scan than deep nesting. Use tags to group related resources instead.

**My vault looks like a mess again after a few weeks. Help?**
Run `ov triage` to clear the inbox. Then run `ov stale 60` to see what hasn't been touched. Trim ruthlessly. Archiving is not failing.

**Can I add more folders to 02-Areas?**
Yes. Good candidates as your life evolves: `Health/`, `Finance/`, `Side Projects/`. Keep the total under ~5 areas or it gets overwhelming.

**How do I create a new MOC?**
```bash
ov mocs new
```
Then give it a title like `"MOC Travel"` or `"MOC Home Improvement"`. The template will be created in `03-Resources/`.

**What does `ov mocs orphan` do?**
It finds notes in `Resources/` and `Areas/` that aren't linked from any MOC. Run this during weekly review to discover scattered notes you should link to a MOC.

**How do I open the HTML version of this guide?**
```bash
open ~/Documents/main-vault/99-Meta/vault-guide.html
```

---

*Last updated: 2026-05-09 · `99-Meta/Vault Guide.md`*
