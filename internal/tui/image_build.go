package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/ghostwright/specter/internal/config"
	"github.com/ghostwright/specter/internal/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// provisionScript is the golden image provisioning script.
// Identical to the one in cmd/specter/commands/image.go.
const provisionScript = `#!/bin/bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

echo "=== Updating packages ==="
apt-get update -qq
apt-get upgrade -y -qq

echo "=== Installing essentials ==="
apt-get install -y -qq jq sqlite3 fail2ban ufw logrotate unzip curl git

echo "=== Installing Docker ==="
curl -fsSL https://get.docker.com | sh

echo "=== Installing Bun ==="
curl -fsSL https://bun.sh/install | bash
cp /root/.bun/bin/bun /usr/local/bin/bun
chmod +x /usr/local/bin/bun
/usr/local/bin/bun --version

echo "=== Installing Caddy ==="
apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
apt-get update -qq
apt-get install -y -qq caddy

echo "=== Configuring firewall ==="
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable

echo "=== Creating specter user ==="
useradd -m -s /bin/bash -G docker,sudo specter
mkdir -p /home/specter/.ssh
cp /root/.ssh/authorized_keys /home/specter/.ssh/authorized_keys
chown -R specter:specter /home/specter/.ssh
chmod 700 /home/specter/.ssh
chmod 600 /home/specter/.ssh/authorized_keys

echo "=== Configuring sudoers for specter user ==="
echo "specter ALL=(ALL) NOPASSWD: /usr/bin/journalctl" > /etc/sudoers.d/specter
chmod 440 /etc/sudoers.d/specter

echo "=== Setting up Bun for specter user ==="
cp -r /root/.bun /home/specter/.bun
chown -R specter:specter /home/specter/.bun

echo "=== Creating directory structure ==="
mkdir -p /home/specter/app/{data,logs,backups,.sessions}
chown -R specter:specter /home/specter/app

echo "=== Adding 2GB swap ==="
fallocate -l 2G /swapfile
chmod 600 /swapfile
mkswap /swapfile
swapon /swapfile
echo '/swapfile swap swap defaults 0 0' >> /etc/fstab

echo "=== Configuring Caddy for snapshot ==="
systemctl stop caddy
systemctl disable caddy
# Placeholder Caddyfile that won't trigger TLS on boot
cat > /etc/caddy/Caddyfile << 'CADDYEOF'
:80 {
    respond "specter: not configured" 503
}
CADDYEOF

echo "=== Verifying bun binary ==="
ls -la /usr/local/bin/bun
/usr/local/bin/bun --version

echo "=== Flushing writes to disk ==="
sync

echo "=== Resetting cloud-init ==="
cloud-init clean --logs

echo "=== Done ==="
`

type imageBuildPhaseInfo struct {
	name    string
	status  string // "", "active", "done", "error"
	detail  string
	elapsed time.Duration
}

// ImageBuildModel tracks an image build operation in the dashboard.
type ImageBuildModel struct {
	phases    []imageBuildPhaseInfo
	startTime time.Time
	spinIdx   int
	done      bool
	err       error
	result    *ImageBuildCompleteMsg
	width     int
	height    int
}

// NewImageBuildModel creates the image build progress display.
func NewImageBuildModel() ImageBuildModel {
	return ImageBuildModel{
		startTime: time.Now(),
		phases: []imageBuildPhaseInfo{
			{name: "Creating build VM (cx23)"},
			{name: "Waiting for VM boot"},
			{name: "Waiting for SSH"},
			{name: "Running provisioning script"},
			{name: "Powering off VM"},
			{name: "Creating snapshot"},
			{name: "Cleaning up build VM"},
		},
	}
}

