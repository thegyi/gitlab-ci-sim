package executor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/thegyi/gitlab-ci-sim/pkg/artifacts"
	"github.com/thegyi/gitlab-ci-sim/pkg/parser"
	"github.com/thegyi/gitlab-ci-sim/pkg/pipeline"
	"github.com/thegyi/gitlab-ci-sim/pkg/variables"
)

func TestExecutorHelpers(t *testing.T) {
	if got := buildShellScript([]string{"echo a", "echo b"}); got != "echo a\necho b" {
		t.Errorf("buildShellScript: got %q", got)
	}

	strs := executionStrings(&pipeline.PipelineJob{
		Name:         "job",
		BeforeScript: []string{"before"},
		Script:       []string{"main"},
		AfterScript:  []string{"after"},
		Variables:    map[string]string{"X": "1"},
	})
	if len(strs) != 5 {
		t.Fatalf("executionStrings: expected 4, got %d", len(strs))
	}

	job := &pipeline.PipelineJob{
		Name:  "job",
		Needs: parser.Needs{{Job: "a"}},
	}
	if needsMet(job, map[string]bool{"a": true}) != true {
		t.Error("needsMet should be true")
	}
	if got := missingNeeds(job, map[string]bool{}); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("missingNeeds: %v", got)
	}

	if allPipelineJobs(&pipeline.Pipeline{Stages: []*pipeline.Stage{
		{Name: "s1", Jobs: []*pipeline.PipelineJob{{Name: "j1"}, {Name: "j2"}}},
		{Name: "s2", Jobs: []*pipeline.PipelineJob{{Name: "j3"}}},
	}}); len(allPipelineJobs(&pipeline.Pipeline{})) != 0 {
		t.Error("allPipelineJobs on empty should be 0")
	}

	if !shouldSaveArtifacts(1, "on_failure") {
		t.Error("should save on_failure when exit 1")
	}
	if !shouldSaveArtifacts(0, "always") {
		t.Error("should save always")
	}
	if shouldSaveArtifacts(1, "on_success") {
		t.Error("should not save on_success when exit 1")
	}
	if shouldPullCache("pull") != true {
		t.Error("should pull cache")
	}
	if shouldPushCache("push") != true {
		t.Error("should push cache")
	}
	if cacheKey(&parser.CacheKey{Prefix: ""}, "", &variables.Context{}) != "default" {
		t.Error("cacheKey default")
	}
	if cacheKey(&parser.CacheKey{Prefix: "v1"}, "", &variables.Context{}) != "v1" {
		t.Error("cacheKey prefix")
	}

	ctx := &variables.Context{
		Vars:     map[string]string{"A": "1", "B": "2", "UNDECL": "x"},
		Declared: map[string]bool{"A": true, "B": true},
	}
	if got := envListFromContext(ctx); len(got) != 2 {
		t.Fatalf("envListFromContext: %v", got)
	}
}

func TestRedactingWriter(t *testing.T) {
	ctx := &variables.Context{
		Vars:   map[string]string{"SECRET": "hunter2"},
		Masked: map[string]bool{"SECRET": true},
	}
	var b strings.Builder
	w := &redactingWriter{dest: &b, values: maskedValues(ctx)}
	fmt.Fprint(w, "hello hunter2 world")
	w.Flush()
	if !strings.Contains(b.String(), "[MASKED]") {
		t.Fatalf("expected [MASKED] in %q", b.String())
	}
}

