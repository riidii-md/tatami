package main

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/workspace"
)

func TestNewTabProcessStartsShellInWorkspace(t *testing.T) {
	ws := &workspace.Workspace{Name: "project", Path: "/tmp/project"}

	process, err := newTabProcess(ws, "/bin/sh", exec.LookPath)
	if err != nil {
		t.Fatalf("newTabProcess: %v", err)
	}

	if process.path != "/bin/sh" {
		t.Fatalf("process path = %q; want /bin/sh", process.path)
	}
	if process.dir != ws.Path {
		t.Fatalf("process dir = %q; want %q", process.dir, ws.Path)
	}
	if !reflect.DeepEqual(process.args, []string{"/bin/sh"}) {
		t.Fatalf("process args = %#v; want shell argv", process.args)
	}
}

func TestNewTabProcessStartsRemoteSession(t *testing.T) {
	ws := &workspace.Workspace{
		Name: "server",
		Remote: &workspace.Remote{
			Host: "user@example.com",
			Key:  "/tmp/my key",
			Path: "/srv/project with quote's",
		},
	}
	lookPath := func(name string) (string, error) {
		if name != "ssh" {
			t.Fatalf("looked up %q; want ssh", name)
		}
		return "/usr/bin/ssh", nil
	}

	process, err := newTabProcess(ws, "/bin/sh", lookPath)
	if err != nil {
		t.Fatalf("newTabProcess: %v", err)
	}

	wantArgs := []string{
		"/usr/bin/ssh",
		"-i", "/tmp/my key",
		"-t",
		"--",
		"user@example.com",
		"cd '/srv/project with quote'\"'\"'s' && exec ${SHELL:-/bin/sh}",
	}
	if process.path != "/usr/bin/ssh" {
		t.Fatalf("process path = %q; want /usr/bin/ssh", process.path)
	}
	if process.dir != "" {
		t.Fatalf("remote process dir = %q; want empty", process.dir)
	}
	if !reflect.DeepEqual(process.args, wantArgs) {
		t.Fatalf("process args = %#v; want %#v", process.args, wantArgs)
	}
}

func TestKittyConfigLaunchesNewTabMode(t *testing.T) {
	cmd := exec.Command("bash", "../../scripts/kitty-integration.sh", "--print")
	cmd.Env = append(os.Environ(), "TATAMI_BIN=/tmp/tatami")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("print Kitty config: %v\n%s", err, output)
	}

	for _, binding := range []string{
		"map kitty_mod+t launch --type=tab --cwd=current /tmp/tatami --new-tab",
		"map cmd+t launch --type=tab --cwd=current /tmp/tatami --new-tab",
	} {
		if !strings.Contains(string(output), binding) {
			t.Errorf("Kitty config missing %q:\n%s", binding, output)
		}
	}
}
