// cmd/ov/triage.go
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
	"github.com/arosenkranz/obsidian-vault-tools/internal/triage"
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

// errExitCode2 is a sentinel wrapped into the error returned by
// runLLMTriage's two AGENTS.md-precondition checks. main.go recognizes it
// and exits the process with code 2 instead of the default 1 (row #104:
// this triage --llm mode intentionally diverges from the exit-1
// convention used elsewhere in ov, mirroring v1 triage_llm.py's own exit
// code for the binary it replaces — manual, non-llm triage keeps its
// existing phase-2 exit-0-on-missing-inbox behavior unchanged, an
// unrelated code path).
var errExitCode2 = errors.New("triage --llm precondition failed")

// llmTriageDeps injects the LLM runner and the tty-dependent confirm
// collaborator so runLLMTriage is fully testable without a real tty or
// subprocess. agentsMD is the vault's AGENTS.md content, read once by the
// caller (newTriageCmd's RunE) and reused across every note in the run.
type llmTriageDeps struct {
	runner   triage.Runner
	confirm  func(string) (bool, error)
	agentsMD string
}

const llmTriageTimeout = 120 * time.Second

// runLLMTriage is the testable core of `ov triage --llm`: for each inbox
// note (up to limit, 0 = unlimited), call triage.Propose, render a diff
// via triage.Apply(..., dryRun=true) + tui.RenderDiff, and either show it
// (dryRun) or drive the a/e/s/d/r/q approval loop (row #102). Missing
// AGENTS.md or a missing inbox directory both return an error wrapping
// errExitCode2 (row #104) before any note is processed.
func runLLMTriage(cfg *config.Config, in *bufio.Reader, out, errw io.Writer, deps llmTriageDeps, dryRun bool, limit int) error {
	agentsPath := filepath.Join(cfg.VaultDir, "AGENTS.md")
	if _, err := os.Stat(agentsPath); err != nil {
		return fmt.Errorf("%w: AGENTS.md not found at vault root", errExitCode2)
	}
	inboxPath := filepath.Join(cfg.VaultDir, cfg.Inbox)
	if info, err := os.Stat(inboxPath); err != nil || !info.IsDir() {
		return fmt.Errorf("%w: %s not found", errExitCode2, cfg.Inbox)
	}

	notes, err := vault.ListInbox(cfg.VaultDir, cfg.Inbox)
	if err != nil {
		return err
	}
	if limit > 0 && len(notes) > limit {
		notes = notes[:limit]
	}
	if len(notes) == 0 {
		fmt.Fprintln(errw, "✓ Inbox is empty")
		return nil
	}

	tcfg := triage.Config{
		VaultDir:  cfg.VaultDir,
		Inbox:     cfg.Inbox,
		Projects:  cfg.Projects,
		Areas:     cfg.Areas,
		Resources: cfg.Resources,
		Archive:   cfg.Archive,
	}

	for _, n := range notes {
		if _, err := os.Stat(n.Path); err != nil {
			continue // vanished mid-loop (walk resilience)
		}
		content, _, err := vault.ReadNote(n.Path)
		if err != nil {
			fmt.Fprintf(errw, "error reading %s: %v\n", n.Name, err)
			continue
		}

		var proposal triage.Proposal
		haveProposal := false
	inner:
		for {
			if !haveProposal {
				fmt.Fprintf(errw, "\n📥 %s\n", n.Name)
				fmt.Fprintln(errw, "  thinking…")
				ctx, cancel := context.WithTimeout(context.Background(), llmTriageTimeout)
				p, err := triage.Propose(ctx, tcfg, n, deps.agentsMD, deps.runner)
				cancel()
				if err != nil {
					fmt.Fprintf(errw, "  LLM call failed: %v\n", err)
					break inner
				}
				proposal = p
				haveProposal = true
			}

			preview, applyErr := triage.Apply(tcfg, n, proposal, time.Now(), true)
			fmt.Fprintf(errw, "\n🤖 LLM proposal → %s\n", proposal.To)
			fmt.Fprintf(errw, "  title:       %s\n", proposal.NewTitle)
			fmt.Fprintf(errw, "  confidence:  %s\n", proposal.Confidence)
			fmt.Fprintf(errw, "  rationale:   %s\n", proposal.Rationale)
			if applyErr != nil {
				fmt.Fprintf(errw, "  rejected: %v\n", applyErr)
				break inner
			}
			fmt.Fprintln(errw, tui.RenderDiff(triage.Diff(content, preview.Content)))

			if dryRun {
				break inner
			}

			fmt.Fprint(errw, "  [a]pprove  [e]dit  [s]kip  [d]elete  [r]e-ask  [q]uit ?  ")
			line, readErr := in.ReadString('\n')
			choice := strings.TrimSpace(line)
			if readErr != nil && line == "" {
				fmt.Fprintln(errw, "\ninterrupted")
				return nil
			}
			if choice == "" {
				choice = "s"
			}
			switch choice[0] {
			case 'a':
				res, err := triage.Apply(tcfg, n, proposal, time.Now(), false)
				if err != nil {
					fmt.Fprintf(errw, "  apply failed: %v\n", err)
				} else {
					fmt.Fprintf(errw, "  ✓ filed → %s\n", res.Target)
					if res.MOCWarning != "" {
						fmt.Fprintf(errw, "  ⚠ %s\n", res.MOCWarning)
					} else if res.MOCSynced {
						fmt.Fprintln(errw, "  ✓ MOC link updated")
					}
				}
				break inner
			case 'e':
				proposal = editProposalFields(in, errw, proposal)
				continue inner
			case 's':
				fmt.Fprintln(errw, "  → skipped")
				break inner
			case 'd':
				ok, cerr := deps.confirm(fmt.Sprintf("Delete %q?", n.Name))
				if cerr != nil || !ok {
					fmt.Fprintln(errw, "  → cancelled")
					break inner
				}
				if err := os.Remove(n.Path); err != nil {
					fmt.Fprintf(errw, "  delete failed: %v\n", err)
				} else {
					fmt.Fprintln(errw, "  → deleted")
				}
				break inner
			case 'r':
				haveProposal = false
				continue inner
			case 'q':
				fmt.Fprintln(errw, "  → quit")
				return nil
			default:
				fmt.Fprintln(errw, "  → invalid choice, skipped")
				break inner
			}
		}
	}
	fmt.Fprintln(errw, "\nTriage complete")
	return nil
}

