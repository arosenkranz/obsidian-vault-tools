// internal/moc/validate_test.go
package moc

import (
	"errors"
	"strings"
	"testing"
)

// Ported 1:1 from tests/test_moc_cleanup.py (design spec §Testing
// strategy tier 1: "port existing pytest suites 1:1 first").

const validateOriginal = "---\ntype: moc\n---\n" +
	"# MOC Test\n\n" +
	"## Resources\n\n" +
	"- [[Foo Note]] — https://example.com/foo\n" +
	"- [[Bar Note]] — https://example.com/bar\n"

// CONTRACT(#107,#109): reordering that keeps every link/wikilink is
// accepted (tests/test_moc_cleanup.py:48-58).
func TestValidateAcceptsReorderingThatKeepsAllLinks(t *testing.T) {
	reordered := "---\ntype: moc\n---\n" +
		"# MOC Test\n\n" +
		"## Resources\n\n" +
		"### Example.com\n" +
		"- [[Bar Note]] — https://example.com/bar\n" +
		"- [[Foo Note]] — https://example.com/foo\n"
	if err := Validate(validateOriginal, reordered); err != nil {
		t.Errorf("unexpected rejection: %v", err)
	}
}

// CONTRACT(#109): deleting an entire entry drops its URL — rejected
// (tests/test_moc_cleanup.py:61-76).
func TestValidateRejectsDroppedEntryURLAndWikilink(t *testing.T) {
	missingEntry := "---\ntype: moc\n---\n" +
		"# MOC Test\n\n" +
		"## Resources\n\n" +
		"- [[Foo Note]] — https://example.com/foo\n"
	err := Validate(validateOriginal, missingEntry)
	if !errors.Is(err, ErrURLDropped) {
		t.Fatalf("err = %v, want ErrURLDropped", err)
	}
	if !strings.Contains(err.Error(), "https://example.com/bar") {
		t.Errorf("err = %v, want it to name the dropped URL", err)
	}
}

// CONTRACT(#108): any frontmatter change rejects the whole proposal
// (tests/test_moc_cleanup.py:79-91).
func TestValidateRejectsFrontmatterChange(t *testing.T) {
	changedFM := "---\ntype: moc\nextra: field\n---\n" +
		"# MOC Test\n\n" +
		"## Resources\n\n" +
		"- [[Foo Note]] — https://example.com/foo\n" +
		"- [[Bar Note]] — https://example.com/bar\n"
	err := Validate(validateOriginal, changedFM)
	if !errors.Is(err, ErrFrontmatterChanged) {
		t.Fatalf("err = %v, want ErrFrontmatterChanged", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "frontmatter") {
		t.Errorf("err = %v, want it to mention frontmatter", err)
	}
}

// CONTRACT(#107): a URL-anchored wikilink may be retitled freely — the
// whole point of the garbled-title-fix feature (tests/test_moc_cleanup.
// py:94-106).
func TestValidateAcceptsRetitlingAURLAnchoredWikilink(t *testing.T) {
	retitled := "---\ntype: moc\n---\n" +
		"# MOC Test\n\n" +
		"## Resources\n\n" +
		"- [[Foo Article]] — https://example.com/foo\n" +
		"- [[Bar Note]] — https://example.com/bar\n"
	if err := Validate(validateOriginal, retitled); err != nil {
		t.Errorf("unexpected rejection: %v", err)
	}
}

// CONTRACT(#107): a BARE wikilink (no URL on its line) must survive
// verbatim — renaming it is rejected (tests/test_moc_cleanup.py:109-129).
func TestValidateRejectsRenamedBareWikilinkWithNoURL(t *testing.T) {
	original := "---\ntype: moc\n---\n# MOC Test\n\n## Resources\n\n- [[Neovim]] — my editor setup\n"
	renamed := "---\ntype: moc\n---\n# MOC Test\n\n## Resources\n\n- [[Neovim Notes]] — my editor setup\n"
	err := Validate(original, renamed)
	if !errors.Is(err, ErrBareWikilinkDropped) {
		t.Fatalf("err = %v, want ErrBareWikilinkDropped", err)
	}
	if !strings.Contains(err.Error(), "Neovim") {
		t.Errorf("err = %v, want it to name the dropped wikilink", err)
	}
}

// CONTRACT(#109): title changed AND its URL vanished entirely — the URL
// check catches cases the wikilink check might miss (tests/
// test_moc_cleanup.py:132-146).
func TestValidateRejectsDroppedURL(t *testing.T) {
	missingURL := "---\ntype: moc\n---\n" +
		"# MOC Test\n\n" +
		"## Resources\n\n" +
		"- [[Foo Note]] — https://example.com/foo\n" +
		"- [[Bar Note]] — some text with no url\n"
	err := Validate(validateOriginal, missingURL)
	if !errors.Is(err, ErrURLDropped) {
		t.Fatalf("err = %v, want ErrURLDropped", err)
	}
}

// Additional table cases beyond the ported pytest suite.

// CONTRACT(#108): identical content (no proposal change) validates.
func TestValidateAcceptsIdenticalContent(t *testing.T) {
	if err := Validate(validateOriginal, validateOriginal); err != nil {
		t.Errorf("unexpected rejection of identical content: %v", err)
	}
}

// CONTRACT(#107): a wikilink and a URL on the SAME line as prose (not a
// bullet entry) still counts as anchored — the check is per-line, not
// per-bullet.
func TestValidateBareLinkCheckIsPerLine(t *testing.T) {
	original := "---\ntype: moc\n---\n# MOC Test\n\nSee [[Setup Guide]] at https://example.com/guide for details.\n"
	retitled := "---\ntype: moc\n---\n# MOC Test\n\nSee [[Setup Docs]] at https://example.com/guide for details.\n"
	if err := Validate(original, retitled); err != nil {
		t.Errorf("unexpected rejection: %v", err)
	}
}

// CONTRACT(#108): a MOC with no frontmatter block at all in either
// version validates (both sides have "no block", which compares equal).
func TestValidateNoFrontmatterEitherSide(t *testing.T) {
	original := "# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	proposed := "# MOC Test\n\n### Group\n- [[Foo]] — https://example.com/foo\n"
	if err := Validate(original, proposed); err != nil {
		t.Errorf("unexpected rejection: %v", err)
	}
}

// CONTRACT(#108): proposing to ADD a frontmatter block where none
// existed is still a frontmatter change — rejected.
func TestValidateRejectsAddingFrontmatterBlock(t *testing.T) {
	original := "# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	proposed := "---\ntype: moc\n---\n# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	if err := Validate(original, proposed); !errors.Is(err, ErrFrontmatterChanged) {
		t.Errorf("err = %v, want ErrFrontmatterChanged", err)
	}
}
