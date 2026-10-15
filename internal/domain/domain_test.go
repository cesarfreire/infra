package domain_test

import (
	"errors"
	"testing"

	"infra/internal/domain"
)

func TestInstanceDisplayName(t *testing.T) {
	if got := (domain.Instance{ID: "i-1", Name: "web"}).DisplayName(); got != "web" {
		t.Errorf("DisplayName() = %q, want %q", got, "web")
	}
	if got := (domain.Instance{ID: "i-1"}).DisplayName(); got != "i-1" {
		t.Errorf("DisplayName() = %q, want %q", got, "i-1")
	}
}

func TestInstanceIsRunning(t *testing.T) {
	tests := map[domain.InstanceState]bool{
		domain.InstanceStateRunning: true,
		domain.InstanceStateStopped: false,
		domain.InstanceStatePending: false,
	}
	for state, want := range tests {
		if got := (domain.Instance{State: state}).IsRunning(); got != want {
			t.Errorf("IsRunning() with state %q = %v, want %v", state, got, want)
		}
	}
}

func TestPortForwardValidate(t *testing.T) {
	tests := []struct {
		name    string
		pf      domain.PortForward
		wantErr bool
	}{
		{"valid", domain.PortForward{LocalPort: 8080, RemotePort: 80}, false},
		{"remote zero", domain.PortForward{LocalPort: 8080, RemotePort: 0}, true},
		{"local too high", domain.PortForward{LocalPort: 70000, RemotePort: 80}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.pf.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, domain.ErrInvalidPort) {
				t.Errorf("Validate() error = %v, want ErrInvalidPort", err)
			}
		})
	}
}
