package group

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"gitlab.com/gitlab-org/fleeting/fleeting/provider"

	"github.com/Growing-Europe/fleeting-plugin-upcloud/internal/config"
	"github.com/Growing-Europe/fleeting-plugin-upcloud/internal/ucloud"
)

// fakeCloud is a deterministic cloud double recording calls and returning
// configured results — no network, no credentials.
type fakeCloud struct {
	created     []ucloud.ServerSpec
	createErrAt int // 1-based call index that errors; 0 = never
	createCalls int
	createErr   error

	listResult []ucloud.Server
	listErr    error

	getResult *ucloud.Server
	getErr    error

	stopped      []string
	stopTimeouts []time.Duration
	stopErr      error
	waited       []string // uuids passed to WaitForState
	waitState    string   // the desired state requested
	waitErr      error
	deleted      []string
	deleteErr    map[string]error

	calls []string // ordered method log, to assert stop->wait->delete sequencing
}

func (f *fakeCloud) Create(_ context.Context, spec ucloud.ServerSpec) (*ucloud.Server, error) {
	f.createCalls++
	f.created = append(f.created, spec)
	if f.createErrAt != 0 && f.createCalls == f.createErrAt {
		if f.createErr != nil {
			return nil, f.createErr
		}
		return nil, errors.New("synthetic create failure")
	}
	return &ucloud.Server{
		UUID:   "uuid-" + spec.Hostname,
		Title:  spec.Title,
		State:  "maintenance",
		Zone:   spec.Zone,
		Labels: spec.Labels,
	}, nil
}

func (f *fakeCloud) ListByLabel(_ context.Context, _, _ string) ([]ucloud.Server, error) {
	return f.listResult, f.listErr
}

func (f *fakeCloud) Get(_ context.Context, uuid string) (*ucloud.Server, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getResult != nil {
		return f.getResult, nil
	}
	return &ucloud.Server{UUID: uuid}, nil
}

func (f *fakeCloud) Stop(_ context.Context, uuid string, timeout time.Duration) error {
	f.stopped = append(f.stopped, uuid)
	f.stopTimeouts = append(f.stopTimeouts, timeout)
	f.calls = append(f.calls, "stop:"+uuid)
	return f.stopErr
}

func (f *fakeCloud) WaitForState(_ context.Context, uuid, state string) (*ucloud.Server, error) {
	f.waited = append(f.waited, uuid)
	f.waitState = state
	f.calls = append(f.calls, "wait:"+uuid)
	if f.waitErr != nil {
		return nil, f.waitErr
	}
	return &ucloud.Server{UUID: uuid, State: state}, nil
}

func (f *fakeCloud) Delete(_ context.Context, uuid string) error {
	f.deleted = append(f.deleted, uuid)
	f.calls = append(f.calls, "delete:"+uuid)
	if f.deleteErr != nil {
		return f.deleteErr[uuid]
	}
	return nil
}

func cfg() config.Config {
	return config.Config{
		Zone: "de-fra1", Plan: "1xCPU-1GB", Template: "tmpl", HostnamePrefix: "fleeting",
		StorageSizeGB: 25, MaxInstances: 3, PublicIPv4: true,
		Labels: map[string]string{"team": "ci"},
	}
}

func TestIncrease_CreatesLabelledServers(t *testing.T) {
	f := &fakeCloud{}
	g := New(cfg(), f)
	n, err := g.Increase(context.Background(), 2)
	if err != nil || n != 2 {
		t.Fatalf("Increase = (%d, %v), want (2, nil)", n, err)
	}
	if len(f.created) != 2 {
		t.Fatalf("want 2 creates, got %d", len(f.created))
	}
	for _, spec := range f.created {
		if spec.Labels[groupLabelKey] != "fleeting" {
			t.Errorf("server not labeled with group scope: %v", spec.Labels)
		}
		if spec.Labels["team"] != "ci" {
			t.Errorf("user labels not merged: %v", spec.Labels)
		}
		if !strings.HasPrefix(spec.Hostname, "fleeting-") {
			t.Errorf("hostname not prefixed: %q", spec.Hostname)
		}
		if spec.Zone != "de-fra1" || spec.Template != "tmpl" || spec.StorageSizeGB != 25 {
			t.Errorf("spec not built from config: %+v", spec)
		}
	}
	// hostnames must be unique per create
	if f.created[0].Hostname == f.created[1].Hostname {
		t.Error("hostnames are not unique")
	}
}

func TestIncrease_FailClosedAtCapacity(t *testing.T) {
	f := &fakeCloud{listResult: []ucloud.Server{{UUID: "a"}, {UUID: "b"}, {UUID: "c"}}} // already 3, cap 3
	g := New(cfg(), f)
	n, err := g.Increase(context.Background(), 2)
	if n != 0 || !errors.Is(err, ErrAtCapacity) {
		t.Fatalf("at capacity: got (%d, %v), want (0, ErrAtCapacity)", n, err)
	}
	if len(f.created) != 0 {
		t.Errorf("must create nothing at capacity, created %d", len(f.created))
	}
}

