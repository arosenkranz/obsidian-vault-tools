// cmd/ov/unpublish.go
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/arosenkranz/obsidian-vault-tools/internal/publish"
	"github.com/arosenkranz/obsidian-vault-tools/internal/tui"
	"github.com/spf13/cobra"
)

// unpublishDeps injects the ssh-backed collaborators (and, for the
// interactive path, the tty-dependent confirm) so runUnpublish is fully
// testable without a real subprocess, network call, or terminal.
type unpublishDeps struct {
	remover publish.Remover
	lister  publish.Lister
	confirm func(string) (bool, error)
}

// sshOpTimeout bounds each individual ssh rm/ls call. v1 had no timeout
// on ssh at all (DECIDE, new in v2).
const sshOpTimeout = 30 * time.Second

func newUnpublishCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "unpublish [<file>...]",
		Short: "Remove file(s) from the docs server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			deps := unpublishDeps{remover: publish.SSHRemover{}, lister: publish.SSHLister{}, confirm: tui.Confirm}
			return runUnpublish(cfg, args, bufio.NewReader(cmd.InOrStdin()), cmd.ErrOrStderr(), deps)
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}

// runUnpublish is the testable core of `ov2 unpublish`. Requires
// cfg.DocsHost, else error+exit 1 (matches publish's own exit-1
// convention, not errExitCode2). Direct-args mode removes each basename
// with NO confirmation (row #76, ported exactly). With no args, a plain
// numbered picker (row #159 DECIDE — mirrors render's own v1 non-gum
// picker style, no new bubbletea component) lists the remote docs
// host's files via deps.lister, then an explicit y/Y confirm gates
// removal (row #77, deps.confirm reuses tui.Confirm).
func runUnpublish(cfg *config.Config, files []string, in *bufio.Reader, errw io.Writer, deps unpublishDeps) error {
	if cfg.DocsHost == "" {
		return errors.New("OV_DOCS_HOST not set: add docs_host to your config (see examples/ov.config.example)")
	}
	ctx := context.Background()
	remotePath := cfg.DocsPath

	if len(files) > 0 {
		for _, f := range files {
			base := filepath.Base(f)
			fmt.Fprintf(errw, "🗑 Removing %s...\n", base)
			rctx, cancel := context.WithTimeout(ctx, sshOpTimeout)
			err := deps.remover.Remove(rctx, cfg.DocsHost, remotePath, base)
			cancel()
			if err != nil {
				return fmt.Errorf("remove %s: %w", base, err)
			}
			fmt.Fprintln(errw, "✓ Removed")
		}
		return nil
	}

	fmt.Fprintln(errw, "Fetching published files...")
	lctx, cancel := context.WithTimeout(ctx, sshOpTimeout)
	remote, err := deps.lister.List(lctx, cfg.DocsHost, remotePath)
	cancel()
	if err != nil {
		return fmt.Errorf("listing remote files: %w", err)
	}
	if len(remote) == 0 {
		fmt.Fprintln(errw, "No files on docs server.")
		return nil
	}

	fmt.Fprintln(errw, "\nPublished files:")
	for i, f := range remote {
		fmt.Fprintf(errw, "  [%d] %s\n", i+1, f)
	}
	fmt.Fprint(errw, "\nUnpublish which? (numbers/comma-separated, \"a\" for all, \"q\" to cancel): ")
	line, readErr := in.ReadString('\n')
	choice := strings.ToLower(strings.TrimSpace(line))
	if readErr != nil && choice == "" {
		choice = "q"
	}
	if choice == "" || choice == "q" {
		fmt.Fprintln(errw, "Cancelled.")
		return nil
	}

	var selected []string
	if choice == "a" {
		selected = remote
	} else {
		for _, tok := range strings.Split(choice, ",") {
			tok = strings.TrimSpace(tok)
			n, convErr := strconv.Atoi(tok)
			if convErr != nil || n < 1 || n > len(remote) {
				return fmt.Errorf("invalid choice: %q", tok)
			}
			selected = append(selected, remote[n-1])
		}
	}
	if len(selected) == 0 {
		fmt.Fprintln(errw, "Cancelled.")
		return nil
	}

	fmt.Fprintln(errw, "Will remove:")
	for _, f := range selected {
		fmt.Fprintf(errw, "  • %s\n", f)
	}
	ok, confirmErr := deps.confirm("Unpublish these files?")
	if confirmErr != nil || !ok {
		fmt.Fprintln(errw, "Cancelled.")
		return nil
	}

	for _, f := range selected {
		fmt.Fprintf(errw, "🗑 Removing %s...\n", f)
		rctx, cancel := context.WithTimeout(ctx, sshOpTimeout)
		err := deps.remover.Remove(rctx, cfg.DocsHost, remotePath, f)
		cancel()
		if err != nil {
			return fmt.Errorf("remove %s: %w", f, err)
		}
		fmt.Fprintln(errw, "✓ Removed")
	}
	return nil
}
