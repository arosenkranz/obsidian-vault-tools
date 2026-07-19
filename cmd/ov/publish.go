// cmd/ov/publish.go
package main

import (
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
	"github.com/arosenkranz/obsidian-vault-tools/internal/publish"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

// publishDeps injects the LLM runner (nil unless --llm) and the rsync
// pusher so runPublish is fully testable without a real subprocess or
// network call (mirrors llmTriageDeps/mocCleanupDeps' injection
// pattern).
type publishDeps struct {
	runner publish.Runner
	pusher publish.Pusher
}

// publishLLMTimeout is 180s-class, matching moc-cleanup's HTML/text
// generation budget rather than triage's 120s (HTML generation is
// comparably heavy) — a cmd/ov-local constant, not shared with
// mocCleanupTimeout/llmTriageTimeout.
const publishLLMTimeout = 180 * time.Second

// publishPushTimeout bounds the rsync push itself. v1 had no timeout on
// rsync at all (DECIDE, new in v2) — published notes/HTML files are
// small, 60s is generous.
const publishPushTimeout = 60 * time.Second

func newPublishCmd() *cobra.Command {
	var vaultFlag string
	var useLLM bool
	var desc string
	cmd := &cobra.Command{
		Use:   "publish <file>",
		Short: "Publish a note to the docs server (optionally LLM-converted to HTML)",
		// row #158 DECIDE: an explicit file argument is required — no
		// interactive picker (v1's gum picker walked the whole vault for
		// *.md, an unbounded candidate set for a plain numbered list).
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			var runner publish.Runner
			if useLLM {
				runner = llm.NewRunner(cfg.LLMCmd, cfg.Model)
			}
			deps := publishDeps{runner: runner, pusher: publish.RsyncPusher{}}
			return runPublish(cfg, args[0], useLLM, desc, cmd.ErrOrStderr(), deps)
		},
	}
	cmd.Flags().StringVar(&vaultFlag, "vault", "", "override vault directory")
	cmd.Flags().BoolVar(&useLLM, "llm", false, "convert a .md note to a self-contained HTML file via LLM before publishing")
	cmd.Flags().StringVar(&desc, "desc", "", "design guidance for --llm (default: clean, modern design)")
	return cmd
}

// runPublish is the testable core of `ov2 publish`. Requires
// cfg.DocsHost, else error+exit 1 — NOT exit 2 (row #68, matching v1's
// own exit code for this command, distinct from triage/mocs-cleanup's
// errExitCode2 sentinel). A .md file without --llm refuses with a hint
// (row #70); --llm on a non-.md file warns and publishes as-is (row
// #71, case-sensitive extension match exactly like v1's
// `[ "$ext" = "md" ]`). The --llm path calls the LLM via deps.runner
// (argv-exec, never a shell — row #72's fix) and decodes with
// publish.Convert/llm.ExtractHTMLBlock (row #74). The output slug is
// publish.Slug's lowercase-hyphenated rule (row #73, distinct from
// vault.Slugify), written to $VAULT_DIR/Published/<slug>.html via a
// conditional vault.WriteNoteAtomic — create-new on first publish,
// hash-conditional overwrite on republish (row #160's fix: v1's plain
// `printf > file` was non-atomic). The final push is always an rsync
// (row #75), printing the live URL when cfg.DocsURL is set.
func runPublish(cfg *config.Config, file string, useLLM bool, desc string, errw io.Writer, deps publishDeps) error {
	if cfg.DocsHost == "" {
		return errors.New("OV_DOCS_HOST not set: add docs_host to your config (see examples/ov.config.example)")
	}

	info, err := os.Stat(file)
	if err != nil {
		return fmt.Errorf("file not found: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s: is a directory", file)
	}

	ext := filepath.Ext(file)
	publishFile := file

	if ext == ".md" && !useLLM {
		return fmt.Errorf("%s is a markdown file — use --llm to convert it to HTML first: ov2 publish %q --llm", file, file)
	}
	if useLLM && ext != ".md" {
		fmt.Fprintf(errw, "⚠ --llm ignored: file is already %s, publishing as-is\n", strings.TrimPrefix(ext, "."))
		useLLM = false
	}

	if useLLM {
		content, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		fmt.Fprintln(errw, "🤖 Converting with LLM...")
		ctx, cancel := context.WithTimeout(context.Background(), publishLLMTimeout)
		html, err := publish.Convert(ctx, deps.runner, string(content), desc)
		cancel()
		if err != nil {
			return fmt.Errorf("llm conversion failed: %w", err)
		}

		stem := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		slug := publish.Slug(stem)
		outAbs, err := vault.ContainPath(cfg.VaultDir, filepath.Join("Published", slug+".html"))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(outAbs), 0o755); err != nil {
			return err
		}
		newContent := html + "\n"
		var expectedHash string
		if _, hash, rerr := vault.ReadNote(outAbs); rerr == nil {
			expectedHash = hash // republish: conditional overwrite, not create-new
		}
		if err := vault.WriteNoteAtomic(outAbs, []byte(newContent), expectedHash); err != nil {
			return err
		}
		fmt.Fprintf(errw, "✓ HTML saved: %s\n", outAbs)
		publishFile = outAbs
	}

	filename := filepath.Base(publishFile)
	fmt.Fprintf(errw, "📤 Publishing %s...\n", filename)
	ctx, cancel := context.WithTimeout(context.Background(), publishPushTimeout)
	err = deps.pusher.Push(ctx, publishFile, cfg.DocsHost, cfg.DocsPath)
	cancel()
	if err != nil {
		return fmt.Errorf("rsync push failed: %w", err)
	}

	if cfg.DocsURL != "" {
		fmt.Fprintf(errw, "\n✓ Live at: %s/%s\n", strings.TrimSuffix(cfg.DocsURL, "/"), filename)
	} else {
		fmt.Fprintf(errw, "\n✓ Published to %s:%s/%s\n", cfg.DocsHost, strings.TrimSuffix(cfg.DocsPath, "/"), filename)
	}
	return nil
}
