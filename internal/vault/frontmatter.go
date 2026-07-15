// Package vault is the pure filesystem domain: notes, frontmatter, naming,
// atomic writes. No network, no LLM, no terminal (design spec §architecture).
package vault

import "strings"

// Frontmatter holds the raw inner lines of a note's frontmatter block.
// Reads are lenient; writes patch single lines; everything else is opaque
// and survives Render byte-for-byte.
type Frontmatter struct {
	lines          []string
	closingNewline bool // whether the closing --- had a trailing \n
}

func NewFrontmatter() *Frontmatter {
	return &Frontmatter{closingNewline: true}
}

// ParseNote mirrors triage_llm.py FM_RE: ^---\n(.*?)\n---\n? with DOTALL.
func ParseNote(text string) (*Frontmatter, string) {
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return nil, text
	}
	// Find the first line that is exactly "---" (mirrors the non-greedy regex).
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return nil, text
	}
	inner := rest[:idx]
	after := rest[idx+len("\n---"):]
	closingNewline := false
	if strings.HasPrefix(after, "\n") {
		closingNewline = true
		after = after[1:]
	} else if after != "" {
		// "---" was a prefix of a longer line (e.g. "----"): not a delimiter.
		// v2 deliberately treats an inner line starting "----" as
		// no-frontmatter — a round-trip-safe divergence from python's FM_RE,
		// whose optional trailing \n would lossily split such a line,
		// leaking the fourth dash into the body (inventory #84).
		return nil, text
	}
	return &Frontmatter{lines: strings.Split(inner, "\n"), closingNewline: closingNewline}, after
}

func (f *Frontmatter) Render() string {
	if f == nil || len(f.lines) == 0 {
		return ""
	}
	s := "---\n" + strings.Join(f.lines, "\n") + "\n---"
	if f.closingNewline {
		s += "\n"
	}
	return s
}

// keyLine returns the index of the first line declaring key, or -1.
// A declaring line is `key:` at zero indent (comments and indented
// continuation lines never match).
func (f *Frontmatter) keyLine(key string) int {
	if f == nil {
		return -1
	}
	for i, line := range f.lines {
		if strings.HasPrefix(line, "#") {
			continue
		}
		k, _, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.TrimSpace(k) == key && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			return i
		}
	}
	return -1
}

func (f *Frontmatter) rawValue(key string) (string, bool) {
	i := f.keyLine(key)
	if i < 0 {
		return "", false
	}
	_, v, _ := strings.Cut(f.lines[i], ":")
	return strings.TrimSpace(v), true
}

func (f *Frontmatter) Get(key string) (string, bool) {
	v, ok := f.rawValue(key)
	if !ok {
		return "", false
	}
	if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
		v = v[1 : len(v)-1]
	}
	return v, true
}

// GetList mirrors the python [a, b] shorthand — including the documented
// moc: [[MOC Music]] -> ["[MOC Music]"] quirk that MOC rename sync relies on.
func (f *Frontmatter) GetList(key string) ([]string, bool) {
	v, ok := f.rawValue(key)
	if !ok || len(v) < 2 || v[0] != '[' || v[len(v)-1] != ']' {
		return nil, false
	}
	inner := strings.TrimSpace(v[1 : len(v)-1])
	if inner == "" {
		return []string{}, true
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out, true
}

// blockEnd returns the index one past the contiguous run of continuation
// lines (indented with space or tab) following the declaring line i —
// e.g. the `  - a` items of a block-style list.
func (f *Frontmatter) blockEnd(i int) int {
	j := i + 1
	for j < len(f.lines) && (strings.HasPrefix(f.lines[j], " ") || strings.HasPrefix(f.lines[j], "\t")) {
		j++
	}
	return j
}

// Set replaces the key's declaring line AND any indented continuation lines
// with a single `key: value` line (a block-style value collapses to the new
// scalar/inline value — never orphaned list items), or appends the key.
func (f *Frontmatter) Set(key, value string) {
	if f == nil {
		return
	}
	line := key + ": " + value
	if i := f.keyLine(key); i >= 0 {
		f.lines[i] = line
		f.lines = append(f.lines[:i+1], f.lines[f.blockEnd(i):]...)
		return
	}
	f.lines = append(f.lines, line)
}

// Delete removes the key's declaring line and its continuation lines.
func (f *Frontmatter) Delete(key string) {
	if f == nil {
		return
	}
	if i := f.keyLine(key); i >= 0 {
		f.lines = append(f.lines[:i], f.lines[f.blockEnd(i):]...)
	}
}
