package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/ghostwright/specter/internal/config"
	"github.com/ghostwright/specter/internal/hetzner"
	"github.com/ghostwright/specter/internal/tui"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update agent code",
	Long:  "Pull latest code, reinstall dependencies, and restart the agent.",
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

	fmt.Printf("  Connecting to %s... ", agentName)
	sshClient, err := hetzner.SSHConnect(agent.IP)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}
	defer sshClient.Close()
	fmt.Println(tui.SuccessStyle.Render("connected"))

	fmt.Printf("  Restarting agent... ")
	_, err = hetzner.SSHRun(sshClient, "systemctl restart specter-agent")
	if err != nil {
		fmt.Println(tui.ErrorStyle.Render("failed"))
		return fmt.Errorf("restart failed: %w", err)
	}
	fmt.Println(tui.SuccessStyle.Render("done"))

	fmt.Printf("  Checking health... ")
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
		fmt.Println(tui.ErrorStyle.Render("failed"))
		return fmt.Errorf("health check failed after restart. Run `specter logs %s` to investigate", agentName)
	}

	fmt.Println(tui.SuccessStyle.Render("healthy"))

	if jsonOutput {
		data, _ := json.MarshalIndent(map[string]string{
			"name":   agentName,
			"status": "updated",
		}, "", "  ")
		fmt.Println(string(data))
	}

	fmt.Printf("\n  %s %s updated and healthy\n\n", tui.SuccessStyle.Render("done"), agentName)
	return nil
}
