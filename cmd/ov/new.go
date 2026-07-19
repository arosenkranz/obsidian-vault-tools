// cmd/ov/new.go
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
	"github.com/arosenkranz/obsidian-vault-tools/internal/newnote"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/spf13/cobra"
)

// newTitleMaxLen mirrors mocs new's Resources-dir 80-char slug budget
// (row #3's split; v1's new_note had no length limit at all, but
// consolidating on "one Slugify everywhere" — row #58, closed for real
// by this task — requires picking a budget: Project/Meeting/Learning
// note titles are closer to MOC titles' deliberate, longer descriptive
// style than capture's quick-dump 60-char budget).
const newTitleMaxLen = 80

func newNewCmd() *cobra.Command {
	var vaultFlag string
	cmd := &cobra.Command{
		Use:   "new <type> <title>",
		Short: "Create a new note from a template (project|meeting|learning|general)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig(vaultFlag)
			if err != nil {
				return err
			}
			rel, err := runNew(cfg, args[0], args[1])
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

// runNew is the testable core of `ov new`: an empty title is an error
// (row #59). folderRel/content are resolved per type (row #59's fixed
// destinations: Project -> Projects root, Meeting -> Areas/Work,
// Learning -> Areas/Learning, General -> Inbox); the filename is built
// via vault.Slugify (row #58's fix — v1's third, disagreeing slug rule
// is retired here for real) routed through vault.ContainPath (defense
// in depth, same posture as mocs new's row #153 fix), and written via
// WriteNoteAtomic create-new mode — refused if the target already
// exists (mirrors row #99's family). v1's obsidian://open auto-open
// side effect is deliberately not ported (same DECIDE as row #7/#154 —
// cited, not re-litigated).
func runNew(cfg *config.Config, noteType, title string) (string, error) {
	if title == "" {
		return "", errors.New("title cannot be empty")
	}

	var folderRel, content string
	switch strings.ToLower(noteType) {
	case "project":
		folderRel = cfg.Projects
		content = newnote.Substitute(newnote.ProjectTemplate, title)
	case "meeting":
		folderRel = filepath.Join(cfg.Areas, "Work")
		content = newnote.Substitute(newnote.MeetingTemplate, title)
	case "learning":
		folderRel = filepath.Join(cfg.Areas, "Learning")
		content = newnote.Substitute(newnote.LearningTemplate, title)
	case "general":
		folderRel = cfg.Inbox
		content = newnote.Bare(title)
	default:
		return "", fmt.Errorf("unknown note type %q (want project, meeting, learning, or general)", noteType)
	}

	slug := vault.Slugify(title, newTitleMaxLen)
	targetAbs, err := vault.ContainPath(cfg.VaultDir, filepath.Join(folderRel, slug+".md"))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
		return "", err
	}
	if err := vault.WriteNoteAtomic(targetAbs, []byte(content), ""); err != nil {
		return "", err
	}

	// vault.ContainPath resolves symlinks on the root internally; Rel
	// must be computed against that SAME resolved root, else the
	// printed path can garble under a symlinked vault dir (the exact
	// bug class found and fixed in phase 4's post-review manual
	// testing — .superpowers/sdd/progress.md — applied here
	// proactively).
	vaultReal, err := filepath.EvalSymlinks(cfg.VaultDir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(vaultReal, targetAbs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}
