package cmd

import (
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
}
