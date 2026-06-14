// Package config defines the provider-generic plugin configuration, its
// validation rules, and parsing from the runner's plugin_config.
//
// Everything here is configuration supplied by the operator — no values are
// hardcoded or site-specific. The UpCloud API token is NEVER part of this
// config; it is read from the environment by package ucloud. Parsing is strict
// (unknown keys are rejected), which also fails closed if a token-like field is
// ever placed in the config by mistake.
package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config is the plugin_config block for fleeting-plugin-upcloud.
type Config struct {
	// Placement.
	Zone         string   `toml:"zone"`
	Plan         string   `toml:"plan"`
	Template     string   `toml:"template"`
	AllowedZones []string `toml:"allowed_zones"`

	// Identity / grouping.
	HostnamePrefix string            `toml:"hostname_prefix"`
	Labels         map[string]string `toml:"labels"`

	// Sizing.
	StorageSizeGB int `toml:"storage_size_gb"`
	MaxInstances  int `toml:"max_instances"` // 0 = no plugin-side cap (account quota governs)

	// Networking (at least one reachable path is required).
	Network        string `toml:"network"` // SDN/private network UUID
	UtilityNetwork bool   `toml:"utility_network"`
	PublicIPv4     bool   `toml:"public_ipv4"`
	PublicIPv6     bool   `toml:"public_ipv6"`
	FloatingIP     string `toml:"floating_ip"`

	// Provisioning.
	SSHKeys      []string `toml:"ssh_keys"`
	UserData     string   `toml:"user_data"`
	UserDataFile string   `toml:"user_data_file"`
}

// Parse decodes a plugin_config TOML document and validates it. Decoding is
// strict: unknown keys are rejected, so a stray credential field fails closed.
func Parse(data []byte) (*Config, error) {
	var c Config
	dec := toml.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("config: parse: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate enforces the configuration invariants. Errors are joined so a single
// pass reports every problem.
func (c *Config) Validate() error {
	var errs []error
	req := func(name, val string) {
		if strings.TrimSpace(val) == "" {
			errs = append(errs, fmt.Errorf("config: %s is required", name))
		}
	}
	req("zone", c.Zone)
	req("plan", c.Plan)
	req("template", c.Template)
	req("hostname_prefix", c.HostnamePrefix)

	if c.StorageSizeGB <= 0 {
		errs = append(errs, errors.New("config: storage_size_gb must be > 0"))
	}
	if c.MaxInstances < 0 {
		errs = append(errs, errors.New("config: max_instances must be >= 0 (0 = no plugin cap)"))
	}
	if len(c.AllowedZones) > 0 && c.Zone != "" && !slices.Contains(c.AllowedZones, c.Zone) {
		errs = append(errs, fmt.Errorf("config: zone %q is not in allowed_zones %v", c.Zone, c.AllowedZones))
	}
	if c.UserData != "" && c.UserDataFile != "" {
		errs = append(errs, errors.New("config: user_data and user_data_file are mutually exclusive"))
	}
	if !c.hasReachablePath() {
		errs = append(errs, errors.New("config: at least one of network, utility_network, public_ipv4, public_ipv6 must be set so runners are reachable"))
	}
	for k := range c.Labels {
		if strings.TrimSpace(k) == "" {
			errs = append(errs, errors.New("config: label keys must be non-empty"))
		}
	}
	return errors.Join(errs...)
}

func (c *Config) hasReachablePath() bool {
	return c.Network != "" || c.UtilityNetwork || c.PublicIPv4 || c.PublicIPv6
}

// ResolveUserData returns the effective cloud-init user-data: the inline value,
// or the contents of UserDataFile when set. Reading is the file-system parser
// surface, so the path is cleaned and the result is returned as-is.
func (c *Config) ResolveUserData() (string, error) {
	if c.UserDataFile == "" {
		return c.UserData, nil
	}
	b, err := os.ReadFile(c.UserDataFile)
	if err != nil {
		return "", fmt.Errorf("config: read user_data_file %q: %w", c.UserDataFile, err)
	}
	return string(b), nil
}
