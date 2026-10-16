package domain

import "time"

// Parameter types of the parameter store.
const (
	ParameterString       = "String"
	ParameterStringList   = "StringList"
	ParameterSecureString = "SecureString"
)

// Parameter is a configuration parameter (SSM Parameter Store).
type Parameter struct {
	Name         string
	ARN          string
	Type         string
	Description  string
	Tier         string
	KeyID        string // KMS key of SecureString parameters
	Version      int64
	LastModified time.Time
	ModifiedBy   string
}

// Secure reports whether the value is encrypted and must be revealed
// explicitly.
func (p Parameter) Secure() bool { return p.Type == ParameterSecureString }

// Secret is a secret (Secrets Manager); its value is read on demand.
type Secret struct {
	Name            string
	ARN             string
	Description     string
	KMSKeyID        string
	RotationEnabled bool
	LastChanged     time.Time
	LastAccessed    time.Time
	LastRotated     time.Time
	Tags            map[string]string
}

// SecretValue is the current version of a secret.
type SecretValue struct {
	Text      string
	Binary    []byte
	VersionID string
	Stages    []string
	CreatedAt time.Time
}
