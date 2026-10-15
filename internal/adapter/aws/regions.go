package aws

import "infra/internal/domain"

// Regions returns the commercial AWS regions. The list is static so the
// selector works before any credentials are valid (ec2:DescribeRegions
// would need them).
func Regions() []domain.Region {
	return []domain.Region{
		{Code: "us-east-1", Name: "N. Virginia"},
		{Code: "us-east-2", Name: "Ohio"},
		{Code: "us-west-1", Name: "N. California"},
		{Code: "us-west-2", Name: "Oregon"},
		{Code: "sa-east-1", Name: "São Paulo"},
		{Code: "ca-central-1", Name: "Canada (Central)"},
		{Code: "ca-west-1", Name: "Canada (Calgary)"},
		{Code: "mx-central-1", Name: "Mexico (Central)"},
		{Code: "eu-central-1", Name: "Frankfurt"},
		{Code: "eu-central-2", Name: "Zurich"},
		{Code: "eu-west-1", Name: "Ireland"},
		{Code: "eu-west-2", Name: "London"},
		{Code: "eu-west-3", Name: "Paris"},
		{Code: "eu-north-1", Name: "Stockholm"},
		{Code: "eu-south-1", Name: "Milan"},
		{Code: "eu-south-2", Name: "Spain"},
		{Code: "af-south-1", Name: "Cape Town"},
		{Code: "me-south-1", Name: "Bahrain"},
		{Code: "me-central-1", Name: "UAE"},
		{Code: "il-central-1", Name: "Tel Aviv"},
		{Code: "ap-east-1", Name: "Hong Kong"},
		{Code: "ap-south-1", Name: "Mumbai"},
		{Code: "ap-south-2", Name: "Hyderabad"},
		{Code: "ap-northeast-1", Name: "Tokyo"},
		{Code: "ap-northeast-2", Name: "Seoul"},
		{Code: "ap-northeast-3", Name: "Osaka"},
		{Code: "ap-southeast-1", Name: "Singapore"},
		{Code: "ap-southeast-2", Name: "Sydney"},
		{Code: "ap-southeast-3", Name: "Jakarta"},
		{Code: "ap-southeast-4", Name: "Melbourne"},
		{Code: "ap-southeast-5", Name: "Malaysia"},
		{Code: "ap-southeast-7", Name: "Thailand"},
	}
}
