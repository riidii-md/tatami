package tui

import (
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	"testing"
)

func TestHubWorkspaceOriginSurvivesActionsWithoutCapturedAuth(t *testing.T) {
	p := herdrhub.SavedHost{ID: "box", Label: "Box", ConnectivityRevision: "revision", Connection: herdrhub.HostConnection{Mode: herdrhub.ModeExplicit, Hostname: "box", Auth: "identity", IdentityFile: "/local/key"}}
	e, _ := p.Endpoint()
	l := NewListView(newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()}))
	l.SetHerdrHubSnapshots([]herdrhub.Endpoint{e}, []herdrhub.Snapshot{herdrhub.StampSnapshot(e, herdrhub.Snapshot{State: herdrhub.StateOnline, Workspaces: []herdrhub.WorkspaceSummary{{Name: "remote", Path: "/srv/remote"}}})})
	var ws *workspace.Workspace
	for _, item := range l.items {
		if item.Endpoint != nil && item.Workspace != nil {
			ws = item.Workspace
		}
	}
	if ws == nil || ws.Remote.Connection == nil || ws.Remote.Source == nil {
		t.Fatalf("lost runtime settings: %+v", ws)
	}
	view := NewActionView(ws, false, false, false)
	safe := safeResultWorkspace(view.Workspace())
	if safe.Remote.Connection != nil || safe.Remote.Source.RootID != "box" {
		t.Fatalf("unsafe handoff %+v", safe.Remote)
	}
	if ws.Remote.Connection == nil {
		t.Fatal("mutated source runtime workspace")
	}
}
