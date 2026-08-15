package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/thegyi/gitlab-ci-sim/pkg/artifacts"
	"github.com/thegyi/gitlab-ci-sim/pkg/parser"
	"github.com/thegyi/gitlab-ci-sim/pkg/pipeline"
	"github.com/thegyi/gitlab-ci-sim/pkg/term"
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
		fmt.Fprintf(w, "%s\n", term.Green("PASSED ✓"))
	} else {
		fmt.Fprintf(w, "%s\n", term.Red("FAILED ✗"))
	}
	fmt.Fprintf(w, "Duration: %s\n", r.Duration.Round(time.Millisecond))
	fmt.Fprintf(w, "───────────────────────────────────────\n")
	for _, jr := range r.JobResults {
		status := term.Green("✓")
		if !jr.Success {
			status = term.Red("✗")
		}
		fmt.Fprintf(w, "  %s %s (%s)\n", status, jr.Name, jr.Duration.Round(time.Millisecond))
	}
	fmt.Fprintf(w, "═══════════════════════════════════════\n")
}

// DockerExecutor runs jobs in Docker containers.
type DockerExecutor struct {
	client string
	store  *artifacts.Store
	cache  *artifacts.Store
	mu     sync.Mutex
}

// NewDockerExecutor creates a new Docker-based executor.
func NewDockerExecutor(store, cache *artifacts.Store) *DockerExecutor {
	client, _ := exec.LookPath("docker")
	if client == "" {
		client = "docker"
	}
	return &DockerExecutor{client: client, store: store, cache: cache}
}

// Run executes the pipeline as a DAG respecting job needs.
func (e *DockerExecutor) Run(pipe *pipeline.Pipeline, vars *variables.Context) *Result {
	start := time.Now()
	result := &Result{Success: true}

	jobs := allPipelineJobs(pipe)
	byName := make(map[string]*pipeline.PipelineJob, len(jobs))
	for _, j := range jobs {
		byName[j.Name] = j
	}

	pending := make([]*pipeline.PipelineJob, len(jobs))
	copy(pending, jobs)
	completedSuccess := make(map[string]bool)
	running := 0
	done := make(chan *JobResult)

	for len(pending) > 0 || running > 0 {
		var stillPending []*pipeline.PipelineJob
		for _, job := range pending {
			if needsMet(job, completedSuccess) {
				go func(j *pipeline.PipelineJob) {
					jr := e.runJob(context.Background(), j, vars)
					done <- jr
				}(job)
				running++
			} else {
				stillPending = append(stillPending, job)
			}
		}
		pending = stillPending

		if running == 0 {
			// No jobs are running and no new ones were ready -> dependency issue or cycle.
			for _, job := range pending {
				e.mu.Lock()
				fmt.Fprintf(os.Stdout, "│  Job %s: %s (dependencies not met)\n", job.Name, term.Yellow("SKIPPED"))
				e.mu.Unlock()
				result.JobResults = append(result.JobResults, &JobResult{
					Name:    job.Name,
					Success: false,
				})
				result.Success = false
			}
			break
		}

		jr := <-done
		running--
		result.JobResults = append(result.JobResults, jr)
		job := byName[jr.Name]
		effective := jr.Success || (job != nil && job.AllowFailure)
		completedSuccess[jr.Name] = effective
		if !effective {
			result.Success = false
		}
	}

	result.Duration = time.Since(start)
	return result
}

