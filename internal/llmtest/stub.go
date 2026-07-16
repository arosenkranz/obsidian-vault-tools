// internal/llmtest/stub.go
//
// Package llmtest provides a TestMain re-exec stub-LLM responder shared by
// internal/llm, cmd/ov, and internal/web's temp-vault integration tests
// (design spec §Testing strategy tier 3: "OV_LLM_CMD pointed at a stub
// responder binary (TestMain re-exec)" — exercises the REAL internal/llm
// subprocess transport deterministically instead of mocking exec.Cmd
// away). A test sets OV_LLM_CMD to the currently-running test binary's own
// absolute path (SelfCmd) plus the env vars below (t.Setenv — child
// processes spawned via exec.Command with a nil Env inherit the parent's
// environment, so these propagate automatically); the test binary's own
// TestMain calls MaybeRunStub before m.Run() so a re-exec'd invocation
// short-circuits into stub-responder mode instead of running the real test
// suite.
package llmtest

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	// StubEnv, when "1", switches this binary into stub-LLM-responder mode.
	StubEnv = "OV_TEST_STUB_LLM"
	// ResponseEnv is written verbatim to stdout on a zero-exit-code run.
	ResponseEnv = "OV_TEST_STUB_RESPONSE"
	// ExitCodeEnv overrides the stub's exit code (default 0).
	ExitCodeEnv = "OV_TEST_STUB_EXIT_CODE"
	// StderrEnv is written verbatim to stderr before exit.
	StderrEnv = "OV_TEST_STUB_STDERR"
	// SleepEnv, in milliseconds, delays the stub before it drains stdin and
	// responds — used to exercise job-timeout / process-group-kill tests.
	SleepEnv = "OV_TEST_STUB_SLEEP_MS"
)

// MaybeRunStub checks StubEnv and, if set, drains stdin (mirrors a real LLM
// CLI's stdin-prompt contract), optionally sleeps, writes StderrEnv to
// stderr if set, then exits with ExitCodeEnv (default 0), writing
// ResponseEnv to stdout first only on a zero exit code. Call from TestMain
// BEFORE m.Run(); it never returns when the stub path is taken (it calls
// os.Exit), so callers do not need to branch on the return value except to
// satisfy the compiler in unreachable code after the call.
func MaybeRunStub() bool {
	if os.Getenv(StubEnv) != "1" {
		return false
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	if ms := os.Getenv(SleepEnv); ms != "" {
		if n, err := strconv.Atoi(ms); err == nil {
			time.Sleep(time.Duration(n) * time.Millisecond)
		}
	}
	if stderr := os.Getenv(StderrEnv); stderr != "" {
		fmt.Fprint(os.Stderr, stderr)
	}
	code := 0
	if c := os.Getenv(ExitCodeEnv); c != "" {
		if n, err := strconv.Atoi(c); err == nil {
			code = n
		}
	}
	if code == 0 {
		fmt.Fprint(os.Stdout, os.Getenv(ResponseEnv))
	}
	os.Exit(code)
	return true // unreachable
}

// SelfCmd returns the absolute path of the currently-running test binary —
// the value to put in OV_LLM_CMD (or config's llm_cmd) so llm.Run re-execs
// this same binary in stub mode. os.Args[0] is already the compiled test
// binary's path under `go test`; filepath.Abs is defensive in case a
// caller ever runs tests from a relative-path working directory.
func SelfCmd() (string, error) {
	return filepath.Abs(os.Args[0])
}
