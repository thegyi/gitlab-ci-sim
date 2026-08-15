package variables

import (
	"os"
	"os/exec"
	"testing"
)

func makeGitRepo(t *testing.T) string {
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })

	if err := os.WriteFile("README.md", []byte("#"), 0o644); err != nil {
		t.Fatal(err)
	}
	exec.Command("git", "init").Run()
	exec.Command("git", "-c", "user.email=test@test", "-c", "user.name=Test", "add", ".").Run()
	exec.Command("git", "-c", "user.email=test@test", "-c", "user.name=Test", "commit", "-m", "init").Run()
	return dir
}

func TestContextWith(t *testing.T) {
	ctx := &Context{Vars: map[string]string{"A": "1", "B": "2"}}
	n := ctx.With(map[string]string{"B": "3", "C": "4"})

	if n.Vars["A"] != "1" {
		t.Errorf("expected A=1, got %q", n.Vars["A"])
	}
	if n.Vars["B"] != "3" {
		t.Errorf("expected B=3, got %q", n.Vars["B"])
	}
	if n.Vars["C"] != "4" {
		t.Errorf("expected C=4, got %q", n.Vars["C"])
	}
	if ctx.Vars["B"] != "2" {
		t.Error("original context should not be mutated")
	}
}

func TestContextExpand(t *testing.T) {
	ctx := &Context{Vars: map[string]string{
		"NAME":  "world",
		"GREET": "hello",
	}}

	if got := ctx.Expand("$GREET, $NAME!"); got != "hello, world!" {
		t.Errorf("expected 'hello, world!', got %q", got)
	}
	if got := ctx.Expand("${GREET}, ${NAME}!"); got != "hello, world!" {
		t.Errorf("expected 'hello, world!', got %q", got)
	}
	if got := ctx.Expand("$UNKNOWN"); got != "$UNKNOWN" {
		t.Errorf("expected '$UNKNOWN' left as-is, got %q", got)
	}
}

func TestContextMissingValues(t *testing.T) {
	ctx := &Context{Vars: map[string]string{
		"KNOWN": "value",
		"EMPTY": "",
	}}

	missing := ctx.MissingValues("echo $KNOWN", "echo $UNKNOWN", "echo $EMPTY", "${KNOWN}-${UNKNOWN}")
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing/empty, got %d", len(missing))
	}
	if missing[0] != "UNKNOWN" || missing[1] != "EMPTY" {
		t.Errorf("expected [UNKNOWN EMPTY], got %v", missing)
	}
}

func TestBuildPrecedence(t *testing.T) {
	makeGitRepo(t)
	ctx, err := Build("main", map[string]string{
		"CI_PROJECT_DIR": "/custom",
		"FROM_CONFIG":    "config",
	}, map[string]bool{"CI_PROJECT_DIR": true}, []string{"FROM_CONFIG=override"})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if ctx.Vars["CI_PROJECT_DIR"] != "/custom" {
		t.Errorf("expected CI_PROJECT_DIR override to win, got %q", ctx.Vars["CI_PROJECT_DIR"])
	}
	if ctx.Vars["FROM_CONFIG"] != "override" {
		t.Errorf("expected CLI override to win, got %q", ctx.Vars["FROM_CONFIG"])
	}
	if ctx.Vars["CI_COMMIT_BRANCH"] != "main" {
		t.Errorf("expected branch main, got %q", ctx.Vars["CI_COMMIT_BRANCH"])
	}
	if ctx.Vars["CI_PIPELINE_SOURCE"] != "push" {
		t.Errorf("expected CI_PIPELINE_SOURCE push, got %q", ctx.Vars["CI_PIPELINE_SOURCE"])
	}
}
