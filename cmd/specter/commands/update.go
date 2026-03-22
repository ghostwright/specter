package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/ghostwright/specter/internal/config"
	"github.com/ghostwright/specter/internal/hetzner"
	"github.com/ghostwright/specter/internal/tui"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Restart agent and refresh dependencies",
	Long:  "Restart the agent service and reinstall dependencies. Code pull is available when deploying from a git repository.",
	Args:  cobra.ExactArgs(1),
	RunE:  runUpdate,
}

func runUpdate(cmd *cobra.Command, args []string) error {
	agentName := args[0]

	state, err := config.LoadState()
	if err != nil {
		return err
	}

	agent, exists := state.GetAgent(agentName)
	if !exists {
		return fmt.Errorf("agent '%s' not found. Run `specter list` to see deployed agents", agentName)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if !jsonOutput {
		fmt.Printf("  Connecting to %s... ", agentName)
	}
	sshClient, err := hetzner.SSHConnect(agent.IP)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}
	defer sshClient.Close()
	if !jsonOutput {
		fmt.Println(tui.SuccessStyle.Render("connected"))
	}

	// Pull latest code if git repo exists
	if !jsonOutput {
		fmt.Printf("  Pulling latest code... ")
	}
	pullOutput, err := hetzner.SSHRun(sshClient, `
cd /home/specter/app
if [ -d .git ]; then
    sudo -u specter git pull origin main 2>&1 || echo "git-pull-skipped"
else
    echo "no-git-repo"
fi
`)
	if err != nil {
		if !jsonOutput {
			fmt.Println(tui.WarningStyle.Render("skipped"))
		}
	} else {
		pullOutput = strings.TrimSpace(pullOutput)
		if !jsonOutput {
			if pullOutput == "no-git-repo" {
				fmt.Println(tui.MutedStyle.Render("no git repo"))
			} else {
				fmt.Println(tui.SuccessStyle.Render("done"))
			}
		}
	}

	// Install dependencies
	if !jsonOutput {
		fmt.Printf("  Installing dependencies... ")
	}
	_, err = hetzner.SSHRun(sshClient, `
cd /home/specter/app
if [ -f package.json ]; then
    sudo -u specter /usr/local/bin/bun install 2>&1
else
    echo "no-package-json"
fi
`)
	if err != nil {
		if !jsonOutput {
			fmt.Println(tui.WarningStyle.Render("skipped"))
		}
	} else {
		if !jsonOutput {
			fmt.Println(tui.SuccessStyle.Render("done"))
		}
	}

	// Restart the service
	if !jsonOutput {
		fmt.Printf("  Restarting agent... ")
	}
	_, err = hetzner.SSHRun(sshClient, "systemctl restart specter-agent")
	if err != nil {
		if !jsonOutput {
			fmt.Println(tui.ErrorStyle.Render("failed"))
		}
		return fmt.Errorf("restart failed: %w", err)
	}
	if !jsonOutput {
		fmt.Println(tui.SuccessStyle.Render("done"))
	}

	// Health check
	if !jsonOutput {
		fmt.Printf("  Checking health... ")
	}
	httpClient := &http.Client{Timeout: 5 * time.Second}
	healthURL := agent.URL + "/health"

	var healthy bool
	for i := 0; i < 10; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := httpClient.Get(healthURL)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			healthy = true
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(2 * time.Second)
	}

	if !healthy {
		if !jsonOutput {
			fmt.Println(tui.ErrorStyle.Render("failed"))
		}
		return fmt.Errorf("health check failed after restart. Run `specter logs %s` to investigate", agentName)
	}

	if !jsonOutput {
		fmt.Println(tui.SuccessStyle.Render("healthy"))
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(map[string]string{
			"name":   agentName,
			"status": "updated",
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("\n  %s %s updated and healthy\n\n", tui.SuccessStyle.Render("done"), agentName)
	return nil
}
