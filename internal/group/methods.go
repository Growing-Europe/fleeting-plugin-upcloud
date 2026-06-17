package group

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/go-hclog"
	"gitlab.com/gitlab-org/fleeting/fleeting/provider"

	"github.com/Growing-Europe/fleeting-plugin-upcloud/internal/upcloud"
)

// Version is reported in ProviderInfo; overridden at build time in M4.
var Version = "dev"

// Init validates configuration, resolves user-data, and builds the UpCloud
// client (token from environment, fail-closed). It reports no suspend/resume
// capability — these VMs are single-use, so the provisioner never suspends them.
func (g *InstanceGroup) Init(_ context.Context, log hclog.Logger, settings provider.Settings) (provider.ProviderInfo, error) {
	g.log = log
	g.settings = settings

	if err := g.Validate(); err != nil {
		return provider.ProviderInfo{}, err
	}
	ud, err := g.ResolveUserData()
	if err != nil {
		return provider.ProviderInfo{}, err
	}
	g.userData = ud
	g.scope = g.HostnamePrefix

	// When the runner does not bring its own credentials, generate an ephemeral
	// SSH key: inject the public half into every server we create and hand the
	// private half to the connector.
	if !g.settings.UseStaticCredentials {
		authKey, privPEM, err := generateSSHKey()
		if err != nil {
			return provider.ProviderInfo{}, err
		}
		g.SSHKeys = append(g.SSHKeys, authKey)
		g.settings.Key = privPEM
	}

	if g.client == nil {
		c, err := upcloud.New()
		if err != nil {
			return provider.ProviderInfo{}, err
		}
		g.client = c
	}

	return provider.ProviderInfo{
		ID:      g.scope,
		MaxSize: g.MaxInstances,
		Version: Version,
	}, nil
}

// Update reconstructs the group view purely from the label-filtered list — the
// only source of truth — and reports each instance's state. This is what makes
// crash recovery correct: a restarted manager rediscovers exactly its servers.
func (g *InstanceGroup) Update(ctx context.Context, fn func(instance string, state provider.State)) error {
	servers, err := g.listOwned(ctx)
	if err != nil {
		return fmt.Errorf("update: list owned: %w", err)
	}
	for i := range servers {
		fn(servers[i].UUID, mapState(servers[i].State))
	}
	return nil
}

// Increase creates up to n servers, each labeled atomically so it is owned the
// instant it exists. It is partial-safe: it returns the number actually created
// and stops at the first error (reporting how far it got). It is fail-closed on
// capacity — it re-lists the live group to honor max_instances even across
// restarts, never exceeding the cap.
func (g *InstanceGroup) Increase(ctx context.Context, n int) (int, error) {
	if n <= 0 {
		return 0, nil
	}
	allowed := n
	if g.MaxInstances > 0 {
		current, err := g.listOwned(ctx)
		if err != nil {
			return 0, fmt.Errorf("increase: capacity check: %w", err)
		}
		room := g.MaxInstances - len(current)
		if room <= 0 {
			return 0, fmt.Errorf("increase: %w (%d/%d)", ErrAtCapacity, len(current), g.MaxInstances)
		}
		if allowed > room {
			allowed = room
		}
	}

	succeeded := 0
	for i := 0; i < allowed; i++ {
		spec := g.buildSpec()
		srv, err := g.client.Create(ctx, spec)
		if err != nil {
			// The server may or may not have been created; if it was, it carries
			// the group label and will be reconciled by the next Update (and the
			// autoscaler can Decrease the excess) — it is never an unowned orphan.
			return succeeded, fmt.Errorf("increase: created %d of %d before error: %w", succeeded, allowed, err)
		}
		g.logf("created instance", "uuid", srv.UUID)
		succeeded++
	}
	return succeeded, nil
}

