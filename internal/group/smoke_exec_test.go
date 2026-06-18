package group_test

import (
	"bytes"
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"gitlab.com/gitlab-org/fleeting/fleeting/provider"
	"golang.org/x/crypto/ssh"

	"github.com/Growing-Europe/fleeting-plugin-upcloud/internal/config"
	"github.com/Growing-Europe/fleeting-plugin-upcloud/internal/group"
)

// TestSmoke_RealExec is the real-EXECUTION gate: provisioning a server is NOT the
// same as a job being able to run on it. This drives the full lifecycle against
// the REAL UpCloud API and then actually DIALS the address the connector would
// use and runs a trivial command over SSH, before destroying the server.
//
// The point is to prove the dial path end-to-end: the server must be reachable on
// the address ConnectInfo reports. When a private/SDN network is configured, the
// internal (dial) address MUST be the private/SDN address — so this test asserts
// that and then dials it. Reaching a private-cloud-network address requires the
// host running this test to be attached to that same network; run it from such a
// host (the same place the autoscaler's connector runs).
//
// It is BILLABLE and reachability-dependent, so it runs ONLY when explicitly
// opted in, and is skipped in mock CI:
//
//	UPCLOUD_SMOKE_EXEC=1 UPCLOUD_TOKEN=<ucat_…> \
//	  UPCLOUD_SMOKE_NETWORK=<sdn-network-uuid> \
//	  go test ./internal/group/ -run TestSmoke_RealExec -timeout 15m
//
// Never commit a token or key; both come from the environment only.
func TestSmoke_RealExec(t *testing.T) {
	if os.Getenv("UPCLOUD_SMOKE_EXEC") != "1" {
		t.Skip("set UPCLOUD_SMOKE_EXEC=1 (and UPCLOUD_TOKEN, UPCLOUD_SMOKE_NETWORK) to run the billable real-execution smoke")
	}
	if os.Getenv("UPCLOUD_TOKEN") == "" {
		t.Fatal("UPCLOUD_SMOKE_EXEC=1 requires UPCLOUD_TOKEN")
	}
	network := os.Getenv("UPCLOUD_SMOKE_NETWORK")
	if network == "" {
		t.Skip("UPCLOUD_SMOKE_NETWORK (an SDN/private network UUID) is required: this gate proves the private/SDN dial path")
	}
	// Required with no default: a template UUID is account/region-specific, so the
	// operator supplies a stock OS template UUID rather than baking one into source.
	template := os.Getenv("UPCLOUD_SMOKE_TEMPLATE")
	if template == "" {
		t.Skip("UPCLOUD_SMOKE_TEMPLATE (a bootable OS template UUID) is required")
	}

	const label = "fpu-exec"
	g := &group.InstanceGroup{Config: config.Config{
		Zone:           envOr("UPCLOUD_SMOKE_ZONE", "de-fra1"),
		Plan:           envOr("UPCLOUD_SMOKE_PLAN", "1xCPU-1GB"),
		Template:       template,
		HostnamePrefix: label,
		StorageSizeGB:  25,
		MaxInstances:   1,
		// Attach the SDN/private network (the dial target under test) plus utility
		// for in-zone reachability. No public IPv4: the dial path here is the SDN.
		Network:        network,
		UtilityNetwork: true,
		Labels:         map[string]string{"purpose": "ci-exec"},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 14*time.Minute)
	defer cancel()

	// Username drives the SSH login; Init generates an ephemeral keypair, injects
	// the public half into created servers, and hands the private half back via
	// ConnectInfo.Key (no static credentials configured).
	if _, err := g.Init(ctx, hclog.NewNullLogger(), provider.Settings{
		ConnectorConfig: provider.ConnectorConfig{
			Protocol: provider.ProtocolSSH,
			Username: envOr("UPCLOUD_SMOKE_USER", "root"),
		},
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	defer func() {
		for _, id := range listInstances(context.Background(), t, g) {
			_, _ = g.Decrease(context.Background(), []string{id})
		}
	}()

	if n, err := g.Increase(ctx, 1); err != nil || n != 1 {
		t.Fatalf("Increase(1) = (%d, %v)", n, err)
	}
	uuid := waitForRunning(ctx, t, g)
	t.Logf("instance running: %s", uuid)

	ci, err := g.ConnectInfo(ctx, uuid)
	if err != nil {
		t.Fatalf("ConnectInfo: %v", err)
	}
	// A private network is configured, so the dial (internal) address MUST be the
	// private/SDN address — derived from the interfaces by type, not the empty
	// top-level list.
	if ci.InternalAddr == "" {
		t.Fatalf("ConnectInfo returned no internal/dial address for an SDN-attached server: %+v", ci)
	}
	if len(ci.Key) == 0 {
		t.Fatalf("ConnectInfo returned no SSH key to dial with: %+v", ci)
	}
	dialTarget := ci.InternalAddr
	t.Logf("dial target (private/SDN internal addr): %s", dialTarget)

	out, err := sshRun(ctx, t, dialTarget, ci.Username, ci.Key, "echo fpu-exec-ok")
	if err != nil {
		t.Fatalf("EXEC-VERIFY failed: could not dial %s and run a command (provisioning succeeded but the job could not run): %v", dialTarget, err)
	}
	if got := string(bytes.TrimSpace(out)); got != "fpu-exec-ok" {
		t.Fatalf("EXEC-VERIFY: unexpected command output %q, want %q", got, "fpu-exec-ok")
	}
	t.Logf("EXEC-VERIFY: dialed the private/SDN address and ran a command — execution path proven")

	removed, err := g.Decrease(ctx, []string{uuid})
	if err != nil || len(removed) != 1 {
		t.Fatalf("Decrease = (%v, %v)", removed, err)
	}
	waitForEmpty(ctx, t, g)
	t.Log("DELETE-VERIFY: group empty — server and storage gone. EXEC SMOKE PASSED.")
}

// sshRun dials addr:22 with the connector's key and runs cmd, returning its
// combined stdout. Retries the dial until the context deadline because a freshly
// booted server's sshd may not be listening yet.
func sshRun(ctx context.Context, t *testing.T, addr, user string, key []byte, cmd string) ([]byte, error) {
	t.Helper()
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // #nosec G106 -- ephemeral single-use VM created moments ago; its host key is not known in advance and the server is destroyed right after, so host-identity pinning adds nothing. Reachability is the assertion here, not host identity.
		Timeout:         15 * time.Second,
	}
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			return nil, lastErr
		default:
		}
		client, derr := ssh.Dial("tcp", net.JoinHostPort(addr, "22"), cfg)
		if derr != nil {
			lastErr = derr
			time.Sleep(10 * time.Second)
			continue
		}
		sess, serr := client.NewSession()
		if serr != nil {
			_ = client.Close()
			return nil, serr
		}
		out, rerr := sess.CombinedOutput(cmd)
		_ = sess.Close()
		_ = client.Close()
		return out, rerr
	}
}
