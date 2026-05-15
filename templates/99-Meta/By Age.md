---
type: dashboard
created: 2026-05-14
modified: 2026-05-14
tags: [meta, dashboard]
status: reference
---
# ⏱️ Notes by Age

> Chronological views. Requires Dataview. See also: [[Vault Dashboard]], [[By Status]].

## 🔥 Recently modified (top 30)

What's actually getting work right now.

```dataview
TABLE WITHOUT ID
  file.link AS "Note",
  dateformat(file.mtime, "yyyy-MM-dd") AS "Modified",
  (date(today) - file.mtime).days AS "Age (d)",
  status AS "Status",
  file.folder AS "Folder"
FROM "01-Projects" OR "02-Areas" OR "03-Resources" OR "00-Inbox"
SORT file.mtime DESC
LIMIT 30
```

## 🆕 Recently created (top 30)

```dataview
TABLE WITHOUT ID
  file.link AS "Note",
  dateformat(file.ctime, "yyyy-MM-dd") AS "Created",
  type AS "Type",
  file.folder AS "Folder"
FROM "01-Projects" OR "02-Areas" OR "03-Resources" OR "00-Inbox"
SORT file.ctime DESC
LIMIT 30
```

## 🪦 Aging active notes (sorted oldest-first)

Active in name, idle in fact. The top of this list is your archive-or-revive shortlist.

```dataview
TABLE WITHOUT ID
  file.link AS "Note",
  (date(today) - file.mtime).days AS "Days idle",
  status AS "Status",
  file.folder AS "Folder"
FROM "01-Projects" OR "02-Areas"
WHERE (status = "active" OR status = "in-progress" OR status = "drafting" OR status = "backlog")
  AND file.mtime < date(today) - dur(30 days)
SORT file.mtime ASC
```

## 🐢 Notes untouched for 90+ days (excluding archive)

Broader sweep — surfaces notes that are slipping into archive territory.

```dataview
TABLE WITHOUT ID
  file.link AS "Note",
  (date(today) - file.mtime).days AS "Days idle",
  status AS "Status",
  file.folder AS "Folder"
FROM "01-Projects" OR "02-Areas" OR "03-Resources"
WHERE file.mtime < date(today) - dur(90 days)
SORT file.mtime ASC
LIMIT 30
```

## 📥 Inbox by age

```dataview
TABLE WITHOUT ID
  file.link AS "Note",
  dateformat(file.ctime, "yyyy-MM-dd") AS "Created",
  (date(today) - file.ctime).days AS "Age (d)",
  source AS "Source"
FROM "00-Inbox"
SORT file.ctime ASC
```

## ❓ Notes with no `modified` field

Data quality — the queries above use `file.mtime` so they still rank, but having an explicit `modified` field is cheap and useful.

```dataview
LIST file.link
FROM "01-Projects" OR "02-Areas" OR "03-Resources"
WHERE !modified
SORT file.path ASC
LIMIT 30
```
