package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate config, vault layout, and LLM command",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			cfg, err := config.Load("")
			if err != nil {
				return err
			}
			if vaultFlag != "" {
				// flag > env > file > default; flag values get the same
				// ~/$VAR expansion Load applies to the file/env value.
				cfg.VaultDir = config.ExpandPath(vaultFlag)
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			fmt.Fprintf(out, "vault      ok  %s\n", cfg.VaultDir)

			folders := append([]string{cfg.Inbox}, cfg.ParaRoots()...)
			folders = append(folders, cfg.Meta)
			for _, root := range folders {
				p := filepath.Join(cfg.VaultDir, root)
				if info, err := os.Stat(p); err == nil && info.IsDir() {
					fmt.Fprintf(out, "folder     ok  %s\n", root)
				} else {
					fmt.Fprintf(out, "folder   WARN  %s missing\n", root)
				}
			}

			argv0 := strings.Fields(cfg.LLMCmd)
			if len(argv0) == 0 {
				fmt.Fprintf(out, "llm      WARN  OV_LLM_CMD empty\n")
			} else if path, err := exec.LookPath(argv0[0]); err != nil {
				fmt.Fprintf(out, "llm      WARN  %q not found on PATH\n", argv0[0])
			} else {
				fmt.Fprintf(out, "llm        ok  %s\n", path)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}
