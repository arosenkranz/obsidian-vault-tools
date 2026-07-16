// internal/llm/transport.go
package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Config is the narrow subset of ov config the llm package needs — kept
// separate from internal/config.Config (design spec's "stateless verbs"
// principle, same pattern as capture.CaptureConfig / triage.Config).
type Config struct {
	Cmd   string // OV_LLM_CMD, e.g. "claude --print"
	Model string // optional; appended as "--model <Model>" when non-empty
}

// maxConcurrentRuns bounds simultaneous LLM subprocesses (design spec
// §LLM subsystem "Job model": semaphore of 2; row #146).
const maxConcurrentRuns = 2

// Runner executes the configured LLM command over subprocess transport,
// bounding concurrent subprocesses to a fixed semaphore. One Runner is
// constructed per process (cmd/ov's triage command, internal/web's
// server) and reused across every call so the semaphore is shared.
type Runner struct {
	cmd   string
	model string
	sem   chan struct{}
}

// NewRunner builds a Runner around the resolved OV_LLM_CMD/OV_MODEL
// values (internal/config.Config.LLMCmd/Model, already precedence-merged
// by config.Load — Runner never reads the environment itself, avoiding
// the row #82-class bug of two independent config readers disagreeing).
func NewRunner(cmd, model string) *Runner {
	return &Runner{cmd: cmd, model: model, sem: make(chan struct{}, maxConcurrentRuns)}
}

// Run invokes the configured LLM command with prompt on stdin and returns
// its stdout. argv is built via shlexSplit — never a shell (row #89, also
// the mechanism that retires publish's `eval`, row #72, in a later
// phase). argv[0] is resolved to an absolute path via exec.LookPath before
// every invocation (row #144). The subprocess's CWD is a freshly created,
// empty scratch directory — never the vault (row #143, prompt injection
// posture). It runs in its own process group; on ctx cancellation or
// deadline the WHOLE group is killed, not just the direct child, because
// `claude` spawns a node subprocess tree a direct-child-only kill would
// orphan (row #145). Concurrent calls are bounded by the semaphore
// (row #146). Exit code + stderr are classified via Classify (row #147).
func (r *Runner) Run(ctx context.Context, prompt string) (string, error) {
	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-r.sem }()
	return runOnce(ctx, r.cmd, r.model, prompt)
}

// lastRunDir is a test-only seam: it runs the real transport once and
// returns the scratch directory used, so tests can assert the subprocess
// never ran with the vault (or the test process's own CWD) as its
// working directory, without parsing stdout for a marker.
func (r *Runner) lastRunDir(ctx context.Context, prompt string) (string, error) {
	scratch, err := os.MkdirTemp("", "ov2-llm-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	if _, err := runInDir(ctx, r.cmd, r.model, prompt, scratch); err != nil {
		return "", err
	}
	return scratch, nil
}

func runOnce(ctx context.Context, cmdStr, model, prompt string) (string, error) {
	scratch, err := os.MkdirTemp("", "ov2-llm-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	return runInDir(ctx, cmdStr, model, prompt, scratch)
}

func runInDir(ctx context.Context, cmdStr, model, prompt, dir string) (string, error) {
	argv, err := shlexSplit(cmdStr)
	if err != nil {
		return "", err
	}
	if len(argv) == 0 {
		return "", ErrEmptyCmd
	}
	if model != "" {
		argv = append(argv, "--model", model)
	}
	absPath, err := exec.LookPath(argv[0])
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrBinaryNotFound, argv[0])
	}

	cmd := exec.CommandContext(ctx, absPath, argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // whole process group, row #145
	}

	runErr := cmd.Run()
	if runErr != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%w: %s", ErrTimeout, ctx.Err())
		}
		return "", Classify(runErr, stderr.String())
	}
	return stdout.String(), nil
}

// shlexSplit is a minimal shell-word splitter: whitespace-separated
// tokens, with single- and double-quoted spans preserved as one token
// (quotes stripped, no further interpretation — no glob/variable
// expansion, this is NOT a shell). Mirrors python shlex.split's behavior
// for the simple `claude --print` / `pi --print -nc -nt --mode json`
// style commands OV_LLM_CMD actually holds (row #89).
func shlexSplit(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	var inQuote rune
	hasToken := false
	for _, r := range s {
		switch {
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			inQuote = r
			hasToken = true
		case r == ' ' || r == '\t' || r == '\n':
			if hasToken {
				out = append(out, cur.String())
				cur.Reset()
				hasToken = false
			}
		default:
			cur.WriteRune(r)
			hasToken = true
		}
	}
	if inQuote != 0 {
		return nil, errors.New("unterminated quote in OV_LLM_CMD")
	}
	if hasToken {
		out = append(out, cur.String())
	}
	return out, nil
}

// HealthCheck attempts a minimal, short-timeout LLM invocation purely to
// surface configuration/auth problems before a user starts a real 120s
// triage call (row #149). Returns nil only if the subprocess actually ran
// and exited 0.
func (r *Runner) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := r.Run(ctx, "Reply with the single word: ok")
	return err
}
