// internal/web/main_test.go
package web

import (
	"os"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llmtest"
)

func TestMain(m *testing.M) {
	if llmtest.MaybeRunStub() {
		return
	}
	os.Exit(m.Run())
}
