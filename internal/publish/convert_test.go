// internal/publish/convert_test.go
package publish

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

// CONTRACT(row #74): Convert builds the prompt, calls the runner, and
// decodes the response via llm.ExtractHTMLBlock (the <html>...</html>
// block when present).
func TestConvertExtractsHTMLBlock(t *testing.T) {
	runner := &fakeRunner{response: "Sure, here you go:\n<html><body>Hi</body></html>\nHope that helps!"}
	got, err := Convert(context.Background(), runner, "# Note\n", "guidance")
	if err != nil {
		t.Fatal(err)
	}
	if got != "<html><body>Hi</body></html>" {
		t.Errorf("got = %q", got)
	}
	if !strings.Contains(runner.gotPrompt, "# Note") {
		t.Errorf("prompt missing note content:\n%s", runner.gotPrompt)
	}
}

// CONTRACT(row #74): no <html> block present -> the raw response is
// used, trimmed.
func TestConvertFallsBackToRawResponse(t *testing.T) {
	runner := &fakeRunner{response: "  <div>no html tag here</div>  \n"}
	got, err := Convert(context.Background(), runner, "content", "guidance")
	if err != nil {
		t.Fatal(err)
	}
	if got != "<div>no html tag here</div>" {
		t.Errorf("got = %q", got)
	}
}

// A runner failure propagates so the caller can classify it (e.g.
// llm.ErrAuth).
func TestConvertPropagatesRunnerError(t *testing.T) {
	wantErr := errors.New("llm auth expired")
	runner := &fakeRunner{err: wantErr}
	_, err := Convert(context.Background(), runner, "content", "guidance")
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}