func TestRunPipelineFailure(t *testing.T) {
	rt := &mockRuntime{exit: []int{1}}
	e := &DockerExecutor{runtime: rt}
	config := &parser.Config{
		Stages: []string{"build"},
		Jobs: map[string]*parser.Job{
			"build": {
				Stage:  "build",
				Script: []string{"exit 1"},
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
	result := e.Run(context.Background(), pipe, ctx)
	if result.Success {
		t.Fatal("expected pipeline failure")
	}
}

func TestRunJobWithArtifacts(t *testing.T) {
	dir := t.TempDir()
	store, err := artifacts.NewStore(filepath.Join(dir, "artifacts"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "out.txt"), []byte("data"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	rt := &FakeRuntime{}
	e := &DockerExecutor{runtime: rt, store: store}
	job := &pipeline.PipelineJob{
		Name:      "save",
		Image:     parser.Image{Name: "alpine:latest"},
		Script:    []string{"echo done"},
		Variables: map[string]string{"CI_COMMIT_BRANCH": "main"},
		Declared:  map[string]bool{"CI_COMMIT_BRANCH": true},
		Artifacts: &parser.Artifacts{Paths: []string{"out.txt"}, When: "always"},
	}
	vars := &variables.Context{
		Vars:     map[string]string{"CI": "true"},
		Declared: map[string]bool{"CI": true},
	}
	jr := e.runJob(context.Background(), job, vars)
	if !jr.Success {
		t.Fatalf("expected success, got: %s", jr.Output)
	}
}

func TestRunJobWithCache(t *testing.T) {
	dir := t.TempDir()
	cache, err := artifacts.NewStore(filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, "vendor"), 0755); err != nil {
		t.Fatalf("make vendor: %v", err)
	}

	rt := &FakeRuntime{}
	e := &DockerExecutor{runtime: rt, cache: cache}
	job := &pipeline.PipelineJob{
		Name:      "cached",
		Image:     parser.Image{Name: "alpine:latest"},
		Script:    []string{"echo done"},
		Variables: map[string]string{"CI_COMMIT_BRANCH": "main"},
		Declared:  map[string]bool{"CI_COMMIT_BRANCH": true},
		Cache:     &parser.Cache{Paths: []string{"vendor"}, Key: &parser.CacheKey{Prefix: "v1"}},
	}
	vars := &variables.Context{
		Vars:     map[string]string{"CI": "true"},
		Declared: map[string]bool{"CI": true},
	}
	jr := e.runJob(context.Background(), job, vars)
	if !jr.Success {
		t.Fatalf("expected success, got: %s", jr.Output)
	}
}

func TestRunJobServiceHealthCheckFailure(t *testing.T) {
	rt := &mockRuntime{exit: []int{1}}
	e := &DockerExecutor{runtime: rt}
	job := &pipeline.PipelineJob{
		Name:   "with_service",
		Image:  parser.Image{Name: "alpine:latest"},
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
	if jr.Success {
		t.Fatal("expected failure because service health check failed")
	}
}

func TestRunJobRetryOnError(t *testing.T) {
	rt := &mockRuntime{err: []error{fmt.Errorf("runtime failure"), nil}, exit: []int{-1, 0}}
	e := &DockerExecutor{runtime: rt}
	job := &pipeline.PipelineJob{
		Name:      "retry_error",
		Image:     parser.Image{Name: "alpine:latest"},
		Script:    []string{"echo test"},
		Retry:     &parser.Retry{Max: 2, When: []string{"always"}},
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

type blockedRuntime struct {
	delay time.Duration
}

func (b *blockedRuntime) CreateNetwork(ctx context.Context, name string) error { return nil }
func (b *blockedRuntime) RemoveNetwork(ctx context.Context, name string) error { return nil }
func (b *blockedRuntime) Stop(ctx context.Context, id string) error            { return nil }
func (b *blockedRuntime) RunDetached(ctx context.Context, opts ServiceOpts) (string, error) {
	return "service-id", nil
}
func (b *blockedRuntime) Run(ctx context.Context, opts RunOpts) (int, error) {
	select {
	case <-ctx.Done():
		return -1, ctx.Err()
	case <-time.After(b.delay):
	}
	return 0, nil
}

func TestRunPipelineContextCancel(t *testing.T) {
	e := &DockerExecutor{runtime: &blockedRuntime{delay: 500 * time.Millisecond}}
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
	runCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result := e.Run(runCtx, pipe, ctx)
	if result.Success {
		t.Fatal("expected canceled pipeline to fail")
	}
	if len(result.JobResults) != 1 {
		t.Fatalf("expected 1 job result, got %d", len(result.JobResults))
	}
	if result.JobResults[0].Success {
		t.Fatal("expected canceled job to fail")
	}
}

func TestRunPipelineAllowFailure(t *testing.T) {
	rt := &mockRuntime{exit: []int{1}}
	e := &DockerExecutor{runtime: rt}
	config := &parser.Config{
		Stages: []string{"build"},
		Jobs: map[string]*parser.Job{
			"build": {
				Stage:        "build",
				Script:       []string{"exit 1"},
				AllowFailure: parser.AllowFailure{Value: true},
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
	result := e.Run(context.Background(), pipe, ctx)
	if !result.Success {
		t.Fatalf("expected allow_failure pipeline to be successful, got: %v", result)
	}
}

func TestAllowFailureExitCodes(t *testing.T) {
	rt := &mockRuntime{exit: []int{1}}
	e := &DockerExecutor{runtime: rt}
	config := &parser.Config{
		Stages: []string{"build"},
		Jobs: map[string]*parser.Job{
			"build": {
				Stage:        "build",
				Script:       []string{"exit 1"},
				AllowFailure: parser.AllowFailure{ExitCodes: []int{1}},
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
	result := e.Run(context.Background(), pipe, ctx)
	if !result.Success {
		t.Fatalf("expected allow_failure for exit code 1, got: %v", result)
	}
}

func TestRunPipelineMissingNeeds(t *testing.T) {
	e := &DockerExecutor{runtime: &FakeRuntime{}}
	config := &parser.Config{
		Stages: []string{"build"},
		Jobs: map[string]*parser.Job{
			"orphan": {
				Stage:  "build",
				Script: []string{"echo"},
				Needs:  parser.Needs{{Job: "does_not_exist"}},
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
	result := e.Run(context.Background(), pipe, ctx)
	if result.Success {
		t.Fatal("expected pipeline with missing needs to fail")
	}
	if len(result.JobResults) != 1 {
		t.Fatalf("expected 1 job result, got %d", len(result.JobResults))
	}
}

func TestRunPipelineNeedsFailure(t *testing.T) {
	rt := &mockRuntime{exit: []int{1, 0}}
	e := &DockerExecutor{runtime: rt}
	config := &parser.Config{
		Stages: []string{"build", "test"},
		Jobs: map[string]*parser.Job{
			"build": {
				Stage:  "build",
				Script: []string{"exit 1"},
			},
			"test": {
				Stage:  "test",
				Script: []string{"echo test"},
				Needs:  parser.Needs{{Job: "build"}},
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
	result := e.Run(context.Background(), pipe, ctx)
	if result.Success {
		t.Fatal("expected pipeline failure")
	}
}

func TestSetTriggerMode(t *testing.T) {
	e := &DockerExecutor{}
	e.SetTriggerMode("gitlab")
	if e.triggerMode != "gitlab" {
		t.Fatal("expected trigger mode to be set")
	}
}

func TestNewRuntime(t *testing.T) {
	if _, err := NewRuntime("fake"); err != nil {
		t.Fatalf("expected fake runtime to succeed: %v", err)
	}
	if _, err := NewRuntime("unknown"); err == nil {
		t.Fatal("expected unknown runtime to fail")
	}
}

func TestRunPipelineOptionalNeeds(t *testing.T) {
	rt := &mockRuntime{exit: []int{1, 0}}
	e := &DockerExecutor{runtime: rt}
	config := &parser.Config{
		Stages: []string{"build", "test"},
		Jobs: map[string]*parser.Job{
			"build": {
				Stage:  "build",
				Script: []string{"exit 1"},
			},
			"test": {
				Stage:  "test",
				Script: []string{"echo test"},
				Needs:  parser.Needs{{Job: "build", Optional: true}},
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
	result := e.Run(context.Background(), pipe, ctx)
	if result.Success {
		t.Fatal("expected pipeline to be unsuccessful because build failed")
	}
	if len(result.JobResults) != 2 {
		t.Fatalf("expected 2 job results, got %d", len(result.JobResults))
	}
	if result.JobResults[0].Success {
		t.Error("build should have failed")
	}
	if !result.JobResults[1].Success {
		t.Error("test with optional need should have run and passed")
	}
}

func TestRunPipelineWithNeeds(t *testing.T) {
	e, err := NewDockerExecutor("fake", nil, nil)
	if err != nil {
		t.Fatalf("NewDockerExecutor: %v", err)
	}
	config := &parser.Config{
		Stages: []string{"build", "test"},
		Jobs: map[string]*parser.Job{
			"build": {
				Stage:  "build",
				Script: []string{"echo build"},
			},
			"test": {
				Stage:  "test",
				Script: []string{"echo test"},
				Needs:  parser.Needs{{Job: "build"}},
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
	result := e.Run(context.Background(), pipe, ctx)
	if !result.Success {
		t.Fatalf("expected pipeline success, got: %v", result)
	}
	if len(result.JobResults) != 2 {
		t.Fatalf("expected 2 job results, got %d", len(result.JobResults))
	}
}

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
		Image:  parser.Image{Name: "alpine:latest"},
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
		Image:        parser.Image{Name: "alpine:latest"},
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
	exit := -1
	if len(m.exit) > 0 {
		if i >= len(m.exit) {
			i = len(m.exit) - 1
		}
		exit = m.exit[i]
	}
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
		Image:     parser.Image{Name: "alpine:latest"},
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
		Image:     parser.Image{Name: "alpine:latest"},
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

func TestRunPipelineParallel(t *testing.T) {
	e, err := NewDockerExecutor("fake", nil, nil)
	if err != nil {
		t.Fatalf("NewDockerExecutor: %v", err)
	}
	config := &parser.Config{
		Stages: []string{"build", "test"},
		Jobs: map[string]*parser.Job{
			"a": {
				Stage:  "build",
				Script: []string{"echo a"},
			},
			"b": {
				Stage:  "build",
				Script: []string{"echo b"},
			},
			"c": {
				Stage:  "test",
				Script: []string{"echo c"},
				Needs:  parser.Needs{{Job: "a"}, {Job: "b"}},
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
	result := e.Run(context.Background(), pipe, ctx)
	if !result.Success {
		t.Fatalf("expected pipeline success, got: %v", result)
	}
	if len(result.JobResults) != 3 {
		t.Fatalf("expected 3 job results, got %d", len(result.JobResults))
	}
}

func TestRunJobAfterScript(t *testing.T) {
	rt := &FakeRuntime{}
	e := &DockerExecutor{runtime: rt}
	job := &pipeline.PipelineJob{
		Name:        "after",
		Image:       parser.Image{Name: "alpine:latest"},
		Script:      []string{"echo main"},
		AfterScript: []string{"echo after"},
		Variables:   map[string]string{"CI_COMMIT_BRANCH": "main"},
		Declared:    map[string]bool{"CI_COMMIT_BRANCH": true},
	}
	vars := &variables.Context{
		Vars:     map[string]string{"CI": "true"},
		Declared: map[string]bool{"CI": true},
	}
	jr := e.runJob(context.Background(), job, vars)
	if !jr.Success {
		t.Fatalf("expected success, got: %s", jr.Output)
	}
	if !strings.Contains(jr.Output, "after") {
		t.Error("expected after_script output")
	}
}

func TestRunJobRuntimeErrorNoRetry(t *testing.T) {
	rt := &mockRuntime{err: []error{fmt.Errorf("runtime failure")}}
	e := &DockerExecutor{runtime: rt}
	job := &pipeline.PipelineJob{
		Name:      "fail",
		Image:     parser.Image{Name: "alpine:latest"},
		Script:    []string{"echo"},
		Variables: map[string]string{"CI_COMMIT_BRANCH": "main"},
		Declared:  map[string]bool{"CI_COMMIT_BRANCH": true},
	}
	vars := &variables.Context{
		Vars:     map[string]string{"CI": "true"},
		Declared: map[string]bool{"CI": true},
	}
	jr := e.runJob(context.Background(), job, vars)
	if jr.Success {
		t.Fatal("expected failure on runtime error")
	}
}

func TestRunJobTriggerLocal(t *testing.T) {
	rt := &FakeRuntime{}
	e := &DockerExecutor{runtime: rt, triggerMode: "local"}
	job := &pipeline.PipelineJob{
		Name:      "trigger",
		Trigger:   &parser.Trigger{Project: "group/proj", Branch: "main"},
		Variables: map[string]string{"CI_SERVER_URL": "https://gitlab.example.com"},
		Declared:  map[string]bool{"CI_SERVER_URL": true},
	}
	vars := &variables.Context{
		Vars:     map[string]string{"CI_SERVER_URL": "https://gitlab.example.com"},
		Declared: map[string]bool{"CI_SERVER_URL": true},
	}
	jr := e.runJob(context.Background(), job, vars)
	if !jr.Success {
		t.Fatalf("expected local trigger to pass, got: %s", jr.Output)
	}
	if !strings.Contains(jr.Output, "not executed locally") {
		t.Error("expected 'not executed locally' message")
	}
}

func TestRunJobGitlabTrigger(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 42, "web_url": "https://gitlab.example.com/pipeline/42"}`))
	}))
	defer server.Close()

	rt := &FakeRuntime{}
	e := &DockerExecutor{runtime: rt, triggerMode: "gitlab"}
	job := &pipeline.PipelineJob{
		Name:    "trigger",
		Trigger: &parser.Trigger{Project: "group%2Fproject", Branch: "main"},
		Variables: map[string]string{
			"CI_SERVER_URL": server.URL,
			"GITLAB_TOKEN":  "secret",
		},
		Declared: map[string]bool{
			"CI_SERVER_URL": true,
			"GITLAB_TOKEN":  true,
		},
	}
	vars := &variables.Context{
		Vars:     map[string]string{"CI_COMMIT_REF_NAME": "main"},
		Declared: map[string]bool{"CI_COMMIT_REF_NAME": true},
	}
	jr := e.runJob(context.Background(), job, vars)
	if !jr.Success {
		t.Fatalf("expected gitlab trigger to pass, got: %s", jr.Output)
	}
	if !strings.Contains(jr.Output, "#42") {
		t.Errorf("expected pipeline number in output, got %s", jr.Output)
	}
}

func TestGitlabToken(t *testing.T) {
	ctx := &variables.Context{
		Vars:     map[string]string{"GITLAB_TOKEN": "abc"},
		Declared: map[string]bool{"GITLAB_TOKEN": true},
	}
	token, header := gitlabToken(ctx)
	if token != "abc" || header != "PRIVATE-TOKEN" {
		t.Fatalf("expected PRIVATE-TOKEN abc, got %s %s", header, token)
	}

	ctx = &variables.Context{
		Vars:     map[string]string{"CI_JOB_TOKEN": "job"},
		Declared: map[string]bool{"CI_JOB_TOKEN": true},
	}
	token, header = gitlabToken(ctx)
	if token != "job" || header != "JOB-TOKEN" {
		t.Fatalf("expected JOB-TOKEN job, got %s %s", header, token)
	}
}

func TestNewDockerExecutorUnknownRuntime(t *testing.T) {
	_, err := NewDockerExecutor("none", nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown runtime")
	}
}

func TestResultPrint(t *testing.T) {
	res := &Result{Success: true, JobResults: []*JobResult{{Name: "build", Success: true}}}
	var b strings.Builder
	res.Print(&b)
	if !strings.Contains(b.String(), "build") {
		t.Fatalf("expected output to contain build, got %q", b.String())
	}
}

func TestRunJobDelayed(t *testing.T) {
	rt := &mockRuntime{exit: []int{0}}
	e := &DockerExecutor{runtime: rt}
	job := &pipeline.PipelineJob{
		Name:      "delayed_job",
		Image:     parser.Image{Name: "alpine:latest"},
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
		Image:  parser.Image{Name: "alpine:latest"},
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

func TestCoverageFromRegex(t *testing.T) {
	s := "Tests passed. Coverage: 87.5%"
	got := coverageFromRegex(`/Coverage: (\d+\.?\d*)%$/`, s)
	if got != "87.5" {
		t.Errorf("expected 87.5, got %q", got)
	}
	got = coverageFromRegex(`Coverage: \d+\.\d+%`, s)
	if got != "Coverage: 87.5%" {
		t.Errorf("expected full match, got %q", got)
	}
}

func TestCoverageFromCobertura(t *testing.T) {
	xml := `<?xml version="1.0"?><coverage line-rate="0.875"></coverage>`
	got := coberturaCoverage(xml)
	if got != "87.50" {
		t.Errorf("expected 87.50, got %q", got)
	}
}

func TestCoverageFromJacoco(t *testing.T) {
	xml := `<?xml version="1.0"?>
<report>
  <counter type="LINE" missed="10" covered="90"/>
  <counter type="LINE" missed="5" covered="95"/>
</report>`
	got := jacocoCoverage(xml)
	if got != "92.50" {
		t.Errorf("expected 92.31, got %q", got)
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
