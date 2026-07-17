// internal/publish/remove.go
package publish

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Remover removes one published file (by basename) from the remote
// docs host over ssh — own injectable interface, distinct from
// llm.Runner (an ssh rm is not an LLM call). SSHRemover is the
// production implementation.
type Remover interface {
	Remove(ctx context.Context, host, remotePath, basename string) error
}

type SSHRemover struct{}

// shellQuoteSingle wraps s in single quotes for the REMOTE shell,
// escaping any embedded single quote (' -> '\''). ssh always hands its
// trailing argv to the remote user's shell as one joined command string
// (this is inherent to the ssh wire protocol, not a local `eval` — the
// LOCAL side stays a plain argv-exec, never `sh -c`, so row #72's fix
// class does not apply here); escaping the basename before it becomes
// part of that remote command string is a hardening v1 lacked (row
// #161: v1's `ssh "$host" "rm -f '${remote_path}/${base}'"` never
// escaped an embedded single quote in $base). Shared by remove.go and
// list.go.
func shellQuoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (SSHRemover) Remove(ctx context.Context, host, remotePath, basename string) error {
	remoteCmd := "rm -f " + shellQuoteSingle(strings.TrimSuffix(remotePath, "/")+"/"+basename)
	cmd := exec.CommandContext(ctx, "ssh", host, remoteCmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh rm: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
