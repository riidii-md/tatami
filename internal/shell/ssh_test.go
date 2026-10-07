package shell

import (
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/workspace"
)

func TestBuildRemoteSSHCommandIncludesProxyJumpAndQuotesValues(t *testing.T) {
	remote := &workspace.Remote{
		Host: "user@macmini",
		Path: "/srv/team project",
		Key:  "/keys/team key",
		Jump: []string{"user@bastion", "relay"},
	}

	got, err := BuildRemoteSSHCommand(remote, "")
	if err != nil {
		t.Fatal(err)
	}
	want := "'ssh' '-a' '-i' '/keys/team key' '-J' 'user@bastion,relay' '-t' '--' 'user@macmini' 'cd '\"'\"'/srv/team project'\"'\"' && exec ${SHELL:-/bin/sh}'"
	if got != want {
		t.Fatalf("SSH command = %q; want %q", got, want)
	}
	if strings.Contains(got, " -A ") || strings.Contains(got, "ForwardAgent") {
		t.Fatalf("SSH command forwards the local agent: %q", got)
	}
}

func TestBuildRemoteSSHCommandRunsLayoutCommandAfterChangingDirectory(t *testing.T) {
	remote := &workspace.Remote{Host: "host", Path: "/srv/project"}
	got, err := BuildRemoteSSHCommand(remote, "npm test")
	if err != nil {
		t.Fatal(err)
	}
	want := "'ssh' '-a' '-t' '--' 'host' 'cd '\"'\"'/srv/project'\"'\"' && npm test'"
	if got != want {
		t.Fatalf("SSH command = %q; want %q", got, want)
	}
}
