package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/thegyi/gitlab-ci-sim/pkg/artifacts"
	"github.com/thegyi/gitlab-ci-sim/pkg/pipeline"
	"github.com/thegyi/gitlab-ci-sim/pkg/variables"
)

// Result holds the outcome of a pipeline execution.
type Result struct {
	Success    bool
	JobResults []*JobResult
	Duration   time.Duration
}

// JobResult holds the outcome of a single job.
type JobResult struct {
	Name     string
	Success  bool
	Output   string
	Duration time.Duration
}

// Print outputs the pipeline result summary.
func (r *Result) Print(w io.Writer) {
	fmt.Fprintf(w, "\n═══════════════════════════════════════\n")
	fmt.Fprintf(w, "Pipeline Result: ")
	if r.Success {
		fmt.Fprintf(w, "PASSED ✓\n")
	} else {
		fmt.Fprintf(w, "FAILED ✗\n")
	}
	fmt.Fprintf(w, "Duration: %s\n", r.Duration.Round(time.Millisecond))
	fmt.Fprintf(w, "───────────────────────────────────────\n")
	for _, jr := range r.JobResults {
		status := "✓"
		if !jr.Success {
			status = "✗"
		}
		fmt.Fprintf(w, "  %s %s (%s)\n", status, jr.Name, jr.Duration.Round(time.Millisecond))
	}
	fmt.Fprintf(w, "═══════════════════════════════════════\n")
}

// DockerExecutor runs jobs in Docker containers.
type DockerExecutor struct {
	client string
	store  *artifacts.Store
}

// NewDockerExecutor creates a new Docker-based executor.
func NewDockerExecutor(store *artifacts.Store) *DockerExecutor {
	client, _ := exec.LookPath("docker")
	if client == "" {
		client = "docker"
	}
	return &DockerExecutor{client: client, store: store}
}

// Run executes the full pipeline stage by stage.
func (e *DockerExecutor) Run(pipe *pipeline.Pipeline, vars *variables.Context) *Result {
	start := time.Now()
	result := &Result{Success: true}

	for _, stage := range pipe.Stages {
		fmt.Fprintf(os.Stdout, "\n┌─ Stage: %s\n", stage.Name)
		for _, job := range stage.Jobs {
			jr := e.runJob(context.Background(), job, vars)
			result.JobResults = append(result.JobResults, jr)
			if !jr.Success && !job.AllowFailure {
				result.Success = false
				result.Duration = time.Since(start)
				return result
			}
		}
	}

	result.Duration = time.Since(start)
	return result
}

// runJob executes a single job in a Docker container.
func (e *DockerExecutor) runJob(ctx context.Context, job *pipeline.PipelineJob, vars *variables.Context) *JobResult {
	start := time.Now()
	fmt.Fprintf(os.Stdout, "│  ┌─ Job: %s [image: %s]\n", job.Name, job.Image)

	jobCtx := vars.With(job.Variables)
	workDir, _ := os.Getwd()

	missing := jobCtx.MissingValues(executionStrings(job)...)
	if len(missing) > 0 {
		msg := fmt.Sprintf("undefined/empty variables: %s", strings.Join(missing, ", "))
		fmt.Fprintf(os.Stdout, "│  │  Error: %s\n", msg)
		fmt.Fprintf(os.Stdout, "│  └─ Job %s: FAILED\n", job.Name)
		return &JobResult{
			Name:     job.Name,
			Success:  false,
			Output:   msg,
			Duration: time.Since(start),
		}
	}

	// Restore artifacts from jobs this one depends on.
	if e.store != nil {
		for _, need := range job.Needs {
			_ = e.store.Restore(need, workDir)
		}
	}

	mainScript := "set -e\n" + buildShellScript(append(job.BeforeScript, job.Script...))
	mainExit, mainOut, err := e.runContainer(ctx, job, jobCtx, workDir, mainScript)
	if err != nil {
		fmt.Fprintf(os.Stdout, "│  │  Error: %v\n", err)
		fmt.Fprintf(os.Stdout, "│  └─ Job %s: FAILED\n", job.Name)
		return &JobResult{
			Name:     job.Name,
			Success:  false,
			Output:   mainOut,
			Duration: time.Since(start),
		}
	}
	if mainOut != "" {
		fmt.Fprint(os.Stdout, mainOut)
	}

	afterOut := ""
	if len(job.AfterScript) > 0 {
		_, afterOut, _ = e.runContainer(ctx, job, jobCtx, workDir, buildShellScript(job.AfterScript))
		if afterOut != "" {
			fmt.Fprint(os.Stdout, afterOut)
		}
	}

	success := mainExit == 0
	if success {
		fmt.Fprintf(os.Stdout, "│  └─ Job %s: PASSED\n", job.Name)
	} else {
		fmt.Fprintf(os.Stdout, "│  └─ Job %s: FAILED (exit %d)\n", job.Name, mainExit)
	}

	// Save artifacts if the job produced any and the policy matches.
	if e.store != nil && job.Artifacts != nil && shouldSaveArtifacts(mainExit, job.Artifacts.When) {
		_ = e.store.Save(job.Name, workDir, job.Artifacts.Paths)
	}

	return &JobResult{
		Name:     job.Name,
		Success:  success,
		Output:   mainOut + afterOut,
		Duration: time.Since(start),
	}
}

func shouldSaveArtifacts(exit int, when string) bool {
	switch when {
	case "always":
		return true
	case "on_failure":
		return exit != 0
	case "on_success", "":
		return exit == 0
	}
	return false
}

// runContainer runs a Docker container with the given script on stdin.
func (e *DockerExecutor) runContainer(ctx context.Context, job *pipeline.PipelineJob, jobCtx *variables.Context, workDir, script string) (int, string, error) {
	args := []string{
		"run", "--rm", "-i",
		"-v", workDir + ":/builds/project",
		"-w", "/builds/project",
		"--entrypoint", "sh",
	}
	for k, v := range jobCtx.Vars {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, job.Image)

	cmd := exec.CommandContext(ctx, e.client, args...)
	cmd.Stdin = strings.NewReader(script)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), out.String(), nil
		}
		return -1, out.String(), fmt.Errorf("docker run: %w", err)
	}
	return 0, out.String(), nil
}

// buildShellScript combines script lines into a single shell script.
func buildShellScript(lines []string) string {
	return strings.Join(lines, "\n")
}

// executionStrings collects all strings that may contain CI variable references.
func executionStrings(job *pipeline.PipelineJob) []string {
	var ss []string
	ss = append(ss, job.Image)
	ss = append(ss, job.BeforeScript...)
	ss = append(ss, job.Script...)
	ss = append(ss, job.AfterScript...)
	for _, svc := range job.Services {
		ss = append(ss, svc.Name, svc.Alias)
		ss = append(ss, svc.Command...)
	}
	if job.Cache != nil {
		ss = append(ss, job.Cache.Key)
		ss = append(ss, job.Cache.Paths...)
	}
	if job.Artifacts != nil {
		ss = append(ss, job.Artifacts.Paths...)
	}
	return ss
}
