package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/ghostwright/specter/internal/config"
	"github.com/ghostwright/specter/internal/hetzner"
	"github.com/ghostwright/specter/internal/tui"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
)

var imageCmd = &cobra.Command{
	Use:   "image",
	Short: "Manage golden snapshots",
}

var imageBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Create a new golden snapshot",
	Long:  "Create a temporary VM, install the toolchain, snapshot it, and clean up.",
	RunE:  runImageBuild,
}

var imageListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show available snapshots",
	RunE:  runImageList,
}

func init() {
	imageCmd.AddCommand(imageBuildCmd)
	imageCmd.AddCommand(imageListCmd)
}

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
ln -sf /root/.bun/bin/bun /usr/local/bin/bun

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

echo "=== Resetting cloud-init ==="
cloud-init clean --logs

echo "=== Done ==="
`

func runImageBuild(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	hc := hetzner.NewClient(cfg.Hetzner.Token)
	totalStart := time.Now()

	// Look up SSH key
	sshKey, err := hc.GetSSHKey(ctx, cfg.Hetzner.SSHKeyName)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("  %s\n\n", tui.TitleStyle.Render(tui.Brand+" IMAGE BUILD"))
	fmt.Println("  Building golden snapshot on cx23 (smallest x86 for minimal disk_size)...")
	fmt.Println()

	// Create temporary VM
	fmt.Printf("  Creating temporary VM... ")
	buildStart := time.Now()
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
		return fmt.Errorf("failed to create build VM: %w", err)
	}
	server := result.Server
	ip := server.PublicNet.IPv4.IP.String()
	fmt.Printf("%s (ID: %d, IP: %s, %s)\n", tui.SuccessStyle.Render("done"),
		server.ID, ip, time.Since(buildStart).Round(time.Second))

	// Cleanup on failure
	cleanupServer := func() {
		fmt.Printf("  Cleaning up build VM... ")
		cleanCtx := context.Background()
		if err := hc.DeleteServer(cleanCtx, server); err == nil {
			fmt.Println(tui.SuccessStyle.Render("done"))
		} else {
			fmt.Printf("%s: %v\n", tui.ErrorStyle.Render("failed"), err)
		}
	}

	// Wait for running
	fmt.Printf("  Waiting for VM... ")
	waitStart := time.Now()
	_, err = hc.WaitForRunning(ctx, server.ID)
	if err != nil {
		cleanupServer()
		return fmt.Errorf("VM failed to start: %w", err)
	}
	fmt.Printf("%s (%s)\n", tui.SuccessStyle.Render("running"), time.Since(waitStart).Round(time.Second))

	// Wait for SSH
	fmt.Printf("  Waiting for SSH... ")
	sshStart := time.Now()
	sshClient, err := hetzner.WaitForSSH(ctx, ip)
	if err != nil {
		cleanupServer()
		return fmt.Errorf("SSH connection failed: %w", err)
	}
	fmt.Printf("%s (%s)\n", tui.SuccessStyle.Render("connected"), time.Since(sshStart).Round(time.Second))

	// Run provisioning
	fmt.Printf("  Running provisioning script... ")
	provStart := time.Now()
	output, err := hetzner.SSHRun(sshClient, provisionScript)
	sshClient.Close()
	if err != nil {
		fmt.Printf("%s\n", tui.ErrorStyle.Render("failed"))
		fmt.Printf("  Output: %s\n", output)
		cleanupServer()
		return fmt.Errorf("provisioning failed: %w", err)
	}
	fmt.Printf("%s (%s)\n", tui.SuccessStyle.Render("done"), time.Since(provStart).Round(time.Second))

	// Power off server before snapshot
	fmt.Printf("  Powering off VM... ")
	powerStart := time.Now()
	if err := hc.PowerOffServer(ctx, server); err != nil {
		cleanupServer()
		return fmt.Errorf("power off failed: %w", err)
	}
	fmt.Printf("%s (%s)\n", tui.SuccessStyle.Render("done"), time.Since(powerStart).Round(time.Second))

	// Create snapshot
	version := "v0.1.0"
	description := fmt.Sprintf("specter-base-cx23-%s", version)
	fmt.Printf("  Creating snapshot (%s)... ", description)
	snapStart := time.Now()
	snap, err := hc.CreateSnapshot(ctx, server, description, map[string]string{
		"managed_by": "specter",
		"version":    version,
	})
	if err != nil {
		cleanupServer()
		return fmt.Errorf("snapshot failed: %w", err)
	}
	fmt.Printf("%s (ID: %d, %s)\n", tui.SuccessStyle.Render("done"),
		snap.ID, time.Since(snapStart).Round(time.Second))

	// Delete build VM
	cleanupServer()

	// Update config
	cfg.Snapshot.ID = snap.ID
	cfg.Snapshot.Version = version
	cfg.Snapshot.DiskSize = snap.DiskSize
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("could not save config: %w", err)
	}

	totalElapsed := time.Since(totalStart).Round(time.Second)

	if jsonOutput {
		data, _ := json.MarshalIndent(map[string]interface{}{
			"snapshot_id": snap.ID,
			"version":     version,
			"disk_size":   snap.DiskSize,
			"elapsed":     totalElapsed.String(),
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("\n  %s Golden snapshot created\n", tui.SuccessStyle.Render("done"))
	fmt.Printf("  Snapshot ID: %d\n", snap.ID)
	fmt.Printf("  Version:     %s\n", version)
	fmt.Printf("  Disk size:   %.0f GB\n", snap.DiskSize)
	fmt.Printf("  Total time:  %s\n\n", totalElapsed)

	return nil
}

func runImageList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	hc := hetzner.NewClient(cfg.Hetzner.Token)
	images, err := hc.API.Image.AllWithOpts(ctx, hcloud.ImageListOpts{
		Type: []hcloud.ImageType{hcloud.ImageTypeSnapshot},
		ListOpts: hcloud.ListOpts{
			LabelSelector: "managed_by=specter",
		},
	})
	if err != nil {
		return fmt.Errorf("error listing snapshots: %w", err)
	}

	if jsonOutput {
		type imgInfo struct {
			ID          int64  `json:"id"`
			Description string `json:"description"`
			Version     string `json:"version"`
			DiskSize    float32 `json:"disk_size"`
			Created     string `json:"created"`
		}
		var infos []imgInfo
		for _, img := range images {
			infos = append(infos, imgInfo{
				ID:          img.ID,
				Description: img.Description,
				Version:     img.Labels["version"],
				DiskSize:    img.DiskSize,
				Created:     img.Created.Format(time.RFC3339),
			})
		}
		data, _ := json.MarshalIndent(infos, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Println()
	fmt.Printf("  %s\n\n", tui.TitleStyle.Render(tui.Brand+" SNAPSHOTS"))

	if len(images) == 0 {
		fmt.Println("  No snapshots found. Run `specter image build` to create one.")
		fmt.Println()
		return nil
	}

	fmt.Printf("  %-12s %-35s %-10s %-10s %s\n",
		tui.MutedStyle.Render("ID"),
		tui.MutedStyle.Render("DESCRIPTION"),
		tui.MutedStyle.Render("VERSION"),
		tui.MutedStyle.Render("DISK"),
		tui.MutedStyle.Render("CREATED"))

	activeID := cfg.Snapshot.ID
	for _, img := range images {
		marker := " "
		if img.ID == activeID {
			marker = tui.SuccessStyle.Render("*")
		}
		fmt.Printf(" %s%-12d %-35s %-10s %-10s %s\n",
			marker,
			img.ID,
			img.Description,
			img.Labels["version"],
			fmt.Sprintf("%.0fGB", img.DiskSize),
			img.Created.Format("2006-01-02"))
	}

	fmt.Println()
	return nil
}
