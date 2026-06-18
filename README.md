# fleeting-plugin-upcloud

> GitLab Runner fleeting plugin for UpCloud — autoscaled, ephemeral VM-per-job CI on EU-sovereign infrastructure.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A [GitLab Runner fleeting](https://gitlab.com/gitlab-org/fleeting/fleeting) provider plugin that
provisions and autoscales [UpCloud](https://upcloud.com) cloud servers as ephemeral CI runners —
**one isolated VM per job**. It plugs into the GitLab Runner
[Instance / Docker Autoscaler executor](https://docs.gitlab.com/runner/executors/docker_autoscaler/),
the official successor to the deprecated `docker-machine` autoscaler.

> **Status: early access — `v0.1.1` released.** The first signed release is published: keyless
> [cosign](https://docs.sigstore.dev/)-signed artifacts with [SLSA](https://slsa.dev) build provenance
> and SBOMs (see [Verifying releases](#verifying-releases)). The full create → connect → destroy
> lifecycle has been exercised against a live UpCloud API. The plugin is **functional but early** —
> interfaces and configuration may still change before `v1.0`, so evaluate carefully before relying on
> it in production. Issues and stars welcome.

## Why

- **VM-per-job isolation.** Each CI job runs in its own ephemeral UpCloud server (`max_use_count = 1`),
  then the server is destroyed — the isolation model hosted CI uses for untrusted code.
- **EU-sovereign.** Runs entirely on UpCloud's EU infrastructure.
- **Autoscaling.** The GitLab Runner manager scales the fleet up and down on demand.

## Requirements

- GitLab Runner with the Instance or Docker Autoscaler executor (fleeting).
- An UpCloud account and an API token (`ucat_…`).
- A bootable OS image / storage template for the runner instances.

### Compatibility

| Component        | Supported                                                        |
| ---------------- | ---------------------------------------------------------------- |
| Go (to build)    | ≥ 1.26                                                           |
| fleeting API     | `provider.InstanceGroup` v0 (9-method interface)                |
| GitLab Runner    | Versions shipping the fleeting Instance/Docker Autoscaler        |
| UpCloud API      | 1.3 (via the official `upcloud-go-api` v8 SDK, bearer token)     |
| Platforms        | linux/amd64, linux/arm64 (release binaries)                      |

The UpCloud API token is read from the environment (e.g. `UPCLOUD_TOKEN`), never
from configuration.

## Installation

Download a release binary from the [Releases](../../releases) page (or `go install`), then reference it
from your runner configuration — see [`config.example.toml`](config.example.toml).

## Verifying releases

Every release ships a keyless [cosign](https://docs.sigstore.dev/) signature over `checksums.txt`
(emitted as a Sigstore bundle, `checksums.txt.sigstore.json`), [SLSA](https://slsa.dev) build
provenance (`multiple.intoto.jsonl`), and a per-archive SBOM (`*.sbom.json`).

**1. Check the artifact digests:**

```sh
sha256sum -c checksums.txt
```

**2. Verify the cosign signature.** Keyless verification *must* pin the signing identity — otherwise it
confirms only that *something* signed the file, not *who*. Replace `v0.1.1` with the tag you downloaded:

```sh
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity 'https://github.com/Growing-Europe/fleeting-plugin-upcloud/.github/workflows/release.yml@refs/tags/v0.1.1' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  checksums.txt
```

**3. Verify the SLSA provenance** with [`slsa-verifier`](https://github.com/slsa-framework/slsa-verifier):

```sh
slsa-verifier verify-artifact fleeting-plugin-upcloud_0.1.1_linux_amd64.tar.gz \
  --provenance-path multiple.intoto.jsonl \
  --source-uri github.com/Growing-Europe/fleeting-plugin-upcloud \
  --source-tag v0.1.1
```

## Configuration

The UpCloud-specific settings go under `[runners.autoscaler.plugin_config]`:

```toml
[runners.autoscaler]
  plugin = "fleeting-plugin-upcloud"
  max_use_count = 1                  # one job per VM, then destroy

  [runners.autoscaler.plugin_config]
    zone     = "de-fra1"             # UpCloud zone
    plan     = "2xCPU-4GB"           # UpCloud server plan
    template = "REPLACE_WITH_TEMPLATE_UUID"
```

### Configuration reference

| Key | Required | Description |
|-----|----------|-------------|
| `zone` | yes | UpCloud zone for the instances. |
| `plan` | yes | UpCloud server plan (size). |
| `template` | yes | Bootable OS image / storage template UUID to clone. |
| `network` | no | Private SDN network UUID to attach. |
| `hostname_prefix` | no | Hostname prefix for created servers (default `fleeting`). |
| `labels` | no | Key/value labels applied to every server. |
| `storage_size_gb` | no | Root storage size in GB. |
| `max_instances` | no | Hard cap on concurrently provisioned servers. |

> **The UpCloud API token is supplied via credentials/environment, never committed.** `config.toml` is
> git-ignored for exactly this reason; copy `config.example.toml` and fill in your own values.

## How it works

The plugin implements the fleeting provider interface (`Init` / `Update` / `Increase` / `Decrease` /
`ConnectInfo`). GitLab Runner's autoscaler decides *when* to scale; the plugin creates and deletes
UpCloud servers and reports their connection info. With `max_use_count = 1`, every server runs exactly
one job and is then destroyed.

Configured networking is **attached** to each server at create time, so the instance actually joins the
network it is meant to be reached on. The dial address reported by `ConnectInfo` is derived from the
server's network interfaces by interface *type* — preferring the private/SDN interface for the internal
address (a private-cloud-network IP is not present in the flattened top-level address list), and the
public interface for the external address.

## Development

```sh
go build ./...
go vet ./...
go test ./...
```

Unit tests require no network or credentials. An optional integration test runs against a real UpCloud
account when credentials are present in the environment, and is skipped otherwise.

## Contributing

This is an **agent-developed** project — the development contract (invariants, build/verify, Definition
of Done) lives in [AGENTS.md](AGENTS.md). The plugin is intentionally **provider-generic**: UpCloud
specifics only, everything site-specific is configuration, no downstream coupling.

## License

[MIT](LICENSE) © Growing Europe
