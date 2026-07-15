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
			fmt.Fprint(cmd.OutOrStdout(), config.RenderTOML(config.ParseLegacy(string(data))))
			return nil
		},
	}
	migrate.Flags().StringVar(&from, "from", "", "path to the old bash-style config")
	cmd.AddCommand(migrate)
	return cmd
}
