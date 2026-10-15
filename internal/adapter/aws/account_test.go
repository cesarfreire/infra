package aws

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"

	"infra/internal/domain"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"access denied", &smithy.GenericAPIError{Code: "AccessDeniedException"}, domain.ErrAccessDenied},
		{"ec2 unauthorized", &smithy.GenericAPIError{Code: "UnauthorizedOperation"}, domain.ErrAccessDenied},
		{"expired token", &smithy.GenericAPIError{Code: "ExpiredToken"}, domain.ErrUnauthenticated},
		{"credential provider", fmt.Errorf("op: failed to refresh cached credentials, token expired"), domain.ErrUnauthenticated},
		{"throttling", &smithy.GenericAPIError{Code: "Throttling"}, nil},
		{"other", errors.New("boom"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify(tt.err)
			if !errors.Is(got, tt.err) {
				t.Errorf("the original error must stay in the chain: %v", got)
			}
			for _, sentinel := range []error{domain.ErrAccessDenied, domain.ErrUnauthenticated} {
				if want := tt.want != nil && errors.Is(tt.want, sentinel); errors.Is(got, sentinel) != want {
					t.Errorf("classify() = %v; Is(%v) = %v", got, sentinel, errors.Is(got, sentinel))
				}
			}
		})
	}
	if classify(nil) != nil {
		t.Error("classify(nil) must be nil")
	}
}

func TestProfileStore(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	credentials := filepath.Join(dir, "credentials")
	os.WriteFile(config, []byte(`
# comment
[default]
region = us-east-1

[profile prod]
sso_session = corp
region=sa-east-1

[sso-session corp]
sso_region = us-east-1

[profile dev]
output = json
`), 0o600)
	os.WriteFile(credentials, []byte(`
[default]
aws_access_key_id = x
[legacy]
aws_access_key_id = y
`), 0o600)

	got, err := (&ProfileStore{configPath: config, credentialsPath: credentials}).List()
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Profile{
		{Name: "default", Region: "us-east-1"},
		{Name: "dev"},
		{Name: "legacy"},
		{Name: "prod", Region: "sa-east-1", SSO: true},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("List() = %+v, want %+v", got, want)
	}
}

func TestProfileStoreMissingFiles(t *testing.T) {
	got, err := (&ProfileStore{configPath: "/nonexistent/a", credentialsPath: "/nonexistent/b"}).List()
	if err != nil || len(got) != 0 {
		t.Errorf("List() = %v, %v; want empty, nil", got, err)
	}
}

type fakeSTS struct{ err error }

func (f fakeSTS) GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &sts.GetCallerIdentityOutput{Account: sdkaws.String("123456789012"), Arn: sdkaws.String("arn:aws:iam::123456789012:user/me")}, nil
}

func TestCallerIdentity(t *testing.T) {
	id, err := NewIdentityChecker(fakeSTS{}).CallerIdentity(context.Background())
	if err != nil || id.Account != "123456789012" {
		t.Errorf("CallerIdentity() = %+v, %v", id, err)
	}
	_, err = NewIdentityChecker(fakeSTS{err: &smithy.GenericAPIError{Code: "ExpiredToken"}}).CallerIdentity(context.Background())
	if !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("expired credentials should be ErrUnauthenticated, got %v", err)
	}
}
