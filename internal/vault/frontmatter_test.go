// internal/vault/frontmatter_test.go
package vault

import "testing"

// CONTRACT: byte-identical no-op round-trip (design spec, review blocker F6/m5).
func TestRoundTripByteIdentical(t *testing.T) {
	cases := []string{
		// flat kv
		"---\ntype: note\ncreated: 2026-07-11\n---\nbody\n",
		// nested map + block list + comment + no-colon line: all opaque, all preserved
		"---\ntags:\n  - deep\n  - nested\nmeta:\n  owner: alex\n# comment\nplainline\n---\n\n# Heading\n",
		// multiline string
		"---\nsummary: |\n  line one\n  line two\nstatus: raw\n---\nbody\n",
		// no trailing newline after closing delimiter
		"---\ntype: note\n---",
		// no frontmatter at all
		"just a body\nno delimiters\n",
		// empty body
		"---\ntype: note\n---\n",
	}
	for i, in := range cases {
		fm, body := ParseNote(in)
		var out string
		if fm == nil {
			out = body
		} else {
			out = fm.Render() + body
		}
		if out != in {
			t.Errorf("case %d: round-trip mismatch\n in: %q\nout: %q", i, in, out)
		}
	}
}

// CONTRACT: lenient read view — quotes stripped, [a, b] becomes a list
// (triage_llm.py split_frontmatter lines 111-136).
func TestLenientView(t *testing.T) {
	fm, _ := ParseNote("---\ntitle: \"Quoted Title\"\nurl: 'single'\ntags: [music, jazz]\nempty_list: []\n---\n")
	if v, ok := fm.Get("title"); !ok || v != "Quoted Title" {
		t.Errorf("title = %q, %v", v, ok)
	}
	if v, _ := fm.Get("url"); v != "single" {
		t.Errorf("url = %q", v)
	}
	if l, ok := fm.GetList("tags"); !ok || len(l) != 2 || l[0] != "music" || l[1] != "jazz" {
		t.Errorf("tags = %v, %v", l, ok)
	}
	if l, ok := fm.GetList("empty_list"); !ok || len(l) != 0 {
		t.Errorf("empty_list = %v, %v", l, ok)
	}
}

// CONTRACT(by accident, load-bearing): moc: [[MOC Music]] parses as a
// one-element list ["[MOC Music]"]. MOC rename sync depends on this quirk
// (tests/test_triage_llm.py:22-28). Keep exactly.
func TestWikilinkQuirk(t *testing.T) {
	fm, _ := ParseNote("---\nmoc: [[MOC Music]]\n---\n")
	l, ok := fm.GetList("moc")
	if !ok || len(l) != 1 || l[0] != "[MOC Music]" {
		t.Errorf("moc quirk broken: %v, %v", l, ok)
	}
}

// Comments and colon-less lines are invisible to Get but preserved by Render.
func TestOpaqueLinesNotParsed(t *testing.T) {
	fm, _ := ParseNote("---\n# a comment\nnocolonhere\ntype: note\n---\n")
	if _, ok := fm.Get("# a comment"); ok {
		t.Error("comments must not be parsed as keys")
	}
	if v, ok := fm.Get("type"); !ok || v != "note" {
		t.Errorf("type = %q, %v", v, ok)
	}
}

