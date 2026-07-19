// cmd/ov/capture.go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/capture"
	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/arosenkranz/obsidian-vault-tools/internal/tui"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

type captureFlags struct {
	title, tags, source, moc string
}

// captureDeps injects every tty-dependent or side-effecting collaborator so
// runCapture is fully testable without a real terminal or network.
type captureDeps struct {
	stdinPiped  func() bool
	interactive func() bool
	pickMOC     func([]vault.MOC) (string, error)
	fetcher     capture.TitleFetcher
	now         func() time.Time
}

func newCaptureCmd() *cobra.Command {
	var vaultFlag string
	var flags captureFlags
	cmd := &cobra.Command{
		Use:   "capture [text]",
		Short: "Quick-dump a note into the inbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			deps := captureDeps{
				stdinPiped:  stdinPiped,
				interactive: interactiveTTY,
				pickMOC:     tui.RunMOCPicker,
				fetcher:     capture.NewHTTPTitleFetcher(),
				now:         time.Now,
			}
			return runCapture(cfg, flags, args, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), deps)
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	cmd.Flags().StringVar(&flags.title, "title", "", "explicit title (default: derived from first line)")
	cmd.Flags().StringVar(&flags.tags, "tags", "", "comma-separated tags")
	cmd.Flags().StringVar(&flags.source, "source", "cli", "source: cli | web | llm")
	cmd.Flags().StringVar(&flags.moc, "moc", "", "link note to a MOC by name (exact match, non-interactive)")
	return cmd
}

// runCapture is the testable core of ov capture: resolve the body
// (positional wins over piped stdin, row #44), resolve the MOC (flag, or an
// interactive picker gated on a real tty, row #136), and delegate to
// capture.Capture. Writes exactly one line to out (the captured note's
// vault-relative path, row #123 discipline extended to a write command);
// everything else goes to errw.
func runCapture(cfg *config.Config, flags captureFlags, args []string, in io.Reader, out, errw io.Writer, deps captureDeps) error {
	var body string
	if len(args) > 0 {
		body = strings.Join(args, " ")
	} else if deps.stdinPiped() {
		b, err := io.ReadAll(in)
		if err != nil {
			return err
		}
		body = string(b)
	} else {
		fmt.Fprintln(errw, "No content provided. Pass body as arg or pipe via stdin.")
		fmt.Fprintln(errw, "Try: ov capture --help")
		return errors.New("no content provided")
	}

	var tagList []string
	for _, t := range strings.Split(flags.tags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tagList = append(tagList, t)
		}
	}
	source := flags.source
	if source == "" {
		source = "cli"
	}

	mocName := flags.moc
	if mocName == "" && deps.interactive() {
		mocs, merr := vault.MOCs(cfg.VaultDir)
		if merr == nil && len(mocs) > 0 {
			chosen, perr := deps.pickMOC(mocs)
			if perr == nil {
				mocName = chosen
			} else if !errors.Is(perr, tui.ErrCancelled) {
				fmt.Fprintf(errw, "MOC picker error: %v\n", perr)
			}
		}
	}

	req := capture.Request{
		Body:       body,
		Title:      flags.title,
		Tags:       tagList,
		Source:     source,
		MOCName:    mocName,
		FetchTitle: true, // CLI always attempts a bare-URL fetch (row #46)
	}
	ccfg := capture.CaptureConfig{VaultDir: cfg.VaultDir, Inbox: cfg.Inbox, Resources: cfg.Resources}
	result, err := capture.Capture(context.Background(), ccfg, req, deps.fetcher, deps.now())
	if err != nil {
		return err
	}

	fmt.Fprintln(out, result.Rel)
	fmt.Fprintf(errw, "Captured: %s\n", result.Rel)
	if result.MOCLinked != "" {
		if result.MOCWarning != "" {
			fmt.Fprintln(errw, result.MOCWarning)
		} else {
			fmt.Fprintf(errw, "Added to [[%s]]\n", result.MOCLinked)
		}
	}
	return nil
}

// stdinPiped reports whether os.Stdin is a pipe/redirect rather than a real
// terminal — used to source the capture body (row #10/#44).
func stdinPiped() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

// interactiveTTY reports whether BOTH stdin and stdout are a real
// interactive terminal — the gate for launching the MOC picker (row #136).
func interactiveTTY() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}
