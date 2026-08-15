package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/thegyi/gitlab-ci-sim/pkg/parser"
)

var lintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Validate the CI configuration without running it",
	RunE:  lintConfig,
}

func init() {
	rootCmd.AddCommand(lintCmd)
}

func lintConfig(cmd *cobra.Command, args []string) error {
	configFile, _ := cmd.Flags().GetString("file")

	config, err := parser.ParseFile(configFile)
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	fmt.Printf("Configuration is valid: %d jobs in %d stages\n",
		len(config.Jobs), len(config.Stages))
	return nil
}
