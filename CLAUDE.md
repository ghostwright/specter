# Specter

Go CLI that deploys AI agent VMs on Hetzner Cloud in 90 seconds. Interactive TUI (Bubbletea v2). Automatic DNS (Cloudflare) and TLS (Let's Encrypt via Caddy). Apache 2.0.

## Build

```bash
make build          # -> bin/specter
make lint           # go vet + gofmt check
make test           # go test ./...
make ci             # lint + build (same as CI workflow)
make install        # go install to $GOPATH/bin
make clean          # rm -rf bin/ dist/
```

## Project Structure

```
cmd/specter/
  main.go                       Entry point, Cobra root
  commands/
    root.go                     Root command, global flags
    deploy.go                   VM creation + DNS + SSH provisioning + TLS
    destroy.go                  VM + DNS teardown
    init.go                     Setup wizard (tokens, firewall, server cache)
    image.go                    Golden snapshot build + list
    list.go                     Agent inventory with health checks
    status.go                   Single agent status
    ssh.go                      SSH into VM
    logs.go                     systemd journal streaming
    update.go                   Agent restart
    version.go                  Version output

internal/
  cloudflare/client.go          Cloudflare DNS API (raw HTTP, not SDK)
  config/
    config.go                   ~/.specter/config.yaml management
    server_types.go             Hetzner server type cache + fuzzy match
    state.go                    Agent state persistence
  hetzner/client.go             Hetzner Cloud API (hcloud-go v2)
  templates/
    cloudinit.go                Cloud-init user-data template  [FROZEN]
    systemd.go                  systemd unit template          [FROZEN]
    caddyfile.go                Caddy reverse proxy template   [FROZEN]
  tui/
    app.go                      Main Bubbletea model (~900 lines)
    agent_list.go               Dashboard list view
    agent_detail.go             Agent detail panel
    deploy_form.go              Deploy form (huh v2)
    deploy_model.go             Deploy data types
    deploy_progress.go          Deploy phase progress
    image_build.go              Image build progress
    setup_wizard.go             First-run setup
    logs_viewport.go            Log viewer
    confirm_dialog.go           Confirmation dialogs
    help_overlay.go             Keyboard help
    status_bar.go               Bottom bar
    dashboard_styles.go         Lipgloss styles
    messages.go                 Bubbletea messages
    theme.go                    Color palette

pkg/version/version.go          Version vars (set via ldflags)
```

## Frozen Files - DO NOT MODIFY

These files are rigorously validated. Modifying them without 3 full deploy-test-destroy cycles will break production deploys.

| File | What Went Wrong Last Time |
|------|--------------------------|
| `internal/templates/systemd.go` | `ReadWritePaths` listed subdirectories instead of `/home/specter/app`. Bun couldn't write lockfiles. 100% deploy failure rate. |
| `internal/templates/cloudinit.go` | Had `systemctl restart caddy` in runcmd. Caddy started before the agent was listening on :3100, returning 502 to all health checks. |
| `internal/templates/caddyfile.go` | Template changes can trigger ACME account creation. Let's Encrypt rate limits: 10 registrations per IP per 3 hours. |
| `cmd/specter/commands/image.go` | Provisioning script must call `sync` before snapshot. Without it, Bun (99MB) was captured as 0 bytes on disk. |

## Key Gotchas

### Deploy Sequence (order matters)

1. Create VM from golden snapshot (~1s)
2. Create DNS A record on Cloudflare (~1s, parallel with boot)
3. Wait for VM boot (68-180s depending on datacenter)
4. Wait for SSH availability (11-24s after VM reports running)
5. Deploy agent code via SSH + start systemd service
6. **Retry loop**: poll `localhost:3100/health` every 1s, up to 30 attempts
7. **Only after agent responds**: enable and start Caddy
8. Caddy provisions TLS via ACME HTTP-01 (~5-8s)
9. Health check `https://agent.domain.com/health` returns 200

Caddy MUST NOT start before the agent is listening on port 3100. This is the single most important invariant in the deploy flow.

### ReadWritePaths

The systemd unit uses `ProtectSystem=strict` and `ProtectHome=read-only`. The agent process can only write to paths listed in `ReadWritePaths`. Currently: `/home/specter/app`. If you add new write locations, they must be in this list or the agent will get permission denied at runtime.

### Sync Before Snapshot

Hetzner snapshots capture disk state at power-off. Unbuffered writes produce 0-byte files. The image build script must call `sync` before `cloud-init clean --logs`. This is how we lost Bun's 99MB binary in early testing.

### Caddy Disabled in Golden Image

Caddy is installed but stopped and disabled in the golden image. A port-80-only placeholder Caddyfile prevents ACME registrations on boot. The deploy script enables and starts Caddy after the agent is verified running. Do not change this order.

### DNS Must Not Be Proxied

Cloudflare DNS records are created with `proxied: false`. If proxied, Caddy's ACME HTTP-01 challenge fails because Cloudflare's proxy intercepts port 80.

### Bubbletea v2 Import Path

Bubbletea v2 is at `charm.land/bubbletea/v2`, NOT `github.com/charmbracelet/bubbletea`. The lipgloss v1 import (`github.com/charmbracelet/lipgloss`) is also still used alongside lipgloss v2 (`charm.land/lipgloss/v2`). Do not try to "upgrade" or remove the v1 import - both are needed.

### Hetzner API Quirks

- `POST /v1/servers` returns 201, not 200
- Server IP is available immediately in the 201 response, even while status is "initializing"
- Snapshot boot time scales with `disk_size`, not `image_size`. Build snapshots on cx23 (40GB disk) for fastest boots.
- `hcloud-go` requires full SSHKey objects (with ID) from `GetByName()`. Cannot pass name-only structs.

### Cloudflare API Quirks

- DNS record creation returns 200, not 201
- Uses raw HTTP client, not cloudflare-go SDK

## Testing

No mocks. All testing is against real Hetzner and Cloudflare infrastructure.

```bash
# Full cycle
source .env.local
specter init
specter deploy test --role swe --yes
specter status test --json
curl https://test.yourdomain.com/health
specter destroy test --yes

# JSON mode (every command)
specter list --json
specter version --json
```

A test deploy costs ~$0.01 (Hetzner hourly billing). Always destroy test VMs when done.

## What NOT to Do

- Do not upgrade lipgloss to v2-only. Both v1 and v2 imports are intentional.
- Do not modify frozen files without running 3 full deploy-test-destroy cycles.
- Do not add `systemctl restart caddy` to cloud-init runcmd. The deploy script controls Caddy startup timing.
- Do not set `proxied: true` on Cloudflare DNS records.
- Do not build golden images on servers larger than cx23. Bigger disk = slower snapshot restore.
- Do not remove the `sync` call from the image build provisioning script.
- Do not assume SSH is available when Hetzner reports the VM as "running". There is a 11-24s gap.

## Commands Reference

Every command supports `--json` for structured output and `--yes` / `-y` for non-interactive use.

| Command | Purpose |
|---------|---------|
| `specter init` | Setup wizard: validates tokens, creates firewall, caches server types |
| `specter deploy <name>` | Full deploy: VM + DNS + TLS + health check |
| `specter list` | All agents with live health status |
| `specter status <name>` | Detailed agent info |
| `specter ssh <name>` | SSH as specter user (--root for admin) |
| `specter logs <name>` | systemd journal (-f for follow, -n for lines) |
| `specter update <name>` | Restart agent, refresh dependencies |
| `specter destroy <name>` | Delete VM + DNS record |
| `specter image build` | Create golden snapshot |
| `specter image list` | Show available snapshots |
| `specter version` | Print version info |