func (m *ImageBuildModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *ImageBuildModel) HandleMsg(msg tea.Msg) {
	switch msg := msg.(type) {
	case ImageBuildPhaseMsg:
		if msg.Phase >= 0 && msg.Phase < len(m.phases) {
			m.phases[msg.Phase].status = msg.Status
			m.phases[msg.Phase].elapsed = msg.Elapsed
			if msg.Sub != "" {
				m.phases[msg.Phase].detail = msg.Sub
			}
			if msg.Err != nil {
				m.phases[msg.Phase].detail = msg.Err.Error()
			}
		}
	case ImageBuildCompleteMsg:
		m.done = true
		m.result = &msg
	case ImageBuildErrorMsg:
		m.done = true
		m.err = msg.Err
	case spinTickMsg:
		m.spinIdx = (m.spinIdx + 1) % len(spinChars)
	}
}

func (m ImageBuildModel) View() string {
	var b strings.Builder

	header := lipgloss.NewStyle().Bold(true).Foreground(primaryColor).
		Render("\u2b21 BUILDING GOLDEN IMAGE")
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(mutedColor).
		Render("  cx23 in nbg1 (smallest x86 for minimal disk_size)"))
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
			if p.detail != "" {
				detail = lipgloss.NewStyle().Foreground(accentColor).Render("  " + p.detail)
			}
		case "error":
			icon = lipgloss.NewStyle().Foreground(errorColor).Render("\u2717")
			if p.detail != "" {
				detail = lipgloss.NewStyle().Foreground(errorColor).Render("  " + p.detail)
			}
		default:
			icon = lipgloss.NewStyle().Foreground(mutedColor).Render("\u25cb")
		}
		b.WriteString(fmt.Sprintf("  %s %-32s%s\n", icon, p.name, detail))
	}

	b.WriteString(fmt.Sprintf("\n  Elapsed: %s", time.Since(m.startTime).Round(time.Second)))

	if m.done && m.result != nil {
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(successColor).Bold(true).
			Render("  \u2713 Golden image built!"))
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(primaryColor).
			Render(fmt.Sprintf("  Snapshot ID: %d  Version: %s  Disk: %.0f GB",
				m.result.SnapshotID, m.result.Version, m.result.DiskSize)))
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(mutedColor).
			Render("  Press esc to return to dashboard."))
	}

	if m.done && m.err != nil {
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(errorColor).Bold(true).
			Render(fmt.Sprintf("  Error: %v", m.err)))
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(mutedColor).
			Render("  Press esc to return to dashboard."))
	}

	return b.String()
}

