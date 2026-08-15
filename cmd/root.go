package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gitlab-ci-sim",
	Short: "Simulate GitLab CI pipelines locally",
	Long: `gitlab-ci-sim parses your .gitlab-ci.yml, resolves includes/extends,
evaluates rules, and executes jobs in Docker containers on your local machine.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringP("file", "f", ".gitlab-ci.yml", "Path to the CI config file")
	rootCmd.PersistentFlags().StringSliceP("variable", "v", nil, "Override variables (KEY=VALUE)")
	rootCmd.PersistentFlags().StringP("env-file", "e", "", "Load variables from a .env file (KEY=VALUE)")
}

// loadEnvFile reads KEY=VALUE lines from a .env-style file.
// Empty lines and lines starting with # are ignored.
func loadEnvFile(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading env file %s: %w", path, err)
	}
	var vars []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			continue
		}
		vars = append(vars, key+"="+value)
	}
	return vars, nil
}
