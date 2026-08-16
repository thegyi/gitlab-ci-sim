package pipeline

import (
	"reflect"
	"sort"
	"testing"

	"github.com/thegyi/gitlab-ci-sim/pkg/parser"
	"github.com/thegyi/gitlab-ci-sim/pkg/variables"
)

func testContext() *variables.Context {
	return &variables.Context{
		Vars: map[string]string{
			"CI_COMMIT_BRANCH": "main",
			"CI_COMMIT_SHA":    "abc123",
		},
		Declared: map[string]bool{
			"CI_COMMIT_BRANCH": true,
			"CI_COMMIT_SHA":    true,
		},
		Masked: map[string]bool{},
	}
}

func jobNames(pipe *Pipeline) []string {
	var names []string
	for _, s := range pipe.Stages {
		for _, j := range s.Jobs {
			names = append(names, j.Name)
		}
	}
	return names
}

func TestBuildBasic(t *testing.T) {
	config := &parser.Config{
		Stages: []string{"build", "test"},
		Jobs: map[string]*parser.Job{
			"build": {
				Stage:  "build",
				Script: []string{"go build"},
			},
			"test": {
				Stage:  "test",
				Script: []string{"go test"},
				Needs:  []string{"build"},
			},
		},
	}
	pipe, err := Build(config, testContext(), nil, false, nil)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(pipe.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(pipe.Stages))
	}
	got := jobNames(pipe)
	want := []string{"build", "test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestBuildUnknownStage(t *testing.T) {
	config := &parser.Config{
		Stages: []string{"build"},
		Jobs: map[string]*parser.Job{
			"bad": {
				Stage:  "deploy",
				Script: []string{"echo oops"},
			},
		},
	}
	_, err := Build(config, testContext(), nil, false, nil)
	if err == nil {
		t.Fatal("expected error for unknown stage")
	}
}

func TestBuildManual(t *testing.T) {
	config := &parser.Config{
		Stages: []string{"build"},
		Jobs: map[string]*parser.Job{
			"manual_job": {
				Stage:  "build",
				Script: []string{"echo hi"},
				When:   "manual",
			},
		},
	}
	// Without allowManual the job is skipped.
	pipe, err := Build(config, testContext(), nil, false, nil)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(jobNames(pipe)) != 0 {
		t.Fatalf("expected manual job to be skipped, got %v", jobNames(pipe))
	}
	// With allowManual the job is included.
	pipe, err = Build(config, testContext(), nil, true, nil)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	got := jobNames(pipe)
	want := []string{"manual_job"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestBuildTags(t *testing.T) {
	config := &parser.Config{
		Stages: []string{"build"},
		Jobs: map[string]*parser.Job{
			"docker_job": {
				Stage:  "build",
				Script: []string{"docker build"},
				Tags:   []string{"docker"},
			},
			"linux_job": {
				Stage:  "build",
				Script: []string{"echo"},
				Tags:   []string{"linux"},
			},
			"untagged": {
				Stage:  "build",
				Script: []string{"echo"},
			},
		},
	}
	pipe, err := Build(config, testContext(), nil, false, []string{"docker"})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	got := jobNames(pipe)
	want := []string{"docker_job", "untagged"}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestBuildParallelScalar(t *testing.T) {
	config := &parser.Config{
		Stages: []string{"test"},
		Jobs: map[string]*parser.Job{
			"test": {
				Stage:    "test",
				Script:   []string{"go test"},
				Parallel: &parser.Parallel{Scalar: 3},
			},
		},
	}
	pipe, err := Build(config, testContext(), nil, false, nil)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	got := jobNames(pipe)
	want := []string{"test 1/3", "test 2/3", "test 3/3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for _, j := range pipe.Stages[0].Jobs {
		if j.Variables["CI_NODE_TOTAL"] != "3" {
			t.Fatalf("expected CI_NODE_TOTAL=3, got %s", j.Variables["CI_NODE_TOTAL"])
		}
	}
}

func TestBuildParallelMatrix(t *testing.T) {
	config := &parser.Config{
		Stages: []string{"test"},
		Jobs: map[string]*parser.Job{
			"test": {
				Stage:  "test",
				Script: []string{"echo"},
				Parallel: &parser.Parallel{
					Matrix: []map[string][]string{
						{
							"PROVIDER": {"aws", "gcp"},
							"ENV":      {"prod"},
						},
					},
				},
			},
		},
	}
	pipe, err := Build(config, testContext(), nil, false, nil)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	got := jobNames(pipe)
	want := []string{
		"test [ENV=prod,PROVIDER=aws]",
		"test [ENV=prod,PROVIDER=gcp]",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestResolveHelpers(t *testing.T) {
	ctx := &variables.Context{
		Vars:     map[string]string{"IMAGE": "node:18"},
		Declared: map[string]bool{"IMAGE": true},
		Masked:   map[string]bool{},
	}

	img := resolveImage(&parser.Job{Image: "$IMAGE"}, nil, ctx)
	if img != "node:18" {
		t.Fatalf("resolveImage: expected node:18, got %q", img)
	}

	img = resolveImage(&parser.Job{}, &parser.JobDefaults{Image: "alpine:latest"}, ctx)
	if img != "alpine:latest" {
		t.Fatalf("resolveImage: expected alpine:latest, got %q", img)
	}

	img = resolveImage(&parser.Job{}, nil, &variables.Context{Vars: map[string]string{}})
	if img != "" {
		t.Fatalf("resolveImage: expected no image, got %q", img)
	}

	job := &parser.Job{BeforeScript: []string{"a"}, AfterScript: []string{"b"}}
	if got := resolveBeforeScript(job, nil); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("resolveBeforeScript: expected [a], got %v", got)
	}
	if got := resolveAfterScript(job, nil); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("resolveAfterScript: expected [b], got %v", got)
	}

	defaults := &parser.JobDefaults{BeforeScript: []string{"default"}}
	if got := resolveBeforeScript(&parser.Job{}, defaults); !reflect.DeepEqual(got, []string{"default"}) {
		t.Fatalf("resolveBeforeScript: expected default, got %v", got)
	}
}

func TestBuildJobFilter(t *testing.T) {
	config := &parser.Config{
		Stages: []string{"build", "test"},
		Jobs: map[string]*parser.Job{
			"build": {
				Stage:  "build",
				Script: []string{"go build"},
			},
			"test": {
				Stage:  "test",
				Script: []string{"go test"},
			},
		},
	}
	pipe, err := Build(config, testContext(), []string{"test"}, false, nil)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	got := jobNames(pipe)
	want := []string{"test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}
