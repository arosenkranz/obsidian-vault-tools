// internal/vault/moc_new_test.go
package vault

import (
	"strings"
	"testing"
	"time"
)

// CONTRACT(#64): skeleton has Overview/Key Notes/Resources/Related MOCs
// sections plus a created-date stamp, title interpolated into the
// heading and blockquote.
func TestNewMOCSkeleton(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	got := NewMOCSkeleton("Travel", now)
	for _, want := range []string{
		"# MOC Travel\n",
		"> Map of Content for Travel - links to all related notes and resources\n",
		"## Overview\n",
		"## Key Notes\n",
		"## Resources\n",
		"## Related MOCs\n",
		"*Created: 2026-07-15*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("NewMOCSkeleton missing %q in:\n%s", want, got)
		}
	}
}

// A title containing only structural markdown characters still renders —
// NewMOCSkeleton itself does no sanitization (the caller, cmd/ov's
// runMocsNew, strips CR/LF and slugifies the filename separately, row
// #153); this is pure content generation.
func TestNewMOCSkeletonInterpolatesRawTitle(t *testing.T) {
	got := NewMOCSkeleton("Home & Garden", time.Now())
	if !strings.Contains(got, "# MOC Home & Garden") {
		t.Errorf("expected the raw title in the heading, got:\n%s", got)
	}
}