// runJob executes a single job in a Docker container.
func (e *DockerExecutor) runJob(ctx context.Context, job *pipeline.PipelineJob, vars *variables.Context) *JobResult {
	start := time.Now()
	var out strings.Builder
	fmt.Fprintf(&out, "│  ┌─ Job: %s [image: %s]\n", job.Name, job.Image)

	jobCtx := vars.With(job.Variables)
	workDir, _ := os.Getwd()

	missing := jobCtx.MissingValues(executionStrings(job)...)
	if len(missing) > 0 {
		msg := fmt.Sprintf("undefined/empty variables: %s", strings.Join(missing, ", "))
		fmt.Fprintf(&out, "│  │  %s: %s\n", term.Red("Error"), msg)
		fmt.Fprintf(&out, "│  └─ Job %s: %s\n", job.Name, term.Red("FAILED"))
		e.flushOutput(&out)
		return &JobResult{
			Name:     job.Name,
			Success:  false,
			Output:   msg,
			Duration: time.Since(start),
		}
	}

	// Create a dedicated Docker network for this job and its services.
	network := ""
	var serviceIDs []string
	if len(job.Services) > 0 {
		network = fmt.Sprintf("gitlab-ci-sim-%s-%d", job.Name, time.Now().UnixNano())
		if err := e.createNetwork(network); err != nil {
			fmt.Fprintf(&out, "│  │  %s creating network: %v\n", term.Red("Error"), err)
			fmt.Fprintf(&out, "│  └─ Job %s: %s\n", job.Name, term.Red("FAILED"))
			e.flushOutput(&out)
			return &JobResult{
				Name:     job.Name,
				Success:  false,
				Output:   err.Error(),
				Duration: time.Since(start),
			}
		}
		for _, svc := range job.Services {
			id, err := e.startService(ctx, network, svc, jobCtx)
			if err != nil {
				fmt.Fprintf(&out, "│  │  %s starting service %s: %v\n", term.Red("Error"), svc.Name, err)
				fmt.Fprintf(&out, "│  └─ Job %s: %s\n", job.Name, term.Red("FAILED"))
				for _, sid := range serviceIDs {
					_ = e.stopContainer(ctx, sid)
				}
				_ = e.removeNetwork(ctx, network)
				e.flushOutput(&out)
				return &JobResult{
					Name:     job.Name,
					Success:  false,
					Output:   err.Error(),
					Duration: time.Since(start),
				}
			}
			serviceIDs = append(serviceIDs, id)
		}
		defer func() {
			for _, sid := range serviceIDs {
				_ = e.stopContainer(ctx, sid)
			}
			_ = e.removeNetwork(ctx, network)
		}()
		// Give Docker DNS a moment to propagate the network aliases.
		time.Sleep(1 * time.Second)
	}

	// Restore cache if configured.
	if e.cache != nil && job.Cache != nil && shouldPullCache(job.Cache.Policy) {
		key := jobCtx.Expand(cacheKey(job.Cache.Key))
		_ = e.cache.Restore(key, workDir)
	}

	// Restore artifacts from jobs this one depends on.
	if e.store != nil {
		for _, need := range job.Needs {
			_ = e.store.Restore(need, workDir)
		}
	}

	mainScript := "set -e\n" + buildShellScript(append(job.BeforeScript, job.Script...))
	mainExit, mainOut, err := e.runContainer(ctx, job, jobCtx, workDir, mainScript, network)
	if err != nil {
		fmt.Fprintf(&out, "│  │  %s: %v\n", term.Red("Error"), err)
		fmt.Fprintf(&out, "│  └─ Job %s: %s\n", job.Name, term.Red("FAILED"))
		e.flushOutput(&out)
		return &JobResult{
			Name:     job.Name,
			Success:  false,
			Output:   mainOut,
			Duration: time.Since(start),
		}
	}
	if mainOut != "" {
		fmt.Fprint(&out, mainOut)
	}

	afterOut := ""
	if len(job.AfterScript) > 0 {
		_, afterOut, _ = e.runContainer(ctx, job, jobCtx, workDir, buildShellScript(job.AfterScript), network)
		if afterOut != "" {
			fmt.Fprint(&out, afterOut)
		}
	}

	success := mainExit == 0
	if success {
		fmt.Fprintf(&out, "│  └─ Job %s: %s\n", job.Name, term.Green("PASSED"))
	} else {
		fmt.Fprintf(&out, "│  └─ Job %s: %s (exit %d)\n", job.Name, term.Red("FAILED"), mainExit)
	}

	// Save artifacts if the job produced any and the policy matches.
	if e.store != nil && job.Artifacts != nil && shouldSaveArtifacts(mainExit, job.Artifacts.When) {
		_ = e.store.Save(job.Name, workDir, job.Artifacts.Paths)
	}

	// Save cache if configured.
	if e.cache != nil && job.Cache != nil && shouldPushCache(job.Cache.Policy) {
		key := jobCtx.Expand(cacheKey(job.Cache.Key))
		paths := make([]string, 0, len(job.Cache.Paths))
		for _, p := range job.Cache.Paths {
			paths = append(paths, jobCtx.Expand(p))
		}
		_ = e.cache.Save(key, workDir, paths)
	}

	result := &JobResult{
		Name:     job.Name,
		Success:  success,
		Output:   mainOut + afterOut,
		Duration: time.Since(start),
	}
	e.flushOutput(&out)
	return result
}

