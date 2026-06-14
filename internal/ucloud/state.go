package ucloud

import "github.com/UpCloudLtd/upcloud-go-api/v8/upcloud"

// State is the provider-generic lifecycle state the instance group reports,
// decoupled from both the SDK's raw strings and the fleeting state enum (wired
// in M3/M4).
type State string

const (
	StateRunning   State = "running"   // booted (note: running != SSH-ready)
	StateCreating  State = "creating"  // provisioning / maintenance
	StateDeleting  State = "deleting"  // stopped on the way to deletion
	StateUnhealthy State = "unhealthy" // error or unrecognized
)

// MapState translates an UpCloud server state into the lifecycle state. In the
// VM-per-job model a server is only ever stopped immediately before deletion,
// so "stopped" maps to deleting. Anything unrecognized fails safe to unhealthy.
func MapState(upcloudState string) State {
	switch upcloudState {
	case upcloud.ServerStateStarted:
		return StateRunning
	case upcloud.ServerStateMaintenance:
		return StateCreating
	case upcloud.ServerStateStopped:
		return StateDeleting
	case upcloud.ServerStateError:
		return StateUnhealthy
	default:
		return StateUnhealthy
	}
}
