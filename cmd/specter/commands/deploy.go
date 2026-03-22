package commands

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"

	"github.com/ghostwright/specter/internal/cloudflare"
	"github.com/ghostwright/specter/internal/config"
	"github.com/ghostwright/specter/internal/hetzner"
	"github.com/ghostwright/specter/internal/templates"
	"github.com/ghostwright/specter/internal/tui"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
)

var (
	deployRole       string
	deployServerType string
	deployLocation   string
	deployYes        bool
	deployEnv        []string
	deployEnvFile    string
)

var deployCmd = &cobra.Command{
	Use:   "deploy <name>",
	Short: "Deploy a new agent",
	Long:  "Provision a VM, configure DNS, deploy code, and set up TLS.",
	Args:  cobra.ExactArgs(1),
	RunE:  runDeploy,
}

func init() {
	deployCmd.Flags().StringVar(&deployRole, "role", "swe", "Agent role")
	deployCmd.Flags().StringVar(&deployServerType, "server-type", "", "Hetzner server type (default from config)")
	deployCmd.Flags().StringVar(&deployLocation, "location", "", "Hetzner location (default from config)")
	deployCmd.Flags().BoolVarP(&deployYes, "yes", "y", false, "Skip confirmation prompts")
	deployCmd.Flags().StringArrayVar(&deployEnv, "env", nil, "Environment variable (KEY=VALUE, repeatable)")
	deployCmd.Flags().StringVar(&deployEnvFile, "env-file", "", "Path to .env file")
}

var validName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

type deployPhase struct {
	name     string
	estimate time.Duration
	elapsed  time.Duration
	status   string // pending, active, done, failed
}

