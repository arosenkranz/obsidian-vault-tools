// internal/moc/propose_test.go
package moc

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	response  string
	err       error
	gotPrompt string
}

func (f *fakeRunner) Run(ctx context.Context, prompt string) (string, error) {
	f.gotPrompt = prompt
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

// CONTRACT(#110,#112): ProposeCleanup builds the prompt, calls the
// runner, and decodes the JSON response into a typed Proposal.
func TestProposeCleanupDecodesResponse(t *testing.T) {
	runner := &fakeRunner{response: `{"new_content":"# MOC Test\n","duplicates_flagged":["a vs b"],"summary":"tidied"}`}
	p, err := ProposeCleanup(context.Background(), runner, "MOC Test.md", "# MOC Test\n\n- [[a]]\n", "MOC Test")
	if err != nil {
		t.Fatal(err)
	}
	if p.NewContent != "# MOC Test\n" || p.Summary != "tidied" || len(p.DuplicatesFlagged) != 1 {
		t.Errorf("Proposal = %+v", p)
	}
	if !strings.Contains(runner.gotPrompt, "MOC name: MOC Test") {
		t.Errorf("prompt missing moc name:\n%s", runner.gotPrompt)
	}
	if !strings.Contains(runner.gotPrompt, "- [[a]]") {
		t.Errorf("prompt missing moc content:\n%s", runner.gotPrompt)
	}
}

// CONTRACT(#112): duplicates_flagged/summary default to Go zero values
// when absent from the response.
func TestProposeCleanupDefaultsMissingFields(t *testing.T) {
	runner := &fakeRunner{response: `{"new_content":"content"}`}
	p, err := ProposeCleanup(context.Background(), runner, "MOC Test.md", "content", "MOC Test")
	if err != nil {
		t.Fatal(err)
	}
	if p.DuplicatesFlagged != nil || p.Summary != "" {
		t.Errorf("Proposal = %+v, want zero-value defaults", p)
	}
}

// Mirrors test_parse_llm_response_rejects_missing_new_content: a response
// missing new_content errors with the raw payload attached.
func TestProposeCleanupRejectsMissingNewContent(t *testing.T) {
	runner := &fakeRunner{response: `{"duplicates_flagged":[],"summary":"x"}`}
	_, err := ProposeCleanup(context.Background(), runner, "MOC Test.md", "content", "MOC Test")
	if err == nil || !strings.Contains(err.Error(), "new_content") {
		t.Errorf("err = %v, want mention of new_content", err)
	}
}

// CONTRACT(#92): a fenced-JSON response still decodes.
func TestProposeCleanupDecodesFencedResponse(t *testing.T) {
	runner := &fakeRunner{response: "```json\n{\"new_content\":\"c\"}\n```"}
	p, err := ProposeCleanup(context.Background(), runner, "x.md", "content", "MOC X")
	if err != nil {
		t.Fatal(err)
	}
	if p.NewContent != "c" {
		t.Errorf("NewContent = %q", p.NewContent)
	}
}

// A runner failure propagates so the caller can classify it (e.g. llm.ErrAuth).
func TestProposeCleanupPropagatesRunnerError(t *testing.T) {
	wantErr := errors.New("llm auth expired")
	runner := &fakeRunner{err: wantErr}
	_, err := ProposeCleanup(context.Background(), runner, "x.md", "content", "MOC X")
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}
