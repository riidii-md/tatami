package main

import (
	"github.com/OleksandrBesan/tatami/internal/config"
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/tui"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	"path/filepath"
	"strings"
	"testing"
)

func TestStructuredHandoffReachesRawAndRenderedWorkspaceConsumers(t *testing.T) {
	paths := &config.Paths{HerdrHostsFile: filepath.Join(t.TempDir(), "hosts.json")}
	store := herdrhub.NewStore(paths.HerdrHostsFile)
	p := herdrhub.SavedHost{ID: "box", Label: "Box", Connection: herdrhub.HostConnection{Mode: herdrhub.ModeExplicit, Hostname: "box", Username: "fixture", Port: 2222, Auth: "certificate", IdentityFile: "/local/key with space", CertificateFile: "/local/cert with space", Jump: []string{"relay"}}}
	if err := store.SaveProfiles([]herdrhub.SavedHost{p}); err != nil {
		t.Fatal(err)
	}
	endpoints, _ := store.List()
	e := endpoints[1]
	result := &tui.Result{Action: tui.ActionCD, Workspace: &workspace.Workspace{Name: "remote", Path: "/srv/my project", Remote: &workspace.Remote{Host: e.Target, Path: "/srv/my project", Jump: e.Via, Source: herdrhub.EndpointOrigin(e)}}}
	resolved, err := resolveHubResult(paths, result)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := newTabProcess(resolved.Workspace, "", func(string) (string, error) { return "/usr/bin/ssh", nil })
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := shell.BuildRemoteSSHCommand(resolved.Workspace.Remote, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-a", "2222", "fixture", "/local/key with space", "CertificateFile=", "/local/cert with space", "IdentitiesOnly=yes", "relay", "-t", "/srv/my project"} {
		if !strings.Contains(strings.Join(raw.args, " "), want) || !strings.Contains(rendered, want) {
			t.Errorf("lost %q: raw=%q rendered=%s", want, raw.args, rendered)
		}
	}
	if result.Workspace.Remote.Connection != nil {
		t.Fatal("captured authentication leaked into Result")
	}
}
