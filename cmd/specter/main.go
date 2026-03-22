package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/ghostwright/specter/cmd/specter/commands"
	"github.com/ghostwright/specter/internal/config"
	"github.com/ghostwright/specter/internal/tui"
)

func main() {
	if len(os.Args) == 1 {
		runDashboard()
	} else {
		if err := commands.Execute(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func runDashboard() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  No config found. Run `specter init` to set up.\n")
		os.Exit(1)
	}

	model := tui.NewAppModel(cfg)
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
