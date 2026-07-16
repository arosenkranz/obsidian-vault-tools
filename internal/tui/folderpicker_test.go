package tui

import "testing"

func TestFolderPickerBrowseNavigation(t *testing.T) {
	s := folderPickerState{folders: []string{"a", "b", "c"}}
	s = s.handleKey("down")
	if s.cursor != 1 {
		t.Fatalf("cursor = %d", s.cursor)
	}
	s = s.handleKey("down")
	if s.cursor != 2 {
		t.Fatalf("cursor = %d", s.cursor)
	}
	s = s.handleKey("down") // clamped at last index
	if s.cursor != 2 {
		t.Fatalf("cursor should clamp at 2, got %d", s.cursor)
	}
	s = s.handleKey("up")
	if s.cursor != 1 {
		t.Fatalf("cursor = %d", s.cursor)
	}
}

func TestFolderPickerSelect(t *testing.T) {
	s := folderPickerState{folders: []string{"a", "b"}, cursor: 1}
	s = s.handleKey("enter")
	if !s.done || s.result != "b" {
		t.Fatalf("state = %+v", s)
	}
}

// CONTRACT(#39): cancel is always available and never fails the caller.
func TestFolderPickerCancel(t *testing.T) {
	s := folderPickerState{folders: []string{"a"}}
	s = s.handleKey("q")
	if !s.cancelled {
		t.Fatal("expected cancelled")
	}
}

// CONTRACT(#37): typing a new path and pressing enter selects it verbatim.
func TestFolderPickerTypeNewPath(t *testing.T) {
	s := folderPickerState{folders: []string{"a"}}
	s = s.handleKey("n")
	if s.mode != modeTyping {
		t.Fatal("expected typing mode")
	}
	for _, ch := range "02-Areas/New" {
		s = s.handleKey(string(ch))
	}
	s = s.handleKey("enter")
	if !s.done || s.result != "02-Areas/New" {
		t.Fatalf("state = %+v", s)
	}
}

func TestFolderPickerTypeNewPathBackspace(t *testing.T) {
	s := folderPickerState{folders: []string{"a"}}
	s = s.handleKey("n")
	s = s.handleKey("x")
	s = s.handleKey("y")
	s = s.handleKey("backspace")
	if s.inputVal != "x" {
		t.Fatalf("inputVal = %q", s.inputVal)
	}
}

func TestFolderPickerTypeEscBackToBrowse(t *testing.T) {
	s := folderPickerState{folders: []string{"a"}}
	s = s.handleKey("n")
	s = s.handleKey("x")
	s = s.handleKey("esc")
	if s.mode != modeBrowse || s.inputVal != "" {
		t.Fatalf("state = %+v", s)
	}
}
