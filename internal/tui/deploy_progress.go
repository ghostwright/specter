package tui

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/ghostwright/specter/internal/cloudflare"
	"github.com/ghostwright/specter/internal/config"
	"github.com/ghostwright/specter/internal/hetzner"
	"github.com/ghostwright/specter/internal/templates"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

type deployPhaseInfo struct {
	name     string
	estimate time.Duration
	elapsed  time.Duration
	status   string // "", "active", "done", "error"
	err      error
}

// DeployProgressModel tracks a deploy operation in the dashboard.
type DeployProgressModel struct {
	agentName  string
	role       string
	serverType string
	location   string
	phases     []deployPhaseInfo
	current    int
	startTime  time.Time
	spinIdx    int
	done       bool
	err        error
	result     *TUIDeployCompleteMsg
	width      int
	height     int
}

// NewDeployProgressModel creates the progress display.
func NewDeployProgressModel(name, role, serverType, location string) DeployProgressModel {
	return DeployProgressModel{
		agentName:  name,
		role:       role,
		serverType: serverType,
		location:   location,
		current:    -1,
		startTime:  time.Now(),
		phases: []deployPhaseInfo{
			{name: "Creating VM on Hetzner", estimate: 1 * time.Second},
			{name: "Creating DNS record", estimate: 1 * time.Second},
			{name: "Waiting for VM boot", estimate: 68 * time.Second},
			{name: "Waiting for SSH", estimate: 15 * time.Second},
			{name: "Deploying agent code", estimate: 20 * time.Second},
			{name: "Starting services", estimate: 3 * time.Second},
			{name: "Provisioning TLS", estimate: 7 * time.Second},
			{name: "Health check", estimate: 2 * time.Second},
		},
	}
}

func (m *DeployProgressModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *DeployProgressModel) HandleMsg(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case TUIDeployPhaseMsg:
		if msg.Phase >= 0 && msg.Phase < len(m.phases) {
			m.phases[msg.Phase].status = msg.Status
			m.phases[msg.Phase].elapsed = msg.Elapsed
			if msg.Err != nil {
				m.phases[msg.Phase].err = msg.Err
			}
			if msg.Status == "active" {
				m.current = msg.Phase
			}
		}
		return nil

	case TUIDeployCompleteMsg:
		m.done = true
		m.result = &msg
		return nil

	case TUIDeployErrorMsg:
		m.done = true
		m.err = msg.Err
		return nil

	case spinTickMsg:
		m.spinIdx = (m.spinIdx + 1) % len(spinChars)
		return nil
	}
	return nil
}

func (m DeployProgressModel) View() string {
	var b strings.Builder

	header := lipgloss.NewStyle().Bold(true).Foreground(primaryColor).
		Render(fmt.Sprintf("\u2b21 DEPLOYING %s", strings.ToUpper(m.agentName)))
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(mutedColor).
		Render(fmt.Sprintf("  %s on %s (%s)", m.role, m.serverType, m.location)))
	b.WriteString("\n\n")

	for _, p := range m.phases {
		var icon, detail string
		switch p.status {
		case "done":
			icon = lipgloss.NewStyle().Foreground(successColor).Render("\u2713")
			elapsed := p.elapsed.Round(time.Second)
			if elapsed < time.Second {
				detail = lipgloss.NewStyle().Foreground(mutedColor).Render("  <1s")
			} else {
				detail = lipgloss.NewStyle().Foreground(mutedColor).Render("  " + elapsed.String())
			}
		case "active":
			icon = lipgloss.NewStyle().Foreground(primaryColor).Render(spinChars[m.spinIdx])
			detail = lipgloss.NewStyle().Foreground(primaryColor).Render("  " + time.Since(m.startTime).Round(time.Second).String())
		case "error":
			icon = lipgloss.NewStyle().Foreground(errorColor).Render("\u2717")
			if p.err != nil {
				detail = lipgloss.NewStyle().Foreground(errorColor).Render("  " + p.err.Error())
			}
		default:
			icon = lipgloss.NewStyle().Foreground(mutedColor).Render("\u25cb")
		}
		b.WriteString(fmt.Sprintf("  %s %-28s%s\n", icon, p.name, detail))
	}

	b.WriteString(fmt.Sprintf("\n  Elapsed: %s", time.Since(m.startTime).Round(time.Second)))

	if m.done && m.result != nil {
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(successColor).Bold(true).
			Render("  \u2713 Agent deployed!"))
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(primaryColor).
			Render(fmt.Sprintf("  URL: %s", m.result.URL)))
	}

	if m.done && m.err != nil {
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(errorColor).Bold(true).
			Render(fmt.Sprintf("  Error: %v", m.err)))
	}

	return b.String()
}

