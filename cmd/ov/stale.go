// cmd/ov/stale.go
package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

func newStaleCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "stale [days]",
		Short: "List notes untouched for N+ days (default 90)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			days := 90
			if len(args) == 1 {
				n, err := strconv.Atoi(args[0])
				if err != nil || n < 0 {
					return fmt.Errorf("days must be a non-negative integer, got %q", args[0])
				}
				days = n
			}
			out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
			// Exclusions: configured Archive + Meta (row #61 fix) plus the
			// file-driven stale_exclude list (row #62, default Daily Notes).
			excludes := append([]string{cfg.Archive, cfg.Meta}, cfg.StaleExclude...)
			now := time.Now()
			notes, err := vault.Stale(cfg.VaultDir, days, now, excludes)
			if err != nil {
				return err
			}
			fmt.Fprintf(errw, "Notes untouched in %d+ days\n", days)
			if len(notes) == 0 {
				fmt.Fprintln(errw, "None found")
				return nil
			}
			for _, n := range notes {
				fmt.Fprintf(out, "%s\t%d\n", n.Rel, vault.AgeDays(now, n.ModTime))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}
