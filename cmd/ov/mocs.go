package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
	"github.com/arosenkranz/obsidian-vault-tools/internal/moc"
	"github.com/arosenkranz/obsidian-vault-tools/internal/tui"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

func newMocsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mocs",
		Short: "Maps of Content",
	}
	cmd.AddCommand(newMocsListCmd(), newMocsOrphanCmd(), newMocsNewCmd(), newMocsAddCmd(), newMocsCleanupCmd())
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

// runMocsNew is the testable core of `ov mocs new`: an empty title is an
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
	// vault.ContainPath resolves symlinks on the root internally (e.g.
	// macOS /tmp -> /private/tmp, iCloud Drive / CloudStorage vault
	// paths) and returns targetAbs built from that RESOLVED root —
	// computing Rel against the raw, unresolved cfg.VaultDir would
	// otherwise print a "../../private/tmp/..."-style garbage path
	// instead of a clean vault-relative one (same fix class as
	// triage.Validate's PARA-root gate, phase 3 Task 4). The file
	// itself is unaffected (targetAbs is already correct) — only the
	// machine-readable stdout path (row #123 discipline) was wrong.
	vaultDirReal, err := filepath.EvalSymlinks(cfg.VaultDir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(vaultDirReal, targetAbs)
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

// runMocsAdd is the testable core of `ov mocs add`: resolves mocName via
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

// mocCleanupDeps injects the LLM runner so runMocsCleanup is fully
// testable without spawning a real subprocess (matches llmTriageDeps'
// injection pattern in triage.go).
type mocCleanupDeps struct {
	runner moc.Runner
}

// mocCleanupTimeout is 180s, not triage's 120s (row #90) — a cmd/ov-
// local constant; internal/llm.Runner.Run is timeout-agnostic via the
// caller-supplied context.
const mocCleanupTimeout = 180 * time.Second

func newMocsCleanupCmd() *cobra.Command {
	var vaultFlag string
	var all bool
	cmd := &cobra.Command{
		Use:   "cleanup [name]",
		Short: "LLM-assisted MOC reorganization (suggest-only, diff+confirm)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			runner := llm.NewRunner(cfg.LLMCmd, cfg.Model)
			deps := mocCleanupDeps{runner: runner}
			return runMocsCleanup(cfg, name, all, bufio.NewReader(cmd.InOrStdin()), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps)
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	cmd.Flags().BoolVar(&all, "all", false, "process every MOC in the vault")
	return cmd
}

// runMocsCleanup is the testable core of `ov mocs cleanup`: name XOR
// --all, else exit 2 (row #114); resolves target(s) via
// vault.FindMOCByName (single) or vault.MOCs (all, row #156) — no
// targets resolved also exits 2 (row #113). For each target: read once
// (content + hash, row #106), ProposeCleanup (180s timeout, row #90),
// Validate (rows #107-109) — a rejection reports the reason and
// continues to the next target, never a partial apply (row #109); an
// identical proposal is reported "unchanged" BEFORE any diff/confirm is
// shown (row #157) — Apply is never even called for this case; otherwise
// the summary/duplicates are printed, the diff is rendered
// (tui.RenderDiff(moc.Diff(...))), and an explicit y/N confirm gates the
// write (EOF = "n" for THIS target only, row #116 — unlike triage's
// abort-on-EOF). moc.Apply writes using the hash captured at the initial
// read (not a second re-read), which still detects any edit made during
// the LLM call or human think time — WriteNoteAtomic itself re-reads and
// compares immediately before rename (row #106's mechanism). Ends with
// a five-bucket summary (applied/skipped/unchanged/rejected/errored,
// mirroring moc_cleanup.py's own counts dict).
func runMocsCleanup(cfg *config.Config, name string, all bool, in *bufio.Reader, out, errw io.Writer, deps mocCleanupDeps) error {
	if (name == "") == !all {
		return fmt.Errorf("%w: pass a MOC name or --all, not both", errExitCode2)
	}

	var targets []vault.MOC
	if all {
		mocs, err := vault.MOCs(cfg.VaultDir)
		if err != nil {
			return err
		}
		targets = mocs
	} else {
		m, err := vault.FindMOCByName(cfg.VaultDir, cfg.Resources, name)
		if err != nil {
			return fmt.Errorf("%w: no MOC found matching %q", errExitCode2, name)
		}
		targets = []vault.MOC{*m}
	}
	if len(targets) == 0 {
		return fmt.Errorf("%w: no MOC found", errExitCode2)
	}

	var counts struct{ applied, skipped, unchanged, rejected, errored int }

	for _, target := range targets {
		fmt.Fprintf(errw, "\n📄 %s\n", target.Name)

		original, hash, err := vault.ReadNote(target.Path)
		if err != nil {
			fmt.Fprintf(errw, "  error reading file: %v\n", err)
			counts.errored++
			continue
		}

		fmt.Fprintln(errw, "  thinking…")
		ctx, cancel := context.WithTimeout(context.Background(), mocCleanupTimeout)
		proposal, err := moc.ProposeCleanup(ctx, deps.runner, target.Path, original, target.Name)
		cancel()
		if err != nil {
			fmt.Fprintf(errw, "  LLM call failed: %v\n", err)
			counts.errored++
			continue
		}

		if err := moc.Validate(original, proposal.NewContent); err != nil {
			fmt.Fprintf(errw, "  ✗ rejected proposal: %v\n", err)
			counts.rejected++
			continue
		}

		if proposal.NewContent == original {
			fmt.Fprintln(errw, "  ✓ already well-organized, no changes proposed")
			counts.unchanged++
			continue
		}

		if proposal.Summary != "" {
			fmt.Fprintf(errw, "  summary: %s\n", proposal.Summary)
		}
		if len(proposal.DuplicatesFlagged) > 0 {
			fmt.Fprintln(errw, "  ⚠ possible duplicates (not merged, review manually):")
			for _, d := range proposal.DuplicatesFlagged {
				fmt.Fprintf(errw, "    - %s\n", d)
			}
		}
		fmt.Fprintln(errw)
		fmt.Fprintln(errw, tui.RenderDiff(moc.Diff(original, proposal.NewContent)))
		fmt.Fprintln(errw)
		fmt.Fprint(errw, "Apply this reorganization? [y/N] ")
		line, readErr := in.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if readErr != nil && answer == "" {
			answer = "n" // EOF -> no, for THIS target only (row #116)
		}

		if answer != "y" {
			fmt.Fprintln(errw, "  → skipped, no changes written")
			counts.skipped++
			continue
		}
		if err := moc.Apply(target.Path, original, proposal.NewContent, hash); err != nil {
			fmt.Fprintf(errw, "  apply failed: %v\n", err)
			counts.errored++
			continue
		}
		fmt.Fprintf(errw, "  ✓ applied → %s\n", target.Rel)
		counts.applied++
	}

	fmt.Fprintln(errw)
	fmt.Fprintln(errw, "cleanup summary")
	fmt.Fprintf(errw, "  applied    %d\n", counts.applied)
	fmt.Fprintf(errw, "  skipped    %d\n", counts.skipped)
	fmt.Fprintf(errw, "  unchanged  %d\n", counts.unchanged)
	fmt.Fprintf(errw, "  rejected   %d\n", counts.rejected)
	fmt.Fprintf(errw, "  errored    %d\n", counts.errored)
	return nil
}
