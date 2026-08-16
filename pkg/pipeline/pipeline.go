package pipeline

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/thegyi/gitlab-ci-sim/pkg/parser"
	"github.com/thegyi/gitlab-ci-sim/pkg/rules"
	"github.com/thegyi/gitlab-ci-sim/pkg/term"
	"github.com/thegyi/gitlab-ci-sim/pkg/variables"
)

// Pipeline represents the resolved, execution-ready pipeline.
type Pipeline struct {
	Stages []*Stage
}

// Stage groups jobs that can run in parallel.
type Stage struct {
	Name string
	Jobs []*PipelineJob
}

// PipelineJob is a job ready for execution.
type PipelineJob struct {
	Name         string
	Image        string
	Script       []string
	BeforeScript []string
	AfterScript  []string
	Variables    map[string]string
	Declared     map[string]bool
	Masked       map[string]bool
	Services     []parser.Service
	Artifacts    *parser.Artifacts
	Cache        *parser.Cache
	Needs        parser.Needs
	Dependencies []string
	AllowFailure parser.AllowFailure
	When         string
	Tags         []string
	Retry        *parser.Retry
	StartIn      string
	Trigger      *parser.Trigger
}

// Build creates an executable pipeline from the parsed config.
// If allowManual is true, jobs with when: manual are treated as runnable.
// If tags is non-empty, only jobs with matching tags (or no tags) are included.
func Build(config *parser.Config, vars *variables.Context, jobFilter []string, allowManual bool, tags []string) (*Pipeline, error) {
	pipe := &Pipeline{}

	// Create stage map for ordering
	stageIndex := make(map[string]int)
	for i, s := range config.Stages {
		stageIndex[s] = i
	}

	// Initialize stages
	stages := make([]*Stage, len(config.Stages))
	for i, name := range config.Stages {
		stages[i] = &Stage{Name: name}
	}

	// Assign jobs to stages
	for name, job := range config.Jobs {
		// Filter by specified jobs if any
		if len(jobFilter) > 0 && !contains(jobFilter, name) {
			continue
		}

		// Evaluate rules, only/except and when.
		res, err := rules.ShouldRun(job, vars, allowManual)
		if err != nil {
			return nil, err
		}
		if !res.Run {
			continue
		}
		if len(tags) > 0 && len(job.Tags) > 0 && !hasAnyTag(job.Tags, tags) {
			continue
		}

		idx, ok := stageIndex[job.Stage]
		if !ok {
			return nil, fmt.Errorf("job %q references unknown stage %q", name, job.Stage)
		}

		jobValues := make(map[string]string)
		for k, v := range job.Variables {
			jobValues[k] = v.Value
		}
		evalCtx := vars.With(jobValues)
		for k, v := range job.Variables {
			evalCtx.Masked[k] = v.Masked
		}
		if res.Variables != nil {
			evalCtx = evalCtx.With(res.Variables)
		}

		pj := &PipelineJob{
			Name:         name,
			Image:        resolveImage(job, config.Default, evalCtx),
			Script:       job.Script,
			BeforeScript: resolveBeforeScript(job, config.Default),
			AfterScript:  resolveAfterScript(job, config.Default),
			Variables:    evalCtx.Vars,
			Declared:     evalCtx.Declared,
			Masked:       evalCtx.Masked,
			Services:     job.Services,
			Artifacts:    job.Artifacts,
			Cache:        job.Cache,
			Needs:        job.Needs,
			Dependencies: job.Dependencies,
			AllowFailure: job.AllowFailure,
			When:         res.When,
			Tags:         job.Tags,
			Retry:        job.Retry,
			StartIn:      job.StartIn,
			Trigger:      job.Trigger,
		}
		expandParallel(stages[idx], pj, name, job.Parallel)
	}

	// Filter out empty stages
	for _, s := range stages {
		if len(s.Jobs) > 0 {
			pipe.Stages = append(pipe.Stages, s)
		}
	}

	return pipe, nil
}

// Print outputs the pipeline structure.
func (p *Pipeline) Print(w io.Writer) {
	fmt.Fprintf(w, "%s\n", term.Bold(fmt.Sprintf("Pipeline: %d stages", len(p.Stages))))
	for _, s := range p.Stages {
		fmt.Fprintf(w, "\n  %s\n", term.Cyan(fmt.Sprintf("Stage: %s (%d jobs)", s.Name, len(s.Jobs))))
		for _, j := range s.Jobs {
			img := j.Image
			if img == "" {
				img = "(default)"
			}
			fmt.Fprintf(w, "    - %s [image: %s]\n", term.Bold(j.Name), img)
			if len(j.Needs) > 0 {
				fmt.Fprintf(w, "      needs: %s\n", strings.Join(j.Needs.Names(), ", "))
			}
			if len(j.Dependencies) > 0 {
				fmt.Fprintf(w, "      dependencies: %s\n", strings.Join(j.Dependencies, ", "))
			}
			if len(j.Services) > 0 {
				fmt.Fprintf(w, "      services: %s\n", serviceNames(j.Services))
			}
			for _, line := range j.Script {
				fmt.Fprintf(w, "        %s %s\n", term.Yellow("$"), line)
			}
		}
	}
}

