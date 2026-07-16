package capture

import "testing"

// CONTRACT(#27): bare URL = trimmed whitespace, then a whole-string
// http(s) URL with no internal whitespace.
func TestIsBareURL(t *testing.T) {
	cases := map[string]bool{
		"https://example.com":          true,
		"http://example.com/a/b":       true,
		"  https://example.com  ":      true,
		"https://example.com and more": false,
		"see https://example.com":      false,
		"not a url":                    false,
		"":                             false,
	}
	for in, want := range cases {
		if got := IsBareURL(in); got != want {
			t.Errorf("IsBareURL(%q) = %v, want %v", in, got, want)
		}
	}
}
