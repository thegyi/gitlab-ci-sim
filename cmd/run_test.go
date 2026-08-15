package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
	"github.com/thegyi/gitlab-ci-sim/pkg/pipeline"
)

func TestParseSelection(t *testing.T) {
	cases := []struct {
		input string
		max   int
		want  []int
	}{
		{"1,3,5", 5, []int{1, 3, 5}},
		{"1-3", 5, []int{1, 2, 3}},
		{"1-3,5", 5, []int{1, 2, 3, 5}},
		{"5-1", 5, []int{1, 2, 3, 4, 5}},
		{"1-10", 5, []int{1, 2, 3, 4, 5}},
		{"", 3, nil},
		{"2,2,2", 3, []int{2}},
	}
	for _, c := range cases {
		got := parseSelection(c.input, c.max)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseSelection(%q, %d) = %v, want %v", c.input, c.max, got, c.want)
		}
	}
}

func newRunCmd(file string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("file", file, "")
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("watch", false, "")
	cmd.Flags().String("branch", "", "")
	cmd.Flags().StringSlice("variable", nil, "")
	cmd.Flags().String("env-file", "", "")
	cmd.Flags().Bool("list", false, "")
	cmd.Flags().Bool("manual", false, "")
	cmd.Flags().Bool("interactive", false, "")
	cmd.Flags().StringSlice("tags", nil, "")
	cmd.Flags().String("runtime", "fake", "")
	cmd.Flags().String("trigger-mode", "local", "")
	return cmd
}

func TestRunJobsDryRun(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build

build:
  stage: build
  image: alpine:latest
  script:
    - echo build
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cmd := newRunCmd(cfg)
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatalf("set dry-run: %v", err)
	}
	if err := runJobs(cmd, nil); err != nil {
		t.Fatalf("runJobs dry-run failed: %v", err)
	}
}

func TestRunJobsFake(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build

build:
  stage: build
  image: alpine:latest
  script:
    - echo build
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cmd := newRunCmd(cfg)
	cmd.Flags().Set("runtime", "fake")
	if err := runJobs(cmd, nil); err != nil {
		t.Fatalf("runJobs fake failed: %v", err)
	}
}

func TestRunJobsWorkflowSkip(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
workflow:
  rules:
    - if: $CI_COMMIT_BRANCH == "never"

stages:
  - build

build:
  stage: build
  image: alpine:latest
  script:
    - echo build
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cmd := newRunCmd(cfg)
	if err := runJobs(cmd, nil); err == nil {
		t.Fatal("expected workflow skip error")
	}
}

func TestRunJobsInvalidPipeline(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build

build:
  stage: unknown
  image: alpine:latest
  script:
    - echo build
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cmd := newRunCmd(cfg)
	if err := runJobs(cmd, nil); err == nil {
		t.Fatal("expected pipeline build error")
	}
}

func TestRunJobsManual(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build

build:
  stage: build
  image: alpine:latest
  when: manual
  script:
    - echo build
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cmd := newRunCmd(cfg)
	cmd.Flags().Set("manual", "true")
	cmd.Flags().Set("runtime", "fake")
	if err := runJobs(cmd, nil); err != nil {
		t.Fatalf("runJobs manual failed: %v", err)
	}
}

func TestRunJobsInteractive(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build

build:
  stage: build
  image: alpine:latest
  script:
    - echo build
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = r
	fmt.Fprint(w, "all\n")
	w.Close()
	defer func() { os.Stdin = oldStdin }()

	cmd := newRunCmd(cfg)
	cmd.Flags().Set("interactive", "true")
	cmd.Flags().Set("runtime", "fake")
	if err := runJobs(cmd, nil); err != nil {
		t.Fatalf("runJobs interactive failed: %v", err)
	}
}

func TestRunJobsWithBranchAndVariable(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build

build:
  stage: build
  image: alpine:latest
  script:
    - echo $DEPLOY_ENV
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cmd := newRunCmd(cfg)
	cmd.Flags().Set("branch", "feature")
	cmd.Flags().Set("variable", "DEPLOY_ENV=staging")
	cmd.Flags().Set("runtime", "fake")
	if err := runJobs(cmd, nil); err != nil {
		t.Fatalf("runJobs with branch/variable failed: %v", err)
	}
}

func TestRunJobsWithEnvFileAndTags(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build

build:
  stage: build
  image: alpine:latest
  tags:
    - docker
  script:
    - echo $FROM_FILE
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	env := filepath.Join(dir, ".env")
	if err := os.WriteFile(env, []byte("FROM_FILE=hello\n"), 0644); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	cmd := newRunCmd(cfg)
	cmd.Flags().Set("env-file", env)
	cmd.Flags().Set("tags", "docker")
	cmd.Flags().Set("runtime", "fake")
	if err := runJobs(cmd, nil); err != nil {
		t.Fatalf("runJobs with env-file/tags failed: %v", err)
	}
}

func TestRunJobsList(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitlab-ci.yml")
	content := `
stages:
  - build

build:
  stage: build
  image: alpine:latest
  script:
    - echo build
`
	if err := os.WriteFile(cfg, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cmd := newRunCmd(cfg)
	if err := cmd.Flags().Set("list", "true"); err != nil {
		t.Fatalf("set list: %v", err)
	}
	if err := runJobs(cmd, nil); err != nil {
		t.Fatalf("runJobs list failed: %v", err)
	}
}

func TestSelectJobs(t *testing.T) {
	pipe := &pipeline.Pipeline{
		Stages: []*pipeline.Stage{
			{Name: "build", Jobs: []*pipeline.PipelineJob{
				{Name: "job_a"},
				{Name: "job_b"},
			}},
			{Name: "test", Jobs: []*pipeline.PipelineJob{
				{Name: "job_c"},
			}},
		},
	}

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = r
	fmt.Fprint(w, "1,3\n")
	w.Close()
	defer func() { os.Stdin = oldStdin }()

	got, err := selectJobs(pipe)
	if err != nil {
		t.Fatalf("selectJobs failed: %v", err)
	}
	want := []string{"job_a", "job_c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}
