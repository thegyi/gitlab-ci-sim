package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newLintCmd(file string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("file", file, "")
	return cmd
}

func TestLintConfigValid(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build
  - test

build_job:
  stage: build
  script:
    - echo building

test_job:
  stage: test
  script:
    - echo testing
  needs:
    - build_job
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := newLintCmd(cfg)
	if err := lintConfig(cmd, nil); err != nil {
		t.Fatalf("lintConfig returned error for valid config: %v", err)
	}
}

func TestLintConfigUnknownStage(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build

bad_job:
  stage: deploy
  script:
    - echo bad
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := newLintCmd(cfg)
	err := lintConfig(cmd, nil)
	if err == nil {
		t.Fatal("expected error for unknown stage")
	}
	if !strings.Contains(err.Error(), "invalid pipeline") {
		t.Fatalf("expected invalid pipeline error, got: %v", err)
	}
}

func TestLintConfigMissingNeeds(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build

build_job:
  stage: build
  script:
    - echo building
  needs:
    - missing_job
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := newLintCmd(cfg)
	err := lintConfig(cmd, nil)
	if err == nil {
		t.Fatal("expected error for missing needs")
	}
	if !strings.Contains(err.Error(), "lint error") {
		t.Fatalf("expected lint error, got: %v", err)
	}
}

func TestLintConfigWarnings(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build

empty_job:
  stage: build
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := newLintCmd(cfg)
	if err := lintConfig(cmd, nil); err != nil {
		t.Fatalf("lintConfig should return nil for warnings, got: %v", err)
	}
}

func TestLintConfigUnknownWhen(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build

build:
  stage: build
  when: unknown
  script:
    - echo
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := newLintCmd(cfg)
	if err := lintConfig(cmd, nil); err == nil {
		t.Fatal("expected error for unknown when")
	}
}

func TestLintConfigCircularNeeds(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build

job_a:
  stage: build
  script:
    - echo a
  needs:
    - job_b

job_b:
  stage: build
  script:
    - echo b
  needs:
    - job_a
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := newLintCmd(cfg)
	err := lintConfig(cmd, nil)
	if err == nil {
		t.Fatal("expected error for circular needs")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Fatalf("expected circular needs error, got: %v", err)
	}
}
