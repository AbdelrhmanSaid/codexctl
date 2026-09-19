package main

import (
	"fmt"
	"os"

	"codexctl/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "codexctl: %v\n", err)
		os.Exit(1)
	}
}
