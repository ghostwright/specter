<p align="center">
  <img src="logo-animated.svg" width="200" alt="Specter">
</p>

<h1 align="center">Specter</h1>
<p align="center"><em>AI agents that earn your trust.</em></p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="Apache 2.0 License"></a>
  <img src="https://img.shields.io/badge/platform-Linux%20(x86)-black.svg" alt="Linux x86">
  <img src="https://img.shields.io/badge/go-1.26-orange.svg" alt="Go 1.26">
  <img src="https://img.shields.io/badge/JSON-mode-green.svg" alt="JSON Mode">
</p>

---

Your AI agent needs a server. Not a sandbox, not a container that disappears, not a shared runtime. A real VM with its own IP, its own TLS certificate, its own systemd process. Specter gives it one in 90 seconds.

```bash
specter deploy scout --role swe --env ANTHROPIC_API_KEY=sk-ant-...
# scout.yourdomain.com is live with HTTPS, health monitoring, and auto-restart
```

One command. Dedicated VM on Hetzner Cloud, automatic DNS on Cloudflare, TLS via Let's Encrypt, systemd hardening, firewall, and a health endpoint. You own the infrastructure. You own the data. No vendor lock-in.

<!-- <img src="demo.gif" width="720" alt="Specter deploy in action"> -->

---

## Why Specter?

AI agents need persistent infrastructure, not ephemeral containers. They need to run for days, hold state, accept webhooks, and survive restarts. Specter gives each agent a production-grade VM with everything pre-configured.

- **Fast** -- 90-second deploys from a golden snapshot. No Docker builds, no package installs at deploy time.
- **Secure** -- Secrets injected via cloud-init and deleted after boot. systemd hardening. Dual firewalls.
- **Observable** -- Health endpoints, systemd journals, live status checks. SSH when you need it.
- **Agent-native** -- Every command has `--json` output and `--yes` for non-interactive use. Built for AI-to-AI orchestration.
- **Cheap** -- Hetzner VMs start at $3.49/month. No markup, no platform fee.
- **Yours** -- Apache 2.0. Fork it, extend it, run it on your own terms.

## Install

```bash
git clone https://github.com/ghostwright/specter.git
cd specter && make build
./bin/specter version
```

## Quick Start

```bash
# 1. Setup wizard (validates tokens, creates firewall)
specter init

# 2. Build golden snapshot (first time only, ~5 min)
specter image build

# 3. Deploy
specter deploy scout --role swe --env ANTHROPIC_API_KEY=sk-ant-...

# 4. Verify
curl https://scout.yourdomain.com/health
{"status":"ok","uptime":12,"version":"0.1.0"}
```

## Commands

| | Command | What it does |
|:---:|---------|-------------|
| :gear: | `specter init` | Setup wizard. Validates API tokens, creates firewall, caches server types. |
| :rocket: | `specter deploy <name>` | Provision VM, DNS, TLS, deploy code, and health check. |
| :mag: | `specter list` | All agents with live health status and cost estimate. |
| :bar_chart: | `specter status <name>` | Detailed view: uptime, version, server info. |
| :key: | `specter ssh <name>` | SSH as the `specter` user. Use `--root` for admin access. |
| :scroll: | `specter logs <name>` | Agent logs via systemd journal. Supports `-f`, `-n`, `--since`. |
| :arrows_counterclockwise: | `specter update <name>` | Restart agent and refresh dependencies. |
| :wastebasket: | `specter destroy <name>` | Delete VM, DNS record, and local state. Handles stale resources. |
| :package: | `specter image build` | Create golden snapshot. Auto-increments version. |
| :framed_picture: | `specter image list` | Show available snapshots with active marker. |
| :label: | `specter version` | Print version, commit, and build date. |

Every command supports `--json` for structured output and `--yes` / `-y` to skip prompts.

## Deploy Flags

