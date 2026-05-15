---
type: dashboard
created: 2026-05-14
modified: 2026-05-14
tags: [meta, dashboard]
status: reference
---
# 📋 Notes by Status

> Live view of every note grouped by `status` frontmatter. Requires Dataview.
> Empty status column = note is missing the `status` field — fix in [[Vault Dashboard]] frontmatter health.

## All notes grouped by status

```dataview
TABLE WITHOUT ID
  rows.file.link AS "Notes",
  rows.file.link.length AS "Count"
FROM "01-Projects" OR "02-Areas" OR "03-Resources"
GROUP BY status
SORT length(rows) DESC
```

---

## 🟢 Active

```dataview
TABLE WITHOUT ID
  file.link AS "Note",
  type AS "Type",
  dateformat(file.mtime, "yyyy-MM-dd") AS "Modified",
  file.folder AS "Folder"
FROM "01-Projects" OR "02-Areas" OR "03-Resources"
WHERE status = "active"
SORT file.mtime DESC
```

## 🚧 In progress / drafting / research / backlog

Project-state values. Roll up so projects can use whichever feels right.

```dataview
TABLE WITHOUT ID
  file.link AS "Note",
  status AS "Status",
  type AS "Type",
  dateformat(file.mtime, "yyyy-MM-dd") AS "Modified"
FROM "01-Projects" OR "02-Areas"
WHERE status = "in-progress" OR status = "drafting" OR status = "research" OR status = "backlog"
SORT status ASC, file.mtime DESC
```

## ✅ Published / done

```dataview
TABLE WITHOUT ID
  file.link AS "Note",
  status AS "Status",
  dateformat(file.mtime, "yyyy-MM-dd") AS "Modified",
  file.folder AS "Folder"
FROM "01-Projects" OR "02-Areas" OR "03-Resources"
WHERE status = "published" OR status = "done"
SORT file.mtime DESC
```

## ⏸️ Paused

```dataview
TABLE WITHOUT ID
  file.link AS "Note",
  type AS "Type",
  dateformat(file.mtime, "yyyy-MM-dd") AS "Modified",
  file.folder AS "Folder"
FROM "01-Projects" OR "02-Areas"
WHERE status = "paused"
SORT file.mtime DESC
```

## 📚 Reference

```dataview
TABLE WITHOUT ID
  file.link AS "Note",
  type AS "Type",
  file.folder AS "Folder"
FROM "03-Resources" OR "02-Areas"
WHERE status = "reference"
SORT file.name ASC
```

## ❓ No status set

```dataview
TABLE WITHOUT ID
  file.link AS "Note",
  type AS "Type",
  file.folder AS "Folder"
FROM "01-Projects" OR "02-Areas" OR "03-Resources"
WHERE !status
SORT file.path ASC
```
