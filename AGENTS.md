# AGENTS.md

Development instructions for autonomous agents working on this repository. This is the **primary
instruction file**. It is written for agents; it is prescriptive, not prose. Humans contributing here
read the same file — there is no separate hand-holding guide.

## What this is
A **generic** GitLab Runner [fleeting](https://gitlab.com/gitlab-org/fleeting/fleeting) provider plugin
for [UpCloud](https://upcloud.com). It provisions and autoscales UpCloud cloud servers as ephemeral CI
runners — one isolated VM per job (`max_use_count = 1`).

## PRIME INVARIANT — provider-generic, zero coupling
This plugin MUST run, unmodified, for anyone using GitLab Runner on UpCloud. Enforce on every change:
- **No** organization-, deployment-, or environment-specific values in code. Zones, plans, images,
  networks, caps, labels, hostnames, credentials — **all configuration**, supplied via plugin config or
  credentials.
- **No** references to any specific downstream infrastructure, internal hostnames, private networks, or
  tooling.
- Sensible defaults; fail-closed on missing credentials; never log a secret.

A change that hardcodes a site-specific value is **wrong** — move it behind configuration.

## Build / verify — run before every commit
Requires **Go ≥ 1.26** (the `upcloud-go-api` and `fleeting` dependencies set the
module's minimum). CI builds and scans on the latest stable Go; lint runs
golangci-lint v2.
```sh
go build ./...
go vet ./...
go test ./...
gofmt -l .        # MUST print nothing
```

## Definition of Done — machine-checkable, all must hold
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` pass.
- [ ] `gofmt -l .` prints nothing.
- [ ] No new hardcoded site-specific value (PRIME INVARIANT).
- [ ] No secret in code, tests, config, or git history; `config.toml` stays git-ignored.
- [ ] Unit tests require no network and no credentials.
- [ ] Public strings (README, `config.example.toml`) use generic placeholders, never real values.

## Architecture / contract
Implement the fleeting provider interface (`provider.InstanceGroup`) — the live
interface is **9 methods**:
`Init` · `Update` · `Increase` · `Decrease` · `ConnectInfo` · `Heartbeat` ·
`Suspend` · `Resume` · `Shutdown`.
`Suspend`/`Resume` are no-ops and the `CapabilitySuspendResume` capability is not
advertised — these VMs are single-use (`max_use_count = 1`), so the provisioner
never suspends them. `Heartbeat`/`Shutdown` are no-ops.

The GitLab Runner autoscaler decides **when** to scale. This plugin only creates / lists / deletes
UpCloud servers and reports their connection info. UpCloud Server API base: `https://api.upcloud.com/1.3`
(use the official `upcloud-go-api` SDK). Read the UpCloud API token from the **environment / credentials**
— never from config files or source.

## Security invariants — non-negotiable
- API token from env/credentials only; never in `config.toml`, never logged, never committed.
- Fail-closed: missing/empty token → hard error at construction, not a silent default.
- `config.toml`, `*.pem`, `*.key`, `.env*` are git-ignored. Ship `config.example.toml` with placeholders.

## Conventions
- Go idioms; table-driven tests; small, focused packages.
- The integration test (real UpCloud account) is gated on env credentials — skipped otherwise and in public CI.
- Semver; tagged releases (`vX.Y.Z`) publish binaries.
