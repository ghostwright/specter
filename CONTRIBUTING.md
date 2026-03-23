# Contributing to Specter

Specter deploys AI agent VMs on Hetzner Cloud in 90 seconds. The deploy flow has been rigorously tested and validated. We want to keep that reliability bar while making the tool better, faster, and more useful.

Whether you are fixing a typo, adding a TUI view, or building a whole new cloud provider, this guide will get you oriented.

## Quick Start

```bash
git clone https://github.com/ghostwright/specter.git
cd specter
make build
./bin/specter version
```

That gives you a working binary. To test against real infrastructure:

```bash
# You need: Hetzner API token, Cloudflare API token + zone ID, SSH key on Hetzner
cp .env.example .env.local      # fill in your tokens
source .env.local
specter init                    # validates tokens, creates firewall
specter deploy test --role swe --yes
specter destroy test --yes
```

There are no mocks. Specter talks to real APIs and creates real VMs. A test deploy costs about $0.01 (Hetzner bills hourly, minimum ~$0.005).

## Architecture

```
specter/
  cmd/specter/
    main.go                     Entry point
    commands/
      root.go                   Cobra root command
      deploy.go                 The big one: VM creation, DNS, SSH, TLS
      destroy.go                Teardown: VM + DNS cleanup
      init.go                   Setup wizard (tokens, firewall, server types)
      image.go                  Golden snapshot build + list
      list.go                   Agent inventory with live health checks
      status.go                 Single agent detail view
      ssh.go                    SSH into agent VM
      logs.go                   systemd journal streaming
      update.go                 Agent restart + dependency refresh
      version.go                Version info
  internal/
    cloudflare/client.go        DNS record management (Cloudflare API)
    config/
      config.go                 YAML config at ~/.specter/config.yaml
      server_types.go           Hetzner server type cache + fuzzy matching
      state.go                  Agent state persistence
    deploy/                     (reserved for future deploy strategies)
    hetzner/client.go           VM lifecycle (hcloud-go SDK)
    templates/
      cloudinit.go              Cloud-init user-data generation
      systemd.go                systemd unit template (frozen)
      caddyfile.go              Caddy reverse proxy config (frozen)
    tui/
      app.go                    Main Bubbletea app model (~900 lines)
      agent_list.go             Dashboard agent list view
      agent_detail.go           Single agent detail panel
      deploy_form.go            Deploy configuration form (huh)
      deploy_model.go           Deploy data model
      deploy_progress.go        Real-time deploy progress with phases
      image_build.go            Image build progress view
      setup_wizard.go           First-run setup TUI
      logs_viewport.go          Log viewer with follow mode
      confirm_dialog.go         Confirmation dialogs
      help_overlay.go           Keyboard shortcuts overlay
      status_bar.go             Bottom status bar
      dashboard_styles.go       Lipgloss styles for the dashboard
      messages.go               Bubbletea message types
      theme.go                  Color palette
  pkg/
    version/version.go          Version variables (set by ldflags)
```

### Key Design Decisions

- **Bubbletea v2** for the TUI (charm.land/bubbletea/v2). The dashboard is a lazydocker-style multi-panel interface.
- **Cobra** for the CLI command tree. Every command supports `--json` and `--yes`.
- **hcloud-go** for the Hetzner Cloud API. Direct HTTP for Cloudflare (no SDK).
- **Golden snapshot** strategy: build once, deploy many. Snapshot boot is faster than provisioning from scratch.
- **No Docker for the CLI itself.** Pure Go binary, cross-compiled for darwin/linux, amd64/arm64.

## Frozen Files

These files are frozen. Do not modify them without running 3 full deploy cycles (create, verify health, destroy) afterward:

| File | Why |
|------|-----|
| `internal/templates/systemd.go` | The systemd unit controls process isolation, memory limits, and filesystem permissions. A bad `ReadWritePaths` broke 100% of deploys until we found it. |
| `internal/templates/cloudinit.go` | Cloud-init injects secrets and configures Caddy. A premature `systemctl restart caddy` in runcmd caused race conditions that took days to diagnose. |
| `internal/templates/caddyfile.go` | Caddy handles TLS via ACME. Changing the template can trigger Let's Encrypt rate limits (10 registrations per IP per 3 hours). |
| `cmd/specter/commands/image.go` | The image build provisioning script must call `sync` before snapshotting. Without it, Bun's 99MB binary was captured as 0 bytes. This was the Phase 3 blocker. |

The 16/16 deploy success rate was hard-won. Every one of these files has a bug story behind it.

## How to Add a TUI View

The TUI follows the Elm architecture via Bubbletea. To add a new view:

1. Create a new file in `internal/tui/` (e.g., `my_view.go`).
2. Define your view state as fields on the main `App` model in `app.go`, or as a sub-model.
3. Add a new view constant to the view enum in `app.go`.
4. Handle keyboard input for your view in the `Update` method's view switch.
5. Add rendering in the `View` method's view switch.
6. Wire navigation: add a key binding that switches to your view.

Study `agent_detail.go` for a simple read-only view, or `deploy_form.go` for an interactive form using huh.

Styles live in `dashboard_styles.go` and `theme.go`. Use the existing color palette for consistency.

## How to Test

There is no mock infrastructure. Testing means deploying real VMs.

```bash
# Full deploy cycle
specter deploy test-$(date +%s) --role swe --yes
specter status test-*
specter logs test-* -n 20
specter destroy test-* --yes

# Image build (takes ~5 min, creates a snapshot on Hetzner)
specter image build --yes

# JSON mode (for verifying programmatic output)
specter list --json
specter status test --json
```

If you are testing deploy changes, run at least 3 consecutive deploys. The failure modes are often intermittent (boot timing, ACME rate limits, DNS propagation).

## Code Style

- **gofmt** and **go vet** must pass. CI checks both.
- Conventional commits: `feat:`, `fix:`, `chore:`, `docs:`.
- Comments explain why, not what.
- No changelog-style comments ("was broken, now fixed").
- Error messages should be actionable. Tell the user what to do, not just what went wrong.

## Good First Issues

If you want to contribute but are not sure where to start:

- **Add `--location` flag validation to deploy** - Currently accepts any string. Should validate against cached server types from `specter init` and suggest the closest match (like `--server-type` already does).
- **Add `specter config show`** - Print the current config (with tokens redacted). Useful for debugging.
- **Improve error messages for expired tokens** - Hetzner and Cloudflare return different error shapes for auth failures. Surface a clear "your token is expired/invalid" message.
- **Add `--since` and `--until` to `specter list`** - Filter agents by creation date.
- **Tab completion for agent names** - Shell completions using Cobra's built-in completion support.

## Pull Requests

1. Fork the repo and create a branch from `main`.
2. Make your changes. Run `make lint` to verify formatting.
3. If you touched deploy logic or templates, test with real deploys.
4. Open a PR against `main`. Describe what changed and why.
5. Tag [@mcheemaa](https://github.com/mcheemaa) for review.

Keep PRs focused. One feature or fix per PR. If your change touches frozen files, explain the testing you did in the PR description.

## Questions?

Open an issue or start a discussion. We are building this in the open and want to hear from you, whether you are deploying AI agents at scale or just curious about the architecture.
