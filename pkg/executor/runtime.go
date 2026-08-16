// Package executor contains the runtime-agnostic executor and runtime backends.
package executor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Volume maps a host path into a container.
type Volume struct {
	Host      string
	Container string
}

// RunOpts holds options for running a one-shot container.
type RunOpts struct {
	Image      string
	WorkDir    string
	Network    string
	Env        []string
	Volumes    []Volume
	Script     string
	Entrypoint string
	Stdout     io.Writer
	Stderr     io.Writer
}

// ServiceOpts holds options for running a long-lived service container.
type ServiceOpts struct {
	Image   string
	Network string
	Alias   string
	Env     []string
	Command []string
}

// Runtime abstracts the container runtime used to execute jobs.
type Runtime interface {
	CreateNetwork(ctx context.Context, name string) error
	RemoveNetwork(ctx context.Context, name string) error
	Run(ctx context.Context, opts RunOpts) (int, error)
	RunDetached(ctx context.Context, opts ServiceOpts) (string, error)
	Stop(ctx context.Context, id string) error
}

// NewRuntime returns a Runtime implementation for the given container command.
// Recognised names: "docker", "podman", or "fake".
func NewRuntime(name string) (Runtime, error) {
	switch name {
	case "fake":
		return &FakeRuntime{}, nil
	case "", "docker", "podman":
		if name == "" {
			name = "docker"
		}
		path, err := exec.LookPath(name)
		if err != nil {
			return nil, fmt.Errorf("%s not found: %w", name, err)
		}
		return &CmdRuntime{binary: path}, nil
	default:
		return nil, fmt.Errorf("unknown runtime %q", name)
	}
}

// CmdRuntime implements Runtime using a container CLI like docker or podman.
type CmdRuntime struct {
	binary string
}

func (r *CmdRuntime) CreateNetwork(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, r.binary, "network", "create", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("network create: %s: %w", string(out), err)
	}
	return nil
}

func (r *CmdRuntime) RemoveNetwork(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, r.binary, "network", "rm", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("network rm: %s: %w", string(out), err)
	}
	return nil
}

func (r *CmdRuntime) Run(ctx context.Context, opts RunOpts) (int, error) {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}

	// When no image is specified, act as a shell executor and run the script
	// directly on the host in the project working directory.
	if opts.Image == "" {
		cmd := exec.CommandContext(ctx, "sh", "-c", opts.Script)
		cmd.Dir = opts.WorkDir
		cmd.Env = append(os.Environ(), opts.Env...)
		cmd.Stdout = opts.Stdout
		cmd.Stderr = opts.Stderr
		err := cmd.Run()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return exitErr.ExitCode(), nil
			}
			return -1, fmt.Errorf("run: %w", err)
		}
		return 0, nil
	}

	if opts.Entrypoint == "" {
		opts.Entrypoint = "sh"
	}
	args := []string{
		"run", "--rm", "-i",
		"-v", fmt.Sprintf("%s:%s", opts.WorkDir, opts.WorkDir),
		"-w", opts.WorkDir,
		"--entrypoint", opts.Entrypoint,
	}
	if opts.Network != "" {
		args = append(args, "--network", opts.Network)
	}
	for _, e := range opts.Env {
		args = append(args, "-e", e)
	}
	for _, v := range opts.Volumes {
		args = append(args, "-v", fmt.Sprintf("%s:%s", v.Host, v.Container))
	}
	args = append(args, opts.Image)

	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Stdin = strings.NewReader(opts.Script)
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return -1, fmt.Errorf("run: %w", err)
	}
	return 0, nil
}

func (r *CmdRuntime) RunDetached(ctx context.Context, opts ServiceOpts) (string, error) {
	if opts.Alias == "" {
		opts.Alias = "service"
	}
	args := []string{
		"run", "-d", "--rm",
		"--network", opts.Network,
		"--network-alias", opts.Alias,
	}
	for _, e := range opts.Env {
		args = append(args, "-e", e)
	}
	args = append(args, opts.Image)
	args = append(args, opts.Command...)

	cmd := exec.CommandContext(ctx, r.binary, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("run service %s: %w", opts.Image, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *CmdRuntime) Stop(ctx context.Context, id string) error {
	cmd := exec.CommandContext(ctx, r.binary, "stop", "-t", "2", id)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("stop: %s: %w", string(out), err)
	}
	return nil
}

// FakeRuntime is a no-op runtime for testing that echoes the given script.
type FakeRuntime struct {
	Networks map[string]bool
}

func (f *FakeRuntime) CreateNetwork(ctx context.Context, name string) error {
	if f.Networks == nil {
		f.Networks = make(map[string]bool)
	}
	f.Networks[name] = true
	return nil
}

func (f *FakeRuntime) RemoveNetwork(ctx context.Context, name string) error {
	if f.Networks != nil {
		delete(f.Networks, name)
	}
	return nil
}

func (f *FakeRuntime) Run(ctx context.Context, opts RunOpts) (int, error) {
	if opts.Stdout != nil {
		fmt.Fprint(opts.Stdout, opts.Script)
	}
	return 0, nil
}

func (f *FakeRuntime) RunDetached(ctx context.Context, opts ServiceOpts) (string, error) {
	return "fake-service-id", nil
}

func (f *FakeRuntime) Stop(ctx context.Context, id string) error {
	return nil
}
