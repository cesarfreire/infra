package app

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"infra/internal/domain"
)

// ConfigDeps are the adapters the configuration use cases need.
type ConfigDeps struct {
	Parameters ParameterRepository
	Secrets    SecretRepository
	Console    ConfigConsoleURLBuilder
	Opener     URLOpener
}

// ConfigService implements the use cases for parameters and secrets.
// Values are read on demand and never cached.
type ConfigService struct {
	d ConfigDeps
}

// NewConfigService wires the configuration use cases with their adapters.
func NewConfigService(d ConfigDeps) *ConfigService { return &ConfigService{d: d} }

// Parameters returns the parameters sorted by name.
func (s *ConfigService) Parameters(ctx context.Context) ([]domain.Parameter, error) {
	ps, err := s.d.Parameters.ListParameters(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing parameters: %w", err)
	}
	slices.SortFunc(ps, func(a, b domain.Parameter) int { return cmp.Compare(a.Name, b.Name) })
	return ps, nil
}

// ParameterValue reads the value of p (decrypted when secure).
func (s *ConfigService) ParameterValue(ctx context.Context, p domain.Parameter) (string, error) {
	v, err := s.d.Parameters.ParameterValue(ctx, p.Name)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", p.Name, err)
	}
	return v, nil
}

// Secrets returns the secrets sorted by name.
func (s *ConfigService) Secrets(ctx context.Context) ([]domain.Secret, error) {
	ss, err := s.d.Secrets.ListSecrets(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing secrets: %w", err)
	}
	slices.SortFunc(ss, func(a, b domain.Secret) int { return cmp.Compare(a.Name, b.Name) })
	return ss, nil
}

// SecretValue reads the current value of sec.
func (s *ConfigService) SecretValue(ctx context.Context, sec domain.Secret) (domain.SecretValue, error) {
	v, err := s.d.Secrets.SecretValue(ctx, sec.ARN)
	if err != nil {
		return domain.SecretValue{}, fmt.Errorf("reading %s: %w", sec.Name, err)
	}
	return v, nil
}

// OpenParameterConsole opens p in the web console and returns the URL.
func (s *ConfigService) OpenParameterConsole(p domain.Parameter) (string, error) {
	return open(s.d.Opener, s.d.Console.ParameterURL(p))
}

// OpenSecretConsole opens sec in the web console and returns the URL.
func (s *ConfigService) OpenSecretConsole(sec domain.Secret) (string, error) {
	return open(s.d.Opener, s.d.Console.SecretURL(sec))
}
