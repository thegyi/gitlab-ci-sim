package main

import (
	"fmt"
	"os"

	"github.com/thegyi/gitlab-ci-sim/cmd"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	return cmd.Execute()
}
