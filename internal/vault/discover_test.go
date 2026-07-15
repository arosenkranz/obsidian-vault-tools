package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// CONTRACT: mined from triage_llm.py discover_folders (169-184) —
// depth <= 2 below each root, sorted, missing roots skipped, files ignored.
func TestDiscoverFolders(t *testing.T) {
	vault := t.TempDir()
	mk := func(parts ...string) {
		if err := os.MkdirAll(filepath.Join(append([]string{vault}, parts...)...), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk("01-Projects", "Work", "ClientA")
	mk("01-Projects", "Home")
	mk("03-Resources", "Music")
	mk("01-Projects", "Work", "ClientA", "TooDeep") // depth 3: excluded
	os.WriteFile(filepath.Join(vault, "01-Projects", "stray.md"), []byte("x"), 0o644)

	got := DiscoverFolders(vault, []string{"01-Projects", "02-Areas", "03-Resources"})
	want := []string{
		"01-Projects",
		"01-Projects/Home",
		"01-Projects/Work",
		"01-Projects/Work/ClientA",
		"03-Resources",
		"03-Resources/Music",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}
