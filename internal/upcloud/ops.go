package upcloud

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/UpCloudLtd/upcloud-go-api/v8/upcloud"
	"github.com/UpCloudLtd/upcloud-go-api/v8/upcloud/request"
)

// tokenFromEnv returns the UpCloud API token from the environment, matching the
// variable the SDK reads (UPCLOUD_TOKEN). Kept as a seam so construction can
// fail closed before handing off to the SDK.
func tokenFromEnv() string { return os.Getenv("UPCLOUD_TOKEN") }

// Create provisions a server from spec and returns its provider-generic view.
// The OS disk is cloned from spec.Template; labels are attached so the server is
// discoverable as a member of the synthetic instance group.
func (c *Client) Create(ctx context.Context, spec ServerSpec) (*Server, error) {
	req := &request.CreateServerRequest{
		Title:    spec.Title,
		Hostname: spec.Hostname,
		Zone:     spec.Zone,
		Plan:     spec.Plan,
		Metadata: upcloud.True,
		Labels:   toLabelSlice(spec.Labels),
		StorageDevices: request.CreateServerStorageDeviceSlice{{
			Action:  request.CreateServerStorageDeviceActionClone,
			Storage: spec.Template,
			Title:   spec.Title + "-os",
			Size:    spec.StorageSizeGB,
			Tier:    upcloud.StorageTierMaxIOPS,
		}},
	}
	if len(spec.SSHKeys) > 0 || spec.UserData != "" {
		req.LoginUser = &request.LoginUser{
			CreatePassword: "no",
			SSHKeys:        spec.SSHKeys,
		}
		req.UserData = spec.UserData
	}
	// Attach the configured interfaces EXPLICITLY. Without a Networking block,
	// UpCloud falls back to default interfaces (public + utility) and never joins
	// the private SDN, so a manager reaching the fleet only over the private network
	// can never dial the instance (the connector hangs on the unreachable utility
	// address). The private SDN interface is what makes the runner reachable.
	if ifaces := networkInterfaces(spec); len(ifaces) > 0 {
		req.Networking = &request.CreateServerNetworking{Interfaces: ifaces}
	}
	var details *upcloud.ServerDetails
	attempt := 0
	_, err := withRetry(ctx, c.retry, func() (struct{}, error) {
		attempt++
		if attempt > 1 {
			// A previous attempt may have created the server before the transient
			// failure surfaced (e.g. a timeout after the API accepted the request).
			// Reconcile by the unique title before re-creating, so a retried create
			// adopts the existing server instead of orphaning a duplicate.
			if adopted, ferr := c.findByLabelsAndTitle(ctx, spec.Labels, spec.Title); ferr == nil && adopted != nil {
				details = adopted
				return struct{}{}, nil
			}
		}
		d, cerr := c.api.CreateServer(ctx, req)
		if cerr != nil {
			return struct{}{}, cerr
		}
		details = d
		return struct{}{}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("upcloud: create server %q: %w", spec.Title, err)
	}
	return serverFromDetails(details), nil
}

// findByLabelsAndTitle looks for an existing server carrying ALL of labels with
// the given (unique) title and returns its full details, or (nil, nil) if none
// matches. It is the idempotency probe for a retried Create: the per-instance
// title is unique, so a match means a prior attempt already succeeded.
func (c *Client) findByLabelsAndTitle(ctx context.Context, labels map[string]string, title string) (*upcloud.ServerDetails, error) {
	filters := make([]request.QueryFilter, 0, len(labels))
	for k, v := range labels {
		filters = append(filters, request.FilterLabel{Label: upcloud.Label{Key: k, Value: v}})
	}
	resp, err := c.api.GetServersWithFilters(ctx, &request.GetServersWithFiltersRequest{Filters: filters})
	if err != nil {
		return nil, err
	}
	for i := range resp.Servers {
		if resp.Servers[i].Title == title {
			return c.api.GetServerDetails(ctx, &request.GetServerDetailsRequest{UUID: resp.Servers[i].UUID})
		}
	}
	return nil, nil
}

// networkInterfaces translates the spec's networking selection into explicit
// UpCloud create interfaces. The private SDN interface (when Network is set) is
// the load-bearing one: it puts the server on the private network the manager
// reaches it over. Order is private, utility, public so the SDN/private address
// is the server's primary internal address.
func networkInterfaces(spec ServerSpec) request.CreateServerInterfaceSlice {
	var ifaces request.CreateServerInterfaceSlice
	if spec.Network != "" {
		ifaces = append(ifaces, request.CreateServerInterface{
			Type:        upcloud.NetworkTypePrivate,
			Network:     spec.Network,
			IPAddresses: request.CreateServerIPAddressSlice{{Family: upcloud.IPAddressFamilyIPv4}},
		})
	}
	if spec.UtilityNetwork {
		ifaces = append(ifaces, request.CreateServerInterface{
			Type:        upcloud.NetworkTypeUtility,
			IPAddresses: request.CreateServerIPAddressSlice{{Family: upcloud.IPAddressFamilyIPv4}},
		})
	}
	if spec.PublicIPv4 {
		ifaces = append(ifaces, request.CreateServerInterface{
			Type:        upcloud.NetworkTypePublic,
			IPAddresses: request.CreateServerIPAddressSlice{{Family: upcloud.IPAddressFamilyIPv4}},
		})
	}
	if spec.PublicIPv6 {
		ifaces = append(ifaces, request.CreateServerInterface{
			Type:        upcloud.NetworkTypePublic,
			IPAddresses: request.CreateServerIPAddressSlice{{Family: upcloud.IPAddressFamilyIPv6}},
		})
	}
	return ifaces
}

// ListByLabel returns every server carrying the given label key=value. This is
// the discovery mechanism for the synthetic, label-based instance group.
func (c *Client) ListByLabel(ctx context.Context, key, value string) ([]Server, error) {
	resp, err := withRetry(ctx, c.retry, func() (*upcloud.Servers, error) {
		return c.api.GetServersWithFilters(ctx, &request.GetServersWithFiltersRequest{
			Filters: []request.QueryFilter{
				request.FilterLabel{Label: upcloud.Label{Key: key, Value: value}},
			},
		})
	})
	if err != nil {
		return nil, fmt.Errorf("upcloud: list servers by label %s=%s: %w", key, value, err)
	}
	out := make([]Server, 0, len(resp.Servers))
	for i := range resp.Servers {
		out = append(out, serverFromServer(&resp.Servers[i]))
	}
	return out, nil
}

// Stop requests a hard stop and is a precondition for deletion.
func (c *Client) Stop(ctx context.Context, uuid string, timeout time.Duration) error {
	if err := retryErr(ctx, c.retry, func() error {
		_, e := c.api.StopServer(ctx, &request.StopServerRequest{
			UUID:     uuid,
			StopType: request.ServerStopTypeHard,
			Timeout:  timeout,
		})
		return e
	}); err != nil {
		return fmt.Errorf("upcloud: stop server %s: %w", uuid, err)
	}
	return nil
}

// WaitForState blocks until the server reaches desiredState or ctx is done.
//
// This is deliberately NOT wrapped in withRetry: WaitForServerState already
// polls internally until the state is reached or its deadline, so retrying it
// would restart the wait rather than ride out a blip. The transient-error
// resilience lives on the surrounding mutating calls (Stop, Delete), which is
// the "wrap the Stop->Wait->Delete sequence, do not replace the wait" intent.
func (c *Client) WaitForState(ctx context.Context, uuid, desiredState string) (*Server, error) {
	details, err := c.api.WaitForServerState(ctx, &request.WaitForServerStateRequest{
		UUID:         uuid,
		DesiredState: desiredState,
	})
	if err != nil {
		return nil, fmt.Errorf("upcloud: wait for server %s state %q: %w", uuid, desiredState, err)
	}
	return serverFromDetails(details), nil
}

// Get returns full details for one server, including connection IPs.
func (c *Client) Get(ctx context.Context, uuid string) (*Server, error) {
	details, err := withRetry(ctx, c.retry, func() (*upcloud.ServerDetails, error) {
		return c.api.GetServerDetails(ctx, &request.GetServerDetailsRequest{UUID: uuid})
	})
	if err != nil {
		return nil, fmt.Errorf("upcloud: get server %s: %w", uuid, err)
	}
	return serverFromDetails(details), nil
}

// Delete removes a server AND its attached storages. Using the
// delete-with-storages call is mandatory: deleting only the server leaves the
// cloned OS disk behind, which bills silently.
func (c *Client) Delete(ctx context.Context, uuid string) error {
	if err := retryErr(ctx, c.retry, func() error {
		return c.api.DeleteServerAndStorages(ctx, &request.DeleteServerAndStoragesRequest{
			UUID: uuid,
		})
	}); err != nil {
		return fmt.Errorf("upcloud: delete server+storages %s: %w", uuid, err)
	}
	return nil
}

// PublicTemplates lists public storage templates (e.g. OS images).
//
// Guard: the SDK builds GetStorages' URL as /storage/{access}/{type}, so setting
// BOTH Access and Type yields the invalid path /storage/public/template → 404.
// We therefore filter by Type only and select public templates client-side.
func (c *Client) PublicTemplates(ctx context.Context) ([]upcloud.Storage, error) {
	resp, err := withRetry(ctx, c.retry, func() (*upcloud.Storages, error) {
		return c.api.GetStorages(ctx, &request.GetStoragesRequest{Type: upcloud.StorageTypeTemplate})
	})
	if err != nil {
		return nil, fmt.Errorf("upcloud: list templates: %w", err)
	}
	out := make([]upcloud.Storage, 0, len(resp.Storages))
	for _, s := range resp.Storages {
		if s.Access == upcloud.StorageAccessPublic {
			out = append(out, s)
		}
	}
	return out, nil
}

func toLabelSlice(m map[string]string) *upcloud.LabelSlice {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic order for stable requests/tests
	ls := make(upcloud.LabelSlice, 0, len(m))
	for _, k := range keys {
		ls = append(ls, upcloud.Label{Key: k, Value: m[k]})
	}
	return &ls
}

func labelsToMap(ls upcloud.LabelSlice) map[string]string {
	if len(ls) == 0 {
		return nil
	}
	m := make(map[string]string, len(ls))
	for _, l := range ls {
		m[l.Key] = l.Value
	}
	return m
}

// serverFromDetails maps a full ServerDetails (returned by create/wait/get),
// which — unlike a plain Server list item — carries labels and IP addresses.
func serverFromDetails(d *upcloud.ServerDetails) *Server {
	s := serverFromServer(&d.Server)
	s.Labels = labelsToMap(d.Labels)
	// Derive the dial addresses from the INTERFACES, not the top-level
	// ServerDetails.IPAddresses: UpCloud does NOT aggregate SDN/private-cloud-network
	// IPs into the flattened top-level list (it is empty for an SDN-only VM), and an
	// SDN interface IP carries an empty Access field — so classifying by the IP's
	// Access (as pickIPs does) misses it entirely and leaves InternalIP empty, which
	// makes the connector dial an empty address and hang. Classify by interface TYPE.
	s.ExternalIP, s.InternalIP = pickIPsFromInterfaces(d.Networking)
	// Defensive fallback to the flattened list for any response shape where the
	// interface networking is absent but the top-level list is populated.
	if s.ExternalIP == "" && s.InternalIP == "" {
		s.ExternalIP, s.InternalIP = pickIPs(d.IPAddresses)
	}
	return &s
}

// pickIPsFromInterfaces selects the external (public) and internal (dial) IPv4
// from the server's network INTERFACES, classifying by interface Type (the
// reliable signal — an SDN/private interface's IP Access is empty). Internal
// PREFERS the private/SDN address (the manager reaches the fleet only over the
// private network); utility is a fallback only when no private interface is attached.
func pickIPsFromInterfaces(n upcloud.ServerNetworking) (external, internal string) {
	var private, utility string
	for _, iface := range n.Interfaces {
		for _, ip := range iface.IPAddresses {
			if ip.Family != upcloud.IPAddressFamilyIPv4 {
				continue
			}
			switch iface.Type {
			case upcloud.NetworkTypePublic:
				if external == "" {
					external = ip.Address
				}
			case upcloud.NetworkTypePrivate:
				if private == "" {
					private = ip.Address
				}
			case upcloud.NetworkTypeUtility:
				if utility == "" {
					utility = ip.Address
				}
			}
		}
	}
	if private != "" {
		internal = private
	} else {
		internal = utility
	}
	return external, internal
}

// pickIPs selects the first public IPv4 (external) and the internal IPv4 the
// manager dials (internal). The manager reaches the fleet ONLY over the private
// network, so a PRIVATE address is PREFERRED for internal; utility is
// only a fallback when no private address is attached. This preference is
// independent of UpCloud's (undocumented) address ordering — a utility address
// listed before a private one must NOT win, or the connector dials the
// unreachable utility IP and hangs. See TestPickIPs_PrefersPrivateOverUtility.
func pickIPs(addrs upcloud.IPAddressSlice) (external, internal string) {
	var private, utility string
	for _, ip := range addrs {
		if ip.Family != upcloud.IPAddressFamilyIPv4 {
			continue
		}
		switch ip.Access {
		case upcloud.IPAddressAccessPublic:
			if external == "" {
				external = ip.Address
			}
		case upcloud.IPAddressAccessPrivate:
			if private == "" {
				private = ip.Address
			}
		case upcloud.IPAddressAccessUtility:
			if utility == "" {
				utility = ip.Address
			}
		}
	}
	if private != "" {
		internal = private
	} else {
		internal = utility
	}
	return external, internal
}

// serverFromServer maps a list item. Labels are NOT present on the list
// representation (only on ServerDetails), so callers that need them must fetch
// details per server.
func serverFromServer(s *upcloud.Server) Server {
	return Server{
		UUID:  s.UUID,
		Title: s.Title,
		State: s.State,
		Zone:  s.Zone,
	}
}