// editProposalFields is the v2 simplification of v1's edit_proposal (row
// #103 DECIDE: the phase 3 TUI redesign supersedes v1's exact prompts; the
// same field-by-field affordance is kept). Only "to" and "new_title" are
// editable — frontmatter_patch editing is deliberately left to a future
// polish pass (not part of this phase's acceptance criteria); an empty
// input line keeps the current value.
func editProposalFields(in *bufio.Reader, errw io.Writer, p triage.Proposal) triage.Proposal {
	fmt.Fprint(errw, "  to ["+p.To+"]: ")
	if line, _ := in.ReadString('\n'); strings.TrimSpace(line) != "" {
		p.To = strings.TrimSpace(line)
	}
	fmt.Fprint(errw, "  new_title ["+p.NewTitle+"]: ")
	if line, _ := in.ReadString('\n'); strings.TrimSpace(line) != "" {
		p.NewTitle = strings.TrimSpace(line)
	}
	return p
}

func newTriageCmd() *cobra.Command {
	var vaultFlag string
	var llmMode, dryRun bool
	var limit int
	cmd := &cobra.Command{
		Use:   "triage",
		Short: "Interactively process inbox notes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			if llmMode {
				agentsMD, err := os.ReadFile(filepath.Join(cfg.VaultDir, "AGENTS.md"))
				if err != nil {
					return fmt.Errorf("%w: AGENTS.md not found at vault root", errExitCode2)
				}
				runner := llm.NewRunner(cfg.LLMCmd, cfg.Model)
				deps := llmTriageDeps{runner: runner, confirm: tui.Confirm, agentsMD: string(agentsMD)}
				return runLLMTriage(cfg, bufio.NewReader(cmd.InOrStdin()), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps, dryRun, limit)
			}
			if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stdout.Fd()) {
				return errors.New("ov triage requires an interactive terminal")
			}
			deps := triageDeps{pickFolder: tui.RunFolderPicker, confirm: tui.Confirm}
			return runTriage(cfg, bufio.NewReader(cmd.InOrStdin()), cmd.ErrOrStderr(), deps)
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	cmd.Flags().BoolVar(&llmMode, "llm", false, "use LLM-assisted triage instead of manual folder picking")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show every LLM proposal (with diff); write nothing (--llm only)")
	cmd.Flags().IntVar(&limit, "limit", 0, "process at most N notes (--llm only; 0 = unlimited)")
	return cmd
}

// runTriage is the testable core of ov triage: for each inbox note, read
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
