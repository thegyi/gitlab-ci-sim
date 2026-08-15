package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/thegyi/gitlab-ci-sim/pkg/artifacts"
	"github.com/thegyi/gitlab-ci-sim/pkg/executor"
	"github.com/thegyi/gitlab-ci-sim/pkg/parser"
	"github.com/thegyi/gitlab-ci-sim/pkg/pipeline"
	"github.com/thegyi/gitlab-ci-sim/pkg/rules"
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
	runCmd.Flags().Bool("list", false, "List jobs that would run and exit")
	runCmd.Flags().Bool("manual", false, "Treat when: manual jobs as runnable")
	runCmd.Flags().Bool("interactive", false, "Interactively select which jobs to run")
	rootCmd.AddCommand(runCmd)
}

func runJobs(cmd *cobra.Command, args []string) error {
	configFile, _ := cmd.Flags().GetString("file")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	watch, _ := cmd.Flags().GetBool("watch")
	branch, _ := cmd.Flags().GetString("branch")
	varOverrides, _ := cmd.Flags().GetStringSlice("variable")
	envFile, _ := cmd.Flags().GetString("env-file")
	envVars, err := loadEnvFile(envFile)
	if err != nil {
		return err
	}
	varOverrides = append(varOverrides, envVars...)
	list, _ := cmd.Flags().GetBool("list")
	manual, _ := cmd.Flags().GetBool("manual")
	interactive, _ := cmd.Flags().GetBool("interactive")

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

		// Apply workflow-level rules
		if config.Workflow != nil {
			wfRes, err := rules.EvaluateWorkflow(config.Workflow, vars)
			if err != nil {
				if !watch {
					return fmt.Errorf("failed to evaluate workflow rules: %w", err)
				}
				fmt.Fprintf(os.Stderr, "%s: %v\n", term.Red("workflow error"), err)
				if err := waitForChange(configFile, &lastMod); err != nil {
					return err
				}
				continue
			}
			if !wfRes.Run {
				if !watch {
					return fmt.Errorf("pipeline skipped by workflow rules")
				}
				fmt.Fprintf(os.Stderr, "%s: pipeline skipped by workflow rules\n", term.Yellow("skip"))
				if err := waitForChange(configFile, &lastMod); err != nil {
					return err
				}
				continue
			}
			if len(wfRes.Variables) > 0 {
				vars = vars.With(wfRes.Variables)
			}
		}

		// Build the pipeline (resolve stages, needs, rules)
		if watch && interactive {
			return fmt.Errorf("--interactive cannot be used with --watch")
		}
		allowManual := manual || len(args) > 0
		if interactive {
			allPipe, err := pipeline.Build(config, vars, nil, true)
			if err != nil {
				return fmt.Errorf("failed to build pipeline: %w", err)
			}
			selected, err := selectJobs(allPipe)
			if err != nil {
				return err
			}
			args = selected
			allowManual = true
		}
		pipe, err := pipeline.Build(config, vars, args, allowManual)
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

		if list {
			fmt.Fprintln(os.Stdout, "Jobs that would run:")
			for _, stage := range pipe.Stages {
				fmt.Fprintf(os.Stdout, "  stage: %s\n", stage.Name)
				for _, job := range stage.Jobs {
					extra := ""
					if len(job.Needs) > 0 {
						extra = fmt.Sprintf(" (needs: %s)", strings.Join(job.Needs, ", "))
					}
					fmt.Fprintf(os.Stdout, "    - %s%s\n", job.Name, extra)
				}
			}
			return nil
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
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			result := exec.Run(ctx, pipe, vars)
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

func selectJobs(pipe *pipeline.Pipeline) ([]string, error) {
	var jobs []string
	idx := 1
	for _, s := range pipe.Stages {
		for _, j := range s.Jobs {
			fmt.Fprintf(os.Stdout, "%d. %s (%s)\n", idx, j.Name, s.Name)
			jobs = append(jobs, j.Name)
			idx++
		}
	}
	fmt.Print("Select jobs (numbers, ranges, comma-separated, or 'all'): ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if line == "" || line == "all" {
		return jobs, nil
	}
	selected := parseSelection(line, len(jobs))
	var out []string
	for _, i := range selected {
		if i >= 1 && i <= len(jobs) {
			out = append(out, jobs[i-1])
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no jobs selected")
	}
	return out, nil
}

func parseSelection(input string, max int) []int {
	var selected []int
	seen := make(map[int]bool)
	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		start, end := 0, 0
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			s, _ := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			e, _ := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			start, end = s, e
			if start > end {
				start, end = end, start
			}
		} else {
			n, _ := strconv.Atoi(part)
			start, end = n, n
		}
		if start < 1 {
			start = 1
		}
		if end > max {
			end = max
		}
		for i := start; i <= end; i++ {
			if !seen[i] {
				seen[i] = true
				selected = append(selected, i)
			}
		}
	}
	return selected
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