func (e *DockerExecutor) flushOutput(out *strings.Builder) {
	e.mu.Lock()
	defer e.mu.Unlock()
	os.Stdout.WriteString(out.String())
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

func cacheKey(k string) string {
	if k == "" {
		return "default"
	}
	return k
}

func shouldPullCache(policy string) bool {
	return policy == "" || policy == "pull" || policy == "pull-push" || policy == "push-pull"
}

func shouldPushCache(policy string) bool {
	return policy == "" || policy == "push" || policy == "pull-push" || policy == "push-pull"
}

// runContainer runs a Docker container with the given script on stdin.
func (e *DockerExecutor) runContainer(ctx context.Context, job *pipeline.PipelineJob, jobCtx *variables.Context, workDir, script, network string) (int, string, error) {
	args := []string{
		"run", "--rm", "-i",
		"-v", workDir + ":/builds/project",
		"-w", "/builds/project",
		"--entrypoint", "sh",
	}
	if network != "" {
		args = append(args, "--network", network)
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

// allPipelineJobs flattens the staged pipeline into a single job list.
func allPipelineJobs(p *pipeline.Pipeline) []*pipeline.PipelineJob {
	var jobs []*pipeline.PipelineJob
	for _, stage := range p.Stages {
		jobs = append(jobs, stage.Jobs...)
	}
	return jobs
}

// needsMet reports whether every job listed in job.Needs has completed successfully.
func needsMet(job *pipeline.PipelineJob, completed map[string]bool) bool {
	for _, need := range job.Needs {
		if !completed[need] {
			return false
		}
	}
	return true
}

func (e *DockerExecutor) createNetwork(name string) error {
	cmd := exec.Command(e.client, "network", "create", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker network create: %s: %w", string(out), err)
	}
	return nil
}

func (e *DockerExecutor) removeNetwork(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, e.client, "network", "rm", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker network rm: %s: %w", string(out), err)
	}
	return nil
}

func (e *DockerExecutor) startService(ctx context.Context, network string, svc parser.Service, jobCtx *variables.Context) (string, error) {
	image := jobCtx.Expand(svc.Name)
	alias := jobCtx.Expand(svc.Alias)
	if alias == "" {
		alias = "service"
	}

	args := []string{
		"run", "-d", "--rm",
		"--network", network,
		"--network-alias", alias,
	}
	for k, v := range jobCtx.Vars {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, image)
	for _, c := range svc.Command {
		args = append(args, jobCtx.Expand(c))
	}

	cmd := exec.CommandContext(ctx, e.client, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("docker run service %s: %w", image, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (e *DockerExecutor) stopContainer(ctx context.Context, id string) error {
	cmd := exec.CommandContext(ctx, e.client, "stop", "-t", "2", id)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker stop: %s: %w", string(out), err)
	}
	return nil
}
