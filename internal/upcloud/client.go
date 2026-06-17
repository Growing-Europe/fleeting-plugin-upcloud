// Package upcloud is a thin, provider-generic wrapper over the official
// upcloud-go-api SDK. It exposes only the operations the fleeting instance
// group needs — create, list-by-label, delete-with-storages, and state waits —
// in terms of plain structs, so the rest of the plugin never depends on SDK
// request/response types directly.
//
// The UpCloud API token is read from the environment only (never from config or
// source); construction fails closed when it is absent.
package upcloud

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/UpCloudLtd/upcloud-go-api/v8/upcloud"
	"github.com/UpCloudLtd/upcloud-go-api/v8/upcloud/client"
	"github.com/UpCloudLtd/upcloud-go-api/v8/upcloud/request"
	"github.com/UpCloudLtd/upcloud-go-api/v8/upcloud/service"
)

// serverAPI is the slice of the SDK service this package depends on. Narrowing
// to an interface keeps the wrapper unit-testable (httptest or a fake) without
// reaching for the concrete *service.Service.
type serverAPI interface {
	CreateServer(ctx context.Context, r *request.CreateServerRequest) (*upcloud.ServerDetails, error)
	GetServersWithFilters(ctx context.Context, r *request.GetServersWithFiltersRequest) (*upcloud.Servers, error)
	GetServerDetails(ctx context.Context, r *request.GetServerDetailsRequest) (*upcloud.ServerDetails, error)
	StopServer(ctx context.Context, r *request.StopServerRequest) (*upcloud.ServerDetails, error)
	WaitForServerState(ctx context.Context, r *request.WaitForServerStateRequest) (*upcloud.ServerDetails, error)
	DeleteServerAndStorages(ctx context.Context, r *request.DeleteServerAndStoragesRequest) error
	GetStorages(ctx context.Context, r *request.GetStoragesRequest) (*upcloud.Storages, error)
}

// Client is the provider-generic UpCloud client used by the plugin.
type Client struct {
	api serverAPI
}

// ErrNoToken is returned when no UpCloud API token is available in the
// environment. Construction fails closed rather than silently defaulting.
var ErrNoToken = errors.New("upcloud: no UpCloud API token in environment (set UPCLOUD_TOKEN)")

// New builds a Client from the environment. The SDK's NewFromEnv selects the
// experimental bearer path when UPCLOUD_TOKEN is set; we additionally require a
// non-empty token so a missing credential is a hard error, not basic-auth
// fallback. Optional ConfigFns (e.g. WithHTTPClient for tests) are passed
// through to the SDK.
func New(opts ...client.ConfigFn) (*Client, error) {
	if tokenFromEnv() == "" {
		return nil, ErrNoToken
	}
	c, err := client.NewFromEnv(opts...)
	if err != nil {
		return nil, fmt.Errorf("upcloud: build client: %w", err)
	}
	return &Client{api: service.New(c)}, nil
}

// newWithAPI is used by tests to inject a fake or an httptest-backed service.
func newWithAPI(api serverAPI) *Client { return &Client{api: api} }

// HTTPClient is a convenience for wiring a custom *http.Client (tests, cassette
// replay) into New via the SDK's WithHTTPClient.
func HTTPClient(hc *http.Client) client.ConfigFn { return client.WithHTTPClient(hc) }

// Server is the provider-generic view of an UpCloud server.
type Server struct {
	UUID   string
	Title  string
	State  string
	Zone   string
	Labels map[string]string

	// Connection addresses (populated by Get / Create / WaitForState, which
	// return full details; ListByLabel items do not carry them).
	ExternalIP string // first public IPv4, for external connections
	InternalIP string // first utility/private IPv4, for in-network connections
}

// ServerSpec describes a server to create. All values are configuration —
// nothing here is hardcoded or site-specific.
type ServerSpec struct {
	Title         string
	Hostname      string
	Zone          string
	Plan          string
	Template      string // storage template UUID to clone
	StorageSizeGB int
	Labels        map[string]string
	SSHKeys       []string
	UserData      string

	// Networking. At least one must be set or the created server is created
	// with UpCloud's DEFAULT interfaces (public + utility) and is NOT attached
	// to the configured private SDN — leaving it unreachable from a manager that
	// reaches the fleet only over the SDN's IPsec tunnel (the dial hangs forever).
	// See Create(): these drive the explicit request.Networking interfaces.
	Network        string // private SDN network UUID (attaches a private interface)
	UtilityNetwork bool   // attach a utility-network interface
	PublicIPv4     bool   // attach a public IPv4 interface
	PublicIPv6     bool   // attach a public IPv6 interface
}
