# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.2] — unreleased

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
