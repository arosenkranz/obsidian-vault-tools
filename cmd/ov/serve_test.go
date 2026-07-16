// cmd/ov/serve_test.go
package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"
)

// DECIDE(#133): the bind guard runs before the listener opens — an
// unauthorized bind never touches the network.
func TestServeBindGuardRejectsNonLoopback(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "AGENTS.md"), []byte("test contract"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := runCmd(t, "serve", "--bind", "0.0.0.0:0")
	if err == nil {
		t.Fatal("expected the bind guard to refuse 0.0.0.0 without --allow-nonlocal-bind")
	}
}

// syncBuffer is a concurrency-safe io.Writer/String() pair — the test
// reads serve's stderr banner from a goroutine racing with cobra's writer.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var listenAddrRe = regexp.MustCompile(`listening on http://(\S+)`)

// waitForListenAddr polls buf for the "listening on http://<addr>" banner
// serve.go prints once net.Listen succeeds, and returns the OS-assigned
// address (bindFlag's ":0" port is resolved by the kernel).
func waitForListenAddr(t *testing.T, buf *syncBuffer, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m := listenAddrRe.FindStringSubmatch(buf.String()); m != nil {
			return m[1]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("server did not print its listen address within %s; stderr so far: %s", timeout, buf.String())
	return ""
}

// TestServeStartsAndStopsOnLoopback proves a genuine start/stop lifecycle,
// not just that RunE returns nil: it dials the real listener with a real
// HTTP request BEFORE cancelling the context, so a Serve implementation
// that silently no-ops (never actually accepting connections) fails this
// test — the listener would already be closed by RunE's deferred Close by
// the time the dial happens, since a no-op Serve returns almost instantly
// (found in Task 8 review: the original version only proved RunE returned
// nil, which passes even for a no-op Serve since a buffered channel absorbs
// the near-instant result before the 200ms sleep elapses).
func TestServeStartsAndStopsOnLoopback(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "AGENTS.md"), []byte("test contract"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := newRootCmd()
	ctx, cancel := context.WithCancel(context.Background())
	root.SetArgs([]string{"serve", "--bind", "127.0.0.1:0"})
	errBuf := &syncBuffer{}
	root.SetErr(errBuf)
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	addr := waitForListenAddr(t, errBuf, 2*time.Second)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("failed to reach the running server before cancellation: %v\nstderr: %s", err, errBuf.String())
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET / = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned an error on shutdown: %v\nstderr: %s", err, errBuf.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not shut down within 5s of context cancellation")
	}
}
