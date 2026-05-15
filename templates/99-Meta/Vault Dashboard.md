---
type: dashboard
created: 2026-05-14
modified: 2026-05-14
tags: [meta, dashboard]
status: reference
---
# 🧭 Vault Dashboard

> Live view. Requires the **Dataview** plugin. See also: [[By Status]], [[By Age]].

## 📥 Inbox

```dataview
TABLE WITHOUT ID
  file.link AS "Note",
  dateformat(file.ctime, "yyyy-MM-dd") AS "Created",
  (date(today) - file.ctime).days AS "Age (d)",
  source AS "Source"
FROM "00-Inbox"
SORT file.ctime ASC
```

## 📊 Vault by folder

```dataview
TABLE WITHOUT ID
  folder AS "Folder",
  rows.file.link.length AS "Notes"
FROM "01-Projects" OR "02-Areas" OR "03-Resources" OR "04-Archive" OR "00-Inbox"
FLATTEN choice(
  startswith(file.folder, "02-Areas/Work"), "02-Areas/Work",
  choice(
    startswith(file.folder, "02-Areas/Learning"), "02-Areas/Learning",
    choice(
      startswith(file.folder, "02-Areas/Personal"), "02-Areas/Personal",
      choice(
        startswith(file.folder, "01-Projects"), "01-Projects",
        choice(
          startswith(file.folder, "03-Resources"), "03-Resources",
          choice(
            startswith(file.folder, "04-Archive"), "04-Archive",
            "00-Inbox"
          )
        )
      )
    )
  )
) AS folder
GROUP BY folder
SORT folder ASC
```

## 🕒 Recently modified (last 14 days)

```dataview
TABLE WITHOUT ID
  file.link AS "Note",
  dateformat(file.mtime, "yyyy-MM-dd") AS "Modified",
  file.folder AS "Folder"
FROM "01-Projects" OR "02-Areas" OR "03-Resources" OR "00-Inbox"
WHERE file.mtime >= date(today) - dur(14 days)
SORT file.mtime DESC
LIMIT 20
```

## ⚠️ Stale active notes (>60 days untouched)

These are notes marked `status: active` (or active project states) that haven't been edited in over 60 days. Either pick them back up or move them to Archive.

```dataview
TABLE WITHOUT ID
  file.link AS "Note",
  status AS "Status",
  dateformat(file.mtime, "yyyy-MM-dd") AS "Last Modified",
  (date(today) - file.mtime).days AS "Age (d)",
  file.folder AS "Folder"
FROM "01-Projects" OR "02-Areas"
WHERE (status = "active" OR status = "in-progress" OR status = "drafting")
  AND file.mtime < date(today) - dur(60 days)
SORT file.mtime ASC
```

## 🧹 Frontmatter health

Notes missing required frontmatter fields. Fix these as you encounter them.

### Missing `type`

```dataview
LIST file.link
FROM "01-Projects" OR "02-Areas" OR "03-Resources"
WHERE !type
SORT file.path ASC
LIMIT 20
```

### Missing `status`

```dataview
LIST file.link
FROM "01-Projects" OR "02-Areas"
WHERE !status
SORT file.path ASC
LIMIT 20
```

## 🎯 Active projects at a glance

```dataview
TABLE WITHOUT ID
  file.link AS "Project",
  status AS "Status",
  dateformat(file.mtime, "yyyy-MM-dd") AS "Last Touched"
FROM "01-Projects"
WHERE type = "project"
SORT file.mtime DESC
```
