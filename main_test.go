package main

import (
	"os"
	"strings"
	"testing"
)

func TestRunMain(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"gitlab-ci-sim", "help"}
	if got := runMain(); got != 0 {
		t.Fatalf("expected exit 0, got %d", got)
	}
}

func TestRunMainError(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"gitlab-ci-sim", "--nonexistent"}
	got := runMain()
	if got != 1 {
		t.Fatalf("expected exit 1, got %d", got)
	}
}

func TestRunHelp(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"gitlab-ci-sim", "help"}
	if err := run(); err != nil {
		t.Fatalf("expected help to succeed, got %v", err)
	}
}

func TestRunInvalidFlag(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"gitlab-ci-sim", "--nonexistent"}
	err := run()
	if err == nil {
		t.Fatal("expected error for invalid flag")
	}
	if !strings.Contains(err.Error(), "unknown") && !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("unexpected error: %v", err)
	}
}
