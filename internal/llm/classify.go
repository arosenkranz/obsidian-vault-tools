// internal/llm/classify.go
package llm

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var (
	// ErrEmptyCmd is returned when the configured LLM command is empty or
	// whitespace-only.
	ErrEmptyCmd = errors.New("OV_LLM_CMD is empty")
	// ErrBinaryNotFound is returned when argv[0] cannot be resolved via
	// exec.LookPath (row #90: "missing binary -> clean error").
	ErrBinaryNotFound = errors.New("llm binary not found in PATH")
	// ErrTimeout is returned when the call's context deadline fired before
	// the subprocess exited.
	ErrTimeout = errors.New("llm call timed out")
	// ErrAuth is returned when stderr indicates an expired/missing login
	// session, so frontends can render a specific, actionable message
	// (row #147) instead of a generic failure.
	ErrAuth = errors.New("llm auth expired — run `claude login` on the Mac")
)

// authMarkers are case-insensitive stderr substrings from `claude --print`
// and similar CLIs indicating an auth/login problem rather than a generic
// failure (row #147).
var authMarkers = []string{
	"not logged in",
	"please run",
	"claude login",
	"authentication",
	"unauthorized",
	"401",
}

// Classify turns a failed subprocess run (any error from exec.Cmd.Run, or
// context.DeadlineExceeded) plus its captured stderr into a typed error a
// frontend can render specifically. v1 raised one generic RuntimeError
// carrying stderr text (triage_llm.py:271-274); the web UI needs to tell
// "auth expired" apart from every other failure.
func Classify(runErr error, stderr string) error {
	lower := strings.ToLower(stderr)
	for _, m := range authMarkers {
		if strings.Contains(lower, m) {
			return fmt.Errorf("%w: %s", ErrAuth, strings.TrimSpace(stderr))
		}
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return fmt.Errorf("llm exited %d: %s", exitErr.ExitCode(), strings.TrimSpace(stderr))
	}
	if runErr != nil {
		return runErr
	}
	return fmt.Errorf("llm call failed: %s", strings.TrimSpace(stderr))
}
