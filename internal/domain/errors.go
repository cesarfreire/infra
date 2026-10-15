package domain

import "errors"

var (
	// ErrInstanceNotRunning is returned when an operation requires a running instance.
	ErrInstanceNotRunning = errors.New("instance is not running")
	// ErrInvalidPort is returned for ports outside the 1-65535 range.
	ErrInvalidPort = errors.New("port must be between 1 and 65535")
)

var (
	// ErrTaskNotRunning is returned when an operation requires a running task.
	ErrTaskNotRunning = errors.New("task is not running")
	// ErrContainerNotRunning is returned when an operation requires a running container.
	ErrContainerNotRunning = errors.New("container is not running")
	// ErrExecNotEnabled is returned when ECS Exec is not available for a task.
	ErrExecNotEnabled = errors.New("ECS Exec is not enabled (enableExecuteCommand)")
)

var (
	// ErrAccessDenied means the credentials are valid but lack permission
	// for the operation (IAM policy, SCP...).
	ErrAccessDenied = errors.New("access denied")
	// ErrUnauthenticated means there are no usable credentials: missing,
	// invalid or expired (e.g. an SSO session that needs a new login).
	ErrUnauthenticated = errors.New("not authenticated")
	// ErrNoRegion is returned when neither the user nor the profile chose a region.
	ErrNoRegion = errors.New("no region configured")
)

var (
	// ErrNotSupported is returned for operations a resource cannot perform
	// (e.g. a CLI session to memcached).
	ErrNotSupported = errors.New("not supported")
	// ErrNotAvailable is returned when a resource is not ready for connections.
	ErrNotAvailable = errors.New("not available")
)

// ErrReadOnly is returned for changes attempted under a read-only profile.
var ErrReadOnly = errors.New("the profile is read-only (settings.json)")

// ErrInvalidState is returned for operations not allowed in a resource's
// current state (e.g. stopping a stopped instance).
var ErrInvalidState = errors.New("not allowed in the current state")
