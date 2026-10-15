package domain

import "fmt"

// PortForward describes a tunnel from a local port to a port on a remote
// target (an instance, or a container).
type PortForward struct {
	// Target is the provider's session target, e.g. an instance ID.
	Target string
	// RemoteHost, when set, makes Target a jump host: the tunnel goes
	// through it to RemoteHost:RemotePort (e.g. a cache endpoint in the VPC).
	RemoteHost string
	LocalPort  int
	RemotePort int
}

// Validate checks that both ports are usable.
func (p PortForward) Validate() error {
	if err := ValidatePort(p.RemotePort); err != nil {
		return fmt.Errorf("remote %w", err)
	}
	if err := ValidatePort(p.LocalPort); err != nil {
		return fmt.Errorf("local %w", err)
	}
	return nil
}

// ValidatePort returns ErrInvalidPort when port is out of range.
func ValidatePort(port int) error {
	if port < 1 || port > 65535 {
		return ErrInvalidPort
	}
	return nil
}
