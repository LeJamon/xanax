// Command rvr is a terminal-first session manager for autonomous AI
// coding agents. See SPEC.md for the v1 design.
package main

import (
	"fmt"
	"os"

	"github.com/LeJamon/rvr/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "rvr:", err)
		os.Exit(1)
	}
}
