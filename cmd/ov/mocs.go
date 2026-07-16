package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

func newMocsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mocs",
		Short: "Maps of Content",
	}
	cmd.AddCommand(newMocsListCmd(), newMocsOrphanCmd(), newMocsNewCmd(), newMocsAddCmd())
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

func newMocsNewCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a new MOC from the skeleton template",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			rel, err := runMocsNew(cfg, args[0], time.Now())
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), rel)
			return nil
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}

// runMocsNew is the testable core of `ov2 mocs new`: an empty title is an
// error (row #64). The filename is built via a slugified title routed
// through vault.ContainPath (row #153's traversal fix — v1 interpolated
// the raw title unsanitized) and refused if a MOC already exists there
// (WriteNoteAtomic's ErrExists, create-new mode). The visible content
// keeps the CR/LF-stripped-only raw title (row #153). v1's auto-open
// side effect (row #7/#154) is deliberately not ported.
func runMocsNew(cfg *config.Config, title string, now time.Time) (string, error) {
	if title == "" {
		return "", errors.New("MOC title cannot be empty")
	}
	clean := strings.NewReplacer("\r", "", "\n", "").Replace(title)
	slug := vault.Slugify(clean, 80)
	filename := "MOC " + slug + ".md"
	targetAbs, err := vault.ContainPath(cfg.VaultDir, filepath.Join(cfg.Resources, filename))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
		return "", err
	}
	content := vault.NewMOCSkeleton(clean, now)
	if err := vault.WriteNoteAtomic(targetAbs, []byte(content), ""); err != nil {
		return "", err
	}
	rel, err := filepath.Rel(cfg.VaultDir, targetAbs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func newMocsAddCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "add <moc-name> <note-name>",
		Short: "Add a note entry to a MOC's Key Notes section",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			rel, err := runMocsAdd(cfg, args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), rel)
			return nil
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}

// runMocsAdd is the testable core of `ov2 mocs add`: resolves mocName via
// vault.FindMOCByName (row #33), sanitizes noteName (row #155 — free
// text, never validated as a real note, matching v1's DECIDE), inserts
// "- [[noteName]]" under "## Key Notes" or appends at EOF (row #66), and
// writes via a conditional WriteNoteAtomic (re-read + hash immediately
// before write, row #106's discipline applied to every MOC mutation).
func runMocsAdd(cfg *config.Config, mocName, noteName string) (string, error) {
	moc, err := vault.FindMOCByName(cfg.VaultDir, cfg.Resources, mocName)
	if err != nil {
		return "", err
	}
	clean := vault.SanitizeWikilinkText(noteName)
	if clean == "" {
		return "", errors.New("note name cannot be empty")
	}
	content, hash, err := vault.ReadNote(moc.Path)
	if err != nil {
		return "", err
	}
	newContent := vault.InsertUnderHeading(content, "## Key Notes", "- [["+clean+"]]")
	if err := vault.WriteNoteAtomic(moc.Path, []byte(newContent), hash); err != nil {
		return "", err
	}
	return moc.Rel, nil
}
