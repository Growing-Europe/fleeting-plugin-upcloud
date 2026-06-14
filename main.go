// Command fleeting-plugin-upcloud is a GitLab Runner fleeting provider plugin that provisions and
// autoscales UpCloud cloud servers as ephemeral CI runners — one isolated VM per job.
//
// STATUS: scaffold. The fleeting InstanceGroup interface
// (Init / Update / Increase / Decrease / ConnectInfo) over the UpCloud Server API is not yet
// implemented. See README.md for the design.
package main

import "fmt"

// Version is set at build time via -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	// TODO: replace with the fleeting plugin entrypoint once the InstanceGroup is implemented, e.g.:
	//   plugin.Main(&InstanceGroup{}, Version)
	fmt.Printf("fleeting-plugin-upcloud %s — scaffold; plugin not yet implemented\n", Version)
}
