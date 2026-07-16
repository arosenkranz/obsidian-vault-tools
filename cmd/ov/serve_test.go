// cmd/ov/serve_test.go
package main

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// DECIDE(#133): the bind guard runs before the listener opens — an
// unauthorized bind never touches the network.
func TestServeBindGuardRejectsNonLoopback(t *testing.T) {
	newVaultFixture(t)
	_, _, err := runCmd(t, "serve", "--bind", "0.0.0.0:0")
	if err == nil {
		t.Fatal("expected the bind guard to refuse 0.0.0.0 without --allow-nonlocal-bind")
	}
}

func TestServeStartsAndStopsOnLoopback(t *testing.T) {
	newVaultFixture(t)
	root := newRootCmd()
	ctx, cancel := context.WithCancel(context.Background())
	root.SetArgs([]string{"serve", "--bind", "127.0.0.1:0"})
	var errBuf bytes.Buffer
	root.SetErr(&errBuf)
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	time.Sleep(200 * time.Millisecond)
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
