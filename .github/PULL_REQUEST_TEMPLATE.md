## What does this PR do?

<!-- Brief description of the change -->

## Does it modify any frozen files?

<!-- Frozen files require 3 successful deploy tests before merging.
     Frozen: systemd.go, deploy.go, image.go, cloudinit.go, hetzner/client.go, cloudflare/client.go -->

- [ ] No frozen files modified
- [ ] Frozen files modified - 3 deploy test results attached below

## How was this tested?

<!-- Describe your testing approach. For infrastructure changes, include deploy test output. -->

## Screenshots

<!-- For TUI changes, include before/after screenshots -->

## Checklist

- [ ] `make build` passes
- [ ] `go vet ./...` is clean
- [ ] Tested via `--json --yes` against real infrastructure
- [ ] No new dependencies added without discussion
