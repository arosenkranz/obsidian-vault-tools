package main

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

func newInboxCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "List inbox notes with ages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
			notes, err := vault.ListInbox(cfg.VaultDir, cfg.Inbox)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					fmt.Fprintf(errw, "inbox directory not found: %s\n", filepath.Join(cfg.VaultDir, cfg.Inbox))
					return nil
				}
				return err
			}
			if len(notes) == 0 {
				fmt.Fprintln(errw, "Inbox is empty")
				return nil
			}
			fmt.Fprintln(errw, "Inbox contents")
			now := time.Now()
			for _, n := range notes {
				age := vault.AgeDays(now, n.ModTime)
				fmt.Fprintf(out, "%s\t%d\n", n.Name, age)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}

// ageMarker mirrors v1 format_age (behavior inventory row #19): ⚠ when a note
// is older than 7 days, • otherwise. The threshold is reachable now that
// AgeDays reports real day counts (row #18 fix). Currently unused by the
// stdout path — row #123 keeps decoration off stdout in phase 1 — but kept
// here for phase 2's TUI inbox picker to reuse for a decorated view.
func ageMarker(age int) string {
	if age > 7 {
		return "⚠"
	}
	return "•"
}
