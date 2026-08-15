package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/thegyi/gitlab-ci-sim/pkg/executor"
	"github.com/thegyi/gitlab-ci-sim/pkg/parser"
	"github.com/thegyi/gitlab-ci-sim/pkg/pipeline"
	"github.com/thegyi/gitlab-ci-sim/pkg/variables"
)

var runCmd = &cobra.Command{
	Use:   "run [job...]",
	Short: "Run one or more jobs locally in Docker",
	Long: `Run the specified jobs (or the entire pipeline if none given)
in Docker containers using the configuration from .gitlab-ci.yml.`,
	RunE: runJobs,
}

func init() {
	runCmd.Flags().Bool("dry-run", false, "Show what would be executed without running")
	runCmd.Flags().String("branch", "", "Simulate a specific branch (default: current git branch)")
	rootCmd.AddCommand(runCmd)
}

func runJobs(cmd *cobra.Command, args []string) error {
	configFile, _ := cmd.Flags().GetString("file")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	branch, _ := cmd.Flags().GetString("branch")
	varOverrides, _ := cmd.Flags().GetStringSlice("variable")

	// Parse the CI configuration
	config, err := parser.ParseFile(configFile)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", configFile, err)
	}

	// Build variable context
	vars, err := variables.Build(branch, config.Variables, varOverrides)
	if err != nil {
		return fmt.Errorf("failed to build variables: %w", err)
	}

	// Build the pipeline (resolve stages, needs, rules)
	pipe, err := pipeline.Build(config, vars, args)
	if err != nil {
		return fmt.Errorf("failed to build pipeline: %w", err)
	}

	if dryRun {
		pipe.Print(os.Stdout)
		return nil
	}

	// Execute the pipeline
	exec := executor.NewDockerExecutor()
	result := exec.Run(pipe, vars)

	result.Print(os.Stdout)
	if !result.Success {
		return fmt.Errorf("pipeline failed")
	}
	return nil
}
