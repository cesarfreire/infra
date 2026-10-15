package aws

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"infra/internal/domain"
)

// fakeEC2 serves one page per entry, chained by NextToken.
type fakeEC2 struct {
	pages [][]types.Instance
	calls *callLog
}

func (f fakeEC2) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	idx := 0
	if in.NextToken != nil {
		idx = int((*in.NextToken)[0] - '0')
	}
	out := &ec2.DescribeInstancesOutput{
		Reservations: []types.Reservation{{Instances: f.pages[idx]}},
	}
	if idx+1 < len(f.pages) {
		out.NextToken = sdkaws.String(string(rune('0' + idx + 1)))
	}
	return out, nil
}

func TestListInstancesMapsAndPaginates(t *testing.T) {
	launch := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	client := fakeEC2{pages: [][]types.Instance{
		{{
			InstanceId:       sdkaws.String("i-1"),
			InstanceType:     types.InstanceTypeT3Micro,
			State:            &types.InstanceState{Name: types.InstanceStateNameRunning},
			PrivateIpAddress: sdkaws.String("10.0.0.1"),
			Placement:        &types.Placement{AvailabilityZone: sdkaws.String("us-east-1a")},
			LaunchTime:       &launch,
			Tags:             []types.Tag{{Key: sdkaws.String("Name"), Value: sdkaws.String("web")}},
		}},
		{{InstanceId: sdkaws.String("i-2")}},
	}}

	got, err := NewInstanceRepository(client).ListInstances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d instances, want 2", len(got))
	}
	first := got[0]
	if first.ID != "i-1" || first.Name != "web" || first.State != domain.InstanceStateRunning ||
		first.Type != "t3.micro" || first.PrivateIP != "10.0.0.1" ||
		first.AvailabilityZone != "us-east-1a" || !first.LaunchTime.Equal(launch) {
		t.Errorf("unexpected mapping: %+v", first)
	}
	if got[1].ID != "i-2" || got[1].Name != "" {
		t.Errorf("unexpected second instance: %+v", got[1])
	}
}

func TestShellSessionDefaultDocument(t *testing.T) {
	cmd := NewSessionCommandBuilder("dev", "sa-east-1", Shells{}).ShellSession("i-1")
	want := []string{"ssm", "start-session", "--target", "i-1", "--region", "sa-east-1", "--profile", "dev"}
	if cmd.Name != "aws" || !slices.Equal(cmd.Args, want) {
		t.Errorf("ShellSession() = %s %v, want aws %v", cmd.Name, cmd.Args, want)
	}
}

func TestShellSessionInteractiveCommand(t *testing.T) {
	cmd := NewSessionCommandBuilder("", "", Shells{Instance: DefaultShell}).ShellSession("i-1")
	doc := slices.Index(cmd.Args, "--document-name")
	if doc < 0 || cmd.Args[doc+1] != "AWS-StartInteractiveCommand" {
		t.Fatalf("expected AWS-StartInteractiveCommand, got %v", cmd.Args)
	}
	var params map[string][]string
	if err := json.Unmarshal([]byte(cmd.Args[slices.Index(cmd.Args, "--parameters")+1]), &params); err != nil {
		t.Fatal(err)
	}
	if got := params["command"]; len(got) != 1 || got[0] != DefaultShell {
		t.Errorf("command parameter = %v, want [%q]", got, DefaultShell)
	}
}

func TestPortForwardSession(t *testing.T) {
	cmd := NewSessionCommandBuilder("", "", Shells{Instance: DefaultShell}).PortForwardSession(domain.PortForward{
		Target: "i-1", LocalPort: 15432, RemotePort: 5432,
	})
	idx := slices.Index(cmd.Args, "--parameters")
	if idx < 0 || slices.Contains(cmd.Args, "--profile") || slices.Contains(cmd.Args, "--region") {
		t.Fatalf("unexpected args: %v", cmd.Args)
	}
	var params map[string][]string
	if err := json.Unmarshal([]byte(cmd.Args[idx+1]), &params); err != nil {
		t.Fatal(err)
	}
	if params["portNumber"][0] != "5432" || params["localPortNumber"][0] != "15432" {
		t.Errorf("unexpected parameters: %v", params)
	}
}

func TestInstanceURL(t *testing.T) {
	got := NewConsoleURLBuilder("us-east-1").InstanceURL("i-1")
	want := "https://us-east-1.console.aws.amazon.com/ec2/home?region=us-east-1#InstanceDetails:instanceId=i-1"
	if got != want {
		t.Errorf("InstanceURL() = %q, want %q", got, want)
	}
}
