// cmd/ov/main_test.go
package main

import (
	"os"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llmtest"
)

func TestMain(m *testing.M) {
	if llmtest.MaybeRunStub() {
		return // unreachable: MaybeRunStub calls os.Exit
	}
	os.Exit(m.Run())
}
