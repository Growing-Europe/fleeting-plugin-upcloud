package group

import (
	"context"
	"net"
	"strconv"
	"time"

	"gitlab.com/gitlab-org/fleeting/fleeting/provider"

	"github.com/Growing-Europe/fleeting-plugin-upcloud/internal/upcloud"
)

const (
	// sshReadinessProbeTimeout bounds a single TCP readiness probe. It MUST stay
	// short: the probe runs inside the autoscaler's Update reconcile, so a long
	// timeout would re-introduce the very hang this gate exists to prevent.
	sshReadinessProbeTimeout = 3 * time.Second
	// defaultSSHPort is the dial port when the connector config does not set one.
	defaultSSHPort = 22
	// maxConcurrentProbes caps parallel readiness probes so a large fleet of
	// freshly-"started" servers does not serialize behind the per-probe timeout.
	maxConcurrentProbes = 8
)

// tcpProbe is the default dial probe: a bounded TCP connect that reports
// reachability (and nothing else). The caller supplies a deadline via ctx.
func tcpProbe(ctx context.Context, addr string) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

// sshPort returns the port the connector dials: the configured protocol port if
// set, else the SSH default.
func (g *InstanceGroup) sshPort() int {
	if p := g.settings.ProtocolPort; p > 0 {
		return p
	}
	return defaultSSHPort
}

// dialAddress returns the address the connector would dial for srv: the
// internal/SDN address if present (the preferred dial target), else the external
// address. Empty means no reachable address has been resolved yet.
func dialAddress(srv *upcloud.Server) string {
	if srv == nil {
		return ""
	}
	if srv.InternalIP != "" {
		return srv.InternalIP
	}
	return srv.ExternalIP
}

// startedState resolves the provider state for a server UpCloud reports as
// "started". UpCloud "started" is a power state, not a readiness signal: the SDN
// address is not yet configured and sshd not yet listening for some seconds after
// it. Reporting StateRunning then makes the autoscaler dial into a blackhole. So
// we report StateRunning ONLY once the SSH port is reachable on the dial address;
// until then StateCreating, which keeps the instance pending (not dialed) and is
// retried on the next reconcile. The probe is bounded and never errors the
// reconcile — a never-ready server ages out via the normal deleting path.
func (g *InstanceGroup) startedState(ctx context.Context, uuid string) provider.State {
	srv, err := g.client.Get(ctx, uuid)
	if err != nil {
		return provider.StateCreating
	}
	addr := dialAddress(srv)
	if addr == "" {
		return provider.StateCreating
	}
	probe := g.dialProbe
	if probe == nil { // defensive: usable even if constructed without Init
		probe = tcpProbe
	}
	pctx, cancel := context.WithTimeout(ctx, sshReadinessProbeTimeout)
	defer cancel()
	if probe(pctx, net.JoinHostPort(addr, strconv.Itoa(g.sshPort()))) != nil {
		return provider.StateCreating
	}
	return provider.StateRunning
}
