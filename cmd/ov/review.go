package main

import (
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

func newReviewCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Weekly review summary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
			now := time.Now()
			fmt.Fprintln(errw, "Weekly Review")

			// Inbox count — unified with `ov inbox` (top-level *.md, row #124).
			inbox, err := vault.ListInbox(cfg.VaultDir, cfg.Inbox)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			fmt.Fprintf(out, "inbox\t%d\n", len(inbox))

			// Modified this week (< 7 days), Archive excluded (rows #60, #61,
			// #125), newest first, capped at 10.
			modified, err := vault.ModifiedWithin(cfg.VaultDir, 7, now, []string{cfg.Archive})
			if err != nil {
				return err
			}
			if len(modified) > 10 {
				modified = modified[:10]
			}
			for _, n := range modified {
				fmt.Fprintf(out, "modified\t%s\n", n.Name)
			}

			// Projects — immediate children (row #60; #127: dir/file glyph
			// distinction dropped as decoration).
			projects, err := vault.ListProjects(cfg.VaultDir, cfg.Projects)
			if err != nil {
				return err
			}
			for _, p := range projects {
				fmt.Fprintf(out, "project\t%s\n", p.Name)
			}

			// MOCs (rows #60, #34).
			mocs, err := vault.MOCs(cfg.VaultDir)
			if err != nil {
				return err
			}
			for _, m := range mocs {
				fmt.Fprintf(out, "moc\t%s\n", m.Name)
			}

			// Hints are pure decoration → stderr (row #123).
			fmt.Fprintln(errw, "Next steps:")
			fmt.Fprintln(errw, "  - Process inbox with 'ov triage'")
			fmt.Fprintln(errw, "  - Check for stale notes with 'ov stale'")
			fmt.Fprintln(errw, "  - Update brag document if work-related wins")
			return nil
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}
