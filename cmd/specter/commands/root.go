package commands

import (
	"fmt"

	"github.com/ghostwright/specter/internal/tui"
	"github.com/ghostwright/specter/pkg/version"
	"github.com/spf13/cobra"
)

var jsonOutput bool

var rootCmd = &cobra.Command{
	Use:   "specter",
	Short: "AI agents that earn your trust",
	Long: fmt.Sprintf(`%s

  %s v%s
  AI agents that earn your trust.

  Deploy and manage persistent AI agent VMs
  on Hetzner Cloud with automatic DNS and TLS.`, tui.Logo(), tui.Brand, version.Version),
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(deployCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(destroyCmd)
	rootCmd.AddCommand(sshCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(imageCmd)
}

func Execute() error {
	return rootCmd.Execute()
}
