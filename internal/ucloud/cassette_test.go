package ucloud

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/UpCloudLtd/upcloud-go-api/v8/upcloud"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// These tests replay recorded UpCloud API interactions through the wrapper, with
// NO network and NO real token, proving the real wire format deserializes
// correctly and that the committed cassettes are safe to ship.
//
// Re-record (read-only, NON-billable calls only) with:
//
//	UCLOUD_RECORD=1 UPCLOUD_TOKEN=<ucat_…> go test ./internal/ucloud/ -run Cassette
//
// The BeforeSaveHook scrubs the bearer token and every site-specific value
// (account username, credit balance, server/storage UUIDs, IPs) before anything
// touches disk, so the cassettes carry zero secrets and zero GE-identifying data.

const redaction = "ucat_REDACTED"

var (
	ucatRe     = regexp.MustCompile(`ucat_[A-Za-z0-9]+`)
	usernameRe = regexp.MustCompile(`("username"\s*:\s*)"[^"]*"`)
	creditsRe  = regexp.MustCompile(`("credits"\s*:\s*)[0-9.]+`)
	uuidRe     = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	ipv4Re     = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	// Server/storage title + hostname are user-chosen and routinely carry
	// org-identifying names; zone is a deployment choice. Field coverage MUST be
	// exhaustive for list/server responses so recordings can't re-leak coupling.
	titleHostRe = regexp.MustCompile(`"(title|hostname)"(\s*:\s*)"[^"]*"`)
	zoneRe      = regexp.MustCompile(`("zone"\s*:\s*)"[^"]*"`)
)

func redactCassetteBody(s string, uuids, ips, names map[string]string) string {
	s = ucatRe.ReplaceAllString(s, redaction)
	s = usernameRe.ReplaceAllString(s, `${1}"redacted-user"`)
	s = creditsRe.ReplaceAllString(s, `${1}0`)
	s = uuidRe.ReplaceAllStringFunc(s, func(u string) string {
		return stable(strings.ToLower(u), uuids, func(n int) string {
			return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
		})
	})
	s = ipv4Re.ReplaceAllStringFunc(s, func(ip string) string {
		return stable(ip, ips, func(n int) string { return fmt.Sprintf("203.0.113.%d", n%254+1) })
	})
	s = titleHostRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := titleHostRe.FindStringSubmatch(m) // [full, key, sep]
		key, sep := sub[1], sub[2]
		ph := stable(m, names, func(n int) string { return fmt.Sprintf("example-%s-%d", key, n) })
		return fmt.Sprintf("%q%s%q", key, sep, ph)
	})
	s = zoneRe.ReplaceAllString(s, `${1}"de-fra1"`)
	return s
}

func stable(orig string, m map[string]string, mk func(int) string) string {
	if p, ok := m[orig]; ok {
		return p
	}
	p := mk(len(m) + 1)
	m[orig] = p
	return p
}

func scrubInteraction(uuids, ips, names map[string]string) func(*cassette.Interaction) error {
	return func(i *cassette.Interaction) error {
		for _, h := range []map[string][]string{i.Request.Headers, i.Response.Headers} {
			for k := range h {
				if strings.EqualFold(k, "Authorization") {
					h[k] = []string{"Bearer " + redaction}
				}
			}
		}
		i.Request.Body = redactCassetteBody(i.Request.Body, uuids, ips, names)
		i.Response.Body = redactCassetteBody(i.Response.Body, uuids, ips, names)
		i.Request.URL = redactCassetteBody(i.Request.URL, uuids, ips, names)
		return nil
	}
}

