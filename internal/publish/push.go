// internal/publish/push.go
package publish

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Pusher pushes a local file to a remote docs host over rsync — an
// interface (mirroring how internal/llm.Runner is injected everywhere)
// so cmd-level tests never attempt a real network call; RsyncPusher is
// the production implementation, argv-exec'd directly (never a shell).
type Pusher interface {
	Push(ctx context.Context, localPath, host, remotePath string) error
}

// RsyncPusher shells out to the real `rsync` binary via argv-exec (row
// #75). Bare "rsync"/"ssh" resolution via the process's own PATH
// (implicit exec.Command lookup, not a pre-resolved absolute path):
// unlike internal/llm.Runner (row #144, hardened for ov serve's
// launchd context with a minimal PATH), publish/unpublish are CLI-only
// per the design's "Web v1 surface" pin — they only ever run from an
// interactive terminal session with a normal PATH.
type RsyncPusher struct{}

func (RsyncPusher) Push(ctx context.Context, localPath, host, remotePath string) error {
	dest := host + ":" + strings.TrimSuffix(remotePath, "/") + "/"
	cmd := exec.CommandContext(ctx, "rsync", "-avz", localPath, dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rsync: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
