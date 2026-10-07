package main

import (
	"path/filepath"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/config"
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/tui"
	"github.com/OleksandrBesan/tatami/internal/workspace"
)

func TestHubHandoffResolvesCurrentProfileAndRejectsStaleRoot(t *testing.T) {
	paths := &config.Paths{HerdrHostsFile: filepath.Join(t.TempDir(), "hosts.json")}
	s := herdrhub.NewStore(paths.HerdrHostsFile)
	p := herdrhub.SavedHost{ID: "box", Label: "Box", Connection: herdrhub.HostConnection{Mode: herdrhub.ModeExplicit, Hostname: "box", Username: "oles", Port: 2222, Auth: "identity", IdentityFile: "/local/key"}}
	if err := s.SaveProfiles([]herdrhub.SavedHost{p}); err != nil {
		t.Fatal(err)
	}
	ep, _ := s.List()
	root := ep[1]
	result := &tui.Result{Action: tui.ActionCD, Workspace: &workspace.Workspace{Name: "remote", Path: "/srv/remote", Remote: &workspace.Remote{Host: root.Target, Path: "/srv/remote", Source: herdrhub.EndpointOrigin(root)}}}
	resolved, err := resolveHubResult(paths, result)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Workspace.Remote.Connection == nil || resolved.Workspace.Remote.Connection.IdentityFile != "/local/key" {
		t.Fatalf("%+v", resolved)
	}
	if result.Workspace.Remote.Connection != nil {
		t.Fatal("mutated TUI handoff")
	}
	p, err = s.Resolve("box")
	if err != nil {
		t.Fatal(err)
	}
	p.Connection.Port = 2223
	if err := s.SaveProfiles([]herdrhub.SavedHost{p}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveHubResult(paths, result); err == nil {
		t.Fatal("changed root accepted")
	}
	if err := s.Delete("box"); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveHubResult(paths, result); err == nil {
		t.Fatal("deleted root accepted")
	}
}
