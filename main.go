package main

import (
	"fmt"
	"os"

	"github.com/thegyi/gitlab-ci-sim/cmd"
)

func main() {
	os.Exit(runMain())
}

func runMain() int {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func run() error {
	return cmd.Execute()
}
