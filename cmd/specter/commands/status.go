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

var statusCmd = &cobra.Command{
	Use:   "status <name>",
	Short: "Show agent details",
	Args:  cobra.ExactArgs(1),
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	agentName := args[0]

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	state, err := config.LoadState()
	if err != nil {
		return err
	}

	agent, exists := state.GetAgent(agentName)
	if !exists {
		return fmt.Errorf("agent '%s' not found. Run `specter list` to see deployed agents", agentName)
	}

	hc := hetzner.NewClient(cfg.Hetzner.Token)
	server, err := hc.GetServerByName(ctx, agentName)
	if err != nil {
		return err
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}
	healthURL := agent.URL + "/health"
	var healthData map[string]interface{}
	var healthStatus string

	resp, err := httpClient.Get(healthURL)
	if err == nil && resp.StatusCode == 200 {
		json.NewDecoder(resp.Body).Decode(&healthData)
		resp.Body.Close()
		healthStatus = "online"
	} else {
		if resp != nil {
			resp.Body.Close()
		}
		healthStatus = "offline"
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(map[string]interface{}{
			"name":        agentName,
			"url":         agent.URL,
			"ip":          agent.IP,
			"server_id":   agent.ServerID,
			"role":        agent.Role,
			"server_type": agent.ServerType,
			"location":    agent.Location,
			"deployed_at": agent.DeployedAt,
			"status":      healthStatus,
			"health":      healthData,
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Println()
	fmt.Printf("  %s\n\n", tui.TitleStyle.Render(tui.Brand+" STATUS"))

	var statusIcon string
	if healthStatus == "online" {
		statusIcon = tui.StatusOnline + " online"
	} else {
		statusIcon = tui.StatusOffline + " offline"
	}

	fmt.Printf("  Name:        %s\n", tui.TitleStyle.Render(agentName))
	fmt.Printf("  Status:      %s\n", statusIcon)
	fmt.Printf("  URL:         %s\n", agent.URL)
	fmt.Printf("  IP:          %s\n", agent.IP)
	fmt.Printf("  Role:        %s\n", agent.Role)
	fmt.Printf("  Server:      %s (%s)\n", agent.ServerType, agent.Location)
	fmt.Printf("  Deployed:    %s\n", agent.DeployedAt.Format(time.RFC3339))

	if server != nil {
		fmt.Printf("  Hetzner ID:  %d\n", server.ID)
		fmt.Printf("  VM Status:   %s\n", server.Status)
	}

	if healthData != nil {
		if uptime, ok := healthData["uptime"].(float64); ok {
			fmt.Printf("  Uptime:      %s\n", formatUptime(int(uptime)))
		}
		if v, ok := healthData["version"].(string); ok {
			fmt.Printf("  Version:     %s\n", v)
		}
	}

	fmt.Println()
	return nil
}