func TestIncrease_ClampsToRemainingRoom(t *testing.T) {
	f := &fakeCloud{listResult: []ucloud.Server{{UUID: "a"}}} // 1 existing, cap 3 -> room 2
	g := New(cfg(), f)
	n, err := g.Increase(context.Background(), 5) // asked 5, only 2 allowed
	if err != nil || n != 2 {
		t.Fatalf("clamp: got (%d, %v), want (2, nil)", n, err)
	}
}

func TestIncrease_PartialFailureReportsProgress(t *testing.T) {
	f := &fakeCloud{createErrAt: 3} // 1st,2nd succeed, 3rd fails
	cfgNoCap := cfg()
	cfgNoCap.MaxInstances = 0 // no cap so allowed = n
	g := New(cfgNoCap, f)
	n, err := g.Increase(context.Background(), 5)
	if n != 2 {
		t.Fatalf("partial: want 2 succeeded, got %d", n)
	}
	if err == nil || !strings.Contains(err.Error(), "created 2 of 5") {
		t.Fatalf("want progress-reporting error, got %v", err)
	}
}

func TestIncrease_NoCapMeansNoPreList(t *testing.T) {
	f := &fakeCloud{listErr: errors.New("list must not be called when uncapped")}
	cfgNoCap := cfg()
	cfgNoCap.MaxInstances = 0
	g := New(cfgNoCap, f)
	if n, err := g.Increase(context.Background(), 1); err != nil || n != 1 {
		t.Fatalf("uncapped increase should not pre-list: (%d, %v)", n, err)
	}
}

func TestIncrease_ZeroOrNegative(t *testing.T) {
	g := New(cfg(), &fakeCloud{})
	if n, err := g.Increase(context.Background(), 0); n != 0 || err != nil {
		t.Errorf("Increase(0) = (%d,%v)", n, err)
	}
	if n, err := g.Increase(context.Background(), -3); n != 0 || err != nil {
		t.Errorf("Increase(-3) = (%d,%v)", n, err)
	}
}

func TestDecrease_DeletesServerAndStoragePartial(t *testing.T) {
	f := &fakeCloud{deleteErr: map[string]error{"b": errors.New("boom")}}
	g := New(cfg(), f)
	removed, err := g.Decrease(context.Background(), []string{"a", "b", "c"})
	if err == nil || !strings.Contains(err.Error(), "decrease b") {
		t.Fatalf("want joined error mentioning b, got %v", err)
	}
	if got := strings.Join(removed, ","); got != "a,c" {
		t.Errorf("removed = %v, want [a c]", removed)
	}
	// every instance is stopped before delete, and delete is attempted for all
	if len(f.stopped) != 3 || len(f.deleted) != 3 {
		t.Errorf("stop/delete attempts: stopped=%v deleted=%v", f.stopped, f.deleted)
	}
	// the soft stop uses the configured stopTimeout
	for _, to := range f.stopTimeouts {
		if to != stopTimeout {
			t.Errorf("stop timeout = %v, want %v", to, stopTimeout)
		}
	}
	// each instance must be stopped, waited-for-stopped, THEN deleted, in order.
	if f.waitState != stateStopped {
		t.Errorf("WaitForState desired = %q, want %q", f.waitState, stateStopped)
	}
	want := []string{
		"stop:a", "wait:a", "delete:a",
		"stop:b", "wait:b", "delete:b",
		"stop:c", "wait:c", "delete:c",
	}
	if strings.Join(f.calls, ",") != strings.Join(want, ",") {
		t.Errorf("call order = %v, want stop->wait->delete per instance %v", f.calls, want)
	}
}

func TestDecrease_WaitFailureSkipsDelete(t *testing.T) {
	// Regression for the live-smoke bug: deleting before the server is stopped
	// 409s. If the wait-for-stopped fails, we must NOT attempt the delete.
	f := &fakeCloud{waitErr: errors.New("never stopped")}
	g := New(cfg(), f)
	removed, err := g.Decrease(context.Background(), []string{"x"})
	if err == nil || len(removed) != 0 {
		t.Fatalf("wait failure must abort the delete: removed=%v err=%v", removed, err)
	}
	if len(f.deleted) != 0 {
		t.Errorf("delete must not run when the server never stopped: %v", f.deleted)
	}
}

func TestDecrease_StopFailureStillDeletes(t *testing.T) {
	f := &fakeCloud{stopErr: errors.New("already stopped")}
	g := New(cfg(), f)
	removed, err := g.Decrease(context.Background(), []string{"x"})
	if err != nil || len(removed) != 1 {
		t.Fatalf("stop failure must not block delete: (%v, %v)", removed, err)
	}
}

