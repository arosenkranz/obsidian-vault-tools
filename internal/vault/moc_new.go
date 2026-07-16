// internal/vault/moc_new.go
package vault

import (
	"fmt"
	"time"
)

// mocSkeletonTemplate ports v1 moc_new's heredoc skeleton verbatim
// (Overview/Key Notes/Resources/Related MOCs + created-date stamp,
// behavior inventory row #64).
const mocSkeletonTemplate = `# MOC %s

> Map of Content for %s - links to all related notes and resources

## Overview

## Key Notes

## Resources

## Related MOCs

---
*Created: %s*
`

// NewMOCSkeleton renders the embedded MOC skeleton for title stamped
// with now's date. Pure content generation — the caller (cmd/ov's
// runMocsNew) strips CR/LF from title before calling this and resolves
// the target path via Slugify + ContainPath (row #153's traversal-safety
// half) before writing via WriteNoteAtomic, matching this package's
// "mechanical ops live in vault" split (design spec's internal/moc doc
// comment).
func NewMOCSkeleton(title string, now time.Time) string {
	return fmt.Sprintf(mocSkeletonTemplate, title, title, now.Format("2006-01-02"))
}
