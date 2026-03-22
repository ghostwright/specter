package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/ghostwright/specter/internal/cloudflare"
	"github.com/ghostwright/specter/internal/config"
	"github.com/ghostwright/specter/internal/hetzner"
	"github.com/ghostwright/specter/internal/tui"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
)

var destroyYes bool

var destroyCmd = &cobra.Command{
	Use:   "destroy <name>",
	Short: "Remove an agent",
	Long:  "Delete the VM, DNS record, and local state for an agent.",
	Args:  cobra.ExactArgs(1),
	RunE:  runDestroy,
}

func init() {
	destroyCmd.Flags().BoolVarP(&destroyYes, "yes", "y", false, "Skip confirmation prompt")
}

func runDestroy(cmd *cobra.Command, args []string) error {
	agentName := args[0]

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	state, err := config.LoadState()
	if err != nil {
		return err
	}

	agent, exists := state.GetAgent(agentName)
	if !exists {
		return fmt.Errorf("agent '%s' not found in local state. Run `specter list` to see deployed agents", agentName)
	}

	if !destroyYes {
		fmt.Printf("\n  Destroy %s? This will delete the VM and DNS record. [y/N] ",
			tui.ErrorStyle.Render(agentName))
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "y") {
			fmt.Println("  Cancelled.")
			return nil
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	hc := hetzner.NewClient(cfg.Hetzner.Token)
	cf := cloudflare.NewClient(cfg.Cloudflare.Token, cfg.Cloudflare.ZoneID)

	// Delete DNS record
	if agent.DNSRecordID != "" {
		if !jsonOutput {
			fmt.Printf("  Deleting DNS record... ")
		}
		if err := cf.DeleteDNSRecord(ctx, agent.DNSRecordID); err != nil {
			if cloudflare.IsNotFound(err) {
				if !jsonOutput {
					fmt.Println(tui.MutedStyle.Render("already deleted"))
				}
			} else {
				if !jsonOutput {
					fmt.Println(tui.WarningStyle.Render("failed: " + err.Error()))
				}
			}
		} else {
			if !jsonOutput {
				fmt.Println(tui.SuccessStyle.Render("done"))
			}
		}
	}

	// Delete server
	if !jsonOutput {
		fmt.Printf("  Deleting server... ")
	}
	if err := hc.DeleteServer(ctx, &hcloud.Server{ID: agent.ServerID}); err != nil {
		if hetzner.IsNotFound(err) {
			if !jsonOutput {
				fmt.Println(tui.MutedStyle.Render("already deleted"))
			}
		} else {
			if !jsonOutput {
				fmt.Println(tui.WarningStyle.Render("failed: " + err.Error()))
			}
		}
	} else {
		if !jsonOutput {
			fmt.Println(tui.SuccessStyle.Render("done"))
		}
	}

	// Remove from state
	state.RemoveAgent(agentName)
	if err := state.Save(); err != nil {
		return fmt.Errorf("could not save state: %w", err)
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(map[string]string{
			"destroyed": agentName,
			"status":    "success",
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("\n  %s %s destroyed\n\n", tui.SuccessStyle.Render("done"), agentName)
	return nil
}
