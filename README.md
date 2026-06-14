# fleeting-plugin-upcloud

> GitLab Runner fleeting plugin for UpCloud — autoscaled, ephemeral VM-per-job CI on EU-sovereign infrastructure.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A [GitLab Runner fleeting](https://gitlab.com/gitlab-org/fleeting/fleeting) provider plugin that
provisions and autoscales [UpCloud](https://upcloud.com) cloud servers as ephemeral CI runners —
**one isolated VM per job**. It plugs into the GitLab Runner
[Instance / Docker Autoscaler executor](https://docs.gitlab.com/runner/executors/docker_autoscaler/),
the official successor to the deprecated `docker-machine` autoscaler.

> 🚧 **Status: early development.** Repository scaffolding is in place; the plugin implementation is in
> progress and **not yet functional**. Issues and stars welcome — production use is not yet supported.

## Why

- **VM-per-job isolation.** Each CI job runs in its own ephemeral UpCloud server (`max_use_count = 1`),
  then the server is destroyed — the isolation model hosted CI uses for untrusted code.
- **EU-sovereign.** Runs entirely on UpCloud's EU infrastructure.
- **Autoscaling.** The GitLab Runner manager scales the fleet up and down on demand.

## Requirements

- GitLab Runner with the Instance or Docker Autoscaler executor (fleeting).
- An UpCloud account and an API token (`ucat_…`).
- A bootable OS image / storage template for the runner instances.

## Installation

Download a release binary from the [Releases](../../releases) page (or `go install`), then reference it
from your runner configuration — see [`config.example.toml`](config.example.toml).

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
