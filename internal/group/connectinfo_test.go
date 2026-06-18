package group

import (
	"context"
	"net"
	"strconv"
	"testing"

	"github.com/Growing-Europe/fleeting-plugin-upcloud/internal/upcloud"
)

// TestConnectInfo_SetsDialPort is the dial-port red test: ConnectInfo.ProtocolPort
// is 0 when the operator omits a port, leaving the dial port to the connector
// framework's default. ConnectInfo MUST resolve it explicitly the same way the
// readiness probe does (configured port else 22), so probe and dial agree without
// relying on undocumented default behavior.
func TestConnectInfo_SetsDialPort(t *testing.T) {
	srv := &upcloud.Server{UUID: "a", State: "started", InternalIP: "10.20.0.2", ExternalIP: "203.0.113.7"}

	t.Run("port unset -> resolves to 22 (never 0)", func(t *testing.T) {
		g := New(cfg(), &fakeCloud{getResult: srv})
		ci, err := g.ConnectInfo(context.Background(), "a")
		if err != nil {
			t.Fatalf("ConnectInfo: %v", err)
		}
		if ci.ProtocolPort == 0 {
			t.Fatal("ConnectInfo.ProtocolPort is 0 — connector would dial <ip>:0 and time out")
		}
		if ci.ProtocolPort != defaultSSHPort {
			t.Errorf("ProtocolPort = %d, want %d", ci.ProtocolPort, defaultSSHPort)
		}
		if got := net.JoinHostPort(ci.InternalAddr, strconv.Itoa(ci.ProtocolPort)); got != "10.20.0.2:22" {
			t.Errorf("dial address = %q, want 10.20.0.2:22", got)
		}
	})

	t.Run("configured port is honored", func(t *testing.T) {
		g := New(cfg(), &fakeCloud{getResult: srv})
		g.settings.ProtocolPort = 2222
		ci, err := g.ConnectInfo(context.Background(), "a")
		if err != nil {
			t.Fatalf("ConnectInfo: %v", err)
		}
		if ci.ProtocolPort != 2222 {
			t.Errorf("ProtocolPort = %d, want 2222 (operator-configured)", ci.ProtocolPort)
		}
	})
}
