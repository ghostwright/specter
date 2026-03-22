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

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Show all deployed agents",
	Aliases: []string{"ls"},
	RunE:  runList,
}

type agentInfo struct {
	Name       string `json:"name"`
	Role       string `json:"role"`
	ServerType string `json:"server_type"`
	Status     string `json:"status"`
	Uptime     string `json:"uptime"`
	URL        string `json:"url"`
	IP         string `json:"ip"`
}

func runList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	hc := hetzner.NewClient(cfg.Hetzner.Token)
	servers, err := hc.ListSpecterServers(ctx)
	if err != nil {
		return err
	}

	state, _ := config.LoadState()

	if len(servers) == 0 {
		if jsonOutput {
			fmt.Println("[]")
			return nil
		}
		fmt.Println()
		fmt.Printf("  %s\n\n", tui.TitleStyle.Render(tui.Brand+" AGENTS"))
		fmt.Printf("  No agents deployed. Run `specter deploy <name>` to get started.\n\n")
		return nil
	}

	stCache, _ := config.LoadServerTypeCache()

	httpClient := &http.Client{Timeout: 3 * time.Second}
	agents := make([]agentInfo, 0, len(servers))
	var onlineCount int
	var monthlyCost float64

	for _, s := range servers {
		name := s.Labels["agent_name"]
		role := s.Labels["role"]
		if name == "" {
			name = s.Name
		}

		info := agentInfo{
			Name:       name,
			Role:       role,
			ServerType: s.ServerType.Name,
			IP:         s.PublicNet.IPv4.IP.String(),
		}

		// Check health endpoint
		var agentState *config.Agent
		if state != nil {
			agentState, _ = state.GetAgent(name)
		}

		healthURL := ""
		if agentState != nil {
			info.URL = agentState.URL
			healthURL = agentState.URL + "/health"
		} else {
			info.URL = fmt.Sprintf("https://%s.%s", name, cfg.Domain)
			healthURL = info.URL + "/health"
		}

		resp, err := httpClient.Get(healthURL)
		if err == nil && resp.StatusCode == 200 {
			var health map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&health)
			resp.Body.Close()

			info.Status = "online"
			onlineCount++

			if uptime, ok := health["uptime"].(float64); ok {
				info.Uptime = formatUptime(int(uptime))
			}
		} else {
			if resp != nil {
				resp.Body.Close()
			}
			if s.Status == "running" {
				info.Status = "unhealthy"
			} else {
				info.Status = "offline"
			}
		}

		monthlyCost += config.GetMonthlyPrice(s.ServerType.Name, stCache)

		agents = append(agents, info)
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(agents, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Println()
	fmt.Printf("  %s\n\n", tui.TitleStyle.Render(tui.Brand+" AGENTS"))

	// Header
	fmt.Printf("  %-12s %-7s %-7s %-11s %-12s %s\n",
		tui.MutedStyle.Render("NAME"),
		tui.MutedStyle.Render("ROLE"),
		tui.MutedStyle.Render("TYPE"),
		tui.MutedStyle.Render("STATUS"),
		tui.MutedStyle.Render("UPTIME"),
		tui.MutedStyle.Render("URL"))

	for _, a := range agents {
		var statusIcon string
		switch a.Status {
		case "online":
			statusIcon = tui.StatusOnline + " online"
		case "unhealthy":
			statusIcon = tui.WarningStyle.Render("◷") + " " + tui.WarningStyle.Render("sick")
		default:
			statusIcon = tui.StatusOffline + " off"
		}

		urlDisplay := a.URL
		if len(urlDisplay) > 28 {
			urlDisplay = urlDisplay[:28] + "..."
		}

		fmt.Printf("  %-12s %-7s %-7s %-11s %-12s %s\n",
			a.Name, a.Role, a.ServerType, statusIcon, a.Uptime, urlDisplay)
	}

	fmt.Printf("\n  %d agents - %d online - $%.2f/mo infra\n\n",
		len(agents), onlineCount, monthlyCost)

	return nil
}

func formatUptime(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	if seconds < 86400 {
		return fmt.Sprintf("%dh %dm", seconds/3600, (seconds%3600)/60)
	}
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	return fmt.Sprintf("%dd %dh", days, hours)
}
