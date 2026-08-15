package rules

import (
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
