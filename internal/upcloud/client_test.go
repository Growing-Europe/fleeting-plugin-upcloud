package upcloud

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/UpCloudLtd/upcloud-go-api/v8/upcloud"
	"github.com/UpCloudLtd/upcloud-go-api/v8/upcloud/request"
)

// fakeAPI is a hand-rolled serverAPI double. It records the last request seen by
// each method and returns the configured result/error, so tests can assert both
// the request the wrapper built and the mapping of the response.
type fakeAPI struct {
	createResp *upcloud.ServerDetails
	listResp   *upcloud.Servers
	waitResp   *upcloud.ServerDetails
	stopResp   *upcloud.ServerDetails
	storResp   *upcloud.Storages
	err        error

	createReq *request.CreateServerRequest
	listReq   *request.GetServersWithFiltersRequest
	stopReq   *request.StopServerRequest
	waitReq   *request.WaitForServerStateRequest
	delReq    *request.DeleteServerAndStoragesRequest
	storReq   *request.GetStoragesRequest
}

func (f *fakeAPI) CreateServer(_ context.Context, r *request.CreateServerRequest) (*upcloud.ServerDetails, error) {
	f.createReq = r
	return f.createResp, f.err
}

func (f *fakeAPI) GetServersWithFilters(_ context.Context, r *request.GetServersWithFiltersRequest) (*upcloud.Servers, error) {
	f.listReq = r
	return f.listResp, f.err
}

func (f *fakeAPI) GetServerDetails(_ context.Context, _ *request.GetServerDetailsRequest) (*upcloud.ServerDetails, error) {
	return f.createResp, f.err
}

func (f *fakeAPI) StopServer(_ context.Context, r *request.StopServerRequest) (*upcloud.ServerDetails, error) {
	f.stopReq = r
	return f.stopResp, f.err
}

func (f *fakeAPI) WaitForServerState(_ context.Context, r *request.WaitForServerStateRequest) (*upcloud.ServerDetails, error) {
	f.waitReq = r
	return f.waitResp, f.err
}

func (f *fakeAPI) DeleteServerAndStorages(_ context.Context, r *request.DeleteServerAndStoragesRequest) error {
	f.delReq = r
	return f.err
}

func (f *fakeAPI) GetStorages(_ context.Context, r *request.GetStoragesRequest) (*upcloud.Storages, error) {
	f.storReq = r
	return f.storResp, f.err
}

func detailsWith(uuid, state string, labels ...upcloud.Label) *upcloud.ServerDetails {
	d := &upcloud.ServerDetails{}
	d.UUID = uuid
	d.Title = "t-" + uuid
	d.State = state
	d.Zone = "zone-x"
	d.Labels = labels
	return d
}

func TestNew_FailsClosedWithoutToken(t *testing.T) {
	t.Setenv("UPCLOUD_TOKEN", "")
	if _, err := New(); !errors.Is(err, ErrNoToken) {
		t.Fatalf("want ErrNoToken, got %v", err)
	}
}

func TestNew_WithTokenBuilds(t *testing.T) {
	t.Setenv("UPCLOUD_TOKEN", "ucat_dummy")
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c == nil || c.api == nil {
		t.Fatal("New returned a client without an api")
	}
}

func TestNew_WrapsSDKConstructionError(t *testing.T) {
	// Token passes our fail-closed guard, but providing basic-auth env too makes
	// the SDK's NewFromEnv reject ("only one authentication method") — exercise
	// the error-wrapping branch.
	t.Setenv("UPCLOUD_TOKEN", "ucat_dummy")
	t.Setenv("UPCLOUD_USERNAME", "user")
	t.Setenv("UPCLOUD_PASSWORD", "pass")
	if _, err := New(); err == nil {
		t.Fatal("want error when both token and basic-auth env are set")
	}
}

func TestNew_AcceptsHTTPClientOption(t *testing.T) {
	t.Setenv("UPCLOUD_TOKEN", "ucat_dummy")
	if HTTPClient(nil) == nil {
		t.Fatal("HTTPClient returned a nil ConfigFn")
	}
	if _, err := New(HTTPClient(&http.Client{})); err != nil {
		t.Fatalf("New with HTTPClient option: %v", err)
	}
}

