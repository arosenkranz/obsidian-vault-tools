// internal/render/markers.go
//
// Package render: goldmark markdown→HTML conversion and RENDER_SOURCE/
// RENDER_BODY/RENDER_TIMESTAMP marker parsing+splicing — a port of
// render_html.py's HTML-pairing mechanism (behavior inventory rows
// #117-119), with row #120's regex-replacement-template bug closed by
// construction: every splice in this file is string index/concatenation
// based, never a regex Replace call fed generated content as the
// replacement argument (Go's regexp.ReplaceAllString treats "$1"/
// "${name}" specially in its replacement string — the exact same bug
// CLASS as Python's re.sub "\1"/"\g<...>", just different
// metacharacters — a generated HTML body containing either family of
// sequence must survive unchanged).
package render

import (
	"errors"
	"regexp"
)

var (
	renderSourceRe    = regexp.MustCompile(`<!--\s*RENDER_SOURCE:\s*(.+?)\s*-->`)
	renderBodyStartRe = regexp.MustCompile(`<!--\s*RENDER_BODY_START\s*-->`)
	renderBodyEndRe   = regexp.MustCompile(`<!--\s*RENDER_BODY_END\s*-->`)
	renderTSRe        = regexp.MustCompile(`<!--\s*RENDER_TIMESTAMP:\s*.+?\s*-->`)
)

// ErrNoMarkers is returned when an HTML file is missing either the
// RENDER_BODY_START or RENDER_BODY_END comment (row #118: "missing
// markers -> skip with warning").
var ErrNoMarkers = errors.New("no RENDER_BODY_START/RENDER_BODY_END markers found")

// spliceBody replaces the content between the RENDER_BODY_START and
// RENDER_BODY_END markers in htmlText with newBody, via direct index
// slicing and string concatenation — NEVER a regexp.ReplaceAllString
// call with newBody as the replacement argument (row #120's fix: a
// generated HTML body containing a literal "\1", "\g<name>", or Go's
// own "$1"/"${name}" replacement-syntax lookalikes must survive
// unchanged; passing it through ANY regex-replacement-template API,
// Python's or Go's, would risk exactly that corruption).
func spliceBody(htmlText, newBody string) (string, error) {
	startLoc := renderBodyStartRe.FindStringIndex(htmlText)
	endLoc := renderBodyEndRe.FindStringIndex(htmlText)
	if startLoc == nil || endLoc == nil || endLoc[0] < startLoc[1] {
		return "", ErrNoMarkers
	}
	return htmlText[:startLoc[1]] + "\n" + newBody + "\n" + htmlText[endLoc[0]:], nil
}

// spliceTimestamp updates an existing RENDER_TIMESTAMP comment in
// place, or inserts one immediately after the RENDER_SOURCE comment on
// first render (row #119). timestampComment is a fully-formatted
// comment string built by the caller (regenerate.go) from a computed
// date — never note-content-derived, so this function carries no row
// #120-class risk on its own; kept in the same index/concatenation
// style as spliceBody for consistency.
func spliceTimestamp(htmlText, timestampComment string) string {
	if loc := renderTSRe.FindStringIndex(htmlText); loc != nil {
		return htmlText[:loc[0]] + timestampComment + htmlText[loc[1]:]
	}
	if loc := renderSourceRe.FindStringIndex(htmlText); loc != nil {
		return htmlText[:loc[1]] + "\n" + timestampComment + htmlText[loc[1]:]
	}
	return htmlText
}