func TestUpdate_ReconcilesFromLabelList(t *testing.T) {
	f := &fakeCloud{listResult: []ucloud.Server{
		{UUID: "a", State: "started"},
		{UUID: "b", State: "maintenance"},
		{UUID: "c", State: "stopped"},
	}}
	g := New(cfg(), f)
	got := map[string]provider.State{}
	if err := g.Update(context.Background(), func(id string, s provider.State) { got[id] = s }); err != nil {
		t.Fatal(err)
	}
	want := map[string]provider.State{"a": provider.StateRunning, "b": provider.StateCreating, "c": provider.StateDeleting}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("state[%s] = %q, want %q", id, got[id], w)
		}
	}
}

func TestUpdate_ListErrorPropagates(t *testing.T) {
	g := New(cfg(), &fakeCloud{listErr: errors.New("api down")})
	if err := g.Update(context.Background(), func(string, provider.State) {}); err == nil {
		t.Fatal("want error when list fails")
	}
}

func TestConnectInfo(t *testing.T) {
	f := &fakeCloud{getResult: &ucloud.Server{UUID: "z", ExternalIP: "203.0.113.7", InternalIP: "10.0.0.5"}}
	g := New(cfg(), f)
	g.settings.Username = "root"
	info, err := g.ConnectInfo(context.Background(), "z")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "z" || info.ExternalAddr != "203.0.113.7" || info.InternalAddr != "10.0.0.5" {
		t.Errorf("bad connect info: %+v", info)
	}
	if info.Username != "root" {
		t.Errorf("connector config not carried through: %+v", info.ConnectorConfig)
	}
}

func TestConnectInfo_GetError(t *testing.T) {
	g := New(cfg(), &fakeCloud{getErr: errors.New("not found")})
	if _, err := g.ConnectInfo(context.Background(), "missing"); err == nil {
		t.Fatal("want error")
	}
}

func TestNoOpMethods(t *testing.T) {
	g := New(cfg(), &fakeCloud{})
	ctx := context.Background()
	if err := g.Heartbeat(ctx, "x"); err != nil {
		t.Errorf("Heartbeat = %v", err)
	}
	if err := g.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown = %v", err)
	}
	ids := []string{"a", "b"}
	if got, err := g.Suspend(ctx, ids); err != nil || len(got) != 2 {
		t.Errorf("Suspend = (%v,%v)", got, err)
	}
	if got, err := g.Resume(ctx, ids); err != nil || len(got) != 2 {
		t.Errorf("Resume = (%v,%v)", got, err)
	}
}

func TestInit_WithClientAndLogger(t *testing.T) {
	f := &fakeCloud{}
	g := New(cfg(), f)
	info, err := g.Init(context.Background(), hclog.NewNullLogger(), provider.Settings{})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if info.ID != "fleeting" || info.MaxSize != 3 || info.Version == "" {
		t.Errorf("bad provider info: %+v", info)
	}
	if len(info.Capabilities) != 0 {
		t.Errorf("must not advertise suspend/resume capability: %v", info.Capabilities)
	}
	// logger is now set; exercise the logging branch in Increase.
	if _, err := g.Increase(context.Background(), 1); err != nil {
		t.Fatalf("Increase after Init: %v", err)
	}
}

func TestInit_InvalidConfig(t *testing.T) {
	bad := cfg()
	bad.Zone = ""
	g := New(bad, &fakeCloud{})
	if _, err := g.Init(context.Background(), hclog.NewNullLogger(), provider.Settings{}); err == nil {
		t.Fatal("want validation error from Init")
	}
}

func TestInit_UserDataFileMissing(t *testing.T) {
	c := cfg()
	c.UserDataFile = "/no/such/user-data-file"
	g := New(c, &fakeCloud{})
	if _, err := g.Init(context.Background(), hclog.NewNullLogger(), provider.Settings{}); err == nil {
		t.Fatal("want user_data_file read error from Init")
	}
}

func TestInit_FailClosedWithoutToken(t *testing.T) {
	t.Setenv("UPCLOUD_TOKEN", "")
	g := &InstanceGroup{Config: cfg()} // no client -> Init must build one and fail closed
	if _, err := g.Init(context.Background(), hclog.NewNullLogger(), provider.Settings{}); err == nil {
		t.Fatal("want fail-closed error when no client and no token")
	}
}

func TestMapState(t *testing.T) {
	cases := map[string]provider.State{
		"started":     provider.StateRunning,
		"maintenance": provider.StateCreating,
		"stopped":     provider.StateDeleting,
		"error":       provider.StateDeleting,
		"weird":       provider.StateCreating,
	}
	for in, want := range cases {
		if got := mapState(in); got != want {
			t.Errorf("mapState(%q) = %q, want %q", in, got, want)
		}
	}
}
