// internal/capture/title.go
package capture

import (
	"regexp"
	"strings"
)

// bareURLRe matches a whole string that is exactly one http(s) URL with no
// internal whitespace (behavior inventory row #27).
var bareURLRe = regexp.MustCompile(`^https?://\S+$`)

// IsBareURL reports whether s (after trimming surrounding whitespace) is
// exactly one http(s) URL with nothing else on the line.
func IsBareURL(s string) bool {
	return bareURLRe.MatchString(strings.TrimSpace(s))
}
