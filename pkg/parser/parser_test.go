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

func TestParseImageWithVariable(t *testing.T) {
	yaml := []byte(`
stages:
  - build

build_job:
  stage: build
  image: $CI_REGISTRY/pr-team/pr/ubuntu-x86_64:focal
  script:
    - echo hello
`)
	config, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	job, ok := config.Jobs["build_job"]
	if !ok {
		t.Fatal("build_job not found")
	}
	want := "$CI_REGISTRY/pr-team/pr/ubuntu-x86_64:focal"
	if job.Image != want {
		t.Errorf("expected image %q, got %q", want, job.Image)
	}
}

func TestParseDefaultVariablesWorkflow(t *testing.T) {
	yaml := []byte(`
stages:
  - build

variables:
  CI_REGISTRY: registry.example.com
  DEPLOY_ENV:
    value: staging

default:
  image: alpine:latest
  before_script:
    - echo setup

workflow:
  rules:
    - if: $CI_COMMIT_BRANCH == "main"

build_job:
  stage: build
  script:
    - echo hello
`)
	config, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if config.Default == nil {
		t.Fatal("default not parsed")
	}
	if config.Default.Image != "alpine:latest" {
		t.Errorf("expected default image, got %q", config.Default.Image)
	}
	if len(config.Default.BeforeScript) != 1 || config.Default.BeforeScript[0] != "echo setup" {
		t.Errorf("unexpected default before_script: %v", config.Default.BeforeScript)
	}
	if config.Variables == nil {
		t.Fatal("variables not parsed")
	}
	if config.Variables["CI_REGISTRY"] != "registry.example.com" {
		t.Errorf("expected CI_REGISTRY, got %q", config.Variables["CI_REGISTRY"])
	}
	if config.Variables["DEPLOY_ENV"] != "staging" {
		t.Errorf("expected DEPLOY_ENV staging, got %q", config.Variables["DEPLOY_ENV"])
	}
	if config.Workflow == nil || len(config.Workflow.Rules) != 1 {
		t.Fatalf("workflow rules not parsed: %v", config.Workflow)
	}
}

func TestParseMissingScript(t *testing.T) {
	yaml := []byte(`
stages:
  - build

build_job:
  stage: build
  image: alpine:latest
`)
	config, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	job := config.Jobs["build_job"]
	if job == nil {
		t.Fatal("build_job not found")
	}
	if len(job.Script) != 0 {
		t.Errorf("expected empty script for job without script, got %v", job.Script)
	}
}

func TestParseUnknownStage(t *testing.T) {
	yaml := []byte(`
stages:
  - build

deploy_job:
  stage: deploy
  script:
    - echo deploy
`)
	config, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	job := config.Jobs["deploy_job"]
	if job.Stage != "deploy" {
		t.Errorf("expected stage 'deploy', got %q", job.Stage)
	}
	stageMap := make(map[string]bool)
	for _, s := range config.Stages {
		stageMap[s] = true
	}
	if stageMap["deploy"] {
		t.Error("unknown stage 'deploy' should not appear in config.Stages")
	}
}
