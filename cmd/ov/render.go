// cmd/ov/render.go
package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/arosenkranz/obsidian-vault-tools/internal/render"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

func newRenderCmd() *cobra.Command {
	var vaultFlag string
	var all bool
	cmd := &cobra.Command{
		Use:   "render [<file>]",
		Short: "Regenerate HTML guide(s) from their paired Markdown source",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			var file string
			if len(args) == 1 {
				file = args[0]
			}
			return runRender(cfg, file, all, bufio.NewReader(cmd.InOrStdin()), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	cmd.Flags().BoolVar(&all, "all", false, "regenerate every paired HTML file")
	return cmd
}

// runRender is the testable core of `ov2 render`. Three modes, matching
// render_html.py's own dispatch order (rows #117-120): a <file>
// argument (resolved through vault.ContainPath — <file> IS meant to be
// vault-relative, unlike publish's arbitrary local path) regenerates
// exactly that one pair; --all regenerates every discovered pair; with
// neither, an interactive numbered picker mirrors v1's own non-gum
// picker (render_html.py:369-389) — plain stdin prompt, no bubbletea.
// Unlike v1's `all(regenerate(...) for ...)` (row #162's BUG: silently
// stops at the first failure while still claiming every pair was
// processed), --all and the "a" choice here always process every pair
// and report an accurate ok/failed count.
func runRender(cfg *config.Config, file string, all bool, in *bufio.Reader, out, errw io.Writer) error {
	pairs, err := render.FindPairedFiles(cfg.VaultDir)
	if err != nil {
		return err
	}

	regenOne := func(p render.Pair) error {
		if err := render.Regenerate(p, time.Now()); err != nil {
			fmt.Fprintf(errw, "✗ %s: %v\n", p.HTMLRel, err)
			return err
		}
		fmt.Fprintf(errw, "✓ Rendered: %s ← %s\n", p.HTMLRel, p.MDRel)
		fmt.Fprintln(out, p.HTMLRel)
		return nil
	}

	regenAll := func(targets []render.Pair) error {
		var ok, failed int
		for _, p := range targets {
			if err := regenOne(p); err != nil {
				failed++
			} else {
				ok++
			}
		}
		fmt.Fprintf(errw, "\nDone. %d file(s) processed (%d ok, %d failed).\n", len(targets), ok, failed)
		if failed > 0 {
			return fmt.Errorf("%d of %d file(s) failed to render", failed, len(targets))
		}
		return nil
	}

	if file != "" {
		target, err := vault.ContainPath(cfg.VaultDir, file)
		if err != nil {
			return err
		}
		for _, p := range pairs {
			if p.HTMLPath == target {
				return regenOne(p)
			}
		}
		return fmt.Errorf("no RENDER_SOURCE comment found in %s (or file not tracked)", file)
	}

	if all {
		if len(pairs) == 0 {
			fmt.Fprintln(errw, "No paired HTML files found in vault.")
			return nil
		}
		return regenAll(pairs)
	}

	if len(pairs) == 0 {
		fmt.Fprintln(errw, "No paired HTML files found in vault.")
		return nil
	}
	fmt.Fprintln(errw, "Paired HTML guides:")
	for i, p := range pairs {
		fmt.Fprintf(errw, "  [%d] %s\n       ← %s\n", i+1, p.HTMLRel, p.MDRel)
	}
	fmt.Fprint(errw, "\n  [a] All  [q] Quit\n\nRegenerate which? ")
	line, readErr := in.ReadString('\n')
	choice := strings.ToLower(strings.TrimSpace(line))
	if readErr != nil && choice == "" {
		choice = "q"
	}
	switch {
	case choice == "" || choice == "q":
		return nil
	case choice == "a":
		return regenAll(pairs)
	default:
		n, convErr := strconv.Atoi(choice)
		if convErr != nil || n < 1 || n > len(pairs) {
			return fmt.Errorf("invalid choice: %q", choice)
		}
		return regenOne(pairs[n-1])
	}
}
