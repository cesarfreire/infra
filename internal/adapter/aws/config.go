// Package aws implements the application ports on top of the AWS SDK and CLI.
package aws

import (
	"context"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

// LoadConfig resolves AWS credentials and region. Empty values fall back to
// the SDK's default chain (environment, shared config, SSO, IMDS...).
func LoadConfig(ctx context.Context, profile, region string) (sdkaws.Config, error) {
	var opts []func(*config.LoadOptions) error
	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	return config.LoadDefaultConfig(ctx, opts...)
}
