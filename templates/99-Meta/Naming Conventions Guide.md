---
type: note
created: 2025-07-27
modified: 2025-07-27
tags: [meta, guide]
status: active
---
# Naming Conventions Guide

## 📁 Folder Names

### Standard Format
- **Use Title Case:** `My Folder Name`
- **No special characters:** Avoid `/`, `&`, `@`, etc.
- **Be descriptive:** Clear purpose, not abbreviated
- **Order when needed:** Use numbers only for sequence: `01-First`, `02-Second`

### Examples
```
✅ Good:
- Learning
- Active Projects  
- Terraform
- Course Templates

❌ Avoid:
- learning (lowercase)
- TF (abbreviated)
- projects_active (underscores)
- my-folder (hyphens in folders)
```

## 📄 File Names

### Daily & Meeting Notes
**Format:** `YYYY-MM-DD Description.md`
```
✅ Examples:
- 2025-07-27 Daily Note.md
- 2025-07-27 Sprint Planning Meeting.md
- 2025-07-27 Jeremy 1-1.md
```

### Evergreen Content
**Format:** `Descriptive Title.md`
```
✅ Examples:
- Terraform Cheatsheet.md
- Course Quality Checklist.md
- Ubuntu Server Fundamentals.md
- DevOps for Devs - Frontend Masters.md
```

### Project Files
**Format:** `Project Name - Specific Topic.md`
```
✅ Examples:
- Storedog Changes.md
- Ava Remote - Setup Guide.md
- Spotify Tools - API Integration.md
```

### Scripts & Technical Docs
**Format:** `Clear Purpose Description.md`
```
✅ Examples:
- Automate Log Enablement.md
- Cool Lab Commands.md
- Puppeteer Scripts - Storedog 2.0.md
- InstruqtLabTool Scripts.md
```

### Session Notes
**Format:** `YYYY-MM-DD-brief-description.md` (kebab-case, all lowercase)
```
✅ Examples:
- 2025-07-29-claude-md-setup.md
- 2026-02-03-claude-code-enhancement-phase1.md
- 2026-01-13-personal-website-mvp-build.md
```
> Sessions use kebab-case to distinguish them from other dated notes (daily notes, journal entries) which use spaces and Title Case.

## 🏷️ Special Cases

### Course Materials
```
Course Name/
├── README.md
├── CHANGELOG.md
├── 01-Module One.md
├── 02-Module Two.md
└── labs/
    ├── 01-setup-lab/
    └── 02-advanced-lab/
```

### Reading Lists & Resources
```
✅ Examples:
- Development Reading List.md
- Homelab and Self-hosting Resources.md
- Music Recommendations.md
- Personal Reading Queue.md
```

### Archive Files
Keep original names but move to Archive folder:
```
Archive/
├── 2024 Cycle 2 Group B Feedback.md
├── Old Project Name.md
└── Completed Course Materials.md
```

## 🔧 Filename Rules

### Always Use:
- **Spaces between words:** `My File Name.md`
- **Title Case:** `Important Document.md`
- **`.md` extension** for all notes
- **ISO dates** for time-sensitive content: `YYYY-MM-DD`

### Never Use:
- **Special characters:** `@`, `#`, `&`, `/`, `\`, `:`
- **All lowercase:** `my file name.md`
- **Underscores in regular files:** `my_file_name.md`
- **Abbreviated words** unless they're standard (like "API", "URL")

### Exceptions:
- **Technical terms:** Keep standard abbreviations like `API`, `URL`, `AWS`
- **Proper nouns:** `GitHub`, `JavaScript`, `Datadog`
- **Course titles:** Use official names like `DevOps for Devs - Frontend Masters`

## 📋 Quick Reference

| Content Type | Format | Example |
|--------------|--------|---------|
| Daily Note | `YYYY-MM-DD Daily Note.md` | `2025-07-27 Daily Note.md` |
| Session | `YYYY-MM-DD-description.md` | `2025-07-29-claude-md-setup.md` |
| Meeting | `YYYY-MM-DD Meeting Name.md` | `2025-07-27 Sprint Planning.md` |
| Learning | `Topic Name.md` | `Terraform Cheatsheet.md` |
| Project | `Project - Detail.md` | `Storedog - API Changes.md` |
| Reference | `Descriptive Name.md` | `Course Quality Checklist.md` |
| Course | `Module Name.md` | `Container Fundamentals.md` |

## 🔄 Migration Strategy

When renaming existing files:
1. **Keep the content the same** - only change the filename
2. **Update any internal links** to the new name
3. **Use Find & Replace** in Obsidian to update links across vault
4. **Do it gradually** - rename 5-10 files per week to avoid disruption

---
*Consistency beats perfection. Pick a standard and stick to it!*
