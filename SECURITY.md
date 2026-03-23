# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, please report it privately. **Do not open a public issue.**

Email: **cheemawrites@gmail.com**

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if you have one)

## What Counts as a Security Issue

- API token exposure (Hetzner, Cloudflare, Anthropic keys in logs, output, or error messages)
- SSH key handling issues (private key leaks, insecure storage)
- Cloud-init secrets not being cleaned up after boot
- systemd service escaping its security sandbox
- Firewall rule bypasses
- Config file permission issues (`~/.specter/config.yaml` should be 0600)

## What Is NOT a Security Issue

- Feature requests for additional hardening
- Bugs that don't expose secrets or allow unauthorized access
- StrictHostKeyChecking=no for SSH (documented trade-off for ephemeral VMs)

## Response Timeline

- **Acknowledgment**: Within 48 hours
- **Assessment**: Within 1 week
- **Fix**: Depends on severity, but we prioritize security patches

## Disclosure

We follow coordinated disclosure. Once a fix is released, we'll credit the reporter (unless they prefer anonymity) and publish details in the release notes.
