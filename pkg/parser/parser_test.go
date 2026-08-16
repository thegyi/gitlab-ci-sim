package parser

import (
	"reflect"
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
	if buildJob.Image.Name != "golang:1.21" {
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
	if job.Image.Name != want {
		t.Errorf("expected image %q, got %q", want, job.Image.Name)
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
	if config.Default.Image.Name != "alpine:latest" {
		t.Errorf("expected default image, got %q", config.Default.Image.Name)
	}
	if len(config.Default.BeforeScript) != 1 || config.Default.BeforeScript[0] != "echo setup" {
		t.Errorf("unexpected default before_script: %v", config.Default.BeforeScript)
	}
	if config.Variables == nil {
		t.Fatal("variables not parsed")
	}
	if config.Variables["CI_REGISTRY"].Value != "registry.example.com" {
		t.Errorf("expected CI_REGISTRY, got %q", config.Variables["CI_REGISTRY"].Value)
	}
	if config.Variables["DEPLOY_ENV"].Value != "staging" {
		t.Errorf("expected DEPLOY_ENV staging, got %q", config.Variables["DEPLOY_ENV"].Value)
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

func TestParseAllJobFields(t *testing.T) {
	yaml := []byte(`
stages:
  - build

build_job:
  stage: build
  image: alpine
  before_script:
    - echo before
  after_script:
    - echo after
  script:
    - echo main
  needs:
    - prep
  dependencies:
    - prep
  tags:
    - docker
  allow_failure: true
  when: delayed
  start_in: 5 minutes
  retry:
    max: 2
    when:
      - script_failure
  parallel: 2
  cache:
    key: v1
    paths:
      - .cache
  artifacts:
    paths:
      - out
    when: on_success
  services:
    - name: redis:alpine
      alias: redis
`)
	config, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	job := config.Jobs["build_job"]
	if job == nil {
		t.Fatal("build_job not found")
	}
	if job.BeforeScript[0] != "echo before" {
		t.Errorf("before_script: %v", job.BeforeScript)
	}
	if job.AfterScript[0] != "echo after" {
		t.Errorf("after_script: %v", job.AfterScript)
	}
	if len(job.Needs) != 1 || job.Needs[0].Job != "prep" {
		t.Errorf("needs: %v", job.Needs)
	}
	if len(job.Dependencies) != 1 || job.Dependencies[0] != "prep" {
		t.Errorf("dependencies: %v", job.Dependencies)
	}
	if !job.AllowFailure.Value || job.When != "delayed" || job.StartIn != "5 minutes" {
		t.Errorf("when/start_in mismatch: %v %v %v", job.AllowFailure, job.When, job.StartIn)
	}
	if job.Retry == nil || job.Retry.Max != 2 {
		t.Errorf("retry: %v", job.Retry)
	}
	if job.Parallel == nil || job.Parallel.Scalar != 2 {
		t.Errorf("parallel: %v", job.Parallel)
	}
	if job.Cache == nil || job.Cache.Key == nil || job.Cache.Key.Prefix != "v1" {
		t.Errorf("cache: %v", job.Cache)
	}
	if job.Artifacts == nil || len(job.Artifacts.Paths) != 1 {
		t.Errorf("artifacts: %v", job.Artifacts)
	}
	if len(job.Services) != 1 || job.Services[0].Name != "redis:alpine" {
		t.Errorf("services: %v", job.Services)
	}
}

func TestParseFileNotFound(t *testing.T) {
	_, err := ParseFile("/nonexistent/gitlab-ci.yml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseTriggerScalarAndMapping(t *testing.T) {
	yaml1 := []byte(`
stages:
  - build
trigger_job:
  stage: build
  trigger: group/project
`)
	config, err := Parse(yaml1)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if config.Jobs["trigger_job"].Trigger.Project != "group/project" {
		t.Errorf("expected trigger project, got %v", config.Jobs["trigger_job"].Trigger)
	}

	yaml2 := []byte(`
stages:
  - build
trigger_job:
  stage: build
  trigger:
    project: group/project
    branch: main
`)
	config, err = Parse(yaml2)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if config.Jobs["trigger_job"].Trigger.Branch != "main" {
		t.Errorf("expected trigger branch main, got %v", config.Jobs["trigger_job"].Trigger.Branch)
	}
}

func TestParseImageObject(t *testing.T) {
	yaml := []byte(`
build:
  image:
    name: alpine:latest
    entrypoint: ["/bin/sh", "-c"]
  script:
    - echo
`)
	config, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	job := config.Jobs["build"]
	if job == nil {
		t.Fatal("build not found")
	}
	if job.Image.Name != "alpine:latest" || len(job.Image.Entrypoint) != 2 {
		t.Errorf("image: %v", job.Image)
	}
}

func TestParseCacheObject(t *testing.T) {
	yaml := []byte(`
build:
  cache:
    key:
      prefix: go
      files:
        - go.mod
    paths:
      - vendor
    untracked: true
    when: always
  script:
    - echo
`)
	config, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	job := config.Jobs["build"]
	if job == nil {
		t.Fatal("build not found")
	}
	if job.Cache == nil || job.Cache.Key == nil {
		t.Fatal("cache not parsed")
	}
	if job.Cache.Key.Prefix != "go" || len(job.Cache.Key.Files) != 1 || job.Cache.Key.Files[0] != "go.mod" {
		t.Errorf("cache key: %v", job.Cache.Key)
	}
	if !job.Cache.Untracked || job.Cache.When != "always" {
		t.Errorf("cache flags: untracked=%v when=%q", job.Cache.Untracked, job.Cache.When)
	}
}

func TestParseNeeds(t *testing.T) {
	yaml := []byte(`
stages:
  - build
  - test

build:
  stage: build
  script:
    - echo

test:
  stage: test
  needs:
    - build
    - job: extra
      optional: true
      artifacts: false
  script:
    - echo
`)
	config, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	job := config.Jobs["test"]
	if job == nil {
		t.Fatal("test not found")
	}
	if len(job.Needs) != 2 {
		t.Fatalf("expected 2 needs, got %d", len(job.Needs))
	}
	if job.Needs[0].Job != "build" || job.Needs[0].Optional || !*job.Needs[0].Artifacts {
		t.Errorf("build need: %v", job.Needs[0])
	}
	if job.Needs[1].Job != "extra" || !job.Needs[1].Optional || *job.Needs[1].Artifacts {
		t.Errorf("extra need: %v", job.Needs[1])
	}
}

func TestParseAllowFailureExitCodes(t *testing.T) {
	yaml := []byte(`
stages:
  - build

build_job:
  stage: build
  image: alpine
  allow_failure:
    exit_codes:
      - 1
      - 255
  script:
    - echo
`)
	config, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	job := config.Jobs["build_job"]
	if job == nil {
		t.Fatal("build_job not found")
	}
	if !reflect.DeepEqual(job.AllowFailure.ExitCodes, []int{1, 255}) {
		t.Errorf("expected exit codes [1, 255], got %v", job.AllowFailure.ExitCodes)
	}
}

func TestParseRulesAndOnlyExcept(t *testing.T) {
	yaml := []byte(`
stages:
  - build

job1:
  stage: build
  rules:
    - if: $CI_COMMIT_BRANCH == "main"
      when: manual
      variables:
        ENV: prod
  script:
    - echo

job2:
  stage: build
  only:
    refs:
      - main
  except:
    refs:
      - develop
  script:
    - echo
`)
	config, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(config.Jobs["job1"].Rules) != 1 {
		t.Errorf("rules: %v", config.Jobs["job1"].Rules)
	}
	if config.Jobs["job1"].Rules[0].Variables["ENV"] != "prod" {
		t.Errorf("rule variables: %v", config.Jobs["job1"].Rules[0].Variables)
	}
	if config.Jobs["job2"].Only == nil || config.Jobs["job2"].Except == nil {
		t.Errorf("only/except not parsed")
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
