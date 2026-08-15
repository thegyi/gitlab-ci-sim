package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestGraphCommand(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build
  - test

build_job:
  stage: build
  image: alpine:latest
  script:
    - echo build

test_job:
  stage: test
  image: alpine:latest
  script:
    - echo test
  needs:
    - build_job
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("file", cfg, "")
	cmd.Flags().String("branch", "", "")
	cmd.Flags().StringSlice("variable", nil, "")
	if err := graph(cmd, nil); err != nil {
		t.Fatalf("graph command failed: %v", err)
	}
}

func TestGraphCommandInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build

bad_job:
  stage: unknown
  script:
    - echo
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("file", cfg, "")
	cmd.Flags().String("branch", "", "")
	cmd.Flags().StringSlice("variable", nil, "")
	if err := graph(cmd, nil); err == nil {
		t.Fatal("expected error for unknown stage")
	}
}
