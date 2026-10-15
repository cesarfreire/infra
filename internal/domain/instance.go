// Package domain holds the provider-agnostic entities of the application.
// It must not import any cloud SDK or UI package.
package domain

import "time"

// InstanceState is the lifecycle state of a compute instance.
type InstanceState string

const (
	InstanceStatePending      InstanceState = "pending"
	InstanceStateRunning      InstanceState = "running"
	InstanceStateStopping     InstanceState = "stopping"
	InstanceStateStopped      InstanceState = "stopped"
	InstanceStateShuttingDown InstanceState = "shutting-down"
	InstanceStateTerminated   InstanceState = "terminated"
)

// Instance is a virtual machine running in a cloud provider.
type Instance struct {
	ID               string
	Name             string
	State            InstanceState
	Type             string
	PrivateIP        string
	PublicIP         string
	AvailabilityZone string
	Platform         string
	LaunchTime       time.Time
	Tags             map[string]string
}

// IsRunning reports whether the instance accepts sessions.
func (i Instance) IsRunning() bool {
	return i.State == InstanceStateRunning
}

// DisplayName returns the Name tag, falling back to the instance ID.
func (i Instance) DisplayName() string {
	if i.Name != "" {
		return i.Name
	}
	return i.ID
}
