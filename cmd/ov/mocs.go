package main

import (
	"fmt"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

func newMocsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mocs",
		Short: "Maps of Content",
	}
	cmd.AddCommand(newMocsListCmd(), newMocsOrphanCmd())
	return cmd
}

func newMocsListCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List MOCs with item counts and descriptions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
			mocs, err := vault.MOCs(cfg.VaultDir)
			if err != nil {
				return err
			}
			if len(mocs) == 0 {
				fmt.Fprintln(errw, "No MOCs found")
				return nil
			}
			fmt.Fprintln(errw, "Maps of Content")
			for _, m := range mocs {
				fmt.Fprintf(out, "%s\t%d\t%s\n", m.Name, m.ItemCount, m.Description)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}

func newMocsOrphanCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "orphan",
		Short: "List notes not linked from any MOC",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
			// Scope: Resources + Areas only (behavior inventory row #65).
			orphans, err := vault.Orphans(cfg.VaultDir, []string{cfg.Resources, cfg.Areas})
			if err != nil {
				return err
			}
			if len(orphans) == 0 {
				fmt.Fprintln(errw, "No orphaned notes")
				return nil
			}
			fmt.Fprintln(errw, "Orphaned notes (not linked from any MOC)")
			for _, n := range orphans {
				fmt.Fprintln(out, n.Rel)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}
