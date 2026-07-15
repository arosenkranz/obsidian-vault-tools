package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Config utilities"}

	var from string
	migrate := &cobra.Command{
		Use:   "migrate",
		Short: "Print TOML converted from the old bash-style config",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := from
			if path == "" {
				path = os.Getenv("OV_CONFIG")
			}
			if path == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				path = filepath.Join(home, ".config", "ov", "config") // OLD default
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read old config: %w", err)
			}
			kv := config.ParseLegacy(string(data))
			if len(kv) == 0 {
				return fmt.Errorf("no OV_* keys found in %s — is this already a v2 TOML config?", path)
			}
			fmt.Fprint(cmd.OutOrStdout(), config.RenderTOML(kv))
			return nil
		},
	}
	migrate.Flags().StringVar(&from, "from", "", "path to the old bash-style config")
	cmd.AddCommand(migrate)
	return cmd
}