func runDeploy(cmd *cobra.Command, args []string) error {
	agentName := args[0]

	if !validName.MatchString(agentName) {
		return fmt.Errorf("invalid name '%s'. Must be lowercase, start with a letter, and contain only letters, numbers, and hyphens", agentName)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	state, err := config.LoadState()
	if err != nil {
		return err
	}
	if _, exists := state.GetAgent(agentName); exists {
		return fmt.Errorf("agent '%s' already exists. Run `specter destroy %s` first, or choose a different name", agentName, agentName)
	}

	if cfg.Snapshot.ID == 0 {
		return fmt.Errorf("no golden snapshot configured. Run `specter image build` first")
	}

	serverType := cfg.Hetzner.DefaultServerType
	if deployServerType != "" {
		serverType = deployServerType
	}
	location := cfg.Hetzner.DefaultLocation
	if deployLocation != "" {
		location = deployLocation
	}

	// Validate server type against cache
	stCache, _ := config.LoadServerTypeCache()
	if err := config.ValidateServerType(serverType, location, stCache); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	hc := hetzner.NewClient(cfg.Hetzner.Token)
	cf := cloudflare.NewClient(cfg.Cloudflare.Token, cfg.Cloudflare.ZoneID)

	fqdn := fmt.Sprintf("%s.%s", agentName, cfg.Domain)
	url := fmt.Sprintf("https://%s", fqdn)

	phases := []deployPhase{
		{name: "Creating VM on Hetzner", estimate: 1 * time.Second},
		{name: "Creating DNS record", estimate: 1 * time.Second},
		{name: "Waiting for VM boot", estimate: 68 * time.Second},
		{name: "Waiting for SSH", estimate: 15 * time.Second},
		{name: "Deploying agent code", estimate: 20 * time.Second},
		{name: "Starting services", estimate: 3 * time.Second},
		{name: "Provisioning TLS", estimate: 7 * time.Second},
		{name: "Health check", estimate: 2 * time.Second},
	}

	totalStart := time.Now()
	currentPhase := 0

	printPhases := func() {
		if jsonOutput {
			return
		}
		fmt.Print("\033[H\033[2J") // clear screen
		fmt.Println()
		fmt.Printf("  %s\n\n", tui.TitleStyle.Render(tui.Brand))
		fmt.Printf("  Deploying %s (%s) to %s...\n\n", tui.TitleStyle.Render(agentName), deployRole, location)

		for i, p := range phases {
			var icon, detail string
			switch {
			case i < currentPhase:
				icon = tui.SuccessStyle.Render("  done")
				detail = fmt.Sprintf("  %s", tui.MutedStyle.Render(p.elapsed.Round(time.Second).String()))
			case i == currentPhase:
				elapsed := time.Since(totalStart)
				for j := 0; j < i; j++ {
					elapsed -= phases[j].elapsed
				}
				icon = tui.WarningStyle.Render("  ...")
				detail = fmt.Sprintf("  %s / ~%s",
					tui.WarningStyle.Render(elapsed.Round(time.Second).String()),
					tui.MutedStyle.Render(p.estimate.Round(time.Second).String()))
			default:
				icon = tui.MutedStyle.Render("  -")
				detail = ""
			}
			fmt.Printf("  %s %-30s%s\n", icon, p.name, detail)
		}

		fmt.Printf("\n  Elapsed: %s\n", time.Since(totalStart).Round(time.Second))
	}

	startPhase := func(idx int) time.Time {
		currentPhase = idx
		printPhases()
		return time.Now()
	}

	endPhase := func(idx int, start time.Time) {
		phases[idx].elapsed = time.Since(start)
		phases[idx].status = "done"
	}

	// Track resources for cleanup on Ctrl+C or error
	var serverID int64
	var dnsRecordID string
	var serverIP string

	cleanup := func() {
		cleanCtx := context.Background()
		if dnsRecordID != "" {
			fmt.Printf("\n  Cleaning up DNS record... ")
			if err := cf.DeleteDNSRecord(cleanCtx, dnsRecordID); err == nil {
				fmt.Println(tui.SuccessStyle.Render("done"))
			} else {
				fmt.Println(tui.ErrorStyle.Render("failed"))
			}
		}
		if serverID != 0 {
			fmt.Printf("  Cleaning up VM... ")
			if err := hc.DeleteServer(cleanCtx, &hcloud.Server{ID: serverID}); err == nil {
				fmt.Println(tui.SuccessStyle.Render("done"))
			} else {
				fmt.Println(tui.ErrorStyle.Render("failed"))
			}
		}
	}

	// Parse environment variables
	envVars := make(map[string]string)
	if deployEnvFile != "" {
		fileVars, err := templates.ParseEnvFile(deployEnvFile)
		if err != nil {
			return fmt.Errorf("could not read env file: %w", err)
		}
		for k, v := range fileVars {
			envVars[k] = v
		}
	}
	for _, e := range deployEnv {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid env var: %s (expected KEY=VALUE)", e)
		}
		envVars[parts[0]] = parts[1]
	}

	// Phase 0: Look up SSH key (G-16)
	phaseStart := startPhase(0)

	sshKey, err := hc.GetSSHKey(ctx, cfg.Hetzner.SSHKeyName)
	if err != nil {
		return err
	}

	// Cloud-init user_data
	userData, err := templates.RenderCloudInit(templates.CloudInitData{
		AgentName: agentName,
		Domain:    cfg.Domain,
		Role:      deployRole,
		EnvVars:   envVars,
	})
	if err != nil {
		return err
	}

	// Get firewall reference
	var firewalls []*hcloud.ServerCreateFirewall
	if cfg.Hetzner.FirewallID > 0 {
		firewalls = []*hcloud.ServerCreateFirewall{
			{Firewall: hcloud.Firewall{ID: cfg.Hetzner.FirewallID}},
		}
	}

	result, err := hc.CreateServer(ctx, hcloud.ServerCreateOpts{
		Name:       fmt.Sprintf("specter-%s", agentName),
		ServerType: &hcloud.ServerType{Name: serverType},
		Image:      &hcloud.Image{ID: cfg.Snapshot.ID},
		Location:   &hcloud.Location{Name: location},
		SSHKeys:    []*hcloud.SSHKey{sshKey},
		UserData:   userData,
		Labels: map[string]string{
			"managed_by": "specter",
			"agent_name": agentName,
			"role":       deployRole,
		},
		Firewalls: firewalls,
	})
	if err != nil {
		return fmt.Errorf("failed to create VM: %w", err)
	}

	serverID = result.Server.ID
	serverIP = result.Server.PublicNet.IPv4.IP.String() // G-02: IP available immediately
	endPhase(0, phaseStart)

	// Phase 1: DNS record (parallel with boot)
	phaseStart = startPhase(1)

	dnsRecord, err := cf.CreateDNSRecord(ctx, fqdn, serverIP)
	if err != nil {
		cleanup()
		return fmt.Errorf("failed to create DNS record: %w", err)
	}
	dnsRecordID = dnsRecord.ID
	endPhase(1, phaseStart)

	// Phase 2: Wait for VM running
	phaseStart = startPhase(2)

	// Background ticker to update the display
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				printPhases()
			}
		}
	}()

	_, err = hc.WaitForRunning(ctx, serverID)
	close(done)
	if err != nil {
		cleanup()
		return fmt.Errorf("VM failed to start: %w", err)
	}
	endPhase(2, phaseStart)

	// Phase 3: Wait for SSH
	phaseStart = startPhase(3)

	sshClient, err := hetzner.WaitForSSH(ctx, serverIP)
	if err != nil {
		cleanup()
		return fmt.Errorf("SSH connection failed: %w", err)
	}
	defer sshClient.Close()
	endPhase(3, phaseStart)

	// Phase 4: Deploy agent code
	phaseStart = startPhase(4)

	agentCode := `
const server = Bun.serve({
  port: 3100,
  fetch(req) {
    const url = new URL(req.url);
    if (url.pathname === "/health") {
      return Response.json({
        status: "ok",
        uptime: Math.floor(process.uptime()),
        version: "0.1.0",
        timestamp: new Date().toISOString()
      });
    }
    return new Response("specter-agent", { status: 200 });
  }
});
console.log("specter-agent listening on port " + server.port);
`

	packageJSON := `{
  "name": "specter-agent",
  "version": "0.1.0",
  "scripts": {
    "start": "bun run index.ts"
  }
}
`

	deployScript := fmt.Sprintf(`
cat > /home/specter/app/index.ts << 'AGENTEOF'
%s
AGENTEOF

cat > /home/specter/app/package.json << 'PKGEOF'
%s
PKGEOF

chown -R specter:specter /home/specter/app/

# Ensure bun is available system-wide (G-10: bun installs per-user)
if [ ! -f /usr/local/bin/bun ]; then
  if [ -f /home/specter/.bun/bin/bun ]; then
    ln -sf /home/specter/.bun/bin/bun /usr/local/bin/bun
  elif [ -f /root/.bun/bin/bun ]; then
    ln -sf /root/.bun/bin/bun /usr/local/bin/bun
  fi
fi

cat > /etc/systemd/system/specter-agent.service << 'SVCEOF'
%s
SVCEOF
`, agentCode, packageJSON, templates.SystemdUnit)

	if _, err := hetzner.SSHRun(sshClient, deployScript); err != nil {
		cleanup()
		return fmt.Errorf("failed to deploy agent code: %w", err)
	}
	endPhase(4, phaseStart)

	// Phase 5: Start services
	phaseStart = startPhase(5)

	startScript := `
systemctl daemon-reload
systemctl enable specter-agent
systemctl start specter-agent
systemctl restart caddy
`
	if _, err := hetzner.SSHRun(sshClient, startScript); err != nil {
		cleanup()
		return fmt.Errorf("failed to start services: %w", err)
	}
	endPhase(5, phaseStart)

	// Phase 6: Wait for HTTPS
	phaseStart = startPhase(6)

	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
	}

	healthURL := fmt.Sprintf("%s/health", url)
	var healthResp *http.Response
	tlsDeadline := time.After(120 * time.Second)
	for {
		select {
		case <-ctx.Done():
			cleanup()
			return ctx.Err()
		case <-tlsDeadline:
			cleanup()
			return fmt.Errorf("TLS/health check timed out after 120s. The server may need debugging. Run `specter ssh %s` to investigate", agentName)
		default:
		}

		healthResp, err = httpClient.Get(healthURL)
		if err == nil && healthResp.StatusCode == 200 {
			break
		}
		if healthResp != nil {
			healthResp.Body.Close()
		}
		time.Sleep(2 * time.Second)
		printPhases()
	}
	endPhase(6, phaseStart)

	// Phase 7: Health check
	phaseStart = startPhase(7)

	var healthData map[string]interface{}
	if healthResp != nil {
		json.NewDecoder(healthResp.Body).Decode(&healthData)
		healthResp.Body.Close()
	}
	endPhase(7, phaseStart)

	// Save state
	state.SetAgent(agentName, &config.Agent{
		ServerID:        serverID,
		IP:              serverIP,
		DNSRecordID:     dnsRecordID,
		URL:             url,
		Role:            deployRole,
		ServerType:      serverType,
		Location:        location,
		DeployedAt:      time.Now(),
		SnapshotVersion: cfg.Snapshot.Version,
	})
	if err := state.Save(); err != nil {
		fmt.Printf("  %s Could not save state: %v\n", tui.WarningStyle.Render("!"), err)
	}

	totalElapsed := time.Since(totalStart).Round(time.Second)

	if jsonOutput {
		data, _ := json.MarshalIndent(map[string]interface{}{
			"name":       agentName,
			"url":        url,
			"ip":         serverIP,
			"server_id":  serverID,
			"elapsed":    totalElapsed.String(),
			"health":     healthData,
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	printPhases()
	fmt.Println()
	fmt.Printf("  %s Agent deployed successfully!\n\n", tui.SuccessStyle.Render("done"))
	fmt.Printf("  URL:       %s\n", tui.TitleStyle.Render(url))
	fmt.Printf("  IP:        %s\n", serverIP)
	fmt.Printf("  Server ID: %d\n", serverID)
	fmt.Printf("  Total:     %s\n\n", totalElapsed)

	return nil
}
