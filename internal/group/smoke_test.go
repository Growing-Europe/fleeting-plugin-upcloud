package group_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"gitlab.com/gitlab-org/fleeting/fleeting/provider"

	"github.com/Growing-Europe/fleeting-plugin-upcloud/internal/config"
	"github.com/Growing-Europe/fleeting-plugin-upcloud/internal/group"
)

// TestSmoke_RealAPI drives the whole instance-group lifecycle against the REAL
// UpCloud API once: Init -> Increase -> (Update until running) -> ConnectInfo ->
// Decrease -> (Update until gone). It is the "passed a real-API smoke at least
// once" v0.1.0 gate.
//
// It is BILLABLE (creates one small server, immediately deleted with its
// storage) and therefore runs ONLY when explicitly opted in:
//
//	UPCLOUD_SMOKE=1 UPCLOUD_TOKEN=<ucat_…> go test ./internal/group/ -run TestSmoke_RealAPI -timeout 15m
//
// Mock CI never sets UPCLOUD_SMOKE, so this skips there. Never commit a token;
// the token comes from the environment only.
func TestSmoke_RealAPI(t *testing.T) {
	if os.Getenv("UPCLOUD_SMOKE") != "1" {
		t.Skip("set UPCLOUD_SMOKE=1 (and UPCLOUD_TOKEN) to run the billable real-API smoke")
	}
	if os.Getenv("UPCLOUD_TOKEN") == "" {
		t.Fatal("UPCLOUD_SMOKE=1 requires UPCLOUD_TOKEN")
	}
	// Required with no default: a template UUID is account/region-specific, so the
	// operator supplies a stock OS template UUID rather than baking one into source.
	template := os.Getenv("UPCLOUD_SMOKE_TEMPLATE")
	if template == "" {
		t.Skip("UPCLOUD_SMOKE_TEMPLATE (a bootable OS template UUID) is required")
	}

	const label = "fpu-smoke"
	g := &group.InstanceGroup{Config: config.Config{
		Zone:           envOr("UPCLOUD_SMOKE_ZONE", "de-fra1"),
		Plan:           envOr("UPCLOUD_SMOKE_PLAN", "1xCPU-1GB"),
		Template:       template,
		HostnamePrefix: label,
		StorageSizeGB:  25,
		MaxInstances:   1,
		PublicIPv4:     true,
		Labels:         map[string]string{"purpose": "ci-smoke"},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 14*time.Minute)
	defer cancel()

	info, err := g.Init(ctx, hclog.NewNullLogger(), provider.Settings{})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Logf("init ok: provider id=%s maxSize=%d", info.ID, info.MaxSize)

	// Best-effort cleanup of anything we created, even on failure.
	defer func() {
		for _, id := range listInstances(context.Background(), t, g) {
			_, _ = g.Decrease(context.Background(), []string{id})
		}
	}()

	n, err := g.Increase(ctx, 1)
	if err != nil || n != 1 {
		t.Fatalf("Increase(1) = (%d, %v)", n, err)
	}

	uuid := waitForRunning(ctx, t, g)
	t.Logf("instance running: %s", uuid)

	ci, err := g.ConnectInfo(ctx, uuid)
	if err != nil {
		t.Fatalf("ConnectInfo: %v", err)
	}
	if ci.ExternalAddr == "" {
		t.Errorf("ConnectInfo returned no external address: %+v", ci)
	}
	t.Logf("connect info: id=%s external=%s internal=%s", ci.ID, ci.ExternalAddr, ci.InternalAddr)

	removed, err := g.Decrease(ctx, []string{uuid})
	if err != nil || len(removed) != 1 {
		t.Fatalf("Decrease = (%v, %v)", removed, err)
	}

	waitForEmpty(ctx, t, g)
	t.Log("DELETE-VERIFY: group empty — server and storage gone. SMOKE PASSED.")
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func listInstances(ctx context.Context, t *testing.T, g *group.InstanceGroup) []string {
	t.Helper()
	var ids []string
	if err := g.Update(ctx, func(id string, _ provider.State) { ids = append(ids, id) }); err != nil {
		t.Logf("update (list) error: %v", err)
	}
	return ids
}

func waitForRunning(ctx context.Context, t *testing.T, g *group.InstanceGroup) string {
	t.Helper()
	for {
		var running string
		if err := g.Update(ctx, func(id string, s provider.State) {
			if s == provider.StateRunning {
				running = id
			}
		}); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if running != "" {
			return running
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for an instance to reach running")
		case <-time.After(10 * time.Second):
		}
	}
}

func waitForEmpty(ctx context.Context, t *testing.T, g *group.InstanceGroup) {
	t.Helper()
	for {
		if len(listInstances(ctx, t, g)) == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for the group to empty after Decrease")
		case <-time.After(10 * time.Second):
		}
	}
}
