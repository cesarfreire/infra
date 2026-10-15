package aws

import (
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/aws/smithy-go"

	"infra/internal/domain"
)

// AWS error codes, across services, that mean "not allowed".
var accessDeniedCodes = map[string]bool{
	"AccessDenied":          true,
	"AccessDeniedException": true,
	"UnauthorizedOperation": true,
	"UnauthorizedException": true,
	"Forbidden":             true,
}

// AWS error codes that mean the credentials themselves are unusable.
var unauthenticatedCodes = map[string]bool{
	"ExpiredToken":                true,
	"ExpiredTokenException":       true,
	"InvalidClientTokenId":        true,
	"UnrecognizedClientException": true,
	"AuthFailure":                 true,
	"SignatureDoesNotMatch":       true,
	"InvalidSignatureException":   true,
	"RequestExpired":              true,
}

// classify tags err with domain.ErrAccessDenied or domain.ErrUnauthenticated
// when it is a permission or credential problem, keeping the original error
// in the chain. Other errors are returned unchanged.
func classify(err error) error {
	if err == nil {
		return nil
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch code := apiErr.ErrorCode(); {
		case accessDeniedCodes[code]:
			return fmt.Errorf("%w: %w", domain.ErrAccessDenied, err)
		case unauthenticatedCodes[code]:
			return fmt.Errorf("%w: %w", domain.ErrUnauthenticated, err)
		}
		return err
	}
	// Credential providers fail before any request is sent and have no
	// common error type.
	var tokenErr *ssocreds.InvalidTokenError
	msg := err.Error()
	if errors.As(err, &tokenErr) ||
		strings.Contains(msg, "failed to refresh cached credentials") ||
		strings.Contains(msg, "failed to retrieve credentials") ||
		strings.Contains(msg, "no EC2 IMDS role found") {
		return fmt.Errorf("%w: %w", domain.ErrUnauthenticated, err)
	}
	return err
}
