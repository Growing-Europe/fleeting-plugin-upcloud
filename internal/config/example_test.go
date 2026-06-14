package config_test

import (
	"fmt"

	"github.com/Growing-Europe/fleeting-plugin-upcloud/internal/config"
)

// ExampleParse shows parsing a plugin_config block and reading a validated value.
func ExampleParse() {
	cfg, err := config.Parse([]byte(`
zone            = "de-fra1"
plan            = "1xCPU-1GB"
template        = "01000000-0000-4000-8000-000030240200"
hostname_prefix = "fleeting"
storage_size_gb = 25
public_ipv4     = true
`))
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(cfg.Zone, cfg.HostnamePrefix)
	// Output: de-fra1 fleeting
}

// ExampleConfig_Validate shows that validation reports a clear, actionable error.
func ExampleConfig_Validate() {
	cfg := config.Config{Plan: "1xCPU-1GB", Template: "t", HostnamePrefix: "x", StorageSizeGB: 10, PublicIPv4: true}
	fmt.Println(cfg.Validate())
	// Output: config: zone is required
}
