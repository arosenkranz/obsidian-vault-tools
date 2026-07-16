package tui

import (
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

func TestMOCPickerSelect(t *testing.T) {
	mocs := []vault.MOC{{Name: "MOC Music"}, {Name: "MOC Code"}}
	s := mocPickerState{mocs: mocs}
	s = s.handleKey("down")
	s = s.handleKey("enter")
	if !s.done || s.result != "MOC Code" {
		t.Fatalf("state = %+v", s)
	}
}

// CONTRACT(#39): MOC selection is always optional — cancelling never fails.
func TestMOCPickerCancel(t *testing.T) {
	s := mocPickerState{mocs: []vault.MOC{{Name: "MOC Music"}}}
	s = s.handleKey("esc")
	if !s.cancelled {
		t.Fatal("expected cancelled")
	}
}

func TestMOCPickerEmptyListCancelsOnEnter(t *testing.T) {
	s := mocPickerState{}
	s = s.handleKey("enter")
	if !s.cancelled {
		t.Fatal("expected cancelled with an empty list")
	}
}