// Decrease stops then deletes each instance WITH its storage (no orphan disks).
// It is partial-safe: failures are collected and the remaining instances are
// still attempted; the IDs actually removed are returned.
func (g *InstanceGroup) Decrease(ctx context.Context, instances []string) ([]string, error) {
	removed := make([]string, 0, len(instances))
	var errs []error
	for _, id := range instances {
		// UpCloud refuses to delete a running server, and Stop is asynchronous —
		// it only *initiates* the shutdown. We must wait for the server to reach
		// 'stopped' before deleting, or the delete fails with SERVER_STATE_ILLEGAL.
		if err := g.client.Stop(ctx, id, stopTimeout); err != nil {
			g.logf("stop request failed (may already be stopping)", "uuid", id, "error", err.Error())
		}
		if _, err := g.client.WaitForState(ctx, id, stateStopped); err != nil {
			errs = append(errs, fmt.Errorf("decrease %s: wait for stopped: %w", id, err))
			continue
		}
		if err := g.client.Delete(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("decrease %s: %w", id, err))
			continue
		}
		removed = append(removed, id)
	}
	return removed, errors.Join(errs...)
}

// ConnectInfo returns the connection details for an instance, including its
// external and internal addresses fetched from live server details.
func (g *InstanceGroup) ConnectInfo(ctx context.Context, instance string) (provider.ConnectInfo, error) {
	srv, err := g.client.Get(ctx, instance)
	if err != nil {
		return provider.ConnectInfo{}, fmt.Errorf("connect info %s: %w", instance, err)
	}
	info := provider.ConnectInfo{ConnectorConfig: g.settings.ConnectorConfig}
	info.ID = srv.UUID
	info.ExternalAddr = srv.ExternalIP
	info.InternalAddr = srv.InternalIP
	return info, nil
}

// Heartbeat is a no-op: instance health is reflected via Update's state report.
func (g *InstanceGroup) Heartbeat(_ context.Context, _ string) error { return nil }

// Suspend is a no-op success. These VMs are single-use (max_use_count = 1), so
// the group declares no suspend/resume capability and the provisioner never
// calls this in practice; treating it as accepted keeps the contract total.
func (g *InstanceGroup) Suspend(_ context.Context, instances []string) ([]string, error) {
	return instances, nil
}

// Resume is a no-op success — see Suspend.
func (g *InstanceGroup) Resume(_ context.Context, instances []string) ([]string, error) {
	return instances, nil
}

// Shutdown has nothing to clean up: SSH keys are injected inline at create
// (no account-level key object) and the client holds no closable resources.
func (g *InstanceGroup) Shutdown(_ context.Context) error { return nil }

func (g *InstanceGroup) buildSpec() upcloud.ServerSpec {
	host := g.scope + "-" + randSuffix()
	return upcloud.ServerSpec{
		Title:         host,
		Hostname:      host,
		Zone:          g.Zone,
		Plan:          g.Plan,
		Template:      g.Template,
		StorageSizeGB: g.StorageSizeGB,
		Labels:        g.groupLabels(),
		SSHKeys:       g.SSHKeys,
		UserData:      g.userData,
		// Reachability: propagate the configured networking so the server is
		// attached to the SDN (g.Network) the manager dials over its tunnel.
		// Omitting this is what left the fleet utility-only and undialable.
		Network:        g.Network,
		UtilityNetwork: g.UtilityNetwork,
		PublicIPv4:     g.PublicIPv4,
		PublicIPv6:     g.PublicIPv6,
	}
}

func (g *InstanceGroup) logf(msg string, kv ...any) {
	if g.log != nil {
		g.log.Info(msg, kv...)
	}
}

// mapState translates an UpCloud server state into a fleeting provider state.
// error maps to deleting so a broken single-use VM is surfaced for teardown and
// replacement; unknown states are treated conservatively as still creating.
func mapState(upcloudState string) provider.State {
	switch upcloudState {
	case "started":
		return provider.StateRunning
	case "maintenance":
		return provider.StateCreating
	case "stopped":
		return provider.StateDeleting
	case "error":
		return provider.StateDeleting
	default:
		return provider.StateCreating
	}
}
