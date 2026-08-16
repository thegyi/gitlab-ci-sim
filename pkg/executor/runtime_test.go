package executor

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
)

func TestRuntimeNewRuntime(t *testing.T) {
	rt, err := NewRuntime("fake")
	if err != nil {
		t.Fatalf("expected fake runtime, got %v", err)
	}
	if _, ok := rt.(*FakeRuntime); !ok {
		t.Fatalf("expected *FakeRuntime, got %T", rt)
	}

	rt, err = NewRuntime("unknown")
	if err == nil {
		t.Fatal("expected error for unknown runtime")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unexpected error: %v", err)
	}

}

func TestFakeRuntime(t *testing.T) {
	f := &FakeRuntime{}

	if err := f.CreateNetwork(context.Background(), "n"); err != nil {
		t.Errorf("CreateNetwork: %v", err)
	}
	if f.Networks["n"] != true {
		t.Error("network not recorded")
	}
	if err := f.RemoveNetwork(context.Background(), "n"); err != nil {
		t.Errorf("RemoveNetwork: %v", err)
	}
	if _, ok := f.Networks["n"]; ok {
		t.Error("network not removed")
	}

	var out strings.Builder
	exit, err := f.Run(context.Background(), RunOpts{Script: "hello", Stdout: &out})
	if err != nil || exit != 0 {
		t.Fatalf("Run: %v, exit %d", err, exit)
	}
	if out.String() != "hello" {
		t.Errorf("expected hello, got %q", out.String())
	}

	id, err := f.RunDetached(context.Background(), ServiceOpts{})
	if err != nil || id != "fake-service-id" {
		t.Errorf("RunDetached: %v %q", err, id)
	}

	if err := f.Stop(context.Background(), id); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestCmdRuntimeDockerBranch(t *testing.T) {
	if _, err := exec.LookPath("false"); err != nil {
		t.Skip("false not available")
	}
	r := &CmdRuntime{binary: "false"}
	exit, err := r.Run(context.Background(), RunOpts{
		Image:      "alpine:latest",
		WorkDir:    t.TempDir(),
		Script:     "echo test",
		Entrypoint: "",
		Stdout:     io.Discard,
		Stderr:     io.Discard,
	})
	if err != nil {
		t.Fatalf("expected non-zero exit without error, got %v", err)
	}
	if exit != 1 {
		t.Errorf("expected exit 1, got %d", exit)
	}
}

func TestShouldSaveCacheForWhen(t *testing.T) {
	if !shouldSaveCacheForWhen("always", false) {
		t.Error("always should save")
	}
	if !shouldSaveCacheForWhen("", true) {
		t.Error("empty when with success should save")
	}
	if !shouldSaveCacheForWhen("on_success", true) {
		t.Error("on_success with success should save")
	}
	if shouldSaveCacheForWhen("on_success", false) {
		t.Error("on_success with failure should not save")
	}
	if !shouldSaveCacheForWhen("always", false) {
		t.Error("always with failure should save")
	}
}

func TestCmdRuntimeShell(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	r := &CmdRuntime{binary: "docker"}
	var out strings.Builder
	exit, err := r.Run(context.Background(), RunOpts{
		Image:      "",
		WorkDir:    t.TempDir(),
		Script:     "echo test",
		Entrypoint: "",
		Stdout:     &out,
		Stderr:     io.Discard,
	})
	if err != nil || exit != 0 {
		t.Fatalf("Run: %v, exit %d", err, exit)
	}
	if !strings.Contains(out.String(), "test") {
		t.Errorf("expected 'test', got %q", out.String())
	}
}