```bash
specter deploy <name> [flags]

--role string         Agent role (default "swe")
--server-type string  Hetzner server type (default from config)
--location string     Hetzner datacenter (default from config)
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

## Server Types

Pricing is fetched dynamically from the Hetzner API during `specter init`. Common x86 types:

| | Type | vCPU | RAM | Disk | Price |
|:---:|------|:----:|:---:|:----:|------:|
| | cx23 | 2 | 4 GB | 40 GB | ~$3.49/mo |
| :star: | cx33 | 4 | 8 GB | 80 GB | ~$5.99/mo |
| | cx43 | 8 | 16 GB | 160 GB | ~$9.99/mo |
| | cx53 | 16 | 32 GB | 320 GB | ~$18.99/mo |

:star: = default. ARM servers (cax*) are not supported -- the golden snapshot is x86.

Mistype a server name and Specter suggests the closest match:

```
$ specter deploy test --server-type potato
unknown server type 'potato'. Did you mean 'cpx11'?
```

## Use with Claude Code

Give Claude this repo and it can deploy and manage your infrastructure. The `CLAUDE.md` file gives it full context automatically.

**Example prompt:**
> Deploy a new agent called "scout" for software engineering on a cx33 in Nuremberg. Use the API key in my .env file.

Claude will run:
```bash
specter deploy scout --role swe --json --yes \
  --env-file .env --server-type cx33 --location nbg1
```

**Example prompt:**
> Show me all running agents and their health status.

```bash
specter list --json
specter status scout --json
```

**Example prompt:**
> SSH into scout and check the logs.

```bash
specter ssh scout
# or from outside:
specter logs scout -n 50
```

Every command supports `--json` for structured output and `--yes` to skip prompts. This makes Specter fully programmable by AI agents, CI/CD pipelines, or scripts.

### JSON Output

```bash
specter deploy scout --role swe --json --yes \
  --env ANTHROPIC_API_KEY=sk-ant-...
```

```json
{
  "status": "deployed",
  "name": "scout",
  "url": "https://scout.yourdomain.com",
  "deploy_time_seconds": 93,
  "phases": [
    {"name": "Creating VM on Hetzner", "seconds": 0.87},
    {"name": "Waiting for VM boot", "seconds": 73.5},
    {"name": "Provisioning TLS", "seconds": 3.2}
  ]
}
```

```bash
specter status scout --json    # health, uptime, server info
specter list --json            # all agents as JSON array
specter destroy scout --json --yes
```

## Architecture

```
Your Machine                      Hetzner Cloud
+------------------+              +------------------------------+
| specter CLI      |              | VM (Ubuntu 24.04)            |
|                  | ---SSH--->   |                              |
| Go binary        |              |   Caddy (auto-TLS)           |
| ~3,500 lines     | --HTTPS-->   |   reverse_proxy :3100        |
|                  |              |   specter-agent (Bun)        |
+------------------+              |     /health -> JSON          |
       |                          |   Docker (sidecars)          |
       |                          |   systemd (auto-restart)     |
       +----> Cloudflare DNS      |   ufw + Cloud Firewall       |
              name.domain -> IP   +------------------------------+
