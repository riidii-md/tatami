package shell

import (
	"strings"

	"github.com/OleksandrBesan/tatami/internal/workspace"
)

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// BuildRemoteSSHCommand builds a local-shell-safe SSH command for a workspace.
// ProxyJump is intentionally used instead of agent forwarding: every hop is
// authenticated by the machine where Tatami is running.
func BuildRemoteSSHCommand(remote *workspace.Remote, command string) string {
	if remote == nil {
		return ""
	}

	parts := []string{"ssh"}
	if remote.Key != "" {
		parts = append(parts, "-i", shellQuote(remote.Key))
	}
	if len(remote.Jump) > 0 {
		parts = append(parts, "-J", shellQuote(strings.Join(remote.Jump, ",")))
	}
	parts = append(parts, "-t", "--", shellQuote(remote.Host))

	remoteCommand := command
	if remoteCommand == "" {
		remoteCommand = "exec ${SHELL:-/bin/sh}"
	}
	if remote.Path != "" {
		remoteCommand = "cd " + shellQuote(remote.Path) + " && " + remoteCommand
	}
	parts = append(parts, shellQuote(remoteCommand))
	return strings.Join(parts, " ")
}
