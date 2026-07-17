// internal/publish/list.go
package publish

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Lister lists basenames currently published on the remote docs host —
// backs unpublish's no-args interactive picker (row #159 DECIDE: a
// plain numbered list read from stdin, matching render's own v1 non-gum
// picker style, not a new bubbletea component).
type Lister interface {
	List(ctx context.Context, host, remotePath string) ([]string, error)
}

type SSHLister struct{}

func (SSHLister) List(ctx context.Context, host, remotePath string) ([]string, error) {
	remoteCmd := "ls -1 " + shellQuoteSingle(strings.TrimSuffix(remotePath, "/"))
	cmd := exec.CommandContext(ctx, "ssh", host, remoteCmd)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return nil, fmt.Errorf("ssh ls: %w: %s", err, stderr)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}
