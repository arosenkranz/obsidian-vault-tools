// internal/llm/classify_test.go
package llm

import (
	"errors"
	"os/exec"
	"testing"
)

// DECIDE(new in v2, row #147): auth/login markers in stderr always
// classify as ErrAuth, independent of the underlying run error.
func TestClassifyAuthMarkers(t *testing.T) {
	cases := []string{
		"Error: not logged in",
		"please run `claude login` to continue",
		"Authentication required",
		"401 Unauthorized",
	}
	for _, stderr := range cases {
		err := Classify(errors.New("exit status 1"), stderr)
		if !errors.Is(err, ErrAuth) {
			t.Errorf("Classify(_, %q) = %v, want ErrAuth", stderr, err)
		}
	}
}

// DECIDE(new in v2, row #147): a plain *exec.ExitError without an auth
// marker classifies as a generic exit-code error, not ErrAuth.
func TestClassifyGenericExitError(t *testing.T) {
	cmd := exec.Command("false")
	runErr := cmd.Run()
	classified := Classify(runErr, "some unrelated stderr")
	if errors.Is(classified, ErrAuth) {
		t.Errorf("Classify unexpectedly matched ErrAuth: %v", classified)
	}
	if classified == nil {
		t.Fatal("expected a non-nil classified error")
	}
}
