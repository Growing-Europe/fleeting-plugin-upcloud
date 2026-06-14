// Command fleeting-plugin-upcloud is a GitLab Runner fleeting provider plugin
// that provisions and autoscales UpCloud cloud servers as ephemeral CI runners —
// one isolated VM per job (max_use_count = 1).
//
// It is launched by the GitLab Runner autoscaler as a go-plugin over gRPC; the
// runner's plugin_config is decoded into the instance group and the provider
// interface is served. See README.md for configuration.
package main

import (
	"gitlab.com/gitlab-org/fleeting/fleeting/plugin"

	"github.com/Growing-Europe/fleeting-plugin-upcloud/internal/group"
)

// Version is set at build time via -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	// The framework json-unmarshals plugin_config into the InstanceGroup and
	// then calls Init; the group needs no construction here. Kept deliberately
	// thin — all logic lives in tested internal packages.
	plugin.Main(&group.InstanceGroup{}, plugin.VersionInfo{
		Name:    "fleeting-plugin-upcloud",
		Version: Version,
	})
}
