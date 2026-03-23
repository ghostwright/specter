package commands

import (
	"encoding/json"
	"fmt"

	"github.com/ghostwright/specter/internal/tui"
	"github.com/ghostwright/specter/pkg/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		if jsonOutput {
			data, _ := json.MarshalIndent(map[string]string{
				"version": version.Version,
				"commit":  version.Commit,
				"date":    version.Date,
			}, "", "  ")
			fmt.Println(string(data))
			return
		}
		fmt.Println()
		fmt.Println(tui.DashboardLogo())
		fmt.Println()
		fmt.Printf("  %s v%s\n", tui.TitleStyle.Render(tui.Brand), version.Version)
		fmt.Printf("  Commit: %s\n", version.Commit)
		fmt.Printf("  Built:  %s\n\n", version.Date)
	},
}
