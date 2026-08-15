package parser

import (
	"testing"
)

func TestParseBasicConfig(t *testing.T) {
	yaml := []byte(`
stages:
  - build
  - test
  - deploy

build_job:
  stage: build
  image: golang:1.21
  script:
    - go build ./...

test_job:
  stage: test
  image: golang:1.21
  script:
    - go test ./...

deploy_job:
  stage: deploy
  image: alpine:latest
  script:
    - echo "deploying"
  when: manual
`)

	config, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(config.Stages) != 3 {
		t.Errorf("expected 3 stages, got %d", len(config.Stages))
	}

	if len(config.Jobs) != 3 {
		t.Errorf("expected 3 jobs, got %d", len(config.Jobs))
	}

	buildJob, ok := config.Jobs["build_job"]
	if !ok {
		t.Fatal("build_job not found")
	}
	if buildJob.Stage != "build" {
		t.Errorf("expected stage 'build', got %q", buildJob.Stage)
	}
	if buildJob.Image != "golang:1.21" {
		t.Errorf("expected image 'golang:1.21', got %q", buildJob.Image)
	}
	if len(buildJob.Script) != 1 || buildJob.Script[0] != "go build ./..." {
		t.Errorf("unexpected script: %v", buildJob.Script)
	}

	deployJob := config.Jobs["deploy_job"]
	if deployJob.When != "manual" {
		t.Errorf("expected when 'manual', got %q", deployJob.When)
	}
}

func TestParseDefaultStages(t *testing.T) {
	yaml := []byte(`
my_job:
  script:
    - echo hello
`)

	config, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Default stages should be build, test, deploy
	if len(config.Stages) != 3 {
		t.Errorf("expected 3 default stages, got %d", len(config.Stages))
	}

	// Job without stage should default to "test"
	job := config.Jobs["my_job"]
	if job == nil {
		t.Fatal("my_job not found")
	}
	if job.Stage != "test" {
		t.Errorf("expected default stage 'test', got %q", job.Stage)
	}
}

func TestParseHiddenJobsIgnored(t *testing.T) {
	yaml := []byte(`
.template:
  image: alpine
  script:
    - echo template

real_job:
  extends: .template
  stage: build
  script:
    - echo real
`)

	config, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if _, ok := config.Jobs[".template"]; ok {
		t.Error("hidden job .template should not be in Jobs map")
	}
	if _, ok := config.Jobs["real_job"]; !ok {
		t.Error("real_job should be in Jobs map")
	}
}

func TestParseEmptyConfig(t *testing.T) {
	_, err := Parse([]byte(""))
	if err != nil {
		t.Fatalf("empty config should not error, got: %v", err)
	}
}

func TestParseInvalidYAML(t *testing.T) {
	_, err := Parse([]byte("}{invalid"))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}
