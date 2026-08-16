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

var graphCmd = &cobra.Command{
	Use:   "graph [job...]",
	Short: "Show the pipeline dependency graph",
	Long: `Parse .gitlab-ci.yml, apply rules, and print an ASCII view of
jobs, stages, and needs/dependencies without executing anything.`,
	RunE: graph,
}

func init() {
	graphCmd.Flags().String("branch", "", "Simulate a specific branch")
	rootCmd.AddCommand(graphCmd)
}

func graph(cmd *cobra.Command, args []string) error {
	configFile, _ := cmd.Flags().GetString("file")
	branch, _ := cmd.Flags().GetString("branch")
	varOverrides, _ := cmd.Flags().GetStringSlice("variable")
	envFile, _ := cmd.Flags().GetString("env-file")
	envVars, err := loadEnvFile(envFile)
	if err != nil {
		return err
	}
	varOverrides = append(varOverrides, envVars...)

	config, err := parser.ParseFile(configFile)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", configFile, err)
	}

	configValues := make(map[string]string)
	configMasked := make(map[string]bool)
	for k, v := range config.Variables {
		configValues[k] = v.Value
		configMasked[k] = v.Masked
	}
	vars, err := variables.Build(branch, configValues, configMasked, varOverrides)
	if err != nil {
		return fmt.Errorf("failed to build variables: %w", err)
	}

	pipe, err := pipeline.Build(config, vars, args, true, nil)
	if err != nil {
		return fmt.Errorf("failed to build pipeline: %w", err)
	}

	totalJobs := 0
	for _, s := range pipe.Stages {
		totalJobs += len(s.Jobs)
	}
	fmt.Fprintf(os.Stdout, "%s\n\n", term.Bold(fmt.Sprintf("Pipeline graph: %d jobs", totalJobs)))

	for _, stage := range pipe.Stages {
		fmt.Fprintf(os.Stdout, "%s\n", term.Cyan(fmt.Sprintf("Stage: %s (%d jobs)", stage.Name, len(stage.Jobs))))
		for _, job := range stage.Jobs {
			img := job.Image.Name
			if img == "" {
				img = "(default)"
			}
			fmt.Fprintf(os.Stdout, "  %s [image: %s]\n", term.Bold(job.Name), img)
			if len(job.Needs) > 0 {
				fmt.Fprintf(os.Stdout, "    %s %s\n", term.Yellow("needs:"), strings.Join(job.Needs.Names(), ", "))
			}
			for _, line := range job.Script {
				fmt.Fprintf(os.Stdout, "    %s %s\n", term.Yellow("$"), line)
			}
		}
		fmt.Fprintln(os.Stdout)
	}

	return nil
}
