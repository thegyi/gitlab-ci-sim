package main

import (
	"os"
	"strings"
	"testing"
)

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
