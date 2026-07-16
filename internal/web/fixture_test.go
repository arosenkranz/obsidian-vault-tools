// internal/web/fixture_test.go
package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// newTestVault creates a temp vault with the standard PARA dirs and a
// matching Config, mirroring cmd/ov/fixture_test.go's newVaultFixture
// pattern (design spec §Testing strategy tier 3) — duplicated in miniature
// here because internal/web cannot import cmd/ov's unexported test helper
// across package boundaries.
func newTestVault(t *testing.T) (vaultDir string, cfg Config) {
	t.Helper()
	vaultDir = t.TempDir()
	for _, d := range []string{"00-Inbox", "01-Projects", "02-Areas", "03-Resources", "04-Archive", "99-Meta"} {
		if err := os.MkdirAll(filepath.Join(vaultDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "AGENTS.md"), []byte("test AGENTS.md contract"), 0o644); err != nil {
		t.Fatal(err)
	}
	return vaultDir, Config{
		VaultDir: vaultDir, Inbox: "00-Inbox", Resources: "03-Resources",
		Projects: "01-Projects", Areas: "02-Areas", Archive: "04-Archive",
		AgentsMD: "test AGENTS.md contract", Bind: "127.0.0.1:4173",
	}
}

type stubFetcher struct {
	title string
	err   error
}

func (f stubFetcher) FetchTitle(ctx context.Context, url string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.title, nil
}

// fakeLLMRunner is an llmRunner used by every test in this package that
// doesn't specifically exercise the real stub-subprocess path (Task 2's
// pattern: fast fake for unit tests, real internal/llmtest stub for
// integration tests proving the actual transport).
type fakeLLMRunner struct {
	response  string
	err       error
	healthErr error
}

func (f *fakeLLMRunner) Run(ctx context.Context, prompt string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

func (f *fakeLLMRunner) HealthCheck(ctx context.Context) error {
	return f.healthErr
}
