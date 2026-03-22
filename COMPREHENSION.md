# Specter CLI - Comprehension Summary

*Updated after Phase 3. All timings are from 16/16 validated deploys.*

## The Measured Deploy Flow

| Phase | Action | Measured Time |
|-------|--------|---------------|
| 1 | Create VM from snapshot (API call) | ~1s |
| 2 | Create DNS A record on Cloudflare (parallel with boot) | ~1s |
| 3 | Wait for VM status=running | 68-180s (varies by location) |
| 4 | Wait for SSH availability | ~11-24s after running |
| 5 | Deploy agent code (write files + systemd unit + verify bun) | ~1-2s |
| 6 | Start agent + verify on localhost:3100 (up to 30s retry) | ~2-4s |
| 7 | Enable Caddy + TLS certificate (ACME HTTP-01) | ~5-8s |
| 8 | Health check (HTTPS 200) | <1s |
| **Total** | | **~90-220s** |

VM boot time varies by location: fsn1 ~70s, nbg1 ~80s, hel1 ~120s. Boot time dominates total deploy time.

DNS propagation (6-7s to Cloudflare resolvers) happens entirely within the VM boot window. There is no DNS-to-TLS timing gap.

## Key Gotchas Affecting Implementation

### Original (Experiments)
- **G-01**: Hetzner POST /v1/servers returns 201, not 200.
- **G-02**: Server IP is in the 201 response immediately, even while status=initializing. Use it to create DNS record in parallel with boot.
- **G-03**: VMs have NO firewall by default. Must use Hetzner Cloud Firewalls (created during `specter init`).
- **G-06/G-09**: Snapshot boot time scales with disk_size, not image_size. Build on cx23 (40GB disk) to minimize boot time.
- **G-07**: Must run `cloud-init clean --logs` before snapshotting, or cloud-init won't re-run user_data on next boot.
- **G-15**: Cloudflare DNS API returns 200 for record creation, not 201.
- **G-16**: hcloud-go requires full SSHKey object (with ID) from GetByName(). Cannot pass name-only struct.
- **G-17**: Bubbletea v2 is at charm.land/bubbletea/v2, not github.com/charmbracelet/bubbletea.

### Phase 3 Discoveries
- **G-19**: `ProtectHome=read-only` blocks writes to `/home/specter/app` unless the directory itself is in ReadWritePaths. Listing only subdirectories is insufficient. Bun needs to write lockfiles and cache to the working directory. Fix: `ReadWritePaths=/home/specter/app`
- **G-20**: Deploy must verify agent responds on localhost:3100 BEFORE starting Caddy. A 30-retry loop (1s intervals, 30s max) prevents Caddy from returning 502 to health checks.
- **G-21**: Cloud-init runcmd must NOT start or restart Caddy. The deploy script controls Caddy startup timing to prevent race conditions.
- **G-22**: Copy Bun binary to /usr/local/bin/bun instead of symlinking. Symlinks can point to nothing after snapshot restore. Supersedes G-10.
- **G-23**: Run `sync` before `cloud-init clean --logs` in the provisioning script. Without sync, large binaries (Bun is 99MB) can be snapshotted as 0-byte files. This was the primary blocker in Phase 3.
- **G-24**: Verify Bun works during deploy (`bun --version`). Reinstall on the fly if broken. Defense in depth against bad snapshots.
- **G-25**: Hetzner snapshots capture disk state at power-off. Unbuffered writes produce 0-byte files. Always sync before snapshot.
- **G-26**: Let's Encrypt rate limits ACME account creation to 10 per IP per 3 hours. Disable Caddy in golden image to prevent spurious registrations on boot.
- **G-27**: Caddy must be disabled in the golden image with a port-80-only placeholder Caddyfile. If enabled, every boot triggers an ACME registration attempt.

## Golden Snapshot Strategy

- Build on cx23 (2 vCPU, 4GB, 40GB disk) - the smallest x86 shared server
- Deploy on cx33 (4 vCPU, 8GB, 80GB disk) - default target
- Why cx23 not cx33: snapshot disk_size is locked to source VM's disk. 40GB copies faster than 80GB.
- Current golden snapshot: v0.1.6 (configured in ~/.specter/config.yaml)
- Contents:
  - Ubuntu 24.04
  - Docker (latest)
  - Bun **copied** (not symlinked) to `/usr/local/bin/bun`
  - Caddy installed but **stopped and disabled** (not auto-starting on boot)
  - Placeholder Caddyfile on port 80 only (no HTTPS/ACME)
  - `sync` called before `cloud-init clean` to flush all disk writes
  - ufw configured (22, 80, 443)
  - fail2ban enabled
  - `specter` user with Docker/sudo groups
  - NOPASSWD sudo for `/usr/bin/journalctl` (for `specter logs`)
  - Directory structure: `/home/specter/app/{data,logs,backups,.sessions}`

## Cloud-init Config Injection

Cloud-init user_data injects per-agent config at boot:
- **write_files**: .env (secrets, agent name, role, custom env vars) and Caddyfile (reverse proxy config)
- **runcmd**: Cleans up secrets only (deletes user-data.txt). Does NOT start or restart Caddy.
- Validated working on snapshot-based VMs (Experiment 02)
- Config is ready before SSH is available - no separate SCP step needed
- Agent code is deployed via SSH (too large for cloud-init's 32KB limit)
- `proxied:false` is MANDATORY on all DNS A records or Caddy TLS breaks

## Deploy Service Startup Sequence (Phase 5-7)

This is the critical sequence that was broken in Phase 1-2 and fixed in Phase 3:

1. Deploy script writes agent code + systemd unit via SSH
2. `systemctl daemon-reload && systemctl enable specter-agent && systemctl start specter-agent`
3. **Retry loop**: polls `curl localhost:3100/health` every 1s, up to 30 attempts
4. Only after agent is confirmed running: `systemctl enable caddy && systemctl restart caddy`
5. Caddy reads the Caddyfile (written by cloud-init), requests Let's Encrypt cert via HTTP-01
6. TLS cert issued in ~4-8s
7. CLI polls `https://agent.specter.tools/health` for HTTPS 200

The key insight: Caddy must NOT start before the agent is listening on 3100. Otherwise Caddy returns 502 and the HTTPS health check never gets 200.

## Architecture

- Two repos: `ghostwright/specter` (Go CLI, control plane) and `ghostwright/specter-agent` (TypeScript/Bun, data plane, not yet built)
- CLI communicates with agents via: SSH (deploy/manage), HTTPS /health (monitoring), systemd service contract
- DNS: Cloudflare API (raw HTTP, not cloudflare-go). Domain locked to Cloudflare nameservers.
- VMs: Hetzner Cloud, default nbg1 (Nuremberg). Also tested in fsn1 (Falkenstein) and hel1 (Helsinki). x86 shared servers.
- Security: Hetzner Cloud Firewalls (created during init, attached at server creation). TCP 22, 80, 443 only.
- systemd hardening: NoNewPrivileges, ProtectSystem=strict, ProtectHome=read-only, ReadWritePaths=/home/specter/app, PrivateTmp, MemoryMax=2G, TasksMax=256
