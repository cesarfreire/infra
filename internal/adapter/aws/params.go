package aws

import (
	"context"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"infra/internal/domain"
)

// SSMAPI is the subset of *ssm.Client used.
type SSMAPI interface {
	ssm.DescribeParametersAPIClient
	GetParameter(ctx context.Context, in *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
}

// ParameterRepository reads the SSM Parameter Store. It implements
// app.ParameterRepository.
type ParameterRepository struct {
	client SSMAPI
}

// NewParameterRepository returns a repository backed by client.
func NewParameterRepository(client SSMAPI) *ParameterRepository {
	return &ParameterRepository{client: client}
}

// ListParameters returns every parameter (metadata only).
func (r *ParameterRepository) ListParameters(ctx context.Context) ([]domain.Parameter, error) {
	var params []domain.Parameter
	pages := ssm.NewDescribeParametersPaginator(r.client, &ssm.DescribeParametersInput{MaxResults: sdkaws.Int32(50)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		for _, p := range page.Parameters {
			params = append(params, domain.Parameter{
				Name:         sdkaws.ToString(p.Name),
				ARN:          sdkaws.ToString(p.ARN),
				Type:         string(p.Type),
				Description:  sdkaws.ToString(p.Description),
				Tier:         string(p.Tier),
				KeyID:        sdkaws.ToString(p.KeyId),
				Version:      p.Version,
				LastModified: sdkaws.ToTime(p.LastModifiedDate),
				ModifiedBy:   sdkaws.ToString(p.LastModifiedUser),
			})
		}
	}
	return params, nil
}

// ParameterValue reads a value, decrypting SecureString parameters.
func (r *ParameterRepository) ParameterValue(ctx context.Context, name string) (string, error) {
	out, err := r.client.GetParameter(ctx, &ssm.GetParameterInput{Name: &name, WithDecryption: sdkaws.Bool(true)})
	if err != nil {
		return "", classify(err)
	}
	if out.Parameter == nil {
		return "", nil
	}
	return sdkaws.ToString(out.Parameter.Value), nil
}

// SecretsManagerAPI is the subset of *secretsmanager.Client used.
type SecretsManagerAPI interface {
	secretsmanager.ListSecretsAPIClient
	GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// SecretRepository reads Secrets Manager. It implements app.SecretRepository.
type SecretRepository struct {
	client SecretsManagerAPI
}

// NewSecretRepository returns a repository backed by client.
func NewSecretRepository(client SecretsManagerAPI) *SecretRepository {
	return &SecretRepository{client: client}
}

// ListSecrets returns every secret (metadata only, never values).
func (r *SecretRepository) ListSecrets(ctx context.Context) ([]domain.Secret, error) {
	var secrets []domain.Secret
	pages := secretsmanager.NewListSecretsPaginator(r.client, &secretsmanager.ListSecretsInput{})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		for _, s := range page.SecretList {
			sec := domain.Secret{
				Name:            sdkaws.ToString(s.Name),
				ARN:             sdkaws.ToString(s.ARN),
				Description:     sdkaws.ToString(s.Description),
				KMSKeyID:        sdkaws.ToString(s.KmsKeyId),
				RotationEnabled: sdkaws.ToBool(s.RotationEnabled),
				LastChanged:     sdkaws.ToTime(s.LastChangedDate),
				LastAccessed:    sdkaws.ToTime(s.LastAccessedDate),
				LastRotated:     sdkaws.ToTime(s.LastRotatedDate),
				Tags:            map[string]string{},
			}
			for _, tag := range s.Tags {
				sec.Tags[sdkaws.ToString(tag.Key)] = sdkaws.ToString(tag.Value)
			}
			secrets = append(secrets, sec)
		}
	}
	return secrets, nil
}

// SecretValue reads the current version of a secret.
func (r *SecretRepository) SecretValue(ctx context.Context, arn string) (domain.SecretValue, error) {
	out, err := r.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &arn})
	if err != nil {
		return domain.SecretValue{}, classify(err)
	}
	return domain.SecretValue{
		Text:      sdkaws.ToString(out.SecretString),
		Binary:    out.SecretBinary,
		VersionID: sdkaws.ToString(out.VersionId),
		Stages:    out.VersionStages,
		CreatedAt: sdkaws.ToTime(out.CreatedDate),
	}, nil
}
