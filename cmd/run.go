package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/thegyi/gitlab-ci-sim/pkg/artifacts"
	"github.com/thegyi/gitlab-ci-sim/pkg/executor"
	"github.com/thegyi/gitlab-ci-sim/pkg/parser"
	"github.com/thegyi/gitlab-ci-sim/pkg/pipeline"
	"github.com/thegyi/gitlab-ci-sim/pkg/term"
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
	runCmd.Flags().Bool("watch", false, "Re-run the pipeline when .gitlab-ci.yml changes")
	runCmd.Flags().String("runtime", "docker", "Container runtime to use: docker, podman, or fake")
	runCmd.Flags().String("trigger-mode", "local", "Trigger handling: local (no-op) or gitlab (call GitLab API)")
	rootCmd.AddCommand(runCmd)
}

func runJobs(cmd *cobra.Command, args []string) error {
	configFile, _ := cmd.Flags().GetString("file")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	watch, _ := cmd.Flags().GetBool("watch")
	branch, _ := cmd.Flags().GetString("branch")
	varOverrides, _ := cmd.Flags().GetStringSlice("variable")

	lastMod := time.Time{}
	if watch {
		info, err := os.Stat(configFile)
		if err != nil {
			return fmt.Errorf("failed to stat %s: %w", configFile, err)
		}
		lastMod = info.ModTime()
	}

	for {
		// Parse the CI configuration
		config, err := parser.ParseFile(configFile)
		if err != nil {
			if !watch {
				return fmt.Errorf("failed to parse %s: %w", configFile, err)
			}
			fmt.Fprintf(os.Stderr, "%s: %v\n", term.Red("parse error"), err)
			if err := waitForChange(configFile, &lastMod); err != nil {
				return err
			}
			continue
		}

		if len(args) > 0 {
			for _, name := range args {
				if _, ok := config.Jobs[name]; !ok {
					return fmt.Errorf("job %q not found in %s", name, configFile)
				}
			}
		}

		// Build variable context
		configValues := make(map[string]string)
		configMasked := make(map[string]bool)
		for k, v := range config.Variables {
			configValues[k] = v.Value
			configMasked[k] = v.Masked
		}
		vars, err := variables.Build(branch, configValues, configMasked, varOverrides)
		if err != nil {
			if !watch {
				return fmt.Errorf("failed to build variables: %w", err)
			}
			fmt.Fprintf(os.Stderr, "%s: %v\n", term.Red("variable error"), err)
			if err := waitForChange(configFile, &lastMod); err != nil {
				return err
			}
			continue
		}

		// Build the pipeline (resolve stages, needs, rules)
		pipe, err := pipeline.Build(config, vars, args)
		if err != nil {
			if !watch {
				return fmt.Errorf("failed to build pipeline: %w", err)
			}
			fmt.Fprintf(os.Stderr, "%s: %v\n", term.Red("pipeline error"), err)
			if err := waitForChange(configFile, &lastMod); err != nil {
				return err
			}
			continue
		}
		if len(pipe.Stages) == 0 {
			if !watch {
				if len(args) > 0 {
					return fmt.Errorf("no jobs matched the filter: %v", args)
				}
				return fmt.Errorf("no jobs to run")
			}
			fmt.Fprintf(os.Stderr, "%s: no jobs to run\n", term.Red("pipeline error"))
			if err := waitForChange(configFile, &lastMod); err != nil {
				return err
			}
			continue
		}

		if dryRun {
			pipe.Print(os.Stdout)
		} else {
			// Execute the pipeline
			cacheDir, err := os.UserCacheDir()
			if err != nil {
				return fmt.Errorf("could not determine cache dir: %w", err)
			}
			artifactStore, err := artifacts.NewStore(filepath.Join(cacheDir, "gitlab-ci-sim", "artifacts"))
			if err != nil {
				return fmt.Errorf("could not create artifact store: %w", err)
			}
			cacheStore, err := artifacts.NewStore(filepath.Join(cacheDir, "gitlab-ci-sim", "cache"))
			if err != nil {
				return fmt.Errorf("could not create cache store: %w", err)
			}
			runtime, _ := cmd.Flags().GetString("runtime")
			exec, err := executor.NewDockerExecutor(runtime, artifactStore, cacheStore)
			if err != nil {
				return fmt.Errorf("could not create executor: %w", err)
			}
			triggerMode, _ := cmd.Flags().GetString("trigger-mode")
			exec.SetTriggerMode(triggerMode)
			result := exec.Run(pipe, vars)
			result.Print(os.Stdout)
		}

		if !watch {
			if dryRun {
				return nil
			}
			return nil
		}

		fmt.Fprintf(os.Stdout, "%s\n", term.Yellow("Watching for changes... (press Ctrl-C to stop)"))
		if err := waitForChange(configFile, &lastMod); err != nil {
			return err
		}
	}
}

func waitForChange(path string, lastMod *time.Time) error {
	for {
		time.Sleep(2 * time.Second)
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("failed to stat %s: %w", path, err)
		}
		if info.ModTime().After(*lastMod) {
			*lastMod = info.ModTime()
			return nil
		}
	}
}