func TestCreate_BuildsRequestAndMaps(t *testing.T) {
	f := &fakeAPI{createResp: detailsWith("uuid-1", "maintenance", upcloud.Label{Key: "g", Value: "v"})}
	c := newWithAPI(f)

	got, err := c.Create(context.Background(), ServerSpec{
		Title: "srv", Hostname: "srv", Zone: "z1", Plan: "1xCPU-1GB",
		Template: "tmpl-uuid", StorageSizeGB: 25,
		Labels:  map[string]string{"b": "2", "a": "1"},
		SSHKeys: []string{"ssh-ed25519 AAAA"}, UserData: "#cloud-config",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.UUID != "uuid-1" || got.State != "maintenance" {
		t.Errorf("bad mapping: %+v", got)
	}
	if got.Labels["g"] != "v" {
		t.Errorf("labels not mapped from details: %+v", got.Labels)
	}
	// request assertions
	r := f.createReq
	if r.Zone != "z1" || r.Plan != "1xCPU-1GB" || r.Metadata != upcloud.True {
		t.Errorf("bad create request: %+v", r)
	}
	if len(r.StorageDevices) != 1 || r.StorageDevices[0].Storage != "tmpl-uuid" || r.StorageDevices[0].Size != 25 {
		t.Errorf("bad storage device: %+v", r.StorageDevices)
	}
	if r.StorageDevices[0].Action != request.CreateServerStorageDeviceActionClone {
		t.Errorf("template must be cloned, got %q", r.StorageDevices[0].Action)
	}
	// labels sorted deterministically: a before b
	if r.Labels == nil || (*r.Labels)[0].Key != "a" || (*r.Labels)[1].Key != "b" {
		t.Errorf("labels not deterministically sorted: %+v", r.Labels)
	}
	if r.LoginUser == nil || len(r.LoginUser.SSHKeys) != 1 || r.UserData != "#cloud-config" {
		t.Errorf("login/user-data not wired: %+v", r.LoginUser)
	}
}

// TestCreate_AttachesConfiguredNetworking is the reachability regression guard.
// A server with a configured private SDN MUST be created with an explicit
// private interface on that network. Without it UpCloud attaches only the
// default interfaces (public + utility) and the instance never joins the SDN —
// so a manager that reaches the fleet only over that private network can never
// dial it (the docker-autoscaler connector hangs on the unreachable address and
// the job system-fails in prepare). This test FAILS on the pre-fix code (no
// Networking block) and passes once Create() attaches the interfaces.
func TestCreate_AttachesConfiguredNetworking(t *testing.T) {
	f := &fakeAPI{createResp: detailsWith("u", "maintenance")}
	c := newWithAPI(f)

	if _, err := c.Create(context.Background(), ServerSpec{
		Title: "srv", Hostname: "srv", Zone: "z1", Plan: "1xCPU-1GB",
		Template: "tmpl-uuid", StorageSizeGB: 25,
		Network: "sdn-uuid", UtilityNetwork: true,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	r := f.createReq
	if r.Networking == nil {
		t.Fatal("CreateServerRequest.Networking is nil — server would get DEFAULT interfaces (public+utility), not the configured SDN; this is the fleet-wide dial-hang regression")
	}
	var hasPrivate, hasUtility bool
	for _, i := range r.Networking.Interfaces {
		if i.Type == upcloud.NetworkTypePrivate && i.Network == "sdn-uuid" {
			hasPrivate = true
			if len(i.IPAddresses) == 0 || i.IPAddresses[0].Family != upcloud.IPAddressFamilyIPv4 {
				t.Errorf("private SDN interface missing IPv4 address request: %+v", i)
			}
		}
		if i.Type == upcloud.NetworkTypeUtility {
			hasUtility = true
		}
	}
	if !hasPrivate {
		t.Error("no private interface attached to the configured SDN network 'sdn-uuid' — server unreachable over the private network")
	}
	if !hasUtility {
		t.Error("utility_network=true but no utility interface attached")
	}
}

// TestPickIPs_PrefersPrivateOverUtility is the dial-SELECTION guard (distinct
// from the attach guard). The manager reaches the fleet ONLY over the private
// SDN, so srv.InternalIP — what the connector dials — MUST be the private/SDN
// address whenever one exists, regardless of UpCloud's (undocumented) address
// ordering. The WRONG candidate (utility, unreachable) is deliberately listed
// FIRST: pre-fix pickIPs (first utility-or-private wins) returns the utility IP
// and this FAILS; with the private-preference it returns the SDN IP and passes.
func TestPickIPs_PrefersPrivateOverUtility(t *testing.T) {
	// utility deliberately FIRST, private SECOND — the ordering that broke it.
	addrs := upcloud.IPAddressSlice{
		{Access: upcloud.IPAddressAccessUtility, Family: upcloud.IPAddressFamilyIPv4, Address: "10.5.4.41"},
		{Access: upcloud.IPAddressAccessPrivate, Family: upcloud.IPAddressFamilyIPv4, Address: "10.20.0.2"},
		{Access: upcloud.IPAddressAccessPublic, Family: upcloud.IPAddressFamilyIPv4, Address: "203.0.113.57"},
	}
	external, internal := pickIPs(addrs)
	if internal != "10.20.0.2" {
		t.Errorf("internal must be the PRIVATE/SDN address (10.20.0.2), got %q — connector would dial the unreachable utility IP and hang", internal)
	}
	if external != "203.0.113.57" {
		t.Errorf("external must be the public address, got %q", external)
	}

	// Fallback: with NO private address, internal falls back to utility.
	fb := upcloud.IPAddressSlice{
		{Access: upcloud.IPAddressAccessUtility, Family: upcloud.IPAddressFamilyIPv4, Address: "10.5.9.9"},
	}
	if _, internal := pickIPs(fb); internal != "10.5.9.9" {
		t.Errorf("with no private address, internal must fall back to utility, got %q", internal)
	}
}

// TestServerFromDetails_DerivesSDNInternalIPFromInterface is the SDN dial guard.
// Models the UpCloud ServerDetails shape for an SDN-attached VM: the top-level ip_addresses list is
// EMPTY (UpCloud does not aggregate private-cloud-network IPs there) and the SDN IP appears ONLY under
// a private-type interface with an EMPTY Access field. So deriving InternalIP from the top-level list
// (or classifying by IP Access) yields "" -> the connector dials an empty address and hangs.
// InternalIP MUST be derived from the interfaces, classified by interface TYPE, preferring the
// private/SDN interface. RED pre-fix (serverFromDetails used pickIPs(top-level)=empty -> "").
func TestServerFromDetails_DerivesSDNInternalIPFromInterface(t *testing.T) {
	d := &upcloud.ServerDetails{}
	d.UUID = "sdn-vm"
	d.State = "started"
	// Top-level flattened list is EMPTY for an SDN-only VM (the real shape).
	d.IPAddresses = upcloud.IPAddressSlice{}
	// The SDN IP lives ONLY under the interface, with an EMPTY Access field; utility listed too.
	d.Networking = upcloud.ServerNetworking{Interfaces: upcloud.ServerInterfaceSlice{
		{Type: upcloud.NetworkTypeUtility, IPAddresses: upcloud.IPAddressSlice{
			{Family: upcloud.IPAddressFamilyIPv4, Access: "utility", Address: "10.5.0.9"},
		}},
		{Type: upcloud.NetworkTypePrivate, IPAddresses: upcloud.IPAddressSlice{
			{Family: upcloud.IPAddressFamilyIPv4, Access: "", Address: "10.20.0.2"},
		}},
	}}
	s := serverFromDetails(d)
	if s.InternalIP != "10.20.0.2" {
		t.Errorf("InternalIP must be the SDN/private interface addr 10.20.0.2 (derived from interfaces, private-preferred), got %q — the connector would dial an empty/utility address and hang", s.InternalIP)
	}

	// Public-only interface -> external set; private absent -> internal falls back to utility.
	d2 := &upcloud.ServerDetails{}
	d2.IPAddresses = upcloud.IPAddressSlice{}
	d2.Networking = upcloud.ServerNetworking{Interfaces: upcloud.ServerInterfaceSlice{
		{Type: upcloud.NetworkTypePublic, IPAddresses: upcloud.IPAddressSlice{{Family: upcloud.IPAddressFamilyIPv4, Access: "public", Address: "203.0.113.94"}}},
		{Type: upcloud.NetworkTypeUtility, IPAddresses: upcloud.IPAddressSlice{{Family: upcloud.IPAddressFamilyIPv4, Access: "utility", Address: "10.5.0.9"}}},
	}}
	s2 := serverFromDetails(d2)
	if s2.ExternalIP != "203.0.113.94" || s2.InternalIP != "10.5.0.9" {
		t.Errorf("expected external=203.0.113.94 internal(utility-fallback)=10.5.0.9, got external=%q internal=%q", s2.ExternalIP, s2.InternalIP)
	}
}

func TestCreate_NoLoginUserWhenNoKeysOrUserData(t *testing.T) {
	f := &fakeAPI{createResp: detailsWith("u", "started")}
	c := newWithAPI(f)
	if _, err := c.Create(context.Background(), ServerSpec{Title: "x", Zone: "z", Plan: "p", Template: "t", StorageSizeGB: 10}); err != nil {
		t.Fatal(err)
	}
	if f.createReq.LoginUser != nil {
		t.Errorf("LoginUser should be nil when no ssh keys / user-data")
	}
	if f.createReq.Labels != nil {
		t.Errorf("Labels should be nil when none supplied")
	}
}

func TestListByLabel_BuildsFilterAndMaps(t *testing.T) {
	f := &fakeAPI{listResp: &upcloud.Servers{Servers: []upcloud.Server{
		{UUID: "a", Title: "ta", State: "started", Zone: "z"},
		{UUID: "b", Title: "tb", State: "stopped", Zone: "z"},
	}}}
	c := newWithAPI(f)
	got, err := c.ListByLabel(context.Background(), "grp", "g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].UUID != "a" || got[1].State != "stopped" {
		t.Errorf("bad list mapping: %+v", got)
	}
	if len(f.listReq.Filters) != 1 {
		t.Fatalf("want 1 filter, got %d", len(f.listReq.Filters))
	}
	fl, ok := f.listReq.Filters[0].(request.FilterLabel)
	if !ok || fl.Key != "grp" || fl.Value != "g1" {
		t.Errorf("bad label filter: %+v", f.listReq.Filters[0])
	}
}

func TestStop_UsesHardStop(t *testing.T) {
	f := &fakeAPI{stopResp: detailsWith("u", "stopped")}
	c := newWithAPI(f)
	if err := c.Stop(context.Background(), "u", 90*time.Second); err != nil {
		t.Fatal(err)
	}
	if f.stopReq.StopType != request.ServerStopTypeHard || f.stopReq.UUID != "u" || f.stopReq.Timeout != 90*time.Second {
		t.Errorf("bad stop request: %+v", f.stopReq)
	}
}

func TestWaitForState_MapsResult(t *testing.T) {
	f := &fakeAPI{waitResp: detailsWith("u", "started", upcloud.Label{Key: "k", Value: "v"})}
	c := newWithAPI(f)
	got, err := c.WaitForState(context.Background(), "u", upcloud.ServerStateStarted)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "started" || got.Labels["k"] != "v" {
		t.Errorf("bad wait mapping: %+v", got)
	}
	if f.waitReq.DesiredState != upcloud.ServerStateStarted {
		t.Errorf("bad desired state: %q", f.waitReq.DesiredState)
	}
}

func TestDelete_UsesDeleteWithStorages(t *testing.T) {
	f := &fakeAPI{}
	c := newWithAPI(f)
	if err := c.Delete(context.Background(), "uuid-9"); err != nil {
		t.Fatal(err)
	}
	if f.delReq == nil || f.delReq.UUID != "uuid-9" {
		t.Errorf("delete-with-storages not called with uuid: %+v", f.delReq)
	}
}

func TestPublicTemplates_GuardAndFilter(t *testing.T) {
	f := &fakeAPI{storResp: &upcloud.Storages{Storages: []upcloud.Storage{
		{UUID: "pub", Title: "Ubuntu", Access: upcloud.StorageAccessPublic, Type: upcloud.StorageTypeTemplate},
		{UUID: "priv", Title: "mine", Access: "private", Type: upcloud.StorageTypeTemplate},
	}}}
	c := newWithAPI(f)
	got, err := c.PublicTemplates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].UUID != "pub" {
		t.Errorf("want only public template, got %+v", got)
	}
	// GUARD: must filter by Type only — setting Access too yields /storage/public/template -> 404.
	if f.storReq.Type != upcloud.StorageTypeTemplate {
		t.Errorf("want Type=template, got %q", f.storReq.Type)
	}
	if f.storReq.Access != "" {
		t.Errorf("Access must be empty to avoid the /storage/{access}/{type} 404; got %q", f.storReq.Access)
	}
}

func TestGet_MapsDetailsAndIPs(t *testing.T) {
	d := detailsWith("u", "started", upcloud.Label{Key: "g", Value: "v"})
	d.IPAddresses = upcloud.IPAddressSlice{
		{Family: upcloud.IPAddressFamilyIPv4, Access: upcloud.IPAddressAccessPublic, Address: "203.0.113.9"},
		{Family: upcloud.IPAddressFamilyIPv4, Access: upcloud.IPAddressAccessUtility, Address: "10.1.2.3"},
		{Family: "IPv6", Access: upcloud.IPAddressAccessPublic, Address: "2001:db8::1"}, // ignored (not IPv4)
	}
	c := newWithAPI(&fakeAPI{createResp: d}) // GetServerDetails returns createResp
	got, err := c.Get(context.Background(), "u")
	if err != nil {
		t.Fatal(err)
	}
	if got.ExternalIP != "203.0.113.9" || got.InternalIP != "10.1.2.3" {
		t.Errorf("bad IP mapping: ext=%q int=%q", got.ExternalIP, got.InternalIP)
	}
	if got.Labels["g"] != "v" || got.State != "started" {
		t.Errorf("bad detail mapping: %+v", got)
	}
}

func TestGet_Error(t *testing.T) {
	c := newWithAPI(&fakeAPI{err: errors.New("nope")})
	if _, err := c.Get(context.Background(), "u"); err == nil {
		t.Fatal("want error")
	}
}

func TestErrorWrapping(t *testing.T) {
	sentinel := errors.New("boom")
	f := &fakeAPI{err: sentinel}
	c := newWithAPI(f)
	ctx := context.Background()
	for name, op := range map[string]func() error{
		"create": func() error { _, e := c.Create(ctx, ServerSpec{}); return e },
		"list":   func() error { _, e := c.ListByLabel(ctx, "k", "v"); return e },
		"stop":   func() error { return c.Stop(ctx, "u", time.Second) },
		"wait":   func() error { _, e := c.WaitForState(ctx, "u", "started"); return e },
		"delete": func() error { return c.Delete(ctx, "u") },
		"tmpl":   func() error { _, e := c.PublicTemplates(ctx); return e },
	} {
		if err := op(); !errors.Is(err, sentinel) {
			t.Errorf("%s: error not wrapped/propagated: %v", name, err)
		}
	}
}

func TestLabelHelpers(t *testing.T) {
	if toLabelSlice(nil) != nil {
		t.Error("nil map should yield nil slice")
	}
	if labelsToMap(nil) != nil {
		t.Error("empty slice should yield nil map")
	}
	ls := toLabelSlice(map[string]string{"z": "1", "a": "2"})
	if (*ls)[0].Key != "a" {
		t.Errorf("not sorted: %+v", ls)
	}
	m := labelsToMap(upcloud.LabelSlice{{Key: "a", Value: "2"}})
	if m["a"] != "2" {
		t.Errorf("bad map: %+v", m)
	}
}
