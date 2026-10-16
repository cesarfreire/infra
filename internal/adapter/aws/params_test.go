package aws

import (
	"context"
	"testing"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"infra/internal/domain"
)

type fakeSSM struct{ get *ssm.GetParameterInput }

func (f *fakeSSM) DescribeParameters(context.Context, *ssm.DescribeParametersInput, ...func(*ssm.Options)) (*ssm.DescribeParametersOutput, error) {
	return &ssm.DescribeParametersOutput{Parameters: []ssmtypes.ParameterMetadata{{
		Name: sdkaws.String("/prod/db/password"), Type: ssmtypes.ParameterTypeSecureString, Version: 3,
		KeyId: sdkaws.String("alias/aws/ssm"), LastModifiedDate: sdkaws.Time(time.Now()),
	}}}, nil
}

func (f *fakeSSM) GetParameter(_ context.Context, in *ssm.GetParameterInput, _ ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	f.get = in
	return &ssm.GetParameterOutput{Parameter: &ssmtypes.Parameter{Value: sdkaws.String("s3cr3t")}}, nil
}

func TestParameters(t *testing.T) {
	f := &fakeSSM{}
	repo := NewParameterRepository(f)
	ps, err := repo.ListParameters(context.Background())
	if err != nil || len(ps) != 1 || !ps[0].Secure() || ps[0].Version != 3 {
		t.Fatalf("ListParameters() = %+v, %v", ps, err)
	}
	v, _ := repo.ParameterValue(context.Background(), ps[0].Name)
	if v != "s3cr3t" || !sdkaws.ToBool(f.get.WithDecryption) {
		t.Errorf("value %q, decrypt %v", v, f.get.WithDecryption)
	}
}

type fakeSecrets struct{}

func (fakeSecrets) ListSecrets(context.Context, *secretsmanager.ListSecretsInput, ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
	return &secretsmanager.ListSecretsOutput{SecretList: []smtypes.SecretListEntry{{
		Name: sdkaws.String("prod/api"), ARN: sdkaws.String("arn:secret:prod/api"), RotationEnabled: sdkaws.Bool(true),
		Tags: []smtypes.Tag{{Key: sdkaws.String("team"), Value: sdkaws.String("core")}},
	}}}, nil
}

func (fakeSecrets) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	return &secretsmanager.GetSecretValueOutput{SecretString: sdkaws.String(`{"user":"api"}`), VersionId: sdkaws.String("v1"),
		VersionStages: []string{"AWSCURRENT"}, Name: in.SecretId}, nil
}

func TestSecrets(t *testing.T) {
	repo := NewSecretRepository(fakeSecrets{})
	ss, err := repo.ListSecrets(context.Background())
	if err != nil || ss[0].Name != "prod/api" || !ss[0].RotationEnabled || ss[0].Tags["team"] != "core" {
		t.Fatalf("ListSecrets() = %+v, %v", ss, err)
	}
	v, _ := repo.SecretValue(context.Background(), ss[0].ARN)
	if v.Text != `{"user":"api"}` || v.Stages[0] != "AWSCURRENT" {
		t.Errorf("value = %+v", v)
	}
}

func TestConfigConsoleURLs(t *testing.T) {
	b := NewConsoleURLBuilder("us-east-1")
	if got := b.ParameterURL(domain.Parameter{Name: "/prod/db/url"}); got != "https://us-east-1.console.aws.amazon.com/systems-manager/parameters/prod%2Fdb%2Furl/description?region=us-east-1" {
		t.Errorf("ParameterURL() = %s", got)
	}
	if got := b.SecretURL(domain.Secret{Name: "prod/api"}); got != "https://us-east-1.console.aws.amazon.com/secretsmanager/secret?name=prod%2Fapi&region=us-east-1" {
		t.Errorf("SecretURL() = %s", got)
	}
}