// cassetteClient returns a Client wired to a go-vcr recorder. In replay mode
// (the default, used in CI) it serves from the committed cassette with no
// network; with UCLOUD_RECORD=1 it records live and scrubs on save.
func cassetteClient(t *testing.T, name string) (*Client, func()) {
	t.Helper()
	recording := os.Getenv("UCLOUD_RECORD") == "1"

	opts := []recorder.Option{}
	if recording {
		uuids, ips, names := map[string]string{}, map[string]string{}, map[string]string{}
		opts = append(opts,
			recorder.WithMode(recorder.ModeRecordOnce),
			recorder.WithHook(scrubInteraction(uuids, ips, names), recorder.BeforeSaveHook),
			recorder.WithSkipRequestLatency(true),
		)
	} else {
		// Replay: match on method+URL with auth/UA ignored, so scrubbed cassettes
		// match regardless of token value or SDK version.
		t.Setenv("UPCLOUD_TOKEN", "ucat_dummy_replay")
		opts = append(opts,
			recorder.WithMode(recorder.ModeReplayOnly),
			recorder.WithMatcher(cassette.NewDefaultMatcher(
				cassette.WithIgnoreAuthorization(),
				cassette.WithIgnoreUserAgent(),
			)),
		)
	}

	rec, err := recorder.New("testdata/"+name, opts...)
	if err != nil {
		t.Fatalf("recorder: %v", err)
	}
	c, err := New(HTTPClient(rec.GetDefaultClient()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, func() {
		if err := rec.Stop(); err != nil {
			t.Errorf("recorder stop: %v", err)
		}
	}
}

func TestCassette_PublicTemplates_Replay(t *testing.T) {
	c, stop := cassetteClient(t, "public_templates")
	defer stop()

	tmpls, err := c.PublicTemplates(context.Background())
	if err != nil {
		t.Fatalf("PublicTemplates: %v", err)
	}
	if len(tmpls) == 0 {
		t.Fatal("expected at least one public template from the cassette")
	}
	for _, s := range tmpls {
		if s.Access != upcloud.StorageAccessPublic {
			t.Errorf("non-public template leaked through the filter: %q (%s)", s.Title, s.Access)
		}
		if s.Type != upcloud.StorageTypeTemplate {
			t.Errorf("non-template storage returned: %q (%s)", s.Title, s.Type)
		}
	}
}

func TestCassette_ListByLabel_Replay(t *testing.T) {
	c, stop := cassetteClient(t, "list_by_label")
	defer stop()

	// The spike servers were deleted, so this label lists empty — which still
	// exercises the real filter URL and the empty-result mapping over the wire.
	servers, err := c.ListByLabel(context.Background(), "fpu-spike", "m-spike")
	if err != nil {
		t.Fatalf("ListByLabel: %v", err)
	}
	if len(servers) != 0 {
		t.Errorf("expected an empty group from the cassette, got %d", len(servers))
	}
}

// TestScrubHook_RedactsCoupling asserts the BeforeSaveHook redaction removes
// every site/org-identifying value — including server title, hostname and zone,
// which a real recording can otherwise carry into a committed fixture.
func TestScrubHook_RedactsCoupling(t *testing.T) {
	uuids, ips, names := map[string]string{}, map[string]string{}, map[string]string{}
	// Synthetic inputs ONLY — never embed a real account's identifiers in source.
	// These stand in for the org-coupling a real recording could carry (a server
	// title, hostname, deployment zone, account login, balance, token, UUID).
	const (
		title    = "acme-ci-runner-001"
		hostname = "acme-ci-runner-host"
		zone     = "zz-test1"
		user     = "acme-bot"
		uuid     = "abcdef01-2345-6789-abcd-ef0123456789"
		token    = "ucat_synthetic000token000value0"
	)
	body := `{"server" : {` +
		`"title" : "` + title + `",` +
		`"hostname" : "` + hostname + `",` +
		`"zone" : "` + zone + `",` +
		`"uuid" : "` + uuid + `"},` +
		`"account" : {"username" : "` + user + `","credits" : 12345.67},` +
		`"token" : "` + token + `"}`

	out := redactCassetteBody(body, uuids, ips, names)

	for _, leaked := range []string{title, hostname, zone, user, uuid, token, "12345.67"} {
		if strings.Contains(out, leaked) {
			t.Errorf("scrub left identifier %q in output:\n%s", leaked, out)
		}
	}
	// Structure/keys survive (so cassettes still replay) and zone normalizes.
	for _, want := range []string{`"title"`, `"hostname"`, `"zone"`, `"uuid"`, `"de-fra1"`, "redacted-user"} {
		if !strings.Contains(out, want) {
			t.Errorf("scrub dropped expected token %q in:\n%s", want, out)
		}
	}
}

// ensure http import is used even if the SDK signature changes.
var _ = http.DefaultClient