func TestSetPatchesInPlace(t *testing.T) {
	in := "---\ntype: note\n# keep me\nstatus: inbox\n---\nbody"
	fm, body := ParseNote(in)
	fm.Set("status", "filed")
	want := "---\ntype: note\n# keep me\nstatus: filed\n---\nbody"
	if got := fm.Render() + body; got != want {
		t.Errorf("patch in place:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSetAppendsNewKey(t *testing.T) {
	fm, _ := ParseNote("---\ntype: note\n---\n")
	fm.Set("area", "Music")
	want := "---\ntype: note\narea: Music\n---\n"
	if got := fm.Render(); got != want {
		t.Errorf("append:\ngot:  %q\nwant: %q", got, want)
	}
}

// BUG(fixed): Set on a block-style key (`tags:` + indented `  - a` items)
// used to replace only the declaring line, orphaning the continuation lines
// as invalid YAML. v2 collapses the whole block to the new value.
func TestSetCollapsesBlockList(t *testing.T) {
	in := "---\ntags:\n  - music\n  - jazz\nstatus: inbox\n---\nbody"
	fm, body := ParseNote(in)
	fm.Set("tags", "[rock, blues]")
	want := "---\ntags: [rock, blues]\nstatus: inbox\n---\nbody"
	if got := fm.Render() + body; got != want {
		t.Errorf("block collapse:\ngot:  %q\nwant: %q", got, want)
	}
}

// BUG(fixed): Delete on a block-style key used to leave its continuation
// lines dangling. v2 removes the whole block.
func TestDeleteRemovesBlockList(t *testing.T) {
	in := "---\ntype: note\ntags:\n  - music\n  - jazz\nstatus: inbox\n---\n"
	fm, _ := ParseNote(in)
	fm.Delete("tags")
	want := "---\ntype: note\nstatus: inbox\n---\n"
	if got := fm.Render(); got != want {
		t.Errorf("block delete:\ngot:  %q\nwant: %q", got, want)
	}
}

// CONTRACT: block keys the edit does not touch survive byte-for-byte
// (companion to TestRoundTripByteIdentical).
func TestSetPreservesUntouchedBlockKeys(t *testing.T) {
	in := "---\ntags:\n  - deep\n  - nested\nstatus: inbox\n---\nbody"
	fm, body := ParseNote(in)
	fm.Set("status", "filed")
	want := "---\ntags:\n  - deep\n  - nested\nstatus: filed\n---\nbody"
	if got := fm.Render() + body; got != want {
		t.Errorf("untouched block key:\ngot:  %q\nwant: %q", got, want)
	}
}

// A note without frontmatter parses to a nil *Frontmatter; every method
// must be nil-safe so callers can operate on the parse result directly.
func TestNilFrontmatterSafe(t *testing.T) {
	fm, body := ParseNote("no frontmatter here\n")
	if fm != nil || body != "no frontmatter here\n" {
		t.Fatalf("fm = %v, body = %q", fm, body)
	}
	if _, ok := fm.Get("type"); ok {
		t.Error("Get on nil fm must report not-found")
	}
	if _, ok := fm.GetList("tags"); ok {
		t.Error("GetList on nil fm must report not-found")
	}
	fm.Set("type", "note") // must not panic
	fm.Delete("type")      // must not panic
	if fm.Render() != "" {
		t.Error("nil fm must render empty")
	}
}

func TestDelete(t *testing.T) {
	fm, _ := ParseNote("---\ntype: note\nstatus: inbox\n---\n")
	fm.Delete("status")
	if got := fm.Render(); got != "---\ntype: note\n---\n" {
		t.Errorf("delete: got %q", got)
	}
	fm.Delete("missing") // must not panic
}

// CONTRACT: new-note frontmatter built in preferred key order
// (triage_llm.py render_frontmatter line 145) — the byte contract for
// every note ov has ever filed.
func TestNewFrontmatterGolden(t *testing.T) {
	fm := NewFrontmatter()
	for _, kv := range [][2]string{
		{"type", "note"}, {"created", "2026-07-11"}, {"modified", "2026-07-11"},
		{"tags", "[music, jazz]"}, {"status", "inbox"}, {"source", "cli"},
	} {
		fm.Set(kv[0], kv[1])
	}
	want := "---\ntype: note\ncreated: 2026-07-11\nmodified: 2026-07-11\ntags: [music, jazz]\nstatus: inbox\nsource: cli\n---\n"
	if got := fm.Render(); got != want {
		t.Errorf("golden mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}
