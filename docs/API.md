# Stable API / backward-compatibility contract (v0.2)

This plugin is consumed as a **binary** GitLab Runner fleeting provider (over gRPC),
so the stable contract is **not** the Go types — it is the configuration keys, the
connector configuration, the provider behavior, and the runtime environment described
here. Within a `v0.2.x` series these are kept backward-compatible: keys are not renamed
or removed, and behavior is not changed in a way that breaks a working configuration.
Adding a new optional key is backward-compatible; removing or renaming one is not and
is reserved for a minor version bump with migration notes in [CHANGELOG.md](../CHANGELOG.md).

## 1. `plugin_config` keys

Set under `[runners.autoscaler.plugin_config]` (see [`config.example.toml`](../config.example.toml)).

| Key | Type | Required | Meaning |
|-----|------|----------|---------|
| `zone` | string | yes | UpCloud zone for the instances. |
| `plan` | string | yes | UpCloud server plan (size). |
| `template` | string | yes | Bootable OS storage-template UUID to clone. |
| `allowed_zones` | []string | no | If set, `zone` must be one of these — validation fails otherwise. |
| `hostname_prefix` | string | no | Hostname prefix for created servers (default `fleeting`). |
| `labels` | map[string]string | no | Labels applied to every server. |
| `storage_size_gb` | int | no | Root storage size in GB. |
| `max_instances` | int | no | Hard cap on concurrent servers (`0` = no plugin-side cap; account quota governs). |
| `network` | string | no | Private/SDN network UUID to attach (the preferred dial target). |
| `utility_network` | bool | no | Attach the zone's utility (SDN-backed private) network. |
| `public_ipv4` | bool | no | Assign a public IPv4. |
| `public_ipv6` | bool | no | Assign a public IPv6. |
| `ssh_keys` | []string | no | Authorized public keys injected into created servers. |
| `user_data` | string | no | cloud-init user-data. |
| `user_data_file` | string | no | Path to a cloud-init user-data file. |

**Validation invariant:** at least one reachable networking path must be configured —
i.e. one of `network`, `utility_network`, `public_ipv4`, or `public_ipv6`. A config
with none fails validation rather than producing an undialable server.

## 2. `connector_config`

Standard fleeting connector configuration (`username`, `use_static_credentials`, …).
When no static credentials are configured, the plugin generates an ephemeral SSH
keypair per group, injects the public key into created servers, and returns the private
key via `ConnectInfo` for the runner's connector to dial with.

## 3. Provider behavior

The plugin implements the fleeting `provider.InstanceGroup` interface (`Init`, `Update`,
`Increase`, `Decrease`, `ConnectInfo`, `Heartbeat`, `Suspend`, `Resume`, `Shutdown`).
With `max_use_count = 1`, every server runs exactly one job and is then destroyed;
`Suspend`/`Resume`/`Heartbeat`/`Shutdown` are no-ops and `CapabilitySuspendResume` is
not advertised.

**Dial-address contract (`ConnectInfo`):** the external address is taken from the public
interface; the internal (dial) address is derived from the server's network interfaces
classified by interface *type*, preferring the private/SDN interface (a private-cloud
address is not present in the flattened top-level address list), falling back to utility.

**Readiness gate (`StateRunning`):** a server is reported `Running` only once its SSH port is
reachable on the dial address; until then it is reported `Creating`. This prevents the autoscaler
from dialing during the window after UpCloud reports the server `started` but before its network is
configured and `sshd` is listening.

> **Operational dependency — set an autoscaler instance creation/readiness timeout.** Because the
> plugin reports `Creating` until the dial port is reachable, a server that never becomes reachable
> (e.g. a broken image) stays `Creating` indefinitely from the plugin's side — the plugin does **not**
> unilaterally delete an instance the autoscaler is still waiting on. Such an instance is reaped only
> when the **GitLab Runner autoscaler's** instance creation/readiness timeout expires and asks the
> plugin to remove it (the plugin then deletes the server **and** its storage). Operators **must**
> configure a sane creation timeout, or a never-ready server could bill until removed manually.

## 4. Environment

The UpCloud API token is read from the environment (`UPCLOUD_TOKEN`, a `ucat_` bearer
token) only — never from configuration or source. A missing/empty token is a hard error
at construction (fail-closed), not a silent default.

## Compatibility notes

- The networking keys `network` / `utility_network` / `public_ipv4` / `public_ipv6` are
  the stable reachability contract for `v0.2.x`.
- Unknown keys are rejected at parse time (strict decoding), so a removed key in a config
  file is surfaced as an error rather than silently ignored.
