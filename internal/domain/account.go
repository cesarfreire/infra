package domain

// Profile is a named set of credentials and settings of the provider's CLI.
type Profile struct {
	Name   string
	Region string // default region; may be empty
	SSO    bool
}

// Region is a provider region.
type Region struct {
	Code string // e.g. sa-east-1
	Name string // e.g. São Paulo
}

// Identity is who the current credentials belong to.
type Identity struct {
	Account string
	ARN     string
}
