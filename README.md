# Specter

AI agents that earn your trust.

Specter is a CLI that provisions and manages persistent AI agent VMs on Hetzner Cloud with automatic DNS and TLS. One command takes you from zero to a production-grade, TLS-secured agent endpoint in under 2 minutes.

## Install

```bash
# From source
go install github.com/ghostwright/specter/cmd/specter@latest

# Or build from this repo
make build
./bin/specter version
```

## Quick Start

```bash
# 1. Set up credentials (interactive wizard)
specter init

# 2. Deploy an agent
specter deploy scout --role swe

# 3. Check status
specter list
specter status scout

# 4. Manage
specter ssh scout
specter logs scout
specter update scout
specter destroy scout
```

## Commands

| Command | Description |
|---------|-------------|
| `specter init` | Interactive setup wizard. Validates Hetzner and Cloudflare tokens, creates firewall, detects golden snapshot. |
| `specter deploy <name>` | Full provision flow: VM from snapshot, DNS record, code deploy, TLS cert, health check. |
| `specter list` | Table of all deployed agents with live health status. |
| `specter status <name>` | Detailed view of one agent including uptime, version, and server info. |
| `specter ssh <name>` | Opens an SSH session to the agent's server. |
| `specter logs <name>` | Streams journalctl logs from the agent's systemd service. |
| `specter update <name>` | Restarts the agent and verifies health. |
| `specter destroy <name>` | Deletes the VM, DNS record, and local state. |
| `specter image build` | Creates a golden VM snapshot with the full toolchain. |
| `specter image list` | Shows available snapshots. |

Every command supports `--json` for scripting. Every interactive command supports `--yes` to skip prompts.

## What Gets Deployed

Each agent gets its own Hetzner Cloud VM with:

- Ubuntu 24.04 from a golden snapshot (Docker, Bun, Caddy pre-installed)
- Automatic TLS via Caddy and Let's Encrypt
- DNS A record on Cloudflare (e.g., `scout.specter.tools`)
- Hetzner Cloud Firewall (ports 22, 80, 443)
- systemd service with auto-restart
- Health endpoint at `https://<name>.<domain>/health`

## Deploy Timing

Measured on cx33 (4 vCPU, 8GB) with cx23-based golden snapshot:

| Phase | Time |
|-------|------|
| Create VM (API) | ~1s |
| Create DNS record | ~1s |
| VM boot from snapshot | ~70-90s |
| SSH available | ~12s |
| Deploy agent code | ~1s |
| Start services | ~1s |
| TLS certificate | ~3-4s |
| Health check | ~1s |
| **Total** | **~90-110s** |

## Prerequisites

- [Hetzner Cloud](https://console.hetzner.cloud) account with an API token
- [Cloudflare](https://dash.cloudflare.com) account with DNS token for your domain
- SSH key uploaded to Hetzner
- Go 1.22+ (for building from source)

## Architecture

Specter is the control plane. It runs on your machine and manages VMs remotely.

```
Your Machine                     Hetzner Cloud (nbg1)
+------------------+             +---------------------------+
| specter CLI (Go) |             | cx33 VM                   |
|                  | ---SSH----> | Caddy (TLS)               |
| specter deploy   |             |   reverse_proxy :3100     |
| specter status   | --HTTPS--> | specter-agent (Bun)       |
| specter destroy  |             |   /health -> JSON         |
+------------------+             +---------------------------+
        |
        +-------> Cloudflare DNS
                  scout.specter.tools -> VM IP
```

## Config

Config is stored at `~/.specter/config.yaml`. Agent state at `~/.specter/agents.yaml`.

## License

Apache 2.0. See [LICENSE](LICENSE).
