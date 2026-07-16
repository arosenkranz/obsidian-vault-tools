package main

import "github.com/arosenkranz/obsidian-vault-tools/internal/config"

// resolveConfig loads config, applies a --vault override (with ~/$VAR
// expansion), and validates that the vault directory exists. Shared by the
// read-only commands; mirrors doctor.go's load sequence.
func resolveConfig(vaultFlag string) (*config.Config, error) {
	cfg, err := config.Load("")
	if err != nil {
		return nil, err
	}
	if vaultFlag != "" {
		cfg.VaultDir = config.ExpandPath(vaultFlag)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
