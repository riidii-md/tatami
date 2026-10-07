package herdrhub

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/OleksandrBesan/tatami/internal/sshconn"
)

type FailureKind string

const (
	FailureUnreachable     FailureKind = "unreachable"
	FailureHostKey         FailureKind = "host-key"
	FailureAuthentication  FailureKind = "authentication"
	FailureMissingTools    FailureKind = "missing-tools"
	FailureInvalidOverride FailureKind = "invalid-override"
	FailureIncompatible    FailureKind = "incompatible-output"
	FailureOutputLimit     FailureKind = "output-limit"
	FailureSelectedTool    FailureKind = "selected-tool"
	FailureIndeterminate   FailureKind = "query-or-connection"
	FailureCancelled       FailureKind = "cancelled"
)

type QueryError struct {
	Kind   FailureKind
	Status int
}

func (e *QueryError) Error() string {
	switch e.Kind {
	case FailureUnreachable:
		return "SSH host is unreachable or timed out; verify destination and network."
	case FailureHostKey:
		return "SSH host-key interaction is required; connect with OpenSSH to verify the host."
	case FailureAuthentication:
		return "SSH authentication is required or rejected; use Test interactively or configure an OpenSSH identity/agent."
	case FailureMissingTools:
		return "No executable Tatami or Herdr was found remotely; install one or configure its executable path."
	case FailureInvalidOverride:
		return "A configured remote executable was not found or is not executable; check Advanced settings."
	case FailureIncompatible:
		return "Remote discovery returned incompatible output; check tool version and shell startup output."
	case FailureOutputLimit:
		return "Remote discovery exceeded its output limit."
	case FailureSelectedTool:
		return fmt.Sprintf("The selected remote tool failed (status %d); check its installation. No fallback connection was opened.", e.Status)
	case FailureCancelled:
		return "Remote discovery was cancelled."
	default:
		return "Remote query or SSH connection failed; verify the remote tool and connection. Exit status alone cannot identify password failure."
	}
}
func (e *QueryError) State() EndpointState {
	switch e.Kind {
	case FailureAuthentication, FailureHostKey:
		return StateAuthenticationNeeded
	case FailureMissingTools, FailureInvalidOverride, FailureIncompatible, FailureOutputLimit, FailureSelectedTool:
		return StateIncompatible
	default:
		return StateOffline
	}
}

// Selection happens once, inside the same SSH process. Never source startup files.
const resolverScript = `resolve() {
 if [ -n "$2" ]; then
  p="$2"
  [ -n "$p" ] && [ -f "$p" ] && [ -x "$p" ] || return 2
  printf '%s\n' "$p"; return 0
 fi
 p=$(command -v "$1")
 if [ -n "$p" ] && [ -f "$p" ] && [ -x "$p" ]; then printf '%s\n' "$p"; return 0; fi
 for base in "$HOME/go/bin" "$HOME/.local/bin" /usr/local/bin /opt/homebrew/bin; do
  p="$base/$1"
  if [ -f "$p" ] && [ -x "$p" ]; then printf '%s\n' "$p"; return 0; fi
 done
 return 1
}
`
const probeScript = resolverScript + `tool=$(resolve tatami "$1")
status=$?
if [ "$status" -eq 0 ]; then
 printf '%s\n' 'TATAMI-HUB-PROBE 1 inventory'
 exec "$tool" hub inventory --json
fi
if [ "$status" -eq 2 ]; then printf '%s\n' 'TATAMI-HUB-PROBE 1 missing'; exit 126; fi
tool=$(resolve herdr "$2")
status=$?
if [ "$status" -eq 0 ]; then
 printf '%s\n' 'TATAMI-HUB-PROBE 1 sessions'
 exec "$tool" session list --json
fi
printf '%s\n' 'TATAMI-HUB-PROBE 1 missing'
if [ "$status" -eq 2 ]; then exit 126; fi
exit 127
`

func remoteScript(script string, args ...string) string {
	command := "sh -c " + sshconn.QuotePOSIX(script) + " tatami"
	for _, arg := range args {
		command += " " + sshconn.QuotePOSIX(arg)
	}
	return command
}
func endpointConnection(e Endpoint) sshconn.Connection {
	if e.Connection != nil {
		c := *e.Connection
		c.Jump = append([]string(nil), e.Via...)
		return c
	}
	return sshconn.Connection{Destination: e.Target, Jump: append([]string(nil), e.Via...)}
}

func EndpointConnection(e Endpoint) sshconn.Connection { return endpointConnection(e) }

