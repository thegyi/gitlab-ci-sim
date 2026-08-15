package executor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

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

type mockRuntime struct {
	calls []RunOpts
	exit  []int
	err   []error
	idx   int
}

func (m *mockRuntime) CreateNetwork(ctx context.Context, name string) error { return nil }
func (m *mockRuntime) RemoveNetwork(ctx context.Context, name string) error { return nil }
func (m *mockRuntime) Stop(ctx context.Context, id string) error            { return nil }
func (m *mockRuntime) RunDetached(ctx context.Context, opts ServiceOpts) (string, error) {
	return "service-id", nil
}
func (m *mockRuntime) Run(ctx context.Context, opts RunOpts) (int, error) {
	m.calls = append(m.calls, opts)
	i := m.idx
	if i >= len(m.exit) {
		i = len(m.exit) - 1
	}
	exit := m.exit[i]
	var err error
	if i < len(m.err) {
		err = m.err[i]
	}
	m.idx++
	return exit, err
}

func TestRunJobRetriesAndSucceeds(t *testing.T) {
	rt := &mockRuntime{exit: []int{1, 0}}
	e := &DockerExecutor{runtime: rt}
	job := &pipeline.PipelineJob{
		Name:      "retry_job",
		Image:     "alpine:latest",
		Script:    []string{"echo test"},
		Retry:     &parser.Retry{Max: 2},
		Variables: map[string]string{"CI_COMMIT_BRANCH": "main"},
		Declared:  map[string]bool{"CI_COMMIT_BRANCH": true},
	}
	vars := &variables.Context{
		Vars:     map[string]string{"CI": "true"},
		Declared: map[string]bool{"CI": true},
	}
	jr := e.runJob(context.Background(), job, vars)
	if !jr.Success {
		t.Fatalf("expected success after retry, got: %s", jr.Output)
	}
	if rt.idx != 2 {
		t.Fatalf("expected 2 runtime calls, got %d", rt.idx)
	}
}

func TestRunJobRetryExhausted(t *testing.T) {
	rt := &mockRuntime{exit: []int{1, 1, 1}}
	e := &DockerExecutor{runtime: rt}
	job := &pipeline.PipelineJob{
		Name:      "retry_job",
		Image:     "alpine:latest",
		Script:    []string{"echo test"},
		Retry:     &parser.Retry{Max: 2},
		Variables: map[string]string{"CI_COMMIT_BRANCH": "main"},
		Declared:  map[string]bool{"CI_COMMIT_BRANCH": true},
	}
	vars := &variables.Context{
		Vars:     map[string]string{"CI": "true"},
		Declared: map[string]bool{"CI": true},
	}
	jr := e.runJob(context.Background(), job, vars)
	if jr.Success {
		t.Fatal("expected failure after exhausting retries")
	}
	if rt.idx != 3 {
		t.Fatalf("expected 3 runtime calls, got %d", rt.idx)
	}
}

func TestIsRetryable(t *testing.T) {
	ctx := context.Background()
	if !isRetryable(nil, 1, nil, ctx) {
		t.Error("expected non-zero exit with default when to be retryable")
	}
	if isRetryable(nil, 0, nil, ctx) {
		t.Error("expected zero exit with default when not to be retryable")
	}
	if !isRetryable(fmt.Errorf("boom"), 0, []string{"runner_system_failure"}, ctx) {
		t.Error("expected runner_system_failure to be retryable on error")
	}
	if isRetryable(nil, 1, []string{"runner_system_failure"}, ctx) {
		t.Error("expected runner_system_failure not to be retryable on non-zero exit without error")
	}
}

func TestParseStartIn(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"5s", 5 * time.Second},
		{"10 minutes", 10 * time.Minute},
		{"1h", time.Hour},
		{"2 days", 48 * time.Hour},
	}
	for _, c := range cases {
		got, err := parseStartIn(c.in)
		if err != nil {
			t.Fatalf("parseStartIn(%q) error: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("parseStartIn(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestRunJobDelayed(t *testing.T) {
	rt := &mockRuntime{exit: []int{0}}
	e := &DockerExecutor{runtime: rt}
	job := &pipeline.PipelineJob{
		Name:      "delayed_job",
		Image:     "alpine:latest",
		Script:    []string{"echo delayed"},
		When:      "delayed",
		StartIn:   "100ms",
		Variables: map[string]string{"CI_COMMIT_BRANCH": "main"},
		Declared:  map[string]bool{"CI_COMMIT_BRANCH": true},
	}
	vars := &variables.Context{
		Vars:     map[string]string{"CI": "true"},
		Declared: map[string]bool{"CI": true},
	}
	start := time.Now()
	jr := e.runJob(context.Background(), job, vars)
	if !jr.Success {
		t.Fatalf("expected success, got: %s", jr.Output)
	}
	if time.Since(start) < 100*time.Millisecond {
		t.Fatal("expected delayed job to wait for start_in")
	}
}

func TestRunJobWithService(t *testing.T) {
	rt := &mockRuntime{exit: []int{0, 0}}
	e := &DockerExecutor{runtime: rt}
	job := &pipeline.PipelineJob{
		Name:   "with_service",
		Image:  "alpine:latest",
		Script: []string{"echo main"},
		Services: []parser.Service{
			{Name: "redis:alpine", Alias: "redis"},
		},
		Variables: map[string]string{"CI_COMMIT_BRANCH": "main"},
		Declared:  map[string]bool{"CI_COMMIT_BRANCH": true},
	}
	vars := &variables.Context{
		Vars:     map[string]string{"CI": "true"},
		Declared: map[string]bool{"CI": true},
	}
	jr := e.runJob(context.Background(), job, vars)
	if !jr.Success {
		t.Fatalf("expected success, got: %s", jr.Output)
	}
	if rt.idx < 2 {
		t.Fatalf("expected at least 2 runtime calls, got %d", rt.idx)
	}
}

func TestRunPipeline(t *testing.T) {
	config := &parser.Config{
		Stages: []string{"build"},
		Jobs: map[string]*parser.Job{
			"build": {
				Stage:  "build",
				Script: []string{"echo build"},
			},
		},
	}
	ctx := &variables.Context{
		Vars:     map[string]string{"CI_COMMIT_BRANCH": "main"},
		Declared: map[string]bool{"CI_COMMIT_BRANCH": true},
		Masked:   map[string]bool{},
	}
	pipe, err := pipeline.Build(config, ctx, nil, false, nil)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	e, err := NewDockerExecutor("fake", nil, nil)
	if err != nil {
		t.Fatalf("NewDockerExecutor failed: %v", err)
	}
	result := e.Run(context.Background(), pipe, ctx)
	if !result.Success {
		t.Fatalf("expected pipeline success, got: %v", result)
	}
	if len(result.JobResults) != 1 || result.JobResults[0].Name != "build" {
		t.Fatalf("expected one build job result, got %v", result.JobResults)
	}
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
