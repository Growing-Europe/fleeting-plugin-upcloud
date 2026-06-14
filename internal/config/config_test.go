package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validTOML = `
zone            = "de-fra1"
plan            = "1xCPU-1GB"
template        = "01000000-0000-4000-8000-000030240200"
hostname_prefix = "fleeting"
storage_size_gb = 25
max_instances   = 10
public_ipv4     = true
labels          = { team = "ci" }
ssh_keys        = ["ssh-ed25519 AAAA"]
`

func TestParse_Valid(t *testing.T) {
	c, err := Parse([]byte(validTOML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Zone != "de-fra1" || c.Plan != "1xCPU-1GB" || c.StorageSizeGB != 25 || c.MaxInstances != 10 {
		t.Errorf("bad decode: %+v", c)
	}
	if !c.PublicIPv4 || c.Labels["team"] != "ci" || len(c.SSHKeys) != 1 {
		t.Errorf("bad decode of networking/labels/keys: %+v", c)
	}
}

func TestParse_StrictRejectsUnknownKey(t *testing.T) {
	// A stray credential field (or any typo) must fail closed, not be ignored.
	_, err := Parse([]byte(validTOML + "\napi_token = \"ucat_should_never_be_here\"\n"))
	if err == nil {
		t.Fatal("expected strict-decode error for unknown key (token-in-config guard)")
	}
	if strings.Contains(err.Error(), "ucat_should_never_be_here") {
		t.Errorf("error message must not echo the stray secret value: %v", err)
	}
}

func TestParse_Malformed(t *testing.T) {
	if _, err := Parse([]byte("zone = = =")); err == nil {
		t.Fatal("expected parse error for malformed TOML")
	}
}

func base() Config {
	return Config{
		Zone: "de-fra1", Plan: "1xCPU-1GB", Template: "tmpl", HostnamePrefix: "fleeting",
		StorageSizeGB: 25, PublicIPv4: true,
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"valid", func(*Config) {}, ""},
		{"missing zone", func(c *Config) { c.Zone = "" }, "zone is required"},
		{"missing plan", func(c *Config) { c.Plan = "" }, "plan is required"},
		{"missing template", func(c *Config) { c.Template = "" }, "template is required"},
		{"missing hostname_prefix", func(c *Config) { c.HostnamePrefix = "" }, "hostname_prefix is required"},
		{"zero storage", func(c *Config) { c.StorageSizeGB = 0 }, "storage_size_gb must be > 0"},
		{"negative max", func(c *Config) { c.MaxInstances = -1 }, "max_instances must be >= 0"},
		{"zone not allowed", func(c *Config) { c.AllowedZones = []string{"fi-hel1"} }, "not in allowed_zones"},
		{"userdata both", func(c *Config) { c.UserData = "x"; c.UserDataFile = "y" }, "mutually exclusive"},
		{"no reachable path", func(c *Config) { c.PublicIPv4 = false }, "runners are reachable"},
		{"empty label key", func(c *Config) { c.Labels = map[string]string{"": "v"} }, "label keys must be non-empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base()
			tt.mutate(&c)
			err := c.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestValidate_AllowedZonesMembership(t *testing.T) {
	c := base()
	c.AllowedZones = []string{"de-fra1", "fi-hel1"}
	if err := c.Validate(); err != nil {
		t.Fatalf("zone in allowed_zones should pass: %v", err)
	}
}

func TestResolveUserData(t *testing.T) {
	c := base()
	c.UserData = "#cloud-config"
	if got, err := c.ResolveUserData(); err != nil || got != "#cloud-config" {
		t.Fatalf("inline: got %q err %v", got, err)
	}

	dir := t.TempDir()
	p := filepath.Join(dir, "ud.yaml")
	if err := os.WriteFile(p, []byte("#from-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	c2 := base()
	c2.UserDataFile = p
	if got, err := c2.ResolveUserData(); err != nil || got != "#from-file" {
		t.Fatalf("file: got %q err %v", got, err)
	}

	c3 := base()
	c3.UserDataFile = filepath.Join(dir, "does-not-exist")
	if _, err := c3.ResolveUserData(); err == nil {
		t.Fatal("expected error for missing user_data_file")
	}
}

// FuzzParse ensures the config parser never panics on arbitrary input.
func FuzzParse(f *testing.F) {
	f.Add([]byte(validTOML))
	f.Add([]byte(""))
	f.Add([]byte("zone = = ="))
	f.Add([]byte("[unclosed"))
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = Parse(data) // must not panic; error is fine
	})
}
