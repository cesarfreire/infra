package aws

import (
	"encoding/json"
	"fmt"
	"strconv"

	"infra/internal/app"
	"infra/internal/domain"
)

// DefaultShell is the remote command for instance shell sessions. The
// default SSM session runs /bin/sh (dash on Debian/Ubuntu), which has no tab
// completion or history, so bash is preferred when the instance has it.
const DefaultShell = "command -v bash >/dev/null && exec bash -l || exec sh"

// DefaultContainerShell is the command run by ECS Exec. ECS Exec does not
// go through a shell, hence the explicit sh -c; many images have no bash.
const DefaultContainerShell = `/bin/sh -c "command -v bash >/dev/null && exec bash || exec sh"`

// Shells are the commands run by interactive sessions.
type Shells struct {
	// Instance runs through AWS-StartInteractiveCommand; empty uses the
	// default SSM session document.
	Instance string
	// Container is the ECS Exec --command.
	Container string
}

// SessionCommandBuilder builds `aws ssm start-session` commands. Sessions go
// through the AWS CLI because the interactive part is handled by the
// session-manager-plugin, which has no Go library. It implements
// app.SessionCommandBuilder, app.ContainerSessionBuilder and
// app.JumpHostSessionBuilder.
type SessionCommandBuilder struct {
	binary  string
	profile string
	region  string
	shells  Shells
}

// NewSessionCommandBuilder returns a builder that targets the given profile and region.
func NewSessionCommandBuilder(profile, region string, shells Shells) SessionCommandBuilder {
	if shells.Container == "" {
		shells.Container = DefaultContainerShell
	}
	return SessionCommandBuilder{binary: "aws", profile: profile, region: region, shells: shells}
}

// ShellSession starts an interactive shell on the instance.
func (b SessionCommandBuilder) ShellSession(instanceID string) app.Command {
	if b.shells.Instance == "" {
		return b.command("ssm", "start-session", "--target", instanceID)
	}
	return b.RemoteCommandSession(instanceID, b.shells.Instance)
}

// RemoteCommandSession runs command interactively on the instance.
func (b SessionCommandBuilder) RemoteCommandSession(instanceID, command string) app.Command {
	return b.command("ssm", "start-session",
		"--target", instanceID,
		"--document-name", "AWS-StartInteractiveCommand",
		"--parameters", parameters(map[string][]string{"command": {command}}),
	)
}

// ContainerShellSession starts an interactive shell in a container with ECS Exec.
func (b SessionCommandBuilder) ContainerShellSession(task domain.Task, c domain.Container) app.Command {
	return b.command("ecs", "execute-command",
		"--cluster", task.Cluster,
		"--task", task.ARN,
		"--container", c.Name,
		"--interactive",
		"--command", b.shells.Container,
	)
}

// ContainerPortForwardSession forwards a local port to a port in the
// container, through the SSM target ECS Exec registers for it.
func (b SessionCommandBuilder) ContainerPortForwardSession(task domain.Task, c domain.Container, pf domain.PortForward) app.Command {
	pf.Target = fmt.Sprintf("ecs:%s_%s_%s", task.Cluster, task.ID, c.RuntimeID)
	return b.PortForwardSession(pf)
}

// PortForwardSession forwards pf.LocalPort to pf.RemotePort on the target,
// or on pf.RemoteHost through the target when it is set.
func (b SessionCommandBuilder) PortForwardSession(pf domain.PortForward) app.Command {
	if pf.RemoteHost != "" {
		return b.command("ssm", "start-session",
			"--target", pf.Target,
			"--document-name", "AWS-StartPortForwardingSessionToRemoteHost",
			"--parameters", parameters(map[string][]string{
				"host":            {pf.RemoteHost},
				"portNumber":      {strconv.Itoa(pf.RemotePort)},
				"localPortNumber": {strconv.Itoa(pf.LocalPort)},
			}),
		)
	}
	return b.command("ssm", "start-session",
		"--target", pf.Target,
		"--document-name", "AWS-StartPortForwardingSession",
		"--parameters", parameters(map[string][]string{
			"portNumber":      {strconv.Itoa(pf.RemotePort)},
			"localPortNumber": {strconv.Itoa(pf.LocalPort)},
		}),
	)
}

// parameters encodes SSM document parameters as the JSON the CLI expects.
func parameters(p map[string][]string) string {
	b, _ := json.Marshal(p)
	return string(b)
}

func (b SessionCommandBuilder) command(args ...string) app.Command {
	if b.region != "" {
		args = append(args, "--region", b.region)
	}
	if b.profile != "" {
		args = append(args, "--profile", b.profile)
	}
	return app.Command{Name: b.binary, Args: args}
}
