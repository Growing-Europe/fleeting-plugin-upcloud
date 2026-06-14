// Package group implements the fleeting provider InstanceGroup as a synthetic,
// label-based instance group over UpCloud.
//
// UpCloud has no native instance-group primitive, so the group is *only* the set
// of servers carrying this manager's group label. That label is load-bearing:
// Update rebuilds the entire view from a label-filtered list (no persisted local
// state), which is what makes crash recovery correct — a restarted manager
// rediscovers exactly the servers it owns. Creation applies the label
// atomically, so a server that exists is always discoverable and can never
// become an unowned orphan.
package group

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/hashicorp/go-hclog"
	"gitlab.com/gitlab-org/fleeting/fleeting/provider"

	"github.com/Growing-Europe/fleeting-plugin-upcloud/internal/config"
	"github.com/Growing-Europe/fleeting-plugin-upcloud/internal/ucloud"
)

// groupLabelKey is the label that defines membership + per-manager ownership.
// Its value is the manager's group scope (hostname_prefix); two managers with
// distinct scopes never see or count each other's servers.
const groupLabelKey = "fpu-group"

// stopTimeout bounds the soft stop issued before deletion.
const stopTimeout = 90 * time.Second

// ErrAtCapacity is returned (wrapped) when the group is already at max_instances.
var ErrAtCapacity = errors.New("group at capacity")

// cloud is the slice of the UpCloud client the group depends on. An interface
// keeps the group deterministically testable with a fake — no network, no creds.
type cloud interface {
	Create(ctx context.Context, spec ucloud.ServerSpec) (*ucloud.Server, error)
	ListByLabel(ctx context.Context, key, value string) ([]ucloud.Server, error)
	Get(ctx context.Context, uuid string) (*ucloud.Server, error)
	Stop(ctx context.Context, uuid string, timeout time.Duration) error
	WaitForState(ctx context.Context, uuid, state string) (*ucloud.Server, error)
	Delete(ctx context.Context, uuid string) error
}

// stateStopped is the UpCloud state a server must reach before it can be deleted.
const stateStopped = "stopped"

// InstanceGroup is the fleeting provider.InstanceGroup implementation. It embeds
// Config so the fleeting framework can json-unmarshal plugin_config directly into
// it (snake_case keys map to the embedded fields' json tags).
type InstanceGroup struct {
	config.Config

	client   cloud
	log      hclog.Logger
	settings provider.Settings
	scope    string // group label value (hostname_prefix)
	userData string // resolved cloud-init user-data (inline or from file)
}

// compile-time assertion that we satisfy the full (drift-checked) interface.
var _ provider.InstanceGroup = (*InstanceGroup)(nil)

// New builds an InstanceGroup with an explicit cloud client (used by tests).
// Production wiring (M4) constructs the real ucloud client in Init. The group
// scope is pre-set from the config so the group is usable without Init.
func New(cfg config.Config, c cloud) *InstanceGroup {
	return &InstanceGroup{Config: cfg, client: c, scope: cfg.HostnamePrefix}
}

func randSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b) // crypto/rand.Read never fails on supported platforms
	return hex.EncodeToString(b)
}

func (g *InstanceGroup) groupLabels() map[string]string {
	labels := map[string]string{groupLabelKey: g.scope}
	for k, v := range g.Labels {
		labels[k] = v
	}
	return labels
}

// listOwned returns every server in this manager's group scope. This is the
// single source of truth — all of Update/Increase/Decrease reconcile from it.
func (g *InstanceGroup) listOwned(ctx context.Context) ([]ucloud.Server, error) {
	return g.client.ListByLabel(ctx, groupLabelKey, g.scope)
}
