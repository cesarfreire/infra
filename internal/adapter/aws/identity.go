package aws

import (
	"context"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"infra/internal/domain"
)

// STSAPI is the subset of *sts.Client used.
type STSAPI interface {
	GetCallerIdentity(ctx context.Context, in *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

// IdentityChecker validates credentials. GetCallerIdentity needs no IAM
// permission, so a failure always means the credentials are unusable.
type IdentityChecker struct {
	client STSAPI
}

// NewIdentityChecker returns a checker backed by client.
func NewIdentityChecker(client STSAPI) *IdentityChecker {
	return &IdentityChecker{client: client}
}

// CallerIdentity returns the account and ARN of the current credentials.
func (c *IdentityChecker) CallerIdentity(ctx context.Context) (domain.Identity, error) {
	out, err := c.client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return domain.Identity{}, classify(err)
	}
	return domain.Identity{Account: sdkaws.ToString(out.Account), ARN: sdkaws.ToString(out.Arn)}, nil
}
