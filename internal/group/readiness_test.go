package group

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"gitlab.com/gitlab-org/fleeting/fleeting/provider"
	"pgregory.net/rapid"

	"github.com/Growing-Europe/fleeting-plugin-upcloud/internal/upcloud"
)

func TestDialAddress(t *testing.T) {
	tests := []struct {
		name string
		srv  *upcloud.Server
		want string
	}{
		{"nil", nil, ""},
		{"internal preferred", &upcloud.Server{InternalIP: "10.20.0.2", ExternalIP: "203.0.113.7"}, "10.20.0.2"},
		{"external fallback", &upcloud.Server{ExternalIP: "203.0.113.7"}, "203.0.113.7"},
		{"none", &upcloud.Server{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dialAddress(tt.srv); got != tt.want {
				t.Errorf("dialAddress = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTCPProbe(t *testing.T) {
	// Reachable: a listener on loopback accepts the connect.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	if err := tcpProbe(context.Background(), ln.Addr().String()); err != nil {
		t.Errorf("tcpProbe to a live listener should succeed, got %v", err)
	}
	// Unreachable: nothing listening on that port after we close it.
	addr := ln.Addr().String()
	ln.Close()
	if err := tcpProbe(context.Background(), addr); err == nil {
		t.Error("tcpProbe to a closed port should fail")
	}
}

func TestSSHPort(t *testing.T) {
	g := &InstanceGroup{}
	if g.sshPort() != defaultSSHPort {
		t.Errorf("default sshPort = %d, want %d", g.sshPort(), defaultSSHPort)
	}
	g.settings.ProtocolPort = 2222
	if g.sshPort() != 2222 {
		t.Errorf("configured sshPort = %d, want 2222", g.sshPort())
	}
}

// startedGroup builds a group whose client.Get returns srv and whose dialProbe
// returns probeErr, for exercising the readiness gate offline.
func startedGroup(srv *upcloud.Server, getErr, probeErr error) *InstanceGroup {
	f := &fakeCloud{getResult: srv, getErr: getErr}
	g := New(cfg(), f)
	g.dialProbe = func(context.Context, string) error { return probeErr }
	return g
}

func TestStartedState_Gate(t *testing.T) {
	withIP := &upcloud.Server{UUID: "a", State: "started", InternalIP: "10.20.0.2"}
	tests := []struct {
		name     string
		srv      *upcloud.Server
		getErr   error
		probeErr error
		want     provider.State
	}{
		{"reachable -> running", withIP, nil, nil, provider.StateRunning},
		{"refused -> creating", withIP, nil, errors.New("connection refused"), provider.StateCreating},
		{"timeout -> creating", withIP, nil, context.DeadlineExceeded, provider.StateCreating},
		{"no dial IP -> creating", &upcloud.Server{UUID: "a", State: "started"}, nil, nil, provider.StateCreating},
		{"get error -> creating", nil, errors.New("api 503"), nil, provider.StateCreating},
		{"external fallback reachable -> running", &upcloud.Server{UUID: "a", State: "started", ExternalIP: "203.0.113.7"}, nil, nil, provider.StateRunning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := startedGroup(tt.srv, tt.getErr, tt.probeErr)
			if got := g.startedState(context.Background(), "a"); got != tt.want {
				t.Errorf("startedState = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestStartedState_NeverDialsWithoutIP: the probe must not even be attempted when
// no dial address is resolved (avoids dialing an empty address).
func TestStartedState_NeverDialsWithoutIP(t *testing.T) {
	f := &fakeCloud{getResult: &upcloud.Server{UUID: "a", State: "started"}} // no IP
	g := New(cfg(), f)
	probed := false
	g.dialProbe = func(context.Context, string) error { probed = true; return nil }
	if got := g.startedState(context.Background(), "a"); got != provider.StateCreating {
		t.Errorf("want StateCreating, got %q", got)
	}
	if probed {
		t.Error("probe must NOT run when there is no dial address")
	}
}

// TestUpdate_ProbesStartedWithBoundedConcurrency: many "started" servers are
// probed concurrently but never more than maxConcurrentProbes at once, and all
// are reported.
func TestUpdate_ProbesStartedWithBoundedConcurrency(t *testing.T) {
	const n = 50
	list := make([]upcloud.Server, n)
	for i := range list {
		list[i] = upcloud.Server{UUID: fmt.Sprintf("s-%d", i), State: "started"}
	}
	f := &fakeCloud{listResult: list, getResult: &upcloud.Server{State: "started", InternalIP: "10.20.0.2"}}
	g := New(cfg(), f)

	var inflight, maxSeen int64
	var mu sync.Mutex
	g.dialProbe = func(context.Context, string) error {
		cur := atomic.AddInt64(&inflight, 1)
		mu.Lock()
		if cur > maxSeen {
			maxSeen = cur
		}
		mu.Unlock()
		atomic.AddInt64(&inflight, -1)
		return nil
	}

	got := map[string]provider.State{}
	var gmu sync.Mutex
	if err := g.Update(context.Background(), func(id string, s provider.State) {
		gmu.Lock()
		got[id] = s
		gmu.Unlock()
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Errorf("reported %d states, want %d", len(got), n)
	}
	for id, s := range got {
		if s != provider.StateRunning {
			t.Errorf("state[%s] = %q, want running", id, s)
		}
	}
	if maxSeen > maxConcurrentProbes {
		t.Errorf("max concurrent probes = %d, exceeds cap %d", maxSeen, maxConcurrentProbes)
	}
}

// TestStartedState_Property: for a "started" server, the gate reports Running IFF
// a dial address is resolvable AND the probe succeeds; otherwise Creating. Never
// errors.
func TestStartedState_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		hasIP := rapid.Bool().Draw(rt, "hasIP")
		probeOK := rapid.Bool().Draw(rt, "probeOK")
		getOK := rapid.Bool().Draw(rt, "getOK")

		var srv *upcloud.Server
		var getErr error
		if getOK {
			srv = &upcloud.Server{UUID: "a", State: "started"}
			if hasIP {
				srv.InternalIP = "10.20.0.2"
			}
		} else {
			getErr = errors.New("api error")
		}
		var probeErr error
		if !probeOK {
			probeErr = errors.New("unreachable")
		}
		g := startedGroup(srv, getErr, probeErr)

		got := g.startedState(context.Background(), "a")
		wantRunning := getOK && hasIP && probeOK
		if wantRunning && got != provider.StateRunning {
			rt.Fatalf("want Running (getOK=%v hasIP=%v probeOK=%v), got %q", getOK, hasIP, probeOK, got)
		}
		if !wantRunning && got != provider.StateCreating {
			rt.Fatalf("want Creating (getOK=%v hasIP=%v probeOK=%v), got %q", getOK, hasIP, probeOK, got)
		}
	})
}
