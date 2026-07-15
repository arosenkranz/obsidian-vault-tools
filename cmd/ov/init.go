package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/spf13/cobra"
)

const configTemplate = `# ov v2 config. The only required key is vault_dir.
vault_dir = ""

# PARA folder names (relative to vault). Defaults shown; uncomment to change.
# inbox = "00-Inbox"
# projects = "01-Projects"
# areas = "02-Areas"
# resources = "03-Resources"
# archive = "04-Archive"
# meta = "99-Meta"

# LLM command for triage. Prompt on stdin, response on stdout.
# llm_cmd = "claude --print"
# model = ""

# Publish targets (ov publish); host is your exe.dev VM.
# docs_host = ""
# docs_path = "/var/www/docs"
# docs_url = ""
`

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the config file if it does not exist",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := os.Getenv("OV_CONFIG")
			if path == "" {
				path = config.DefaultPath()
			}
			if _, err := os.Stat(path); err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "→ %s already exists, leaving alone\n", path)
				return nil
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(configTemplate), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ Created %s (set vault_dir)\n", path)
			return nil
		},
	}
}