func EndpointOrigin(e Endpoint) *sshconn.Origin {
	if e.RootID == "" {
		return nil
	}
	return &sshconn.Origin{RootID: e.RootID, RootRevision: e.RootRevision, EndpointKey: e.Key(), RouteFingerprint: RouteFingerprint(e), Target: e.Target, Via: append([]string(nil), e.Via...)}
}
func endpointCommand(e Endpoint, op sshconn.Operation, remote string) (string, []string, error) {
	if err := validateRoutedEndpoint(e); err != nil {
		return "", nil, err
	}
	c, err := sshconn.Build(endpointConnection(e), op, remote)
	return c.Executable, c.Args, err
}
func ProbeArgs(e Endpoint, batch bool) (string, []string, error) {
	op := sshconn.Interactive
	if batch {
		op = sshconn.Background
	}
	return endpointCommand(e, op, remoteScript(probeScript, e.TatamiExecutable, e.HerdrExecutable))
}
func remoteHerdrCommand(e Endpoint, args ...string) string {
	script := resolverScript + `tool=$(resolve herdr "$1")
status=$?
if [ "$status" -ne 0 ]; then exit 127; fi
shift
exec "$tool" "$@"
`
	return remoteScript(script, append([]string{e.HerdrExecutable}, args...)...)
}
func commandStatus(err error) int {
	if err == nil {
		return 0
	}
	var status interface{ ExitCode() int }
	if errors.As(err, &status) {
		return status.ExitCode()
	}
	return -1
}
func transportFailure(ctx context.Context, r ExecResult, err error) *QueryError {
	q := &QueryError{Kind: FailureIndeterminate, Status: commandStatus(err)}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			q.Kind = FailureCancelled
		} else {
			q.Kind = FailureUnreachable
		}
		return q
	}
	text := strings.ToLower(string(r.Stderr))
	switch {
	case strings.Contains(text, "host key verification failed"), strings.Contains(text, "authenticity of host"), strings.Contains(text, "remote host identification"):
		q.Kind = FailureHostKey
	case strings.Contains(text, "permission denied"), strings.Contains(text, "authentication failed"):
		q.Kind = FailureAuthentication
	case strings.Contains(text, "connection refused"), strings.Contains(text, "could not resolve"), strings.Contains(text, "timed out"), strings.Contains(text, "no route to host"):
		q.Kind = FailureUnreachable
	}
	return q
}
func parseProbe(ctx context.Context, e Endpoint, r ExecResult, err error) (Snapshot, error) {
	if ctx.Err() != nil {
		return Snapshot{}, transportFailure(ctx, r, err)
	}
	if r.StdoutTruncated || r.StderrTruncated || len(r.Stdout) > (2<<20)+128 || len(r.Stderr) > 4096 {
		return Snapshot{}, &QueryError{Kind: FailureOutputLimit, Status: commandStatus(err)}
	}
	header, body, found := bytes.Cut(r.Stdout, []byte("\n"))
	framing := found && len(header) <= 128
	kind := ""
	if framing {
		switch string(header) {
		case "TATAMI-HUB-PROBE 1 inventory":
			kind = "inventory"
		case "TATAMI-HUB-PROBE 1 sessions":
			kind = "sessions"
		case "TATAMI-HUB-PROBE 1 missing":
			kind = "missing"
		default:
			framing = false
		}
	}
	if !framing {
		if err != nil {
			return Snapshot{}, transportFailure(ctx, r, err)
		}
		return Snapshot{}, &QueryError{Kind: FailureIncompatible}
	}
	if len(body) > 2<<20 {
		return Snapshot{}, &QueryError{Kind: FailureOutputLimit, Status: commandStatus(err)}
	}
	status := commandStatus(err)
	if kind == "missing" && len(body) != 0 {
		return Snapshot{}, &QueryError{Kind: FailureIncompatible, Status: status}
	}
	if kind == "missing" && len(body) == 0 {
		if status == 127 {
			return Snapshot{}, &QueryError{Kind: FailureMissingTools, Status: status}
		}
		if status == 126 {
			return Snapshot{}, &QueryError{Kind: FailureInvalidOverride, Status: status}
		}
	}
	if err != nil {
		if status == 255 {
			return Snapshot{}, &QueryError{Kind: FailureIndeterminate, Status: status}
		}
		if status < 0 {
			return Snapshot{}, transportFailure(ctx, r, err)
		}
		return Snapshot{}, &QueryError{Kind: FailureSelectedTool, Status: status}
	}
	if kind == "inventory" {
		inv, parseErr := ParseInventory(body)
		if parseErr != nil {
			return Snapshot{}, &QueryError{Kind: FailureIncompatible}
		}
		return snapshotFromInventory(e, inv, 0), nil
	}
	if kind == "sessions" {
		sessions, parseErr := ParseSessions(e.Key(), body)
		if parseErr != nil {
			return Snapshot{}, &QueryError{Kind: FailureIncompatible}
		}
		return Snapshot{EndpointID: e.Key(), State: StateOnline, Sessions: sessions, LastSuccess: time.Now().UTC(), Legacy: true}, nil
	}
	return Snapshot{}, &QueryError{Kind: FailureIncompatible}
}
