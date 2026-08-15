package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thegyi/gitlab-ci-sim/pkg/pipeline"
	"github.com/thegyi/gitlab-ci-sim/pkg/variables"
)

func fakeDocker(t *testing.T) string {
	t.Helper()
	script := []byte("#!/bin/sh\n# ignore docker args; just echo stdin and done\ncat\necho '--- done ---'\n")
	p := filepath.Join(t.TempDir(), "docker")
	if err := os.WriteFile(p, script, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBuildShellScript(t *testing.T) {
	lines := []string{"echo one", "echo two"}
	if got := buildShellScript(lines); got != "echo one\necho two" {
		t.Errorf("unexpected script: %q", got)
	}
}

func TestRunJobRejectsMissingVariables(t *testing.T) {
	e := &DockerExecutor{client: fakeDocker(t)}
	job := &pipeline.PipelineJob{
		Name:   "test",
		Image:  "alpine:latest",
		Script: []string{"echo $UNKNOWN"},
	}
	vars := &variables.Context{Vars: map[string]string{} }
	jr := e.runJob(context.Background(), job, vars)
	if jr.Success {
		t.Error("expected job to fail because $UNKNOWN is not defined")
	}
	if !strings.Contains(jr.Output, "undefined/empty variables: UNKNOWN") {
		t.Errorf("expected missing variable message, got: %q", jr.Output)
	}
}

func TestRunJobFakeContainer(t *testing.T) {
	e := &DockerExecutor{client: fakeDocker(t)}
	job := &pipeline.PipelineJob{
		Name:         "test",
		Image:        "alpine:latest",
		BeforeScript: []string{"echo before"},
		Script:       []string{"echo script"},
		Variables:    map[string]string{"CI_COMMIT_BRANCH": "main"},
	}
	vars := &variables.Context{Vars: map[string]string{"CI": "true"}}
	jr := e.runJob(context.Background(), job, vars)
	if !jr.Success {
		t.Fatalf("expected success, got output: %s", jr.Output)
	}
	if !strings.Contains(jr.Output, "before") {
		t.Error("expected before_script output")
	}
	if !strings.Contains(jr.Output, "script") {
		t.Error("expected script output")
	}
	fmt.Fprint(os.Stderr, "captured output:\n"+jr.Output)
}
