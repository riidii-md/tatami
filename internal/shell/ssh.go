package shell

import (
	"errors"
	"github.com/OleksandrBesan/tatami/internal/sshconn"
	"github.com/OleksandrBesan/tatami/internal/workspace"
)

func shellQuote(value string) string {
	return sshconn.QuotePOSIX(value)
}

// BuildRemoteSSHCommand builds a local-shell-safe SSH command for a workspace.
// ProxyJump is intentionally used instead of agent forwarding: every hop is
// authenticated by the machine where Tatami is running.
func BuildRemoteSSHCommand(remote *workspace.Remote, command string) (string, error) {
	cmd, err := BuildRemoteSSH(remote, command)
	if err != nil {
		return "", err
	}
	return sshconn.RenderPOSIX(cmd), nil
}

// BuildRemoteSSH adapts both persisted legacy remotes and resolved hub profiles.
func BuildRemoteSSH(remote *workspace.Remote, command string) (sshconn.Command, error) {
	if remote == nil {
		return sshconn.Command{}, errors.New("remote SSH settings are required")
	}
	if !sshconn.SafeText(remote.Path) {
		return sshconn.Command{}, errors.New("remote workspace path contains unsupported characters")
	}

	connection := sshconn.Connection{Destination: remote.Host, IdentityFile: remote.Key, Jump: remote.Jump}
	if remote.Connection != nil {
		connection = *remote.Connection
	}

	remoteCommand := command
	if remoteCommand == "" {
		remoteCommand = "exec ${SHELL:-/bin/sh}"
	}
	if remote.Path != "" {
		remoteCommand = "cd " + shellQuote(remote.Path) + " && " + remoteCommand
	}
	return sshconn.Build(connection, sshconn.Attach, remoteCommand)
}