// RunDeployCmd starts the full deploy pipeline as a tea.Cmd.
// It sends TUIDeployPhaseMsg/TUIDeployCompleteMsg/TUIDeployErrorMsg via p.Send().
func RunDeployCmd(p *tea.Program, cfg *config.Config, name, role, serverType, location string, envVars map[string]string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		totalStart := time.Now()

		hc := hetzner.NewClient(cfg.Hetzner.Token)
		cf := cloudflare.NewClient(cfg.Cloudflare.Token, cfg.Cloudflare.ZoneID)

		fqdn := fmt.Sprintf("%s.%s", name, cfg.Domain)
		agentURL := fmt.Sprintf("https://%s", fqdn)

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

		sendPhase := func(phase int, status string, elapsed time.Duration, err error) {
			p.Send(TUIDeployPhaseMsg{
				Phase:   phase,
				Status:  status,
				Elapsed: elapsed,
				Err:     err,
			})
		}

		// Phase 0: Create VM
		sendPhase(0, "active", 0, nil)
		phaseStart := time.Now()

		sshKey, err := hc.GetSSHKey(ctx, cfg.Hetzner.SSHKeyName)
		if err != nil {
			cleanup()
			return TUIDeployErrorMsg{Err: err}
		}

		userData, err := templates.RenderCloudInit(templates.CloudInitData{
			AgentName: name,
			Domain:    cfg.Domain,
			Role:      role,
			EnvVars:   envVars,
		})
		if err != nil {
			cleanup()
			return TUIDeployErrorMsg{Err: err}
		}

		var firewalls []*hcloud.ServerCreateFirewall
		if cfg.Hetzner.FirewallID > 0 {
			firewalls = []*hcloud.ServerCreateFirewall{
				{Firewall: hcloud.Firewall{ID: cfg.Hetzner.FirewallID}},
			}
		}

		result, err := hc.CreateServer(ctx, hcloud.ServerCreateOpts{
			Name:       fmt.Sprintf("specter-%s", name),
			ServerType: &hcloud.ServerType{Name: serverType},
			Image:      &hcloud.Image{ID: cfg.Snapshot.ID},
			Location:   &hcloud.Location{Name: location},
			SSHKeys:    []*hcloud.SSHKey{sshKey},
			UserData:   userData,
			Labels: map[string]string{
				"managed_by": "specter",
				"agent_name": name,
				"role":       role,
			},
			Firewalls: firewalls,
		})
		if err != nil {
			cleanup()
			return TUIDeployErrorMsg{Err: fmt.Errorf("failed to create VM: %w", err)}
		}

		serverID = result.Server.ID
		serverIP = result.Server.PublicNet.IPv4.IP.String()
		sendPhase(0, "done", time.Since(phaseStart), nil)

		// Phase 1: DNS record
		sendPhase(1, "active", 0, nil)
		phaseStart = time.Now()

		dnsRecord, err := cf.CreateDNSRecord(ctx, fqdn, serverIP)
		if err != nil {
			cleanup()
			return TUIDeployErrorMsg{Err: fmt.Errorf("failed to create DNS record: %w", err)}
		}
		dnsRecordID = dnsRecord.ID
		sendPhase(1, "done", time.Since(phaseStart), nil)

		// Phase 2: Wait for VM running
		sendPhase(2, "active", 0, nil)
		phaseStart = time.Now()

		_, err = hc.WaitForRunning(ctx, serverID)
		if err != nil {
			cleanup()
			return TUIDeployErrorMsg{Err: fmt.Errorf("VM failed to start: %w", err)}
		}
		sendPhase(2, "done", time.Since(phaseStart), nil)

		// Phase 3: Wait for SSH
		sendPhase(3, "active", 0, nil)
		phaseStart = time.Now()

		sshClient, err := hetzner.WaitForSSH(ctx, serverIP)
		if err != nil {
			cleanup()
			return TUIDeployErrorMsg{Err: fmt.Errorf("SSH connection failed: %w", err)}
		}
		defer sshClient.Close()
		sendPhase(3, "done", time.Since(phaseStart), nil)

		// Phase 4: Deploy agent code
		sendPhase(4, "active", 0, nil)
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

if ! /usr/local/bin/bun --version > /dev/null 2>&1; then
  curl -fsSL https://bun.sh/install | bash
  cp /root/.bun/bin/bun /usr/local/bin/bun
fi

cat > /etc/systemd/system/specter-agent.service << 'SVCEOF'
%s
SVCEOF
`, agentCode, packageJSON, templates.SystemdUnit)

		if _, err := hetzner.SSHRun(sshClient, deployScript); err != nil {
			cleanup()
			return TUIDeployErrorMsg{Err: fmt.Errorf("failed to deploy agent code: %w", err)}
		}
		sendPhase(4, "done", time.Since(phaseStart), nil)

		// Phase 5: Start services
		sendPhase(5, "active", 0, nil)
		phaseStart = time.Now()

		startScript := `
systemctl daemon-reload
systemctl enable specter-agent
systemctl start specter-agent

for i in $(seq 1 30); do
  if curl -sf http://localhost:3100/health > /dev/null 2>&1; then
    break
  fi
  sleep 1
done

systemctl enable caddy
systemctl restart caddy
`
		if _, err := hetzner.SSHRun(sshClient, startScript); err != nil {
			cleanup()
			return TUIDeployErrorMsg{Err: fmt.Errorf("failed to start services: %w", err)}
		}
		sendPhase(5, "done", time.Since(phaseStart), nil)

		// Phase 6: Wait for HTTPS/TLS
		sendPhase(6, "active", 0, nil)
		phaseStart = time.Now()

		httpClient := &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
			},
		}

		healthURL := fmt.Sprintf("%s/health", agentURL)
		tlsDeadline := time.After(120 * time.Second)
		for {
			select {
			case <-tlsDeadline:
				cleanup()
				return TUIDeployErrorMsg{Err: fmt.Errorf("TLS/health check timed out after 120s")}
			default:
			}

			resp, err := httpClient.Get(healthURL)
			if err == nil && resp.StatusCode == 200 {
				resp.Body.Close()
				break
			}
			if resp != nil {
				resp.Body.Close()
			}
			time.Sleep(2 * time.Second)
		}
		sendPhase(6, "done", time.Since(phaseStart), nil)

		// Phase 7: Health check
		sendPhase(7, "active", 0, nil)
		phaseStart = time.Now()
		sendPhase(7, "done", time.Since(phaseStart), nil)

		// Save state
		state, _ := config.LoadState()
		state.SetAgent(name, &config.Agent{
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

		return TUIDeployCompleteMsg{
			AgentName: name,
			URL:       agentURL,
			IP:        serverIP,
			ServerID:  serverID,
			Elapsed:   totalElapsed,
		}
	}
}
