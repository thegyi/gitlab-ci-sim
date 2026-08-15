package rules

import (
	"os"
	"os/exec"
	"testing"

	"github.com/thegyi/gitlab-ci-sim/pkg/parser"
	"github.com/thegyi/gitlab-ci-sim/pkg/variables"
)

func testContext() *variables.Context {
	return &variables.Context{Vars: map[string]string{
		"CI":                 "true",
		"CI_COMMIT_BRANCH":   "main",
		"CI_PIPELINE_SOURCE": "push",
		"CI_PROJECT_NAME":    "demo",
	}}
}

func TestEvalIfEquality(t *testing.T) {
	ctx := testContext()
	ok, err := evalIf(`CI_COMMIT_BRANCH == "main"`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for main branch")
	}
}

func TestEvalIfRegex(t *testing.T) {
	ctx := testContext()
	ok, err := evalIf(`CI_COMMIT_BRANCH =~ /^main$/`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected regex match")
	}
}

func TestEvalIfCombined(t *testing.T) {
	ctx := testContext()
	ok, err := evalIf(`CI == "true" && CI_COMMIT_BRANCH == "main"`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected combined expression to be true")
	}
}

func TestShouldRunManual(t *testing.T) {
	ctx := testContext()
	job := &parser.Job{Name: "deploy", When: "manual"}
	res, err := ShouldRun(job, ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Run {
		t.Error("manual job should not run by default")
	}
}

func TestShouldRunRuleVariable(t *testing.T) {
	ctx := testContext()
	job := &parser.Job{
		Name: "deploy",
		Rules: []parser.Rule{
			{If: `CI_COMMIT_BRANCH == "main"`, Variables: map[string]string{"ENV": "prod"}},
		},
	}
	res, err := ShouldRun(job, ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Run {
		t.Error("rule should match")
	}
	if res.Variables["ENV"] != "prod" {
		t.Errorf("expected rule variable ENV=prod, got %v", res.Variables["ENV"])
	}
}

func TestOnlyRefs(t *testing.T) {
	ctx := testContext()
	job := &parser.Job{
		Name: "deploy",
		Only: &parser.OnlyExcept{Refs: []string{"main", "release"}},
	}
	res, err := ShouldRun(job, ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Run {
		t.Error("job on main should run")
	}

	ctx.Vars["CI_COMMIT_BRANCH"] = "feature"
	res, err = ShouldRun(job, ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Run {
		t.Error("job on feature should not run")
	}
}

func TestOnlyMergeRequests(t *testing.T) {
	ctx := testContext()
	job := &parser.Job{
		Name: "deploy",
		Only: &parser.OnlyExcept{Refs: []string{"merge_requests"}},
	}
	res, err := ShouldRun(job, ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Run {
		t.Error("job should not run outside merge request")
	}

	ctx.Vars["CI_PIPELINE_SOURCE"] = "merge_request_event"
	res, err = ShouldRun(job, ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Run {
		t.Error("job should run for merge request")
	}
}

func TestWhenNever(t *testing.T) {
	ctx := testContext()
	job := &parser.Job{
		Name:  "deploy",
		Rules: []parser.Rule{{If: `CI_COMMIT_BRANCH == "main"`, When: "never"}},
	}
	res, err := ShouldRun(job, ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Run {
		t.Error("when: never should not run")
	}
}

func TestWhenManualAllowOverride(t *testing.T) {
	ctx := testContext()
	job := &parser.Job{Name: "deploy", When: "manual"}
	res, err := ShouldRun(job, ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Run {
		t.Error("manual job should not run by default")
	}

	res, err = ShouldRun(job, ctx, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Run {
		t.Error("manual job should run when explicitly allowed")
	}
}

func TestEvalIfRegexNotMatch(t *testing.T) {
	ctx := testContext()
	ok, err := evalIf(`CI_COMMIT_BRANCH !~ /^feature/`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("!~ should be true because main does not match ^feature")
	}
}

func TestChangesMatch(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })

	if err := os.WriteFile("main.go", []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("README.md", []byte("#"), 0o644); err != nil {
		t.Fatal(err)
	}

	exec.Command("git", "init").Run()
	exec.Command("git", "-c", "user.email=test@test", "-c", "user.name=Test", "add", ".").Run()
	exec.Command("git", "-c", "user.email=test@test", "-c", "user.name=Test", "commit", "-m", "init").Run()

	if err := os.WriteFile("main.go", []byte("package main\n// changed"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !changesMatch([]string{"*.go"}) {
		t.Error("expected changesMatch to find modified .go file")
	}
	if changesMatch([]string{"*.yaml"}) {
		t.Error("expected no match for .yaml files")
	}
}

func TestExistsMatch(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })

	if err := os.WriteFile("ci.yml", []byte("stages: [build]"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !existsMatch([]string{"*.yml"}) {
		t.Error("expected existsMatch to find ci.yml")
	}
	if existsMatch([]string{"*.yaml"}) {
		t.Error("expected no match for .yaml files")
	}
}
