package aws

import (
	"fmt"
	"net/url"
	"strings"

	"infra/internal/domain"
)

// ConsoleURLBuilder builds AWS Management Console links. It implements
// app.ConsoleURLBuilder, app.ContainerConsoleURLBuilder,
// app.KeyValueConsoleURLBuilder and app.CacheConsoleURLBuilder.
type ConsoleURLBuilder struct {
	region string
}

// NewConsoleURLBuilder returns a builder for the given region.
func NewConsoleURLBuilder(region string) ConsoleURLBuilder {
	return ConsoleURLBuilder{region: region}
}

// InstanceURL links to the EC2 instance details page.
func (b ConsoleURLBuilder) InstanceURL(instanceID string) string {
	region := url.QueryEscape(b.region)
	return fmt.Sprintf("https://%[1]s.console.aws.amazon.com/ec2/home?region=%[1]s#InstanceDetails:instanceId=%[2]s",
		region, url.QueryEscape(instanceID))
}

// ClusterURL links to the ECS cluster page.
func (b ConsoleURLBuilder) ClusterURL(cluster string) string {
	return b.ecsURL("clusters/" + url.PathEscape(cluster) + "/services")
}

// ServiceURL links to the ECS service page.
func (b ConsoleURLBuilder) ServiceURL(cluster, service string) string {
	return b.ecsURL("clusters/" + url.PathEscape(cluster) + "/services/" + url.PathEscape(service) + "/health")
}

// TaskURL links to the ECS task page.
func (b ConsoleURLBuilder) TaskURL(cluster, taskID string) string {
	return b.ecsURL("clusters/" + url.PathEscape(cluster) + "/tasks/" + url.PathEscape(taskID) + "/configuration")
}

func (b ConsoleURLBuilder) ecsURL(path string) string {
	region := url.QueryEscape(b.region)
	return fmt.Sprintf("https://%[1]s.console.aws.amazon.com/ecs/v2/%[2]s?region=%[1]s", region, path)
}

// TableURL links to the DynamoDB table page.
func (b ConsoleURLBuilder) TableURL(table string) string {
	region := url.QueryEscape(b.region)
	return fmt.Sprintf("https://%[1]s.console.aws.amazon.com/dynamodbv2/home?region=%[1]s#table?name=%[2]s",
		region, url.QueryEscape(table))
}

// CacheURL links to the ElastiCache cache page.
func (b ConsoleURLBuilder) CacheURL(c domain.Cache) string {
	kind := c.Engine
	if c.Serverless {
		kind = "serverless"
	}
	region := url.QueryEscape(b.region)
	return fmt.Sprintf("https://%[1]s.console.aws.amazon.com/elasticache/home?region=%[1]s#/%[2]s/%[3]s",
		region, url.PathEscape(kind), url.PathEscape(c.ID))
}

// QueueURL links to the SQS queue page.
func (b ConsoleURLBuilder) QueueURL(q domain.Queue) string {
	region := url.QueryEscape(b.region)
	return fmt.Sprintf("https://%[1]s.console.aws.amazon.com/sqs/v3/home?region=%[1]s#/queues/%[2]s",
		region, url.QueryEscape(q.URL))
}

// LoadBalancerURL links to the load balancer page of the EC2 console.
func (b ConsoleURLBuilder) LoadBalancerURL(lb domain.LoadBalancer) string {
	region := url.QueryEscape(b.region)
	return fmt.Sprintf("https://%[1]s.console.aws.amazon.com/ec2/home?region=%[1]s#LoadBalancer:loadBalancerArn=%[2]s",
		region, url.QueryEscape(lb.ARN))
}

// ParameterURL links to the parameter in the Systems Manager console.
func (b ConsoleURLBuilder) ParameterURL(p domain.Parameter) string {
	region := url.QueryEscape(b.region)
	return fmt.Sprintf("https://%[1]s.console.aws.amazon.com/systems-manager/parameters/%[2]s/description?region=%[1]s",
		region, url.PathEscape(strings.TrimPrefix(p.Name, "/")))
}

// SecretURL links to the secret in the Secrets Manager console.
func (b ConsoleURLBuilder) SecretURL(s domain.Secret) string {
	region := url.QueryEscape(b.region)
	return fmt.Sprintf("https://%[1]s.console.aws.amazon.com/secretsmanager/secret?name=%[2]s&region=%[1]s",
		region, url.QueryEscape(s.Name))
}
