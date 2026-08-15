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

func TestEvaluateWorkflow(t *testing.T) {
	ctx := testContext()

	// No workflow -> run
	res, err := EvaluateWorkflow(nil, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Run {
		t.Error("nil workflow should run")
	}

	// Matching rule with when: never -> do not run
	wf := &parser.Workflow{
		Rules: []parser.Rule{{If: `CI_COMMIT_BRANCH == "main"`, When: "never"}},
	}
	res, err = EvaluateWorkflow(wf, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Run {
		t.Error("workflow when: never should not run")
	}

	// Matching rule with variables -> run and set variables
	wf = &parser.Workflow{
		Rules: []parser.Rule{{If: `CI_COMMIT_BRANCH == "main"`, Variables: map[string]string{"GLOBAL": "set"}}},
	}
	res, err = EvaluateWorkflow(wf, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Run {
		t.Error("workflow rule should run")
	}
	if res.Variables["GLOBAL"] != "set" {
		t.Errorf("expected workflow variable GLOBAL=set, got %q", res.Variables["GLOBAL"])
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

func TestExceptRefs(t *testing.T) {
	ctx := testContext()
	job := &parser.Job{
		Name:   "deploy",
		Except: &parser.OnlyExcept{Refs: []string{"main"}},
	}
	res, err := ShouldRun(job, ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Run {
		t.Error("job should not run on main because of except")
	}
}

func TestOnlyVariables(t *testing.T) {
	ctx := testContext()
	job := &parser.Job{
		Name: "deploy",
		Only: &parser.OnlyExcept{
			Refs:      []string{"main"},
			Variables: []string{`$DEPLOY == "true"`},
		},
	}
	ctx.Vars["DEPLOY"] = "true"
	res, err := ShouldRun(job, ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Run {
		t.Error("job should run when ref and variable match")
	}

	ctx.Vars["DEPLOY"] = "false"
	res, err = ShouldRun(job, ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Run {
		t.Error("job should not run when variable does not match")
	}

	job = &parser.Job{
		Name: "deploy",
		Only: &parser.OnlyExcept{
			Variables: []string{"$DEPLOY"},
		},
	}
	ctx.Vars["DEPLOY"] = "yes"
	res, err = ShouldRun(job, ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Run {
		t.Error("bare variable should be truthy when non-empty")
	}
}

func TestExceptVariables(t *testing.T) {
	ctx := testContext()
	job := &parser.Job{
		Name:   "deploy",
		Except: &parser.OnlyExcept{Variables: []string{`$SKIP == "yes"`}},
	}
	ctx.Vars["SKIP"] = "yes"
	res, err := ShouldRun(job, ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Run {
		t.Error("job should be excluded when except variable matches")
	}

	ctx.Vars["SKIP"] = "no"
	res, err = ShouldRun(job, ctx, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Run {
		t.Error("job should run when except variable does not match")
	}
}

func TestMatchRefs(t *testing.T) {
	cases := []struct {
		refs   []string
		branch string
		source string
		want   bool
	}{
		{[]string{"main"}, "main", "push", true},
		{[]string{"tags"}, "refs/tags/v1", "push", true},
		{[]string{"merge_requests"}, "main", "merge_request_event", true},
		{[]string{"pipelines"}, "main", "pipeline", true},
		{[]string{"schedules"}, "main", "schedule", true},
		{[]string{"main"}, "feature", "push", false},
	}
	for _, c := range cases {
		got := matchRefs(c.refs, c.branch, c.source)
		if got != c.want {
			t.Errorf("matchRefs(%v, %q, %q) = %v, want %v", c.refs, c.branch, c.source, got, c.want)
		}
	}
}

func TestBuildResult(t *testing.T) {
	cases := []struct {
		when   string
		manual bool
		want   bool
	}{
		{"", false, true},
		{"on_success", false, true},
		{"never", false, false},
		{"manual", false, false},
		{"manual", true, true},
		{"delayed", false, true},
	}
	for _, c := range cases {
		res := buildResult(c.when, nil, c.manual)
		if res.Run != c.want {
			t.Errorf("buildResult(%q, manual=%v).Run = %v, want %v", c.when, c.manual, res.Run, c.want)
		}
	}
}

func TestMergeAndFormatVariables(t *testing.T) {
	merged := MergeVariables(
		map[string]string{"A": "1", "B": "2"},
		map[string]string{"B": "3", "C": "4"},
	)
	if merged["A"] != "1" || merged["B"] != "3" || merged["C"] != "4" {
		t.Fatalf("unexpected merged variables: %v", merged)
	}

	formatted := FormatVariables(map[string]string{"Z": "1", "A": "2"})
	want := "A=2\nZ=1\n"
	if formatted != want {
		t.Fatalf("FormatVariables: expected %q, got %q", want, formatted)
	}
}

func TestEvalIfVariableBraces(t *testing.T) {
	ctx := testContext()
	ok, err := evalIf(`${CI_COMMIT_BRANCH} == "main"`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected true for braced variable")
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
