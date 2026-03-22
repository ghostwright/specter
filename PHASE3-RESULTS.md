# Phase 3: Deploy Reliability Fix - Results

## Date
2026-03-22

## Root Causes Found and Fixed

### Root Cause 1: Bun binary 0 bytes after snapshot (G-25)

The golden image provisioning script ran `cp /root/.bun/bin/bun /usr/local/bin/bun` (or `ln -sf` in earlier versions), but the write was not flushed to disk before the VM was powered off for snapshotting. The Hetzner snapshot captured the file metadata (inode, permissions, size=0) but not the actual data blocks. On boot from the snapshot, `/usr/local/bin/bun` existed but was 0 bytes, causing `Exec format error` (exit code 203/EXEC) on every systemd start attempt.

**Fix**: Added `sync` before `cloud-init clean --logs` in the image build provisioning script, and added a verification step (`/usr/local/bin/bun --version`) to fail fast if the binary is broken. Also changed `ln -sf` to `cp` so the binary is a real file, not a symlink.

**Defense in depth**: The deploy script now checks `if ! /usr/local/bin/bun --version` and installs bun fresh if the binary is broken. This makes deploys resilient even against a bad golden image.

### Root Cause 2: ReadWritePaths too restrictive (original diagnosis, confirmed)

`ReadWritePaths=/home/specter/app/data /home/specter/app/logs /home/specter/app/.sessions` only allowed writes to three subdirectories. Bun needs to write to the app directory itself (lockfiles, cache). Changed to `ReadWritePaths=/home/specter/app`.

### Root Cause 3: Startup verification was fire-and-forget (original diagnosis, confirmed)

The deploy script started the agent and Caddy without verifying the agent was listening. Replaced with a retry loop that polls `localhost:3100/health` for up to 30 seconds before starting Caddy.

### Root Cause 4: Cloud-init restarted Caddy prematurely

The cloud-init template included `systemctl restart caddy` in its `runcmd`, which could start Caddy before the deploy script reached Phase 5. Removed this because Phase 5 handles Caddy startup.

### Root Cause 5: Caddy not disabled in golden image

The golden image left Caddy enabled, so it started on boot with a stale/default Caddyfile. Each boot from the snapshot triggered a new ACME account registration, hitting Let's Encrypt's rate limit (10 registrations per IP per 3 hours). Fixed by stopping and disabling Caddy in the image, and writing a port-80-only placeholder Caddyfile.

## Files Changed

### `internal/templates/systemd.go` (line 26)
- `ReadWritePaths=/home/specter/app/data /home/specter/app/logs /home/specter/app/.sessions`
- Changed to: `ReadWritePaths=/home/specter/app`

### `cmd/specter/commands/deploy.go` (Phase 4, lines 346-350 and 644-648)
- Replaced bun existence check (`[ ! -f /usr/local/bin/bun ]`) with functional check (`! /usr/local/bin/bun --version`)
- If bun is broken, installs fresh via `curl -fsSL https://bun.sh/install | bash` and copies binary

### `cmd/specter/commands/deploy.go` (Phase 5, lines 375-390 and 665-680)
- Replaced fire-and-forget start with retry loop (up to 30s polling localhost:3100/health)
- Added `systemctl enable caddy` before restart (Caddy is disabled in new image)

### `cmd/specter/commands/image.go` (provisioning script)
- Changed `ln -sf` to `cp` for bun binary
- Added `bun --version` verification after copy
- Added Caddy stop/disable before snapshot
- Added port-80-only placeholder Caddyfile
- Added `sync` before cloud-init clean
- Added final bun verification step

### `internal/templates/cloudinit.go` (line 33)
- Removed `systemctl restart caddy` from cloud-init runcmd

## Test Results

16/16 deploys successful. Zero failures.

| Test | Location | Server | Start (s) | TLS (s) | Total (s) | Status |
|------|----------|--------|-----------|---------|-----------|--------|
| 1    | fsn1     | cx33   | 2.6       | 5.5     | 94        | OK     |
| 2    | fsn1     | cx33   | 2.2       | 5.5     | 89        | OK     |
| 3    | fsn1     | cx33   | 2.5       | 78.6*   | 175       | OK     |
| 4    | fsn1     | cx33   | 2.6       | 5.4     | 92        | OK     |
| 5    | fsn1     | cx33   | 2.4       | 74.3*   | 172       | OK     |
| 6    | hel1     | cx33   | 2.6       | 7.7     | 180       | OK     |
| 7    | hel1     | cx33   | 2.5       | 5.3     | 176       | OK     |
| 8    | hel1     | cx33   | 3.1       | 5.4     | 148       | OK     |
| 9    | hel1     | cx33   | 2.5       | 7.5     | 118       | OK     |
| 10   | hel1     | cx33   | 3.0       | 7.6     | 153       | OK     |
| 11   | hel1     | cx33   | 2.7       | 5.2     | 130       | OK     |
| 12   | hel1     | cx33   | 2.2       | 7.7     | 92        | OK     |
| 13   | hel1     | cx23   | 3.6       | 7.7     | 220       | OK     |
| 14   | hel1     | cx33   | 2.2       | 5.2     | 96        | OK     |
| 15   | hel1     | cx33   | 2.5       | 5.5     | 118       | OK (sim) |
| 16   | hel1     | cx33   | 2.2       | 5.4     | 90        | OK (sim) |

*Tests 3 and 5 hit ACME rate limits from previous agent's testing (10 registrations per IP per 3h). Not a deploy bug.

### Timing breakdown (excluding rate-limited tests)
- **Service start**: 2.2-3.6s (mean: 2.6s)
- **TLS provisioning**: 5.2-7.7s (mean: 6.2s)
- **Total deploy**: 89-220s depending on boot time and location

### Additional tests passed
- cx23 (smaller server): OK, slightly slower boot
- Simultaneous deploys (2 at once): Both succeeded
- Deploy with --env-file: OK

## New Gotchas

### G-25: Bun binary becomes 0 bytes after Hetzner snapshot if disk not synced
The Hetzner snapshot process captures the disk state at power-off time. If file writes are buffered and not flushed with `sync`, large binaries (bun is 99MB) can be captured as 0-byte files. Always run `sync` before `cloud-init clean --logs` in the provisioning script.

### G-26: Let's Encrypt rate limits ACME account creation per IP
Let's Encrypt limits new ACME account registrations to 10 per IP address per 3 hours. Each fresh Caddy boot without a cached ACME account creates a new registration. Mitigations: disable Caddy in the golden image (prevents spurious registrations on boot), use different Hetzner locations to get different IPs if rate-limited.

### G-27: Caddy must be disabled in the golden image
If Caddy is enabled (default after apt install), it starts on every snapshot boot with whatever Caddyfile is present. This triggers unnecessary ACME registrations and wastes rate limit budget. Disable Caddy in the image, enable it in the deploy script.

## Deploy Timing Expectations

| Phase | Expected | Notes |
|-------|----------|-------|
| Create VM | <1s | API call |
| Create DNS | <1s | Cloudflare instant propagation |
| VM boot | 68-180s | Varies by location and server type |
| SSH ready | 11-24s | After VM reports running |
| Deploy code | 1-2s | Write files + systemd unit |
| Start services | 2-4s | Agent starts in 1-2s, rest is retry loop overhead |
| TLS provisioning | 5-8s | ACME HTTP-01 challenge |
| Health check | <1s | Already verified during TLS phase |
| **Total** | **90-220s** | Dominated by VM boot time |

## Golden Image

Active snapshot: v0.1.5 (ID: 369223967), built on cx23 (40GB disk).
