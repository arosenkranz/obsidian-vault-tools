// internal/capture/capture.go
package capture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// CaptureConfig is the narrow subset of ov config this package needs — kept
// separate from internal/config.Config so this package stays a thin,
// dependency-light core verb (design spec's "stateless verbs" principle).
type CaptureConfig struct {
	VaultDir  string
	Inbox     string
	Resources string
}

// Request is the frontend-agnostic capture input — same shape whether it
// came from CLI flags or a web form.
type Request struct {
	Body       string
	Title      string // explicit; empty triggers auto-derivation
	Tags       []string
	Source     string
	MOCName    string // explicit MOC name; "" = no MOC link
	FetchTitle bool   // whether to attempt a bare-URL title fetch (CLI: always true, row #46; web: opt-in checkbox, row #135)
}

// Result reports what Capture did, for the frontend to render.
type Result struct {
	Path       string // absolute path of the written note
	Rel        string // vault-relative path
	Title      string // resolved title (post-slugify)
	MOCLinked  string // MOC name if linked, else ""
	MOCWarning string // non-fatal MOC update failure message, row #55
}

var trailingWordRe = regexp.MustCompile(`\s+\S*$`)

// Capture is the core capture verb: derive title, resolve MOC, stamp a
// filename, write the note via WriteNoteAtomic, and best-effort append a
// MOC entry. Never aborts on a MOC-update failure (row #55) — the note is
// already safely on disk by the time the MOC is touched. Both cmd/ov
// capture and internal/web's capture handler call this same function.
func Capture(ctx context.Context, cfg CaptureConfig, req Request, fetcher TitleFetcher, now time.Time) (Result, error) {
	body := strings.TrimRight(req.Body, " \t\n\r")
	if body == "" {
		return Result{}, errors.New("empty body, refusing to capture")
	}

	firstLine := firstNonEmptyLine(body)
	urlTitle := ""
	if req.FetchTitle && IsBareURL(firstLine) && fetcher != nil {
		if t, err := fetcher.FetchTitle(ctx, strings.TrimSpace(firstLine)); err == nil {
			urlTitle = t
		}
	}

	title := req.Title
	if title == "" {
		if urlTitle != "" {
			title = urlTitle
		} else {
			title = firstLine
		}
	}
	title = vault.Slugify(title, 60)

	var moc *vault.MOC
	if req.MOCName != "" {
		m, err := vault.FindMOCByName(cfg.VaultDir, cfg.Resources, req.MOCName)
		if err != nil {
			return Result{}, fmt.Errorf("MOC not found: %s", req.MOCName)
		}
		moc = m
	}

	snippet := firstNonEmptyLine(body)
	if IsBareURL(firstLine) && urlTitle != "" {
		snippet = urlTitle
	}
	snippet = truncateSnippet(snippet, IsBareURL(firstLine))

	inboxDir := filepath.Join(cfg.VaultDir, cfg.Inbox)
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		return Result{}, err
	}
	stamp := now.Format("2006-01-02 1504")
	stem := stamp + " " + title
	path, _, err := NextAvailablePath(inboxDir, stem, ".md")
	if err != nil {
		return Result{}, err
	}

	mocName := ""
	if moc != nil {
		mocName = moc.Name
	}
	content := BuildNote(Params{
		Title:   title,
		Body:    body,
		Tags:    req.Tags,
		Source:  req.Source,
		MOCName: mocName,
		Created: now.Format("2006-01-02"),
	})
	if err := vault.WriteNoteAtomic(path, []byte(content), ""); err != nil {
		return Result{}, err
	}

	rel, _ := filepath.Rel(cfg.VaultDir, path)
	result := Result{Path: path, Rel: filepath.ToSlash(rel), Title: title}

	if moc != nil {
		result.MOCLinked = moc.Name
		if err := appendMOCConditional(moc.Path, title, snippet); err != nil {
			result.MOCWarning = fmt.Sprintf("captured, but failed to update MOC %s: %v", moc.Name, err)
		}
	}
	return result, nil
}

// appendMOCConditional re-reads the MOC (hash-conditional per design spec
// §core contracts) immediately before writing, so a concurrent Obsidian
// Sync edit surfaces as a refusal rather than a silent clobber.
func appendMOCConditional(mocPath, title, snippet string) error {
	content, hash, err := vault.ReadNote(mocPath)
	if err != nil {
		return err
	}
	newContent := vault.AppendMOCEntry(content, title, snippet)
	return vault.WriteNoteAtomic(mocPath, []byte(newContent), hash)
}

func firstNonEmptyLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

// truncateSnippet mirrors v1: >60 chars gets word-boundary truncation plus
// "..."; bare URLs are never truncated mid-path (row #48).
func truncateSnippet(s string, isURL bool) string {
	if len([]rune(s)) <= 60 || isURL {
		return s
	}
	r := []rune(s)[:60]
	truncated := trailingWordRe.ReplaceAllString(string(r), "")
	return strings.TrimSpace(truncated) + "..."
}
