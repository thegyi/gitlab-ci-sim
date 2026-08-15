package pipeline

import (
	"fmt"
	"io"

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
	Needs        []string
	AllowFailure bool
	Trigger      *parser.Trigger
}

// Build creates an executable pipeline from the parsed config.
func Build(config *parser.Config, vars *variables.Context, jobFilter []string) (*Pipeline, error) {
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
		res, err := rules.ShouldRun(job, vars, contains(jobFilter, name))
		if err != nil {
			return nil, err
		}
		if !res.Run {
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
			AllowFailure: job.AllowFailure,
			Trigger:      job.Trigger,
		}
		stages[idx].Jobs = append(stages[idx].Jobs, pj)
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
			for _, line := range j.Script {
				fmt.Fprintf(w, "        %s %s\n", term.Yellow("$"), line)
			}
		}
	}
}

func resolveImage(job *parser.Job, defaults *parser.JobDefaults, vars *variables.Context) string {
	img := ""
	if job.Image != "" {
		img = job.Image
	} else if defaults != nil && defaults.Image != "" {
		img = defaults.Image
	} else {
		img = "alpine:latest"
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
