// internal/publish/transport_test.go
package publish

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stubBinOnPath writes an executable shell script named name to a fresh
// temp dir, prepends that dir to PATH for the duration of the test, and
// lets these tests exercise the REAL subprocess transport (argv-exec,
// never a shell on the LOCAL side) against a local stand-in binary
// instead of a real network call — same spirit as internal/llm's own
// subprocess tests, applied here to rsync/ssh instead of an LLM CLI.
func stubBinOnPath(t *testing.T, name, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub shell scripts require a POSIX shell")
	}
	dir := t.TempDir()
	binPath := filepath.Join(dir, name)
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func mustReadTrimmed(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// CONTRACT(row #75): RsyncPusher argv-execs "rsync -avz <local>
// <host>:<remotePath>/" — never a shell.
func TestRsyncPusherArgvExec(t *testing.T) {
	tmp := t.TempDir()
	argvFile := filepath.Join(tmp, "argv.txt")
	stubBinOnPath(t, "rsync", `echo "$@" > `+shellQuoteSingle(argvFile))

	local := filepath.Join(tmp, "note.html")
	if err := os.WriteFile(local, []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := (RsyncPusher{}).Push(context.Background(), local, "docs.example.com", "/var/www/docs"); err != nil {
		t.Fatal(err)
	}
	got := mustReadTrimmed(t, argvFile)
	want := "-avz " + local + " docs.example.com:/var/www/docs/"
	if got != want {
		t.Errorf("rsync argv = %q, want %q", got, want)
	}
}

// CONTRACT(row #76): SSHRemover argv-execs "ssh <host> <remoteCmd>" —
// the LOCAL side is a plain argv-exec, never `sh -c`.
func TestSSHRemoverArgvExec(t *testing.T) {
	tmp := t.TempDir()
	argvFile := filepath.Join(tmp, "argv.txt")
	stubBinOnPath(t, "ssh", `echo "$@" > `+shellQuoteSingle(argvFile))

	if err := (SSHRemover{}).Remove(context.Background(), "docs.example.com", "/var/www/docs", "note.html"); err != nil {
		t.Fatal(err)
	}
	got := mustReadTrimmed(t, argvFile)
	want := "docs.example.com rm -f '/var/www/docs/note.html'"
	if got != want {
		t.Errorf("ssh argv = %q, want %q", got, want)
	}
}

// DECIDE(new in v2, row #161): a basename containing a single quote is
// escaped for the remote command, not passed through raw — v1 never
// escaped this.
func TestSSHRemoverEscapesSingleQuote(t *testing.T) {
	tmp := t.TempDir()
	argvFile := filepath.Join(tmp, "argv.txt")
	stubBinOnPath(t, "ssh", `echo "$@" > `+shellQuoteSingle(argvFile))

	if err := (SSHRemover{}).Remove(context.Background(), "host", "/docs", "it's-a-note.html"); err != nil {
		t.Fatal(err)
	}
	got := mustReadTrimmed(t, argvFile)
	if !strings.Contains(got, `'\''`) {
		t.Errorf("expected an escaped single quote in the remote command, got %q", got)
	}
}

// CONTRACT: SSHLister parses one basename per line from `ssh host ls -1
// remotePath`.
func TestSSHListerParsesOutput(t *testing.T) {
	stubBinOnPath(t, "ssh", `echo "a.html"; echo "b.html"`)

	got, err := (SSHLister{}).List(context.Background(), "docs.example.com", "/var/www/docs")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a.html" || got[1] != "b.html" {
		t.Errorf("got = %v", got)
	}
}

// An empty remote listing returns an empty (not nil-panicking) slice.
func TestSSHListerEmpty(t *testing.T) {
	stubBinOnPath(t, "ssh", `true`)
	got, err := (SSHLister{}).List(context.Background(), "host", "/docs")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got = %v, want empty", got)
	}
}

// CONTRACT: a non-zero exit from the remote side is surfaced as an
// error carrying stderr.
func TestSSHRemoverPropagatesFailure(t *testing.T) {
	stubBinOnPath(t, "ssh", `echo "permission denied" >&2; exit 1`)
	err := (SSHRemover{}).Remove(context.Background(), "host", "/docs", "note.html")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("err = %v, expected stderr to be included", err)
	}
}