func serviceNames(services []parser.Service) string {
	names := make([]string, 0, len(services))
	for _, s := range services {
		n := s.Name
		if s.Alias != "" {
			n = fmt.Sprintf("%s (%s)", s.Name, s.Alias)
		}
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

func hasAnyTag(jobTags, filter []string) bool {
	set := make(map[string]bool, len(filter))
	for _, t := range filter {
		set[t] = true
	}
	for _, t := range jobTags {
		if set[t] {
			return true
		}
	}
	return false
}

func expandParallel(stage *Stage, base *PipelineJob, name string, parallel *parser.Parallel) {
	if parallel == nil || (parallel.Scalar == 0 && len(parallel.Matrix) == 0) {
		stage.Jobs = append(stage.Jobs, base)
		return
	}
	if parallel.Scalar > 0 {
		for i := 1; i <= parallel.Scalar; i++ {
			job := cloneJob(base)
			job.Name = fmt.Sprintf("%s %d/%d", name, i, parallel.Scalar)
			job.Variables["CI_NODE_INDEX"] = strconv.Itoa(i)
			job.Variables["CI_NODE_TOTAL"] = strconv.Itoa(parallel.Scalar)
			stage.Jobs = append(stage.Jobs, job)
		}
		return
	}

	// Build the list of all matrix combinations across all matrix blocks.
	var combinations []map[string]string
	for _, block := range parallel.Matrix {
		combos := cartesianProduct(block)
		combinations = append(combinations, combos...)
	}
	total := len(combinations)
	for i, combo := range combinations {
		job := cloneJob(base)
		labelParts := make([]string, 0, len(combo))
		keys := make([]string, 0, len(combo))
		for k := range combo {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			job.Variables[k] = combo[k]
			labelParts = append(labelParts, fmt.Sprintf("%s=%s", k, combo[k]))
		}
		job.Name = fmt.Sprintf("%s [%s]", name, strings.Join(labelParts, ","))
		job.Variables["CI_NODE_INDEX"] = strconv.Itoa(i + 1)
		job.Variables["CI_NODE_TOTAL"] = strconv.Itoa(total)
		stage.Jobs = append(stage.Jobs, job)
	}
}

func cloneJob(base *PipelineJob) *PipelineJob {
	j := *base
	j.Variables = make(map[string]string, len(base.Variables))
	for k, v := range base.Variables {
		j.Variables[k] = v
	}
	j.Declared = make(map[string]bool, len(base.Declared))
	for k, v := range base.Declared {
		j.Declared[k] = v
	}
	j.Masked = make(map[string]bool, len(base.Masked))
	for k, v := range base.Masked {
		j.Masked[k] = v
	}
	j.Needs = append(parser.Needs{}, base.Needs...)
	j.Dependencies = append([]string{}, base.Dependencies...)
	j.Services = append([]parser.Service{}, base.Services...)
	j.Script = append([]string{}, base.Script...)
	j.BeforeScript = append([]string{}, base.BeforeScript...)
	j.AfterScript = append([]string{}, base.AfterScript...)
	return &j
}

func cartesianProduct(block map[string][]string) []map[string]string {
	keys := make([]string, 0, len(block))
	for k := range block {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var result []map[string]string
	var dfs func(int, map[string]string)
	dfs = func(idx int, current map[string]string) {
		if idx == len(keys) {
			combo := make(map[string]string, len(current))
			for k, v := range current {
				combo[k] = v
			}
			result = append(result, combo)
			return
		}
		k := keys[idx]
		for _, v := range block[k] {
			current[k] = v
			dfs(idx+1, current)
		}
	}
	dfs(0, make(map[string]string))
	return result
}

func resolveImage(job *parser.Job, defaults *parser.JobDefaults, vars *variables.Context) string {
	img := ""
	if job.Image != "" {
		img = job.Image
	} else if defaults != nil && defaults.Image != "" {
		img = defaults.Image
	}
	return vars.Expand(img)
}

func resolveBeforeScript(job *parser.Job, defaults *parser.JobDefaults) []string {
	if len(job.BeforeScript) > 0 {
		return job.BeforeScript
	}
	if defaults != nil {
		return defaults.BeforeScript
	}
	return nil
}

func resolveAfterScript(job *parser.Job, defaults *parser.JobDefaults) []string {
	if len(job.AfterScript) > 0 {
		return job.AfterScript
	}
	if defaults != nil {
		return defaults.AfterScript
	}
	return nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
