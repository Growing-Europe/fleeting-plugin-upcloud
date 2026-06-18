# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] — unreleased

### Fixed

- **`ConnectInfo` sets the dial port explicitly.** The reported connection info now
  resolves the dial port (the configured `connector_config` port, else 22) instead
  of leaving it `0` and relying on the connector framework's default. This keeps the
  dialed port consistent with the port the readiness gate probes and avoids
  depending on undocumented default behavior.
- **Network-readiness gate on `StateRunning`.** A server is reported ready
  (`StateRunning`) only once its SSH port is reachable on the dial address;
  until then it stays `StateCreating`. UpCloud's `started` power state precedes
  network readiness by some seconds (the address is not yet configured and sshd
  not yet listening), so reporting ready immediately made the autoscaler dial into
  a black hole and the job fail in *preparing environment*. The probe is bounded
  and never blocks the reconcile. A never-reachable server stays `Creating` and is
  reaped by the autoscaler's instance creation/readiness timeout — set a sane one
  (see [docs/API.md](docs/API.md#3-provider-behavior)).

### Added

- **Bounded retry with exponential backoff on transient UpCloud API errors**
  (HTTP 429/5xx, network timeouts) for the create/list/get/stop/delete calls,
  honoring context cancellation (no retry past a cancelled context or deadline).
- **Idempotent server creation under retry.** A retried create reconciles by the
  unique per-instance name and adopts an already-created server instead of
  provisioning a duplicate, so a create the API accepted but failed to acknowledge
  is never orphaned.
- `docs/API.md` documenting the stable v0.2 configuration/behavior contract that is
  kept backward-compatible within the `v0.2.x` series.

### Changed

- The networking keys `network` / `utility_network` / `public_ipv4` / `public_ipv6`
  are the finalized v0.2 reachability contract.
- `Suspend` / `Resume` remain documented no-ops: these are single-use VMs
  (`max_use_count = 1`) that the autoscaler never suspends, and UpCloud stop-billing
  is plan-dependent (attached storage and public IPs bill regardless of power state)
  — so suspending instead of destroying has no benefit in this model.

### Removed

- **`floating_ip` config key** — it was declared but never implemented (a silent
  no-op). See *Migration* below.

### Migration (v0.1.x → v0.2.0)

- **Remove `floating_ip` from `plugin_config` if you set it.** It never had any
  effect; with strict config decoding, leaving it present now fails to parse rather
  than being silently ignored.
- No other configuration changes are required — the networking keys and all other
  keys are unchanged and backward-compatible.

## [0.1.2] — 2026-06-18

### Fixed

- **Configured networking is attached to created servers.** When a private/SDN
  network is configured, it is now added as an explicit network interface at
  create time, so the instance actually joins that network. Previously only the
  default interfaces (public + utility) were attached and the instance never
  joined the configured network.
- **The dial (internal) address prefers the private/SDN interface.** Selection now
  prefers the private address over the utility address and is independent of the
  order in which addresses are returned; utility is used only as a fallback when no
  private interface is present.
- **Dial addresses are derived from the network interfaces, classified by interface
  type** — not from the flattened top-level address list. For a server attached to
  a private cloud network, the provider returns an empty top-level address list and
  exposes the private address only under a private-type interface whose IP carries
  an empty `access` field. The previous derivation therefore left the internal
  address empty and the connector dialed an unreachable/empty target.

### Added

- A golden `ServerDetails` test fixture and interface-type table tests that pin the
  dial-address derivation (private-preferred internal, utility fallback,
  public → external, IPv6 ignored, empty-interfaces fallback to the flat list).
- A real-execution smoke gate (opt-in, billable, skipped in CI) that, after
  provisioning, dials the address `ConnectInfo` reports over SSH and runs a command
  before destroying the server — asserting that *provisioning* a server is not
  mistaken for the job being *able to run* on it.
- Documentation of the reachability contract (configured networking must be
  attached; dial addresses derived from interfaces by type) and a `RELEASING.md`
  covering the signed-release process and Go module immutability.

### Changed

- `go.mod` now `retract`s `v0.1.0` (its release pipeline produced no signed
  artifacts; it is superseded by `v0.1.1` and later). The `v0.1.0` tag itself
  remains published and immutable.
- Routine CI action and dependency maintenance (pinned-SHA bumps).

The signed-release pipeline — keyless cosign signature over the checksums, SLSA
build provenance, and per-archive SBOMs — is unchanged in behavior from `v0.1.1`.

## [0.1.1] — 2026-06-17

### Added

- First signed release: a keyless [cosign](https://docs.sigstore.dev/) signature
  over the checksums (Sigstore bundle), [SLSA](https://slsa.dev) build provenance,
  and per-archive SBOMs.

## [0.1.0] — 2026-06-15 [RETRACTED]

Initial tag. Its release pipeline produced no signed artifacts; it is superseded by
`v0.1.1` and retracted in `go.mod`. Do not depend on this version.
