package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/ghostwright/specter/internal/cloudflare"
	"github.com/ghostwright/specter/internal/config"
	"github.com/ghostwright/specter/internal/hetzner"
	"github.com/ghostwright/specter/internal/tui"
	"github.com/spf13/cobra"
)

var (
	initHetznerToken string
	initCFToken      string
	initCFZoneID     string
	initDomain       string
	initSSHKeyName   string
	initYes          bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Set up Specter (tokens, SSH key, firewall)",
	Long:  "Interactive setup wizard. Validates API tokens, creates a firewall, and writes config.",
	RunE:  runInit,
}

func init() {
	initCmd.Flags().StringVar(&initHetznerToken, "hetzner-token", "", "Hetzner Cloud API token")
	initCmd.Flags().StringVar(&initCFToken, "cloudflare-token", "", "Cloudflare DNS API token")
	initCmd.Flags().StringVar(&initCFZoneID, "cloudflare-zone-id", "", "Cloudflare Zone ID")
	initCmd.Flags().StringVar(&initDomain, "domain", "", "Base domain (e.g. specter.tools)")
	initCmd.Flags().StringVar(&initSSHKeyName, "ssh-key", "", "SSH key name on Hetzner")
	initCmd.Flags().BoolVarP(&initYes, "yes", "y", false, "Skip confirmation prompts")
}

func runInit(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	reader := bufio.NewReader(os.Stdin)

	if config.Exists() && !initYes {
		fmt.Printf("%s Existing config found. Reconfigure? [y/N] ", tui.WarningStyle.Render("!"))
		answer, _ := reader.ReadString('\n')
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "y") {
			fmt.Println("Keeping existing config.")
			return nil
		}
	}

	fmt.Println()
	fmt.Println(tui.Logo())
	fmt.Println()
	fmt.Printf("  %s\n", tui.TitleStyle.Render("Setup Wizard"))
	fmt.Println()

	cfg := config.DefaultConfig()

	// Hetzner token
	if initHetznerToken != "" {
		cfg.Hetzner.Token = initHetznerToken
	} else {
		fmt.Print("  Hetzner Cloud API token: ")
		token, _ := reader.ReadString('\n')
		cfg.Hetzner.Token = strings.TrimSpace(token)
	}

	fmt.Printf("  Validating Hetzner token... ")
	hc := hetzner.NewClient(cfg.Hetzner.Token)
	if err := hc.ValidateToken(ctx); err != nil {
		fmt.Println(tui.ErrorStyle.Render("failed"))
		return fmt.Errorf("Hetzner token validation failed: %w. Check your token at console.hetzner.cloud", err)
	}
	fmt.Println(tui.SuccessStyle.Render("valid"))

	// Cloudflare token
	if initCFToken != "" {
		cfg.Cloudflare.Token = initCFToken
	} else {
		fmt.Print("  Cloudflare DNS token: ")
		token, _ := reader.ReadString('\n')
		cfg.Cloudflare.Token = strings.TrimSpace(token)
	}

	// Cloudflare zone ID
	if initCFZoneID != "" {
		cfg.Cloudflare.ZoneID = initCFZoneID
	} else {
		fmt.Print("  Cloudflare Zone ID: ")
		zoneID, _ := reader.ReadString('\n')
		cfg.Cloudflare.ZoneID = strings.TrimSpace(zoneID)
	}

	fmt.Printf("  Validating Cloudflare token... ")
	cf := cloudflare.NewClient(cfg.Cloudflare.Token, cfg.Cloudflare.ZoneID)
	if err := cf.ValidateToken(ctx); err != nil {
		fmt.Println(tui.ErrorStyle.Render("failed"))
		return fmt.Errorf("Cloudflare token validation failed: %w", err)
	}
	fmt.Println(tui.SuccessStyle.Render("valid"))

	// Domain
	if initDomain != "" {
		cfg.Domain = initDomain
	} else {
		fmt.Printf("  Domain [%s]: ", cfg.Domain)
		domain, _ := reader.ReadString('\n')
		domain = strings.TrimSpace(domain)
		if domain != "" {
			cfg.Domain = domain
		}
	}

	// SSH key
	if initSSHKeyName != "" {
		cfg.Hetzner.SSHKeyName = initSSHKeyName
	} else {
		keys, err := hc.ListSSHKeys(ctx)
		if err != nil {
			return fmt.Errorf("could not list SSH keys: %w", err)
		}
		if len(keys) == 0 {
			return fmt.Errorf("no SSH keys found on Hetzner. Upload one at console.hetzner.cloud -> Security -> SSH Keys")
		}
		if len(keys) == 1 {
			cfg.Hetzner.SSHKeyName = keys[0].Name
			fmt.Printf("  SSH key: %s (auto-detected)\n", tui.SuccessStyle.Render(keys[0].Name))
		} else {
			fmt.Println("  Available SSH keys:")
			for i, k := range keys {
				fmt.Printf("    [%d] %s\n", i+1, k.Name)
			}
			fmt.Print("  Select SSH key [1]: ")
			choice, _ := reader.ReadString('\n')
			choice = strings.TrimSpace(choice)
			idx := 0
			if choice != "" {
				fmt.Sscanf(choice, "%d", &idx)
				idx--
			}
			if idx < 0 || idx >= len(keys) {
				idx = 0
			}
			cfg.Hetzner.SSHKeyName = keys[idx].Name
		}
	}

	// Create firewall
	fmt.Printf("  Creating firewall (specter-default)... ")
	fw, err := hc.CreateFirewall(ctx, "specter-default")
	if err != nil {
		fmt.Println(tui.ErrorStyle.Render("failed"))
		return err
	}
	cfg.Hetzner.FirewallID = fw.ID
	fmt.Println(tui.SuccessStyle.Render("done"))

	// Fetch and cache server types
	fmt.Printf("  Fetching server types... ")
	serverTypes, err := hc.ListServerTypes(ctx)
	if err != nil {
		fmt.Println(tui.WarningStyle.Render("failed (will use fallback)"))
	} else {
		cache := &config.ServerTypeCache{
			Types:     serverTypes,
			FetchedAt: time.Now(),
		}
		if err := cache.Save(); err != nil {
			fmt.Println(tui.WarningStyle.Render("could not cache"))
		} else {
			fmt.Printf("%s (%d types)\n", tui.SuccessStyle.Render("cached"), len(serverTypes))
		}
	}

	// Check for existing snapshot
	fmt.Printf("  Checking for golden snapshot... ")
	snap, err := hc.FindSpecterSnapshot(ctx)
	if err != nil {
		fmt.Println(tui.ErrorStyle.Render("failed"))
		return err
	}
	if snap != nil {
		cfg.Snapshot.ID = snap.ID
		if v, ok := snap.Labels["version"]; ok {
			cfg.Snapshot.Version = v
		}
		cfg.Snapshot.DiskSize = snap.DiskSize
		fmt.Printf("%s (ID: %d)\n", tui.SuccessStyle.Render("found"), snap.ID)
	} else {
		fmt.Println(tui.WarningStyle.Render("none found"))
		fmt.Println("  Run `specter image build` to create one before deploying.")
	}

	// Save config
	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Println()
	if jsonOutput {
		data, _ := json.MarshalIndent(cfg, "", "  ")
		fmt.Println(string(data))
	} else {
		fmt.Printf("  %s Config saved to ~/.specter/config.yaml\n", tui.SuccessStyle.Render("done"))
		fmt.Println()
	}

	return nil
}
