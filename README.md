# Specter

**AI agents that earn your trust.**

Specter is a CLI that deploys persistent AI agent VMs on Hetzner Cloud. One command provisions a production VM with automatic DNS, TLS, and health monitoring -- all in under 2 minutes. You own the infrastructure. You own the data. No vendor lock-in.

## What Specter Does

Deploy AI agents to dedicated cloud VMs with a single command. Each agent gets its own Hetzner VM with Docker, Bun, and Caddy pre-installed from a golden snapshot. Caddy handles automatic TLS via Let's Encrypt. A Cloudflare DNS record maps `agent-name.yourdomain.com` to the VM. Every deploy is monitored with a health endpoint and systemd auto-restart.

## Quick Start

### Prerequisites

Before you begin, you need three things:

**1. A Hetzner Cloud account**

- Sign up at [console.hetzner.cloud](https://console.hetzner.cloud)
- Create a project
- Go to **Security > API Tokens** and generate a token with **Read & Write** permissions

**2. A Cloudflare account with a domain**

- Sign up at [dash.cloudflare.com](https://dash.cloudflare.com)
- Add a domain (or use an existing one)
- Get your **Zone ID** from the domain overview page (right sidebar, under "API")
- Go to **Profile > API Tokens** and create a token with **Edit zone DNS** permission

**3. An SSH key on Hetzner**

If you don't have an SSH key:
```bash
ssh-keygen -t ed25519
```

Upload your public key at **Hetzner Console > Security > SSH Keys**. Remember the name you give it.

### Install

```bash
# From source
git clone https://github.com/ghostwright/specter.git
cd specter
make build

# Verify
./bin/specter version
```

### Deploy Your First Agent

```bash
# 1. Run the setup wizard
./bin/specter init

# 2. Build a golden snapshot (first time only, ~5 minutes)
./bin/specter image build

# 3. Deploy an agent
./bin/specter deploy scout --role swe

# 4. Verify
curl https://scout.yourdomain.com/health
# {"status":"ok","uptime":12,"version":"0.1.0","timestamp":"..."}
```

That's it. Your agent is live with HTTPS, auto-restart, and health monitoring.

## Commands

| Command | Description |
|---------|-------------|
| `specter init` | Interactive setup wizard. Validates API tokens, creates firewall, caches server types. |
| `specter deploy <name>` | Provision VM, DNS, code, TLS, and health check. |
| `specter list` | Table of all agents with live health status and cost estimate. |
| `specter status <name>` | Detailed view of one agent: uptime, version, server info. |
| `specter ssh <name>` | SSH as the `specter` user. Use `--root` for admin access. |
| `specter logs <name>` | View agent logs. Supports `-n`, `--since`, `-f`. |
| `specter update <name>` | Pull code, reinstall deps, restart, and verify health. |
| `specter destroy <name>` | Delete VM, DNS record, and local state. Handles stale resources. |
| `specter image build` | Create golden snapshot. Auto-increments version. |
| `specter image list` | Show available snapshots with active marker. |
| `specter version` | Print version, commit, and build date. |

### Global Flags

- `--json` -- Machine-readable JSON output (every command)
- `--yes` / `-y` -- Skip confirmation prompts

### Deploy Flags

```bash
specter deploy <name> [flags]

--role string       Agent role (default "swe")
--server-type string  Hetzner server type (default from config)
--location string     Hetzner location (default from config)
--env KEY=VALUE       Environment variable (repeatable)
--env-file PATH       Path to .env file
--yes                 Skip confirmation
```

### Environment Variables

Inject secrets and configuration into your agent's `.env` file:

```bash
# Individual variables
specter deploy scout --role swe \
  --env ANTHROPIC_API_KEY=sk-ant-... \
  --env SLACK_BOT_TOKEN=xoxb-...

# From a file
specter deploy scout --role swe --env-file ./scout.env

# Both (--env overrides values from --env-file)
specter deploy scout --role swe \
  --env-file ./base.env \
  --env ANTHROPIC_API_KEY=sk-ant-...
```

### Logs Flags

```bash
specter logs <name> [flags]

-f, --follow        Follow log output (default false)
-n, --lines int     Number of lines to show (default 100)
    --since string  Show logs since (e.g., "5m", "1h", "2d", "2026-03-22 17:00")
```

## Server Types

Specter validates server types dynamically from the Hetzner API. Common x86 types:

| Type | vCPU | RAM | Disk | Price |
|------|------|-----|------|-------|
| cx23 | 2 | 4 GB | 40 GB | ~$3.49/mo |
| cx33 | 4 | 8 GB | 80 GB | ~$5.99/mo |
| cx43 | 8 | 16 GB | 160 GB | ~$9.99/mo |
| cx53 | 16 | 32 GB | 320 GB | ~$18.99/mo |

ARM servers (cax*) are not supported because the golden snapshot is built on x86. Specter will suggest the closest valid type if you mistype:

```
$ specter deploy test --server-type potato
unknown server type 'potato'. Did you mean 'cpx11'?

Available x86 types:
  cx23      2 vCPU     4 GB RAM    40 GB disk  $3.49/mo
  cx33      4 vCPU     8 GB RAM    80 GB disk  $5.99/mo
  ...
```

## For AI Agents (MCP / Claude Code)

Every command supports `--json` for structured output and `--yes` for non-interactive mode. This makes Specter natively operable by AI agents:

```bash
# Deploy and get structured result
specter deploy scout --role swe --json --yes \
  --env ANTHROPIC_API_KEY=sk-ant-...
# Returns: {"status":"deployed","name":"scout","url":"https://...","phases":[...]}

# Monitor
specter status scout --json
specter list --json

# Tear down
specter destroy scout --json --yes
```

The JSON deploy output includes per-phase timing for performance analysis:

```json
{
  "status": "deployed",
  "deploy_time_seconds": 90,
  "phases": [
    {"name": "Creating VM on Hetzner", "seconds": 1.08},
    {"name": "Waiting for VM boot", "seconds": 73.5},
    {"name": "Provisioning TLS", "seconds": 3.0}
  ]
}
```

## Architecture

Specter is a control plane that runs on your machine and manages VMs remotely.

```
Your Machine                     Hetzner Cloud (nbg1)
+------------------+             +---------------------------+
| specter CLI (Go) |             | VM (cx33)                 |
|                  | ---SSH----> | Caddy (auto-TLS)          |
| specter deploy   |             |   reverse_proxy :3100     |
| specter status   | --HTTPS--> | specter-agent (Bun)       |
| specter logs     |             |   /health -> JSON         |
| specter ssh      |             | Docker (sidecars)         |
| specter destroy  |             | systemd (auto-restart)    |
+------------------+             | ufw + Cloud Firewall      |
        |                        +---------------------------+
        |                                  |
        +-------> Cloudflare DNS           |
                  scout.example.com -> VM IP
```

### What Gets Deployed

Each agent VM includes:

- **Ubuntu 24.04** from a golden snapshot
- **Docker 29.x** for sidecar containers
- **Bun 1.3.x** as the JavaScript runtime
- **Caddy 2.11.x** for automatic TLS via Let's Encrypt
- **systemd** service with security hardening (NoNewPrivileges, ProtectHome, PrivateTmp, MemoryMax)
- **Hetzner Cloud Firewall** (ports 22, 80, 443)
- **ufw** as a secondary firewall layer
- **fail2ban** for SSH brute-force protection
- **2 GB swap** for memory-intensive workloads

### Deploy Flow

1. CLI creates VM from golden snapshot (~1s API call)
2. DNS A record created on Cloudflare (parallel with VM boot)
3. VM boots from snapshot (~70s)
4. SSH becomes available (~12s after boot)
5. Agent code deployed via SSH
6. systemd service started, Caddy restarted
7. Caddy provisions TLS certificate (~4s)
8. Health check verifies HTTPS endpoint
9. Cloud-init user-data (containing secrets) deleted from disk

**Total: ~90-110 seconds.**

### Golden Snapshot Strategy

The golden snapshot is built on the smallest x86 server (cx23, 40 GB disk) to minimize boot time. Hetzner copies the full disk allocation during snapshot boot, so smaller disks mean faster deploys. The snapshot can be deployed on any x86 server type (cx23 or larger).

## Security

- **API tokens** are never logged, printed in errors, or included in JSON output
- **Config file** at `~/.specter/config.yaml` is stored with 0600 permissions
- **Cloud-init user-data** containing secrets is automatically deleted after boot
- **systemd hardening**: NoNewPrivileges, ProtectSystem=strict, ProtectHome=read-only, PrivateTmp, MemoryMax=2G, TasksMax=256
- **SSH**: Uses StrictHostKeyChecking=no (known limitation for ephemeral VMs)
- **Firewall**: Hetzner Cloud Firewall + ufw, only ports 22/80/443 open

## Deploy Timing

Measured on cx33 (4 vCPU, 8 GB) with cx23-based golden snapshot:

| Phase | Time |
|-------|------|
| Create VM (API call) | ~1s |
| Create DNS record | ~1s |
| VM boot from snapshot | ~70-90s |
| SSH available | ~12s |
| Deploy agent code | ~1s |
| Start services | ~1s |
| TLS certificate | ~3-4s |
| Health check | ~1s |
| **Total** | **~90-110s** |

## Configuration

Config is stored at `~/.specter/config.yaml` (0600 permissions). Agent state at `~/.specter/agents.yaml`. Server type cache at `~/.specter/server_types.json`.

## FAQ

**How much does it cost?**
Infrastructure is $3-19/month per agent depending on server type. API costs (Anthropic, etc.) are separate and typically dominate at $50-500/month.

**Can I use my own domain?**
Yes. During `specter init`, specify your domain. You need a Cloudflare-managed DNS zone for that domain.

**What if Hetzner is down?**
Specter stores agent state locally. If the Hetzner API is unreachable during deploy, it fails cleanly. Running agents continue to work as long as the VMs are up.

**Is it secure?**
Secrets are injected via cloud-init (HTTPS to Hetzner API) and cleaned up after boot. The VM is firewalled to ports 22/80/443. systemd runs the agent with hardened security directives. See the Security section above.

**Can I SSH into the VM?**
Yes. `specter ssh <name>` connects as the `specter` user. Use `--root` for admin access.

## Contributing

```bash
# Clone and build
git clone https://github.com/ghostwright/specter.git
cd specter
make build

# Run against real infrastructure
source .env.local  # your API tokens
./bin/specter init
./bin/specter deploy test --role swe --yes
./bin/specter destroy test --yes
```

## License

Apache 2.0. See [LICENSE](LICENSE).
