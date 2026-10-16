package app_test

import (
	"context"
	"errors"
	"testing"

	"infra/internal/app"
	"infra/internal/domain"
)

type fakeConfig struct{ err error }

func (f fakeConfig) ListParameters(context.Context) ([]domain.Parameter, error) {
	return []domain.Parameter{{Name: "/b"}, {Name: "/a"}}, f.err
}

func (f fakeConfig) ParameterValue(_ context.Context, name string) (string, error) {
	return "value of " + name, f.err
}

func (f fakeConfig) ListSecrets(context.Context) ([]domain.Secret, error) {
	return []domain.Secret{{Name: "z"}, {Name: "y"}}, f.err
}

func (f fakeConfig) SecretValue(_ context.Context, arn string) (domain.SecretValue, error) {
	return domain.SecretValue{Text: "secret of " + arn}, f.err
}

func TestConfigService(t *testing.T) {
	svc := app.NewConfigService(app.ConfigDeps{Parameters: fakeConfig{}, Secrets: fakeConfig{}})
	ctx := context.Background()
	ps, _ := svc.Parameters(ctx)
	ss, _ := svc.Secrets(ctx)
	if ps[0].Name != "/a" || ss[0].Name != "y" {
		t.Errorf("not sorted: %v %v", ps, ss)
	}
	if v, _ := svc.ParameterValue(ctx, ps[0]); v != "value of /a" {
		t.Errorf("value = %q", v)
	}
	if v, _ := svc.SecretValue(ctx, domain.Secret{Name: "y", ARN: "arn:y"}); v.Text != "secret of arn:y" {
		t.Errorf("secret = %+v", v)
	}
	boom := errors.New("boom")
	failing := app.NewConfigService(app.ConfigDeps{Parameters: fakeConfig{err: boom}, Secrets: fakeConfig{err: boom}})
	if _, err := failing.SecretValue(ctx, domain.Secret{Name: "y"}); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}
