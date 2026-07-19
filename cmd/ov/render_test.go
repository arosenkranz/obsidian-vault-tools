// cmd/ov/render_test.go
package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CONTRACT: a <file> argument regenerates exactly that pair.
func TestRenderSingleFile(t *testing.T) {
	vaultDir := newVaultFixture(t)
	mdPath := filepath.Join(vaultDir, "03-Resources", "guide.md")
	htmlPath := filepath.Join(vaultDir, "03-Resources", "guide.html")
	if err := os.WriteFile(mdPath, []byte("# Guide\n\nHello.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(htmlPath, []byte("<!-- RENDER_SOURCE: 03-Resources/guide.md -->\n<!-- RENDER_BODY_START -->stale<!-- RENDER_BODY_END -->"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runCmd(t, "render", "03-Resources/guide.html", "--vault", vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != "03-Resources/guide.html" {
		t.Errorf("stdout = %q", stdout)
	}
	got, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "stale") {
		t.Errorf("stale content survived: %s", got)
	}
}

// CONTRACT: --all regenerates every discovered pair.
func TestRenderAllFlag(t *testing.T) {
	vaultDir := newVaultFixture(t)
	for _, name := range []string{"a", "b"} {
		mdPath := filepath.Join(vaultDir, "03-Resources", name+".md")
		htmlPath := filepath.Join(vaultDir, "03-Resources", name+".html")
		if err := os.WriteFile(mdPath, []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(htmlPath, []byte("<!-- RENDER_SOURCE: 03-Resources/"+name+".md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stdout, _, err := runCmd(t, "render", "--all", "--vault", vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Fields(stdout)) != 2 {
		t.Fatalf("stdout = %q, want 2 lines", stdout)
	}
}

// BUG(fixed)(#162): --all does not stop at the first failure — it
// processes every pair and reports both the successes and the failure.
func TestRenderAllContinuesPastFailure(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "bad.html"), []byte("<!-- RENDER_SOURCE: 03-Resources/missing.md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "good.md"), []byte("# Good\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "good.html"), []byte("<!-- RENDER_SOURCE: 03-Resources/good.md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runCmd(t, "render", "--all", "--vault", vaultDir)
	if err == nil {
		t.Fatal("expected a non-nil error because one of two pairs failed")
	}
	if !strings.Contains(stdout, "good.html") {
		t.Errorf("expected good.html to still be processed despite bad.html's failure, stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stderr, "2 file(s) processed (1 ok, 1 failed)") {
		t.Errorf("expected an accurate ok/failed summary, stderr=%q", stderr)
	}
}

// CONTRACT: no pairs found prints a message and exits 0.
func TestRenderNoPairsFound(t *testing.T) {
	vaultDir := newVaultFixture(t)
	_, stderr, err := runCmd(t, "render", "--vault", vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "No paired HTML files found") {
		t.Errorf("stderr = %q", stderr)
	}
}

// BUG(fixed): a file argument that attempts to traverse outside the
// vault is rejected, not silently resolved.
func TestRenderFileArgTraversalRejected(t *testing.T) {
	vaultDir := newVaultFixture(t)
	_, _, err := runCmd(t, "render", "../../etc/passwd", "--vault", vaultDir)
	if err == nil {
		t.Fatal("expected an error for a traversal-attempting file argument")
	}
}

// CONTRACT: with no args and no --all, an interactive numbered picker
// (row #69's per-command resolution) reads a plain choice from stdin —
// mirrors v1's own non-gum picker (render_html.py:369-389).
func TestRenderInteractivePickerNumberSelection(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "only.md"), []byte("# Only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "only.html"), []byte("<!-- RENDER_SOURCE: 03-Resources/only.md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var out, errw bytes.Buffer
	if err := runRender(cfg, "", false, bufio.NewReader(strings.NewReader("1\n")), &out, &errw); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "03-Resources/only.html" {
		t.Errorf("out = %q", out.String())
	}
}

// CONTRACT: the interactive picker's "q" choice (and EOF) cancels
// cleanly without regenerating anything.
func TestRenderInteractivePickerQuit(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "only.md"), []byte("# Only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "only.html"), []byte("<!-- RENDER_SOURCE: 03-Resources/only.md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var out, errw bytes.Buffer
	if err := runRender(cfg, "", false, bufio.NewReader(strings.NewReader("q\n")), &out, &errw); err != nil {
		t.Fatal(err)
	}
	if out.String() != "" {
		t.Errorf("expected nothing regenerated, out = %q", out.String())
	}
}
