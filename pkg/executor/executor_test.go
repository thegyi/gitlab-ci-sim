package executor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/thegyi/gitlab-ci-sim/pkg/parser"
	"github.com/thegyi/gitlab-ci-sim/pkg/pipeline"
	"github.com/thegyi/gitlab-ci-sim/pkg/variables"
)

func TestBuildShellScript(t *testing.T) {
	lines := []string{"echo one", "echo two"}
	if got := buildShellScript(lines); got != "echo one\necho two" {
		t.Errorf("unexpected script: %q", got)
	}
}

func TestRunJobRejectsMissingVariables(t *testing.T) {
	e := &DockerExecutor{runtime: &FakeRuntime{}}
	job := &pipeline.PipelineJob{
		Name:   "test",
		Image:  "alpine:latest",
		Script: []string{"echo $UNKNOWN"},
	}
	vars := &variables.Context{Vars: map[string]string{}, Declared: map[string]bool{}}
	jr := e.runJob(context.Background(), job, vars)
	if jr.Success {
		t.Error("expected job to fail because $UNKNOWN is not defined")
	}
	if !strings.Contains(jr.Output, "undefined/empty variables: UNKNOWN") {
		t.Errorf("expected missing variable message, got: %q", jr.Output)
	}
}

func TestRunJobFakeContainer(t *testing.T) {
	e := &DockerExecutor{runtime: &FakeRuntime{}}
	job := &pipeline.PipelineJob{
		Name:         "test",
		Image:        "alpine:latest",
		BeforeScript: []string{"echo before"},
		Script:       []string{"echo script"},
		Variables:    map[string]string{"CI_COMMIT_BRANCH": "main"},
		Declared:     map[string]bool{"CI_COMMIT_BRANCH": true},
	}
	vars := &variables.Context{Vars: map[string]string{"CI": "true"}, Declared: map[string]bool{"CI": true}}
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

func TestTriggerPipelineFailsWithoutToken(t *testing.T) {
	e := &DockerExecutor{runtime: &FakeRuntime{}}
	ctx := &variables.Context{
		Vars:     map[string]string{"CI_SERVER_URL": "https://gitlab.example.com"},
		Declared: map[string]bool{},
		Masked:   map[string]bool{},
	}
	_, err := e.triggerPipeline(ctx, &parser.Trigger{Project: "group/project", Branch: "main"})
	if err == nil {
		t.Fatal("expected error without token")
	}
	if !strings.Contains(err.Error(), "no GITLAB_TOKEN or CI_JOB_TOKEN") {
		t.Errorf("expected token error, got %v", err)
	}
}
