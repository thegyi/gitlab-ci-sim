package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
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

// DockerExecutor runs jobs in a container runtime.
type DockerExecutor struct {
	runtime     Runtime
	store       *artifacts.Store
	cache       *artifacts.Store
	mu          sync.Mutex
	triggerMode string
}

// SetTriggerMode configures how trigger: jobs are handled ("local" or "gitlab").
func (e *DockerExecutor) SetTriggerMode(mode string) {
	e.triggerMode = mode
}

// NewDockerExecutor creates a new container executor with the given runtime.
func NewDockerExecutor(runtime string, store, cache *artifacts.Store) (*DockerExecutor, error) {
	rt, err := NewRuntime(runtime)
	if err != nil {
		return nil, err
	}
	return &DockerExecutor{runtime: rt, store: store, cache: cache}, nil
}

// Run executes the pipeline as a DAG respecting job needs.
func (e *DockerExecutor) Run(ctx context.Context, pipe *pipeline.Pipeline, vars *variables.Context) *Result {
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
	canceled := false

	for len(pending) > 0 || running > 0 {
		if !canceled && ctx.Err() == nil {
			var stillPending []*pipeline.PipelineJob
			for _, job := range pending {
				if needsMet(job, completedSuccess) {
					go func(j *pipeline.PipelineJob) {
						done <- e.runJob(ctx, j, vars)
					}(job)
					running++
				} else {
					stillPending = append(stillPending, job)
				}
			}
			pending = stillPending
		}

		if running == 0 {
			if canceled {
				for _, job := range pending {
					e.mu.Lock()
					fmt.Fprintf(os.Stdout, "│  Job %s: %s (canceled)\n", job.Name, term.Red("CANCELED"))
					e.mu.Unlock()
					result.JobResults = append(result.JobResults, &JobResult{
						Name:    job.Name,
						Success: false,
					})
				}
				result.Success = false
				break
			}
			// No jobs are running and no new ones were ready -> dependency issue or cycle.
			for _, job := range pending {
				msg := "dependencies not met"
				if missing := missingNeeds(job, completedSuccess); len(missing) > 0 {
					msg = fmt.Sprintf("dependencies not met: %s", strings.Join(missing, ", "))
				}
				e.mu.Lock()
				fmt.Fprintf(os.Stdout, "│  Job %s: %s (%s)\n", job.Name, term.Yellow("SKIPPED"), msg)
				e.mu.Unlock()
				result.JobResults = append(result.JobResults, &JobResult{
					Name:    job.Name,
					Success: false,
				})
				result.Success = false
			}
			break
		}

		select {
		case jr := <-done:
			running--
			result.JobResults = append(result.JobResults, jr)
			job := byName[jr.Name]
			effective := jr.Success || (job != nil && job.AllowFailure)
			completedSuccess[jr.Name] = effective
			if !effective {
				result.Success = false
			}
		case <-ctx.Done():
			canceled = true
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
	e.flushOutput(&out)
	out.Reset()

	jobCtx := vars.With(job.Variables)
	if job.Declared != nil {
		jobCtx.Declared = job.Declared
	}
	if job.Masked != nil {
		jobCtx.Masked = job.Masked
	}
	if job.Trigger != nil {
		project := jobCtx.Expand(job.Trigger.Project)
		branch := jobCtx.Expand(job.Trigger.Branch)
		if e.triggerMode != "gitlab" {
			fmt.Fprintf(&out, "│  │  %s: trigger to %s (branch: %s) is not executed locally\n", term.Yellow("Note"), project, branch)
			fmt.Fprintf(&out, "│  └─ Job %s: %s\n", job.Name, term.Green("PASSED"))
			e.flushOutput(&out)
			return &JobResult{
				Name:     job.Name,
				Success:  true,
				Output:   "trigger job not executed locally",
				Duration: time.Since(start),
			}
		}
		msg, err := e.triggerPipeline(jobCtx, job.Trigger)
		if err != nil {
			fmt.Fprintf(&out, "│  │  %s: %v\n", term.Red("Error"), err)
			fmt.Fprintf(&out, "│  └─ Job %s: %s\n", job.Name, term.Red("FAILED"))
			e.flushOutput(&out)
			return &JobResult{
				Name:     job.Name,
				Success:  false,
				Output:   err.Error(),
				Duration: time.Since(start),
			}
		}
		fmt.Fprintf(&out, "│  │  %s: %s\n", term.Green("Trigger"), msg)
		fmt.Fprintf(&out, "│  └─ Job %s: %s\n", job.Name, term.Green("PASSED"))
		e.flushOutput(&out)
		return &JobResult{
			Name:     job.Name,
			Success:  true,
			Output:   msg,
			Duration: time.Since(start),
		}
	}
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

	// Create a dedicated container network for this job and its services.
	network := ""
	var serviceIDs []string
	if len(job.Services) > 0 {
		network = fmt.Sprintf("gitlab-ci-sim-%s-%d", job.Name, time.Now().UnixNano())
		if err := e.runtime.CreateNetwork(ctx, network); err != nil {
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
		envList := envListFromContext(jobCtx)
		for _, svc := range job.Services {
			image := jobCtx.Expand(svc.Name)
			alias := jobCtx.Expand(svc.Alias)
			if alias == "" {
				alias = "service"
			}
			cmd := make([]string, 0, len(svc.Command))
			for _, c := range svc.Command {
				cmd = append(cmd, jobCtx.Expand(c))
			}
			id, err := e.runtime.RunDetached(ctx, ServiceOpts{
				Image:   image,
				Network: network,
				Alias:   alias,
				Env:     envList,
				Command: cmd,
			})
			if err != nil {
				fmt.Fprintf(&out, "│  │  %s starting service %s: %v\n", term.Red("Error"), svc.Name, err)
				fmt.Fprintf(&out, "│  └─ Job %s: %s\n", job.Name, term.Red("FAILED"))
				for _, sid := range serviceIDs {
					_ = e.runtime.Stop(ctx, sid)
				}
				_ = e.runtime.RemoveNetwork(ctx, network)
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
				_ = e.runtime.Stop(ctx, sid)
			}
			_ = e.runtime.RemoveNetwork(ctx, network)
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
		artifactSources := job.Needs
		if len(job.Dependencies) > 0 {
			artifactSources = job.Dependencies
		}
		for _, need := range artifactSources {
			_ = e.store.Restore(need, workDir)
		}
	}

	if job.When == "delayed" && job.StartIn != "" {
		d, err := parseStartIn(job.StartIn)
		if err == nil {
			fmt.Fprintf(&out, "│  │  %s: delaying for %s\n", term.Yellow("Delayed"), job.StartIn)
			e.flushOutput(&out)
			out.Reset()
			time.Sleep(d)
		}
	}

	mainScript := "set -e\n" + buildShellScript(append(job.BeforeScript, job.Script...))
	envList := envListFromContext(jobCtx)
	redact := maskedValues(jobCtx)
	var mainBuf bytes.Buffer
	mainSafe := &safeWriter{w: io.MultiWriter(&mainBuf, os.Stdout)}
	mainRedactorOut := &redactingWriter{dest: mainSafe, values: redact}
	mainRedactorErr := &redactingWriter{dest: mainSafe, values: redact}

	maxAttempts := 0
	if job.Retry != nil {
		maxAttempts = job.Retry.Max
	}
	retryWhen := []string{"script_failure"}
	if job.Retry != nil && len(job.Retry.When) > 0 {
		retryWhen = job.Retry.When
	}

	mainExit := -1
	var mainErr error
	for attempt := 0; attempt <= maxAttempts; attempt++ {
		if maxAttempts > 0 {
			fmt.Fprintf(&out, "│  │  %s: attempt %d/%d\n", term.Yellow("Info"), attempt+1, maxAttempts+1)
			e.flushOutput(&out)
			out.Reset()
		}
		mainExit, mainErr = e.runtime.Run(ctx, RunOpts{
			Image:      job.Image,
			WorkDir:    workDir,
			Network:    network,
			Env:        envList,
			Script:     mainScript,
			Entrypoint: "sh",
			Stdout:     mainRedactorOut,
			Stderr:     mainRedactorErr,
		})
		_ = mainRedactorOut.Flush()
		_ = mainRedactorErr.Flush()
		if mainErr == nil && mainExit == 0 {
			break
		}
		if attempt < maxAttempts && isRetryable(mainErr, mainExit, retryWhen, ctx) {
			continue
		}
		break
	}
	_ = mainRedactorOut.Flush()
	_ = mainRedactorErr.Flush()

	if mainErr != nil {
		fmt.Fprintf(&out, "│  │  %s: %v\n", term.Red("Error"), mainErr)
		fmt.Fprintf(&out, "│  └─ Job %s: %s\n", job.Name, term.Red("FAILED"))
		e.flushOutput(&out)
		return &JobResult{
			Name:     job.Name,
			Success:  false,
			Output:   mainBuf.String(),
			Duration: time.Since(start),
		}
	}

	afterBuf := bytes.Buffer{}
	if len(job.AfterScript) > 0 {
		afterSafe := &safeWriter{w: io.MultiWriter(&afterBuf, os.Stdout)}
		afterRedactorOut := &redactingWriter{dest: afterSafe, values: redact}
		afterRedactorErr := &redactingWriter{dest: afterSafe, values: redact}
		_, _ = e.runtime.Run(ctx, RunOpts{
			Image:      job.Image,
			WorkDir:    workDir,
			Network:    network,
			Env:        envList,
			Script:     buildShellScript(job.AfterScript),
			Entrypoint: "sh",
			Stdout:     afterRedactorOut,
			Stderr:     afterRedactorErr,
		})
		_ = afterRedactorOut.Flush()
		_ = afterRedactorErr.Flush()
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
		Output:   mainBuf.String() + afterBuf.String(),
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

type safeWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *safeWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

type redactingWriter struct {
	dest   io.Writer
	values []string
	buf    []byte
}

func (r *redactingWriter) Write(p []byte) (int, error) {
	r.buf = append(r.buf, p...)
	for {
		idx := bytes.IndexAny(r.buf, "\n\r")
		if idx < 0 {
			break
		}
		line := r.buf[:idx+1]
		r.buf = r.buf[idx+1:]
		if _, err := r.dest.Write([]byte(r.redact(string(line)))); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

func (r *redactingWriter) Flush() error {
	if len(r.buf) > 0 {
		_, err := r.dest.Write([]byte(r.redact(string(r.buf))))
		r.buf = r.buf[:0]
		return err
	}
	return nil
}

func (r *redactingWriter) redact(s string) string {
	for _, v := range r.values {
		s = strings.ReplaceAll(s, v, "[MASKED]")
	}
	return s
}

func maskedValues(ctx *variables.Context) []string {
	var values []string
	for k, v := range ctx.Vars {
		if ctx.Masked[k] && v != "" {
			values = append(values, v)
		}
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	return values
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

var gitlabHTTPClient = &http.Client{Timeout: 30 * time.Second}

func gitlabToken(ctx *variables.Context) (string, string) {
	if t := ctx.Get("GITLAB_TOKEN"); t != "" {
		return t, "PRIVATE-TOKEN"
	}
	if t := os.Getenv("GITLAB_TOKEN"); t != "" {
		return t, "PRIVATE-TOKEN"
	}
	if t := ctx.Get("CI_JOB_TOKEN"); t != "" {
		return t, "JOB-TOKEN"
	}
	if t := os.Getenv("CI_JOB_TOKEN"); t != "" {
		return t, "JOB-TOKEN"
	}
	return "", ""
}

func (e *DockerExecutor) triggerPipeline(ctx *variables.Context, t *parser.Trigger) (string, error) {
	serverURL := ctx.Get("CI_SERVER_URL")
	if serverURL == "" {
		serverURL = os.Getenv("CI_SERVER_URL")
	}
	if serverURL == "" {
		return "", fmt.Errorf("CI_SERVER_URL is not set")
	}
	project := ctx.Expand(t.Project)
	if project == "" {
		return "", fmt.Errorf("trigger project is empty")
	}
	ref := ctx.Expand(t.Branch)
	if ref == "" {
		ref = ctx.Get("CI_COMMIT_REF_NAME")
	}
	if ref == "" {
		ref = "master"
	}
	token, header := gitlabToken(ctx)
	if token == "" {
		return "", fmt.Errorf("no GITLAB_TOKEN or CI_JOB_TOKEN available")
	}

	body := map[string]interface{}{"ref": ref}
	bodyJSON, _ := json.Marshal(body)
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/pipeline", strings.TrimRight(serverURL, "/"), url.PathEscape(project))
	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", err
	}
	req.Header.Set(header, token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := gitlabHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("gitlab returned %d: %s", resp.StatusCode, string(respBody))
	}
	var created struct {
		ID     int    `json:"id"`
		WebURL string `json:"web_url"`
	}
	_ = json.Unmarshal(respBody, &created)
	return fmt.Sprintf("triggered downstream pipeline #%d %s", created.ID, created.WebURL), nil
}

func envListFromContext(jobCtx *variables.Context) []string {
	var env []string
	for k, v := range jobCtx.Vars {
		if !jobCtx.Declared[k] {
			continue
		}
		env = append(env, k+"="+v)
	}
	return env
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
func parseStartIn(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.NewReplacer("seconds", "s", "second", "s", "minutes", "m", "minute", "m", "hours", "h", "hour", "h").Replace(s)
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func needsMet(job *pipeline.PipelineJob, completed map[string]bool) bool {
	for _, need := range job.Needs {
		if !completed[need] {
			return false
		}
	}
	return true
}

func missingNeeds(job *pipeline.PipelineJob, completed map[string]bool) []string {
	var missing []string
	for _, need := range job.Needs {
		if !completed[need] {
			missing = append(missing, need)
		}
	}
	return missing
}

func isRetryable(err error, exit int, when []string, ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	if len(when) == 0 {
		return err == nil && exit != 0
	}
	for _, w := range when {
		switch w {
		case "always":
			return true
		case "script_failure":
			if err == nil && exit != 0 {
				return true
			}
		case "runner_system_failure", "api_failure", "stuck_or_timeout_failure", "job_execution_timeout":
			if err != nil {
				return true
			}
		case "unknown_failure":
			if exit != 0 || err != nil {
				return true
			}
		}
	}
	return false
}
