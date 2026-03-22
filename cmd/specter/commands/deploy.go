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

	tea "charm.land/bubbletea/v2"
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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	hc := hetzner.NewClient(cfg.Hetzner.Token)
	cf := cloudflare.NewClient(cfg.Cloudflare.Token, cfg.Cloudflare.ZoneID)

	fqdn := fmt.Sprintf("%s.%s", agentName, cfg.Domain)
	agentURL := fmt.Sprintf("https://%s", fqdn)

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

	// JSON mode: no TUI, run deploy inline
	if jsonOutput {
		return runDeployJSON(ctx, cfg, state, hc, cf, agentName, deployRole, serverType, location, agentURL, fqdn, envVars, phases)
	}

	// TUI mode: Bubbletea program
	tuiPhases := make([]tui.Phase, len(phases))
	for i, p := range phases {
		tuiPhases[i] = tui.Phase{
			Name:     p.name,
			Estimate: p.estimate,
		}
	}

	model := tui.NewDeployModel(agentName, deployRole, location, serverType, tuiPhases)
	p := tea.NewProgram(model,
		tea.WithInput(os.Stdin),
		tea.WithOutput(os.Stderr),
	)

	// Track resources for cleanup
	var serverID int64
	var dnsRecordID string
	var serverIP string

	cleanup := func() {
		cleanCtx := context.Background()
		if dnsRecordID != "" {
			cf.DeleteDNSRecord(cleanCtx, dnsRecordID)
		}
		if serverID != 0 {
			hc.DeleteServer(cleanCtx, &hcloud.Server{ID: serverID})
		}
	}

	// Deploy orchestration runs in a goroutine, sending messages to the TUI
	go func() {
		totalStart := time.Now()

		// Phase 0: Create VM
		p.Send(tui.PhaseStartMsg(0))
		phaseStart := time.Now()

		sshKey, err := hc.GetSSHKey(ctx, cfg.Hetzner.SSHKeyName)
		if err != nil {
			p.Send(tui.DeployErrMsg{Err: err})
			return
		}

		userData, err := templates.RenderCloudInit(templates.CloudInitData{
			AgentName: agentName,
			Domain:    cfg.Domain,
			Role:      deployRole,
			EnvVars:   envVars,
		})
		if err != nil {
			p.Send(tui.DeployErrMsg{Err: err})
			return
		}

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
			p.Send(tui.DeployErrMsg{Err: fmt.Errorf("failed to create VM: %w", err)})
			return
		}

		serverID = result.Server.ID
		serverIP = result.Server.PublicNet.IPv4.IP.String()
		p.Send(tui.PhaseDoneMsg{Index: 0, Elapsed: time.Since(phaseStart)})

		// Phase 1: DNS record
		p.Send(tui.PhaseStartMsg(1))
		phaseStart = time.Now()

		dnsRecord, err := cf.CreateDNSRecord(ctx, fqdn, serverIP)
		if err != nil {
			cleanup()
			p.Send(tui.DeployErrMsg{Err: fmt.Errorf("failed to create DNS record: %w", err)})
			return
		}
		dnsRecordID = dnsRecord.ID
		p.Send(tui.PhaseDoneMsg{Index: 1, Elapsed: time.Since(phaseStart)})

		// Phase 2: Wait for VM running
		p.Send(tui.PhaseStartMsg(2))
		phaseStart = time.Now()

		_, err = hc.WaitForRunning(ctx, serverID)
		if err != nil {
			cleanup()
			p.Send(tui.DeployErrMsg{Err: fmt.Errorf("VM failed to start: %w", err)})
			return
		}
		p.Send(tui.PhaseDoneMsg{Index: 2, Elapsed: time.Since(phaseStart)})

		// Phase 3: Wait for SSH
		p.Send(tui.PhaseStartMsg(3))
		phaseStart = time.Now()

		sshClient, err := hetzner.WaitForSSH(ctx, serverIP)
		if err != nil {
			cleanup()
			p.Send(tui.DeployErrMsg{Err: fmt.Errorf("SSH connection failed: %w", err)})
			return
		}
		defer sshClient.Close()
		p.Send(tui.PhaseDoneMsg{Index: 3, Elapsed: time.Since(phaseStart)})

		// Phase 4: Deploy agent code
		p.Send(tui.PhaseStartMsg(4))
		phaseStart = time.Now()

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
			p.Send(tui.DeployErrMsg{Err: fmt.Errorf("failed to deploy agent code: %w", err)})
			return
		}
		p.Send(tui.PhaseDoneMsg{Index: 4, Elapsed: time.Since(phaseStart)})

		// Phase 5: Start services
		p.Send(tui.PhaseStartMsg(5))
		phaseStart = time.Now()

		startScript := `
systemctl daemon-reload
systemctl enable specter-agent
systemctl start specter-agent
systemctl restart caddy
`
		if _, err := hetzner.SSHRun(sshClient, startScript); err != nil {
			cleanup()
			p.Send(tui.DeployErrMsg{Err: fmt.Errorf("failed to start services: %w", err)})
			return
		}
		p.Send(tui.PhaseDoneMsg{Index: 5, Elapsed: time.Since(phaseStart)})

		// Phase 6: Wait for HTTPS
		p.Send(tui.PhaseStartMsg(6))
		phaseStart = time.Now()

		httpClient := &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
			},
		}

		healthURL := fmt.Sprintf("%s/health", agentURL)
		var healthResp *http.Response
		tlsDeadline := time.After(120 * time.Second)
		for {
			select {
			case <-ctx.Done():
				cleanup()
				p.Send(tui.DeployErrMsg{Err: ctx.Err()})
				return
			case <-tlsDeadline:
				cleanup()
				p.Send(tui.DeployErrMsg{Err: fmt.Errorf("TLS/health check timed out after 120s")})
				return
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
		}
		p.Send(tui.PhaseDoneMsg{Index: 6, Elapsed: time.Since(phaseStart)})

		// Phase 7: Health check
		p.Send(tui.PhaseStartMsg(7))
		phaseStart = time.Now()

		if healthResp != nil {
			healthResp.Body.Close()
		}
		p.Send(tui.PhaseDoneMsg{Index: 7, Elapsed: time.Since(phaseStart)})

		// Save state
		state.SetAgent(agentName, &config.Agent{
			ServerID:        serverID,
			IP:              serverIP,
			DNSRecordID:     dnsRecordID,
			URL:             agentURL,
			Role:            deployRole,
			ServerType:      serverType,
			Location:        location,
			DeployedAt:      time.Now(),
			SnapshotVersion: cfg.Snapshot.Version,
		})
		state.Save()

		totalElapsed := time.Since(totalStart).Round(time.Second)

		p.Send(tui.DeployDoneMsg{
			Result: tui.DeployResult{
				URL:      agentURL,
				IP:       serverIP,
				ServerID: serverID,
				Elapsed:  totalElapsed,
			},
		})
	}()

	finalModel, err := p.Run()
	if err != nil {
		return err
	}

	m := finalModel.(tui.DeployModel)
	if m.Quitting() {
		cleanup()
		return fmt.Errorf("deploy interrupted by user")
	}
	if m.Err() != nil {
		return m.Err()
	}

	return nil
}

