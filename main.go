package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/AbdelrhmanSaid/codexctl/internal/cli"
	"github.com/AbdelrhmanSaid/codexctl/internal/tui"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		// A cancelled prompt or a styled report has already said so on
		// screen.
		if errors.Is(err, tui.ErrCancelled) {
			os.Exit(130)
		}
		if errors.Is(err, cli.ErrReported) {
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "codexctl: %v\n", err)
		os.Exit(1)
	}
}
