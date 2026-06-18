package upcloud

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/UpCloudLtd/upcloud-go-api/v8/upcloud"
)

// ifaces is a terse builder for a ServerNetworking from (type, access, address)
// triples, so the table cases below read as the interface SHAPE they model.
type ifSpec struct {
	typ, access, addr string
	family            string // defaults to IPv4 when empty
}

func networking(specs ...ifSpec) upcloud.ServerNetworking {
	ifaces := make(upcloud.ServerInterfaceSlice, 0, len(specs))
	for i, s := range specs {
		fam := s.family
		if fam == "" {
			fam = upcloud.IPAddressFamilyIPv4
		}
		ifaces = append(ifaces, upcloud.ServerInterface{
			Index: i + 1,
			Type:  s.typ,
			IPAddresses: upcloud.IPAddressSlice{
				{Family: fam, Access: s.access, Address: s.addr},
			},
		})
	}
	return upcloud.ServerNetworking{Interfaces: ifaces}
}

// TestPickIPsFromInterfaces_ByType classifies dial addresses by interface TYPE,
// not by the IP Access field. An SDN/private interface's address carries an EMPTY
// Access, so Access-based classification (pickIPs) misses it; only the interface
// Type is a reliable signal. Internal PREFERS the private address and falls back
// to utility only when no private interface is present.
func TestPickIPsFromInterfaces_ByType(t *testing.T) {
	tests := []struct {
		name             string
		net              upcloud.ServerNetworking
		wantExt, wantInt string
	}{
		{
			name:    "private preferred over utility for internal, regardless of order",
			net:     networking(ifSpec{typ: "utility", access: "utility", addr: "10.11.0.5"}, ifSpec{typ: "private", access: "", addr: "10.10.0.5"}),
			wantExt: "", wantInt: "10.10.0.5",
		},
		{
			name:    "utility is the internal fallback when no private interface exists",
			net:     networking(ifSpec{typ: "public", access: "public", addr: "203.0.113.10"}, ifSpec{typ: "utility", access: "utility", addr: "10.11.0.5"}),
			wantExt: "203.0.113.10", wantInt: "10.11.0.5",
		},
		{
			name:    "public interface yields the external address",
			net:     networking(ifSpec{typ: "public", access: "public", addr: "203.0.113.10"}, ifSpec{typ: "private", access: "", addr: "10.10.0.5"}),
			wantExt: "203.0.113.10", wantInt: "10.10.0.5",
		},
		{
			name:    "IPv6 interface addresses are ignored (IPv4-only dial)",
			net:     networking(ifSpec{typ: "private", access: "", addr: "2001:db8::5", family: upcloud.IPAddressFamilyIPv6}, ifSpec{typ: "utility", access: "utility", addr: "10.11.0.5"}),
			wantExt: "", wantInt: "10.11.0.5",
		},
		{
			name:    "no interfaces yields empty external and internal",
			net:     networking(),
			wantExt: "", wantInt: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext, in := pickIPsFromInterfaces(tt.net)
			if ext != tt.wantExt || in != tt.wantInt {
				t.Errorf("pickIPsFromInterfaces = (ext=%q, int=%q), want (ext=%q, int=%q)", ext, in, tt.wantExt, tt.wantInt)
			}
		})
	}
}

// TestServerFromDetails_GoldenSDNFixture parses the golden ServerDetails JSON
// (the real SDN-attached shape: empty top-level ip_addresses; the private address
// only under a private-type interface with empty access) and asserts the dial
// addresses are derived from the interfaces by type — external from public,
// internal from the private/SDN interface.
func TestServerFromDetails_GoldenSDNFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/server_details_sdn.json")
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	var d upcloud.ServerDetails
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal golden fixture into ServerDetails: %v", err)
	}
	// Guard the fixture models the shape under test: the top-level list IS empty,
	// so a flat-list classifier would yield "" and the connector would hang.
	if len(d.IPAddresses) != 0 {
		t.Fatalf("fixture invariant broken: top-level ip_addresses must be empty, got %d", len(d.IPAddresses))
	}
	s := serverFromDetails(&d)
	if s.ExternalIP != "203.0.113.10" {
		t.Errorf("ExternalIP = %q, want 203.0.113.10 (from the public interface)", s.ExternalIP)
	}
	if s.InternalIP != "10.10.0.5" {
		t.Errorf("InternalIP = %q, want 10.10.0.5 (from the private/SDN interface, derived by type) — the connector dials this address", s.InternalIP)
	}
}

// TestServerFromDetails_FallsBackToFlatList covers the defensive path: when the
// response carries no interface networking but DOES populate the top-level
// ip_addresses list, the dial addresses are taken from that flat list.
func TestServerFromDetails_FallsBackToFlatList(t *testing.T) {
	d := &upcloud.ServerDetails{}
	d.IPAddresses = upcloud.IPAddressSlice{
		{Family: upcloud.IPAddressFamilyIPv4, Access: upcloud.IPAddressAccessPublic, Address: "203.0.113.20"},
		{Family: upcloud.IPAddressFamilyIPv4, Access: upcloud.IPAddressAccessPrivate, Address: "10.12.0.7"},
	}
	s := serverFromDetails(d)
	if s.ExternalIP != "203.0.113.20" || s.InternalIP != "10.12.0.7" {
		t.Errorf("flat-list fallback = (ext=%q, int=%q), want (ext=203.0.113.20, int=10.12.0.7)", s.ExternalIP, s.InternalIP)
	}
}
