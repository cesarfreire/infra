package aws

import (
	"context"
	"fmt"
	"strings"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"golang.org/x/sync/errgroup"

	"infra/internal/domain"
)

// ELBAPI is the subset of *elasticloadbalancingv2.Client used.
type ELBAPI interface {
	elb.DescribeLoadBalancersAPIClient
	elb.DescribeListenersAPIClient
	elb.DescribeTargetGroupsAPIClient
	DescribeTargetHealth(ctx context.Context, in *elb.DescribeTargetHealthInput, optFns ...func(*elb.Options)) (*elb.DescribeTargetHealthOutput, error)
}

// LoadBalancerRepository reads ELB v2 load balancers. It implements
// app.LoadBalancerRepository.
type LoadBalancerRepository struct {
	client ELBAPI
}

// NewLoadBalancerRepository returns a repository backed by client.
func NewLoadBalancerRepository(client ELBAPI) *LoadBalancerRepository {
	return &LoadBalancerRepository{client: client}
}

// ListLoadBalancers returns every application, network and gateway load balancer.
func (r *LoadBalancerRepository) ListLoadBalancers(ctx context.Context) ([]domain.LoadBalancer, error) {
	var lbs []domain.LoadBalancer
	pages := elb.NewDescribeLoadBalancersPaginator(r.client, &elb.DescribeLoadBalancersInput{})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		for _, lb := range page.LoadBalancers {
			d := domain.LoadBalancer{
				Name:      sdkaws.ToString(lb.LoadBalancerName),
				ARN:       sdkaws.ToString(lb.LoadBalancerArn),
				Type:      string(lb.Type),
				Scheme:    string(lb.Scheme),
				DNSName:   sdkaws.ToString(lb.DNSName),
				VPC:       sdkaws.ToString(lb.VpcId),
				CreatedAt: sdkaws.ToTime(lb.CreatedTime),
			}
			if lb.State != nil {
				d.State = string(lb.State.Code)
			}
			for _, z := range lb.AvailabilityZones {
				d.Zones = append(d.Zones, sdkaws.ToString(z.ZoneName))
			}
			lbs = append(lbs, d)
		}
	}
	return lbs, nil
}

// ListListeners returns the listeners of lb.
func (r *LoadBalancerRepository) ListListeners(ctx context.Context, lb domain.LoadBalancer) ([]domain.Listener, error) {
	var listeners []domain.Listener
	pages := elb.NewDescribeListenersPaginator(r.client, &elb.DescribeListenersInput{LoadBalancerArn: &lb.ARN})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		for _, l := range page.Listeners {
			listeners = append(listeners, domain.Listener{
				Protocol: string(l.Protocol),
				Port:     int(sdkaws.ToInt32(l.Port)),
				Action:   describeActions(l.DefaultActions),
			})
		}
	}
	return listeners, nil
}

func describeActions(actions []types.Action) string {
	var parts []string
	for _, a := range actions {
		switch a.Type {
		case types.ActionTypeEnumForward:
			target := lastSegmentN(sdkaws.ToString(a.TargetGroupArn), 2)
			if target == "" && a.ForwardConfig != nil {
				var names []string
				for _, tg := range a.ForwardConfig.TargetGroups {
					names = append(names, lastSegmentN(sdkaws.ToString(tg.TargetGroupArn), 2))
				}
				target = strings.Join(names, ", ")
			}
			parts = append(parts, "forward to "+target)
		case types.ActionTypeEnumRedirect:
			if c := a.RedirectConfig; c != nil {
				parts = append(parts, fmt.Sprintf("redirect to %s:%s", sdkaws.ToString(c.Protocol), sdkaws.ToString(c.Port)))
			}
		case types.ActionTypeEnumFixedResponse:
			if c := a.FixedResponseConfig; c != nil {
				parts = append(parts, "fixed response "+sdkaws.ToString(c.StatusCode))
			}
		default:
			parts = append(parts, string(a.Type))
		}
	}
	return strings.Join(parts, "; ")
}

// lastSegmentN returns the n-th segment from the end of a "/"-separated ARN
// (target group ARNs end in name/id).
func lastSegmentN(arn string, n int) string {
	parts := strings.Split(arn, "/")
	if len(parts) < n {
		return arn
	}
	return parts[len(parts)-n]
}

// ListTargetGroups returns the target groups of lb with the health of their
// targets, read in parallel.
func (r *LoadBalancerRepository) ListTargetGroups(ctx context.Context, lb domain.LoadBalancer) ([]domain.TargetGroup, error) {
	var groups []domain.TargetGroup
	pages := elb.NewDescribeTargetGroupsPaginator(r.client, &elb.DescribeTargetGroupsInput{LoadBalancerArn: &lb.ARN})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		for _, tg := range page.TargetGroups {
			health := string(tg.HealthCheckProtocol)
			if p := sdkaws.ToString(tg.HealthCheckPath); p != "" {
				health += " " + p
			}
			groups = append(groups, domain.TargetGroup{
				Name:        sdkaws.ToString(tg.TargetGroupName),
				ARN:         sdkaws.ToString(tg.TargetGroupArn),
				Protocol:    string(tg.Protocol),
				Port:        int(sdkaws.ToInt32(tg.Port)),
				TargetType:  string(tg.TargetType),
				HealthCheck: strings.TrimSpace(health),
			})
		}
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(describeConcurrency)
	for i := range groups {
		g.Go(func() error {
			out, err := r.client.DescribeTargetHealth(ctx, &elb.DescribeTargetHealthInput{TargetGroupArn: &groups[i].ARN})
			if err != nil {
				return classify(err)
			}
			for _, d := range out.TargetHealthDescriptions {
				t := domain.Target{}
				if d.Target != nil {
					t.ID = sdkaws.ToString(d.Target.Id)
					t.Port = int(sdkaws.ToInt32(d.Target.Port))
					t.Zone = sdkaws.ToString(d.Target.AvailabilityZone)
				}
				if h := d.TargetHealth; h != nil {
					t.State, t.Reason, t.Description = string(h.State), string(h.Reason), sdkaws.ToString(h.Description)
				}
				groups[i].Targets = append(groups[i].Targets, t)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return groups, nil
}
