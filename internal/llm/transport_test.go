// internal/llm/transport_test.go
package llm

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llmtest"
)

func stubCmd(t *testing.T) string {
	t.Helper()
	self, err := llmtest.SelfCmd()
	if err != nil {
		t.Fatal(err)
	}
	return self
}

// CONTRACT(#89): OV_LLM_CMD is shlex-split into argv, never a shell; the
// prompt is delivered on stdin; --model is appended when set.
func TestRunSuccessDeliversPromptAndReturnsStdout(t *testing.T) {
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.ResponseEnv, `{"to": "ok"}`)
	r := NewRunner(stubCmd(t), "")
	got, err := r.Run(context.Background(), "hello prompt")
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"to": "ok"}` {
		t.Errorf("Run() = %q", got)
	}
}

// CONTRACT(#90): missing binary produces a clean, typed error, not a panic
// or an opaque exec error.
func TestRunMissingBinary(t *testing.T) {
	r := NewRunner("this-binary-does-not-exist-anywhere-12345", "")
	_, err := r.Run(context.Background(), "prompt")
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Errorf("err = %v, want ErrBinaryNotFound", err)
	}
}

// DECIDE(new in v2, row #89 companion): an empty OV_LLM_CMD is a clean
// error, not an empty-argv exec.Command panic.
func TestRunEmptyCmd(t *testing.T) {
	r := NewRunner("   ", "")
	_, err := r.Run(context.Background(), "prompt")
	if !errors.Is(err, ErrEmptyCmd) {
		t.Errorf("err = %v, want ErrEmptyCmd", err)
	}
}

// CONTRACT(#90): a non-zero exit is classified with the exit code and
// stderr attached.
func TestRunNonZeroExit(t *testing.T) {
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.ExitCodeEnv, "7")
	t.Setenv(llmtest.StderrEnv, "boom")
	r := NewRunner(stubCmd(t), "")
	_, err := r.Run(context.Background(), "prompt")
	if err == nil || !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "7") {
		t.Errorf("err = %v, want exit code 7 and stderr 'boom'", err)
	}
}

// DECIDE(new in v2, row #147): stderr auth/login markers classify as
// ErrAuth regardless of exit code text.
func TestRunAuthFailureClassified(t *testing.T) {
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.ExitCodeEnv, "1")
	t.Setenv(llmtest.StderrEnv, "Error: not logged in. Please run `claude login`.")
	r := NewRunner(stubCmd(t), "")
	_, err := r.Run(context.Background(), "prompt")
	if !errors.Is(err, ErrAuth) {
		t.Errorf("err = %v, want ErrAuth", err)
	}
}

// BUG(fixed)(#145): a job whose deadline fires kills the WHOLE process
// group, not just the direct child — proven here by a stub that sleeps
// past the deadline; the call must return promptly (bounded by
// WaitDelay), not hang for the stub's full sleep duration.
func TestRunTimeoutKillsPromptly(t *testing.T) {
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.SleepEnv, "5000") // 5s, far longer than the deadline below
	t.Setenv(llmtest.ResponseEnv, "should never be seen")
	r := NewRunner(stubCmd(t), "")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := r.Run(ctx, "prompt")
	elapsed := time.Since(start)
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("err = %v, want ErrTimeout", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("Run took %v after a 200ms deadline — process group was not killed promptly", elapsed)
	}
}

// DECIDE(new in v2, row #146): a third concurrent Run blocks until a slot
// frees rather than spawning unbounded subprocesses.
func TestRunSemaphoreBoundsConcurrency(t *testing.T) {
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.SleepEnv, "300")
	t.Setenv(llmtest.ResponseEnv, "ok")
	r := NewRunner(stubCmd(t), "")

	const n = 4
	start := time.Now()
	done := make(chan struct{}, n)
	for range n {
		go func() {
			_, _ = r.Run(context.Background(), "prompt")
			done <- struct{}{}
		}()
	}
	for range n {
		<-done
	}
	elapsed := time.Since(start)
	// 4 calls through a semaphore(2), each taking >=300ms, must take at
	// least two full "waves" — a generous floor that would fail if the
	// semaphore did not bound concurrency at all (all 4 running at once
	// would finish in ~300ms total).
	if elapsed < 500*time.Millisecond {
		t.Errorf("elapsed = %v, want >= 500ms (semaphore(2) should force at least 2 waves)", elapsed)
	}
}

// CONTRACT(#143): the LLM subprocess never runs with the vault (or any
// caller-chosen directory) as its CWD — it must be an empty scratch
// directory. Proven via the test-only lastRunDir seam, which runs the
// real transport once and reports the scratch directory it used.
func TestRunUsesScratchCWDNeverCallerDir(t *testing.T) {
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.ResponseEnv, "ok")
	r := NewRunner(stubCmd(t), "")
	cwd, err := r.lastRunDir(context.Background(), "prompt")
	if err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	if cwd == wd {
		t.Errorf("subprocess CWD = %q, must not equal the test process's own CWD %q", cwd, wd)
	}
	if !strings.Contains(cwd, "ov2-llm-") {
		t.Errorf("subprocess CWD = %q, want an ov2-llm-* scratch dir", cwd)
	}
}
