// cmd/ov/triage.go
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/arosenkranz/obsidian-vault-tools/internal/tui"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// triageDeps injects the tty-dependent picker/confirm collaborators so
// runTriage is fully testable without a real terminal.
type triageDeps struct {
	pickFolder func([]string) (string, error)
	confirm    func(string) (bool, error)
}

func newTriageCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "triage",
		Short: "Interactively process inbox notes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stdout.Fd()) {
				return errors.New("ov2 triage requires an interactive terminal")
			}
			deps := triageDeps{pickFolder: tui.RunFolderPicker, confirm: tui.Confirm}
			return runTriage(cfg, bufio.NewReader(cmd.InOrStdin()), cmd.ErrOrStderr(), deps)
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	return cmd
}

// runTriage is the testable core of ov2 triage: for each inbox note, read
// one key choice from in and act on it. deps.pickFolder/deps.confirm are
// injected so tests never open a real tty (production wires them to the
// bubbletea/huh implementations in internal/tui). Key map mirrors v1
// (behavior inventory row #57, #102): Enter/f = folder picker + move, s =
// skip, d = delete (with an explicit confirm, row #132), q = quit, EOF =
// quit.
func runTriage(cfg *config.Config, in *bufio.Reader, errw io.Writer, deps triageDeps) error {
	notes, err := vault.ListInbox(cfg.VaultDir, cfg.Inbox)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(errw, "No inbox directory found")
			return nil
		}
		return err
	}
	if len(notes) == 0 {
		fmt.Fprintln(errw, "Inbox is empty")
		return nil
	}
	for _, n := range notes {
		if _, err := os.Stat(n.Path); err != nil {
			continue // vanished mid-loop (walk resilience)
		}
		fmt.Fprintf(errw, "\n%s\n", n.Name)
		fmt.Fprint(errw, "  [Enter] Pick folder   [s] Skip   [d] Delete   [q] Quit   Choice: ")
		line, readErr := in.ReadString('\n')
		choice := strings.TrimSpace(line)
		if readErr != nil && line == "" {
			fmt.Fprintln(errw, "\ninterrupted")
			return nil
		}
		switch choice {
		case "q", "Q":
			fmt.Fprintln(errw, "Triage complete")
			return nil
		case "d", "D":
			ok, cerr := deps.confirm(fmt.Sprintf("Delete %q?", n.Name))
			if cerr != nil || !ok {
				fmt.Fprintln(errw, "  -> Skipped")
				continue
			}
			if rerr := os.Remove(n.Path); rerr != nil {
				fmt.Fprintf(errw, "  -> Delete failed: %v\n", rerr)
				continue
			}
			fmt.Fprintln(errw, "  -> Deleted")
		case "s", "S":
			fmt.Fprintln(errw, "  -> Skipped")
		case "", "f", "F":
			folders := vault.ListAllFolders(cfg.VaultDir, cfg.ParaRoots())
			if len(folders) == 0 {
				fmt.Fprintln(errw, "  -> No PARA folders found, skipped")
				continue
			}
			dest, perr := deps.pickFolder(folders)
			if perr != nil {
				fmt.Fprintln(errw, "  -> Skipped")
				continue
			}
			destAbs, eerr := vault.EnsureFolder(cfg.VaultDir, dest)
			if eerr != nil {
				fmt.Fprintf(errw, "  -> %v\n", eerr)
				continue
			}
			newPath, merr := vault.MoveNote(n.Path, destAbs)
			if merr != nil {
				fmt.Fprintf(errw, "  -> Move failed: %v\n", merr)
				continue
			}
			rel, _ := filepath.Rel(cfg.VaultDir, newPath)
			fmt.Fprintf(errw, "  -> Moved to %s\n", filepath.ToSlash(rel))
		default:
			fmt.Fprintln(errw, "  -> Invalid choice, skipped")
		}
	}
	fmt.Fprintln(errw, "\nTriage complete")
	return nil
}
