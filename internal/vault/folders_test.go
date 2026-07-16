// internal/vault/folders_test.go
package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// DECIDE(#35): the human picker walks full depth (unlike DiscoverFolders'
// depth<=2 used for the LLM prompt), mirroring v1 bash's
// list_all_para_folders.
func TestListAllFolders(t *testing.T) {
	vaultDir := t.TempDir()
	for _, d := range []string{"01-Projects", "02-Areas/Work", "02-Areas/Work/Clients", "03-Resources"} {
		if err := os.MkdirAll(filepath.Join(vaultDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := ListAllFolders(vaultDir, []string{"01-Projects", "02-Areas", "03-Resources", "04-Archive"})
	want := []string{"01-Projects", "02-Areas", "02-Areas/Work", "02-Areas/Work/Clients", "03-Resources"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListAllFolders = %v, want %v", got, want)
	}
}

func TestListAllFoldersMissingRootSkipped(t *testing.T) {
	vaultDir := t.TempDir()
	got := ListAllFolders(vaultDir, []string{"01-Projects"})
	if got != nil {
		t.Errorf("ListAllFolders = %v, want nil for an all-missing root set", got)
	}
}
