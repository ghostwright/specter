# Specter CLI - Comprehension Summary

## The Measured Deploy Flow

Based on Experiment 05 (full deploy) and Experiment 09 (snapshot sizing), the optimized deploy flow with cx23 snapshot on cx33 target:

| Phase | Action | Measured Time |
|-------|--------|---------------|
| 1 | Create VM from snapshot (API call) | ~1s |
| 2 | Create DNS A record on Cloudflare (parallel with boot) | ~1s |
| 3 | Wait for VM status=running | ~68s (cx23 snap on cx33) |
| 4 | Wait for SSH availability | ~12-16s after running |
| 5 | Deploy agent code (git clone + bun install) | ~15-30s |
| 6 | Start systemd services + restart Caddy | ~3s |
| 7 | TLS certificate issuance (Caddy automatic) | ~4s |
| 8 | Health check (HTTPS 200) | ~1s |
| **Total** | | **~100-120s** |

The VM boot from snapshot is 77% of total time. DNS propagation (6-7s to Cloudflare resolvers) happens entirely within the boot wait window.

## Key Gotchas Affecting Implementation

- **G-02**: Server IP is in the 201 response immediately, even while status=initializing. Use it to create DNS record in parallel with boot.
- **G-06/G-09**: Snapshot boot time scales with disk_size, not image_size. Build on cx23 (40GB), deploy on cx33 (80GB disk, faster I/O). The cx23 snapshot boots in 68s on cx33 vs 103-113s for cx33 snapshot.
- **G-07**: Must run `cloud-init clean --logs` before snapshotting, or cloud-init won't re-run user_data on next boot.
- **G-10**: Bun installs per-user in ~/.bun/. Must symlink to /usr/local/bin/bun for system-wide access.
- **G-15**: Cloudflare DNS API returns 200 for record creation, not 201.
- **G-16**: hcloud-go requires full SSHKey object (with ID) from GetByName() - cannot pass name-only struct.
- **G-17**: Bubbletea v2 is at charm.land/bubbletea/v2, not github.com/charmbracelet/bubbletea.
- **G-03**: VMs have NO firewall by default. Must use Hetzner Cloud Firewalls (created during `specter init`).
- **G-01**: Hetzner POST /v1/servers returns 201, not 200.

## Golden Snapshot Strategy

- Build on cx23 (2 vCPU, 4GB, 40GB disk, $3.49/mo) - the smallest x86 shared server
- Deploy on cx33 (4 vCPU, 8GB, 80GB disk, $5.99/mo) - default target
- Why cx23 not cx33: snapshot disk_size is locked to source VM's disk. 40GB copies faster than 80GB. The cx23 snap on cx33 target boots in 68s vs 103-113s for cx33 snap on cx33.
- Current golden snapshot ID: 369155740 (specter-base-cx23-v0.1.0, 1.18 GB image)
- Contents: Ubuntu 24.04, Docker 29.3.0, Bun 1.3.11, Caddy 2.11.2, ufw, fail2ban, specter user

## Cloud-init Config Injection

Cloud-init user_data is used for per-agent config at boot time:
- write_files: inject .env (secrets) and Caddyfile (reverse proxy config)
- runcmd: restart Caddy after Caddyfile is written
- Validated working on snapshot-based VMs (Experiment 02)
- Config is ready before SSH is available - no separate SCP step needed for config
- Agent code is deployed via SSH/git clone (too large for cloud-init's 32KB limit)
- proxied:false is MANDATORY on all DNS A records or Caddy TLS breaks

## Architecture

- Two repos: ghostwright/specter (Go CLI, control plane) and ghostwright/specter-agent (TypeScript/Bun, data plane)
- CLI communicates with agents via: SSH (deploy/manage), HTTPS /health (monitoring), systemd service contract
- DNS: Cloudflare API (raw HTTP, not cloudflare-go library). Domain locked to Cloudflare nameservers.
- VMs: Hetzner Cloud, nbg1 (Nuremberg), x86 shared servers
- Security: Hetzner Cloud Firewalls (created during init, attached at server creation). TCP 22, 80, 443.

## Pushback

None. The decisions are well-backed by measured data. The only concern is ARM support being excluded, but that's correctly deferred since the golden snapshot is x86.
