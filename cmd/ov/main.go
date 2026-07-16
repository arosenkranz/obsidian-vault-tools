package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ov2:", err)
		if errors.Is(err, errExitCode2) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
