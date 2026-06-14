package ucloud

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
	details, err := c.api.CreateServer(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ucloud: create server %q: %w", spec.Title, err)
	}
	return serverFromDetails(details), nil
}

// ListByLabel returns every server carrying the given label key=value. This is
// the discovery mechanism for the synthetic, label-based instance group.
func (c *Client) ListByLabel(ctx context.Context, key, value string) ([]Server, error) {
	resp, err := c.api.GetServersWithFilters(ctx, &request.GetServersWithFiltersRequest{
		Filters: []request.QueryFilter{
			request.FilterLabel{Label: upcloud.Label{Key: key, Value: value}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("ucloud: list servers by label %s=%s: %w", key, value, err)
	}
	out := make([]Server, 0, len(resp.Servers))
	for i := range resp.Servers {
		out = append(out, serverFromServer(&resp.Servers[i]))
	}
	return out, nil
}

// Stop requests a hard stop and is a precondition for deletion.
func (c *Client) Stop(ctx context.Context, uuid string, timeout time.Duration) error {
	if _, err := c.api.StopServer(ctx, &request.StopServerRequest{
		UUID:     uuid,
		StopType: request.ServerStopTypeHard,
		Timeout:  timeout,
	}); err != nil {
		return fmt.Errorf("ucloud: stop server %s: %w", uuid, err)
	}
	return nil
}

// WaitForState blocks until the server reaches desiredState or ctx is done.
func (c *Client) WaitForState(ctx context.Context, uuid, desiredState string) (*Server, error) {
	details, err := c.api.WaitForServerState(ctx, &request.WaitForServerStateRequest{
		UUID:         uuid,
		DesiredState: desiredState,
	})
	if err != nil {
		return nil, fmt.Errorf("ucloud: wait for server %s state %q: %w", uuid, desiredState, err)
	}
	return serverFromDetails(details), nil
}

// Get returns full details for one server, including connection IPs.
func (c *Client) Get(ctx context.Context, uuid string) (*Server, error) {
	details, err := c.api.GetServerDetails(ctx, &request.GetServerDetailsRequest{UUID: uuid})
	if err != nil {
		return nil, fmt.Errorf("ucloud: get server %s: %w", uuid, err)
	}
	return serverFromDetails(details), nil
}

// Delete removes a server AND its attached storages. Using the
// delete-with-storages call is mandatory: deleting only the server leaves the
// cloned OS disk behind, which bills silently.
func (c *Client) Delete(ctx context.Context, uuid string) error {
	if err := c.api.DeleteServerAndStorages(ctx, &request.DeleteServerAndStoragesRequest{
		UUID: uuid,
	}); err != nil {
		return fmt.Errorf("ucloud: delete server+storages %s: %w", uuid, err)
	}
	return nil
}

// PublicTemplates lists public storage templates (e.g. OS images).
//
// Guard: the SDK builds GetStorages' URL as /storage/{access}/{type}, so setting
// BOTH Access and Type yields the invalid path /storage/public/template → 404.
// We therefore filter by Type only and select public templates client-side.
func (c *Client) PublicTemplates(ctx context.Context) ([]upcloud.Storage, error) {
	resp, err := c.api.GetStorages(ctx, &request.GetStoragesRequest{Type: upcloud.StorageTypeTemplate})
	if err != nil {
		return nil, fmt.Errorf("ucloud: list templates: %w", err)
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
	s.ExternalIP, s.InternalIP = pickIPs(d.IPAddresses)
	return &s
}

// pickIPs selects the first public IPv4 (external) and the first utility or
// private IPv4 (internal) from a server's addresses.
func pickIPs(addrs upcloud.IPAddressSlice) (external, internal string) {
	for _, ip := range addrs {
		if ip.Family != upcloud.IPAddressFamilyIPv4 {
			continue
		}
		switch ip.Access {
		case upcloud.IPAddressAccessPublic:
			if external == "" {
				external = ip.Address
			}
		case upcloud.IPAddressAccessUtility, upcloud.IPAddressAccessPrivate:
			if internal == "" {
				internal = ip.Address
			}
		}
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