// RunImageBuildCmd starts the full image build pipeline as a tea.Cmd.
// It sends ImageBuildPhaseMsg/ImageBuildCompleteMsg/ImageBuildErrorMsg via p.Send().
func RunImageBuildCmd(p *tea.Program, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		hc := hetzner.NewClient(cfg.Hetzner.Token)
		totalStart := time.Now()

		sendPhase := func(phase int, status string, elapsed time.Duration, sub string, err error) {
			p.Send(ImageBuildPhaseMsg{
				Phase:   phase,
				Status:  status,
				Elapsed: elapsed,
				Sub:     sub,
				Err:     err,
			})
		}

		// Look up SSH key
		sshKey, err := hc.GetSSHKey(ctx, cfg.Hetzner.SSHKeyName)
		if err != nil {
			return ImageBuildErrorMsg{Err: err}
		}

		// Phase 0: Create temporary VM
		sendPhase(0, "active", 0, "", nil)
		phaseStart := time.Now()

		result, err := hc.CreateServer(ctx, hcloud.ServerCreateOpts{
			Name:       "specter-image-build",
			ServerType: &hcloud.ServerType{Name: "cx23"},
			Image:      &hcloud.Image{Name: "ubuntu-24.04"},
			Location:   &hcloud.Location{Name: cfg.Hetzner.DefaultLocation},
			SSHKeys:    []*hcloud.SSHKey{sshKey},
			Labels: map[string]string{
				"managed_by": "specter",
				"purpose":    "image-build",
			},
		})
		if err != nil {
			return ImageBuildErrorMsg{Err: fmt.Errorf("failed to create build VM: %w", err)}
		}

		server := result.Server
		ip := server.PublicNet.IPv4.IP.String()
		sendPhase(0, "done", time.Since(phaseStart), "", nil)

		// Cleanup on failure
		cleanupServer := func() {
			cleanCtx := context.Background()
			hc.DeleteServer(cleanCtx, server)
		}

		// Phase 1: Wait for running
		sendPhase(1, "active", 0, "", nil)
		phaseStart = time.Now()
		_, err = hc.WaitForRunning(ctx, server.ID)
		if err != nil {
			cleanupServer()
			return ImageBuildErrorMsg{Err: fmt.Errorf("VM failed to start: %w", err)}
		}
		sendPhase(1, "done", time.Since(phaseStart), "", nil)

		// Phase 2: Wait for SSH
		sendPhase(2, "active", 0, "", nil)
		phaseStart = time.Now()
		sshClient, err := hetzner.WaitForSSH(ctx, ip)
		if err != nil {
			cleanupServer()
			return ImageBuildErrorMsg{Err: fmt.Errorf("SSH connection failed: %w", err)}
		}
		sendPhase(2, "done", time.Since(phaseStart), "", nil)

		// Phase 3: Run provisioning script
		sendPhase(3, "active", 0, "starting...", nil)
		phaseStart = time.Now()

		// Run provisioning with streaming output to capture === lines
		output, err := hetzner.SSHRun(sshClient, provisionScript)
		sshClient.Close()

		// Extract the last === line from output for sub-status
		if output != "" {
			lines := strings.Split(output, "\n")
			for i := len(lines) - 1; i >= 0; i-- {
				if strings.HasPrefix(strings.TrimSpace(lines[i]), "===") {
					sub := strings.Trim(strings.TrimSpace(lines[i]), "= ")
					sendPhase(3, "active", time.Since(phaseStart), sub, nil)
					break
				}
			}
		}

		if err != nil {
			sendPhase(3, "error", time.Since(phaseStart), "", err)
			cleanupServer()
			return ImageBuildErrorMsg{Err: fmt.Errorf("provisioning failed: %w", err)}
		}
		sendPhase(3, "done", time.Since(phaseStart), "", nil)

		// Phase 4: Power off
		sendPhase(4, "active", 0, "", nil)
		phaseStart = time.Now()
		if err := hc.PowerOffServer(ctx, server); err != nil {
			cleanupServer()
			return ImageBuildErrorMsg{Err: fmt.Errorf("power off failed: %w", err)}
		}
		sendPhase(4, "done", time.Since(phaseStart), "", nil)

		// Determine version
		existingSnap, _ := hc.FindSpecterSnapshot(ctx)
		currentVersion := "v0.0.0"
		if existingSnap != nil {
			if v, ok := existingSnap.Labels["version"]; ok {
				currentVersion = v
			}
		}
		version := config.BumpVersion(currentVersion)

		// Phase 5: Create snapshot
		description := fmt.Sprintf("specter-base-cx23-%s", version)
		sendPhase(5, "active", 0, description, nil)
		phaseStart = time.Now()
		snap, err := hc.CreateSnapshot(ctx, server, description, map[string]string{
			"managed_by": "specter",
			"version":    version,
		})
		if err != nil {
			cleanupServer()
			return ImageBuildErrorMsg{Err: fmt.Errorf("snapshot failed: %w", err)}
		}
		sendPhase(5, "done", time.Since(phaseStart), "", nil)

		// Phase 6: Delete build VM
		sendPhase(6, "active", 0, "", nil)
		phaseStart = time.Now()
		if delErr := hc.DeleteServer(context.Background(), server); delErr != nil {
			sendPhase(6, "error", time.Since(phaseStart), "", delErr)
		} else {
			sendPhase(6, "done", time.Since(phaseStart), "", nil)
		}

		// Update config
		cfg.Snapshot.ID = snap.ID
		cfg.Snapshot.Version = version
		cfg.Snapshot.DiskSize = snap.DiskSize
		if err := cfg.Save(); err != nil {
			return ImageBuildErrorMsg{Err: fmt.Errorf("could not save config: %w", err)}
		}

		totalElapsed := time.Since(totalStart).Round(time.Second)

		return ImageBuildCompleteMsg{
			SnapshotID: snap.ID,
			Version:    version,
			DiskSize:   snap.DiskSize,
			Elapsed:    totalElapsed,
		}
	}
}
