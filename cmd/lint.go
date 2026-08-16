package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thegyi/gitlab-ci-sim/pkg/parser"
	"github.com/thegyi/gitlab-ci-sim/pkg/pipeline"
	"github.com/thegyi/gitlab-ci-sim/pkg/term"
	"github.com/thegyi/gitlab-ci-sim/pkg/variables"
)

var lintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Validate the CI configuration without running it",
	RunE:  lintConfig,
}

func init() {
	rootCmd.AddCommand(lintCmd)
}

var validWhens = map[string]bool{
	"on_success": true,
	"on_failure": true,
	"always":     true,
	"never":      true,
	"manual":     true,
	"delayed":    true,
}

func lintConfig(cmd *cobra.Command, args []string) error {
	configFile, _ := cmd.Flags().GetString("file")

	config, err := parser.ParseFile(configFile)
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	configValues := make(map[string]string)
	configMasked := make(map[string]bool)
	for k, v := range config.Variables {
		configValues[k] = v.Value
		configMasked[k] = v.Masked
	}
	vars, err := variables.Build("", configValues, configMasked, nil)
	if err != nil {
		return fmt.Errorf("failed to build variables: %w", err)
	}
	_, err = pipeline.Build(config, vars, nil, true, nil)
	if err != nil {
		return fmt.Errorf("invalid pipeline: %w", err)
	}

	var warnings, errs []string
	jobNames := make(map[string]bool, len(config.Jobs))
	for name := range config.Jobs {
		jobNames[name] = true
	}
	stageNames := make(map[string]bool, len(config.Stages))
	for _, s := range config.Stages {
		stageNames[s] = true
	}

	for name, job := range config.Jobs {
		if len(job.Script) == 0 && job.Trigger == nil {
			warnings = append(warnings, fmt.Sprintf("job %q has no script or trigger", name))
		}
		if job.Stage != "" && !stageNames[job.Stage] {
			errs = append(errs, fmt.Sprintf("job %q references unknown stage %q", name, job.Stage))
		}
		for _, n := range job.Needs {
			if !jobNames[n.Job] {
				errs = append(errs, fmt.Sprintf("job %q needs unknown job %q", name, n.Job))
			}
		}
		for _, d := range job.Dependencies {
			if !jobNames[d] {
				errs = append(errs, fmt.Sprintf("job %q depends on unknown job %q", name, d))
			}
		}
		for _, r := range job.Rules {
			if r.When != "" && !validWhens[r.When] {
				errs = append(errs, fmt.Sprintf("job %q has unknown rule when %q", name, r.When))
			}
		}
		if job.When != "" && !validWhens[job.When] {
			errs = append(errs, fmt.Sprintf("job %q has unknown when %q", name, job.When))
		}
	}

	if hasCycle(config.Jobs) {
		errs = append(errs, "circular needs/dependencies detected")
	}

	for _, w := range warnings {
		fmt.Fprintf(os.Stdout, "%s: %s\n", term.Yellow("warning"), w)
	}
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "%s: %s\n", term.Red("error"), e)
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d lint error(s) found:\n%s", len(errs), strings.Join(errs, "\n"))
	}

	fmt.Fprintf(os.Stdout, "Configuration is valid: %d jobs in %d stages\n",
		len(config.Jobs), len(config.Stages))
	return nil
}

func hasCycle(jobs map[string]*parser.Job) bool {
	visited := make(map[string]bool)
	rec := make(map[string]bool)
	var dfs func(name string) bool
	dfs = func(name string) bool {
		visited[name] = true
		rec[name] = true
		job := jobs[name]
		if job == nil {
			rec[name] = false
			return false
		}
		deps := make([]string, 0, len(job.Needs)+len(job.Dependencies))
		for _, n := range job.Needs {
			deps = append(deps, n.Job)
		}
		deps = append(deps, job.Dependencies...)
		for _, d := range deps {
			if !visited[d] && dfs(d) {
				return true
			} else if rec[d] {
				return true
			}
		}
		rec[name] = false
		return false
	}
	for name := range jobs {
		if !visited[name] && dfs(name) {
			return true
		}
	}
	return false
}