// runDeployJSON runs the deploy without TUI, outputting JSON at the end.
func runDeployJSON(ctx context.Context, cfg *config.Config, state *config.State,
	hc *hetzner.Client, cf *cloudflare.Client,
	agentName, role, serverType, location, agentURL, fqdn string,
	envVars map[string]string, phases []deployPhase) error {

	totalStart := time.Now()

	// Track resources for cleanup
	var serverID int64
	var dnsRecordID string
	var serverIP string

	cleanup := func() {
		cleanCtx := context.Background()
		if dnsRecordID != "" {
			cf.DeleteDNSRecord(cleanCtx, dnsRecordID)
		}
		if serverID != 0 {
			hc.DeleteServer(cleanCtx, &hcloud.Server{ID: serverID})
		}
	}

	// Phase 0: Create VM
	phaseStart := time.Now()

	sshKey, err := hc.GetSSHKey(ctx, cfg.Hetzner.SSHKeyName)
	if err != nil {
		return err
	}

	userData, err := templates.RenderCloudInit(templates.CloudInitData{
		AgentName: agentName,
		Domain:    cfg.Domain,
		Role:      role,
		EnvVars:   envVars,
	})
	if err != nil {
		return err
	}

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
			"role":       role,
		},
		Firewalls: firewalls,
	})
	if err != nil {
		return fmt.Errorf("failed to create VM: %w", err)
	}

	serverID = result.Server.ID
	serverIP = result.Server.PublicNet.IPv4.IP.String()
	phases[0].elapsed = time.Since(phaseStart)

	// Phase 1: DNS record
	phaseStart = time.Now()
	dnsRecord, err := cf.CreateDNSRecord(ctx, fqdn, serverIP)
	if err != nil {
		cleanup()
		return fmt.Errorf("failed to create DNS record: %w", err)
	}
	dnsRecordID = dnsRecord.ID
	phases[1].elapsed = time.Since(phaseStart)

	// Phase 2: Wait for VM running
	phaseStart = time.Now()
	_, err = hc.WaitForRunning(ctx, serverID)
	if err != nil {
		cleanup()
		return fmt.Errorf("VM failed to start: %w", err)
	}
	phases[2].elapsed = time.Since(phaseStart)

	// Phase 3: Wait for SSH
	phaseStart = time.Now()
	sshClient, err := hetzner.WaitForSSH(ctx, serverIP)
	if err != nil {
		cleanup()
		return fmt.Errorf("SSH connection failed: %w", err)
	}
	defer sshClient.Close()
	phases[3].elapsed = time.Since(phaseStart)

	// Phase 4: Deploy agent code
	phaseStart = time.Now()

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
	phases[4].elapsed = time.Since(phaseStart)

	// Phase 5: Start services
	phaseStart = time.Now()
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
	phases[5].elapsed = time.Since(phaseStart)

	// Phase 6: Wait for HTTPS
	phaseStart = time.Now()

	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
	}

	healthURL := fmt.Sprintf("%s/health", agentURL)
	var healthResp *http.Response
	tlsDeadline := time.After(120 * time.Second)
	for {
		select {
		case <-ctx.Done():
			cleanup()
			return ctx.Err()
		case <-tlsDeadline:
			cleanup()
			return fmt.Errorf("TLS/health check timed out after 120s")
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
	}
	phases[6].elapsed = time.Since(phaseStart)

	// Phase 7: Health check
	phaseStart = time.Now()
	var healthData map[string]interface{}
	if healthResp != nil {
		json.NewDecoder(healthResp.Body).Decode(&healthData)
		healthResp.Body.Close()
	}
	phases[7].elapsed = time.Since(phaseStart)

	// Save state
	state.SetAgent(agentName, &config.Agent{
		ServerID:        serverID,
		IP:              serverIP,
		DNSRecordID:     dnsRecordID,
		URL:             agentURL,
		Role:            role,
		ServerType:      serverType,
		Location:        location,
		DeployedAt:      time.Now(),
		SnapshotVersion: cfg.Snapshot.Version,
	})
	state.Save()

	totalElapsed := time.Since(totalStart).Round(time.Second)

	type phaseResult struct {
		Name    string  `json:"name"`
		Seconds float64 `json:"seconds"`
	}
	phaseResults := make([]phaseResult, len(phases))
	for i, ph := range phases {
		phaseResults[i] = phaseResult{
			Name:    ph.name,
			Seconds: ph.elapsed.Seconds(),
		}
	}

	data, _ := json.MarshalIndent(map[string]interface{}{
		"status":              "deployed",
		"name":                agentName,
		"role":                role,
		"url":                 agentURL,
		"ip":                  serverIP,
		"server_id":           serverID,
		"server_type":         serverType,
		"location":            location,
		"dns_record_id":       dnsRecordID,
		"deploy_time_seconds": totalElapsed.Seconds(),
		"phases":              phaseResults,
		"health":              healthData,
	}, "", "  ")
	fmt.Println(string(data))
	return nil
}