```

### What Gets Deployed

Each agent VM includes:

- **Ubuntu 24.04** from a golden snapshot
- **Docker 29.x** for sidecar containers
- **Bun 1.3.x** as the JavaScript runtime
- **Caddy 2.11.x** for automatic TLS via Let's Encrypt
- **systemd** with security hardening (NoNewPrivileges, ProtectSystem, PrivateTmp, MemoryMax)
- **Hetzner Cloud Firewall** + **ufw** (ports 22, 80, 443 only)
- **fail2ban** for SSH brute-force protection
- **2 GB swap** for memory-intensive workloads

Currently deploys a minimal health endpoint. The full specter-agent runtime is under development.

### Deploy Flow

| Step | What happens | Time |
|:----:|-------------|-----:|
| 1 | Create VM from golden snapshot | ~1s |
| 2 | Create DNS A record on Cloudflare | ~1s |
| 3 | VM boots from snapshot | ~70-90s |
| 4 | SSH becomes available | ~12s |
| 5 | Agent code deployed via SSH | ~1s |
| 6 | systemd + Caddy started | ~1s |
| 7 | TLS certificate provisioned | ~3-4s |
| 8 | Health check passes | ~1s |
| | **Total** | **~90-110s** |

### Golden Snapshot

The snapshot is built on the smallest x86 server (cx23, 40 GB disk) to minimize boot time. Hetzner copies the full disk allocation during snapshot restore, so smaller disks = faster deploys. Any cx23-based snapshot works on all larger x86 types.

## Security

- **Token redaction** -- API tokens are never included in JSON output or error messages
- **File permissions** -- Config at `~/.specter/config.yaml` is stored with 0600
- **Secret cleanup** -- Cloud-init user-data (containing env vars) is deleted after boot
- **systemd hardening** -- NoNewPrivileges, ProtectSystem=strict, ProtectHome=read-only, PrivateTmp, MemoryMax=2G, TasksMax=256
- **Dual firewall** -- Hetzner Cloud Firewall + ufw, only ports 22/80/443
- **SSH** -- Uses StrictHostKeyChecking=no (known trade-off for ephemeral VMs)

## Prerequisites

Before running `specter init`, you need:

**1. Hetzner Cloud account** -- [console.hetzner.cloud](https://console.hetzner.cloud). Create a project, then generate an API token with Read & Write permissions under Security > API Tokens.

**2. Cloudflare account with a domain** -- [dash.cloudflare.com](https://dash.cloudflare.com). Get your Zone ID from the domain overview sidebar. Create an API token with Edit zone DNS permission.

**3. SSH key on Hetzner** -- Upload your public key at Hetzner Console > Security > SSH Keys. If you need one: `ssh-keygen -t ed25519`.

## FAQ

**How much does it cost?**
<br>Infrastructure is $3-19/month per agent depending on server type. Pricing is fetched from the Hetzner API and may change. Agent API costs (Anthropic, OpenAI, etc.) are separate.

**Can I use my own domain?**
<br>Yes. During `specter init`, specify your domain. You need a Cloudflare-managed DNS zone.

**What if Hetzner is down?**
<br>Specter stores agent state locally. If the API is unreachable during deploy, it fails cleanly. Running agents keep working as long as VMs are up.

**Is it secure?**
<br>Secrets are injected via cloud-init over HTTPS and cleaned up after boot. The VM is firewalled to three ports. systemd runs the agent with hardened directives. See the Security section.

**Can I SSH into the VM?**
<br>Yes. `specter ssh <name>` connects as the `specter` user. Use `--root` for admin access.

**What about the agent code?**
<br>Currently deploys a minimal health endpoint (v0.1.0 placeholder). The full specter-agent runtime with tool use, task queues, and MCP integration is under development.

## Contributing

```bash
git clone https://github.com/ghostwright/specter.git
cd specter && make build

# Test against real infrastructure
source .env.local
specter init
specter deploy test --role swe --yes
specter destroy test --yes
```

We need testing across server types, better agent runtimes, and documentation. If you are building AI agents that need real infrastructure, this is the project.

## For LLMs and AI Agents

If you're an LLM reading this repo for the first time, here's what you need to know.

**What Specter does:** It provisions dedicated Hetzner Cloud VMs with automatic DNS (Cloudflare) and TLS (Let's Encrypt via Caddy). Each VM runs as an independent agent server with its own subdomain, health endpoint, and systemd process management. The CLI handles the full lifecycle: create, monitor, update, destroy.

**How to use it programmatically:** Always pass `--json` and `--yes`. Never use interactive mode.

```bash
# Deploy
specter deploy <name> --role swe --json --yes --env KEY=VALUE

# Check health
curl -sf https://<name>.yourdomain.com/health | jq .

# List all agents
specter list --json

# Status of one agent
specter status <name> --json

# Destroy
specter destroy <name> --json --yes

# Build golden snapshot (needed before first deploy)
specter image build --json
```

**Key things to know:**
- VM boot takes 70-90 seconds. Don't timeout before 120s.
- TLS provisioning takes 5-8 seconds after services start.
- The golden snapshot must exist before deploying. Run `specter image build` first.
- x86 only. ARM servers are not supported.
- Locations: `nbg1` (Nuremberg), `fsn1` (Falkenstein), `hel1` (Helsinki). All EU.
- DNS records must have `proxied: false` on Cloudflare or TLS breaks.
- The health endpoint is always at `https://<name>.<domain>/health` and returns JSON.

**For deeper context:** Read `CLAUDE.md` in the repo root. It has the full project structure, frozen files list, and architecture details.

## License

Apache 2.0. See [LICENSE](LICENSE).
