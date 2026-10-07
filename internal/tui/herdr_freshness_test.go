package tui

import (
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	"testing"
)

func TestDelayedChildResultCannotReviveRemovedReaddedRoute(t *testing.T) {
	app := NewApp(newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()}), withoutHerdrSessions())
	p := herdrhub.LegacyProfile(herdrhub.Endpoint{ID: "root", Label: "Root", Target: "root"})
	root, _ := p.Endpoint()
	child, _ := herdrhub.DescendantEndpoint(root, herdrhub.Endpoint{ID: "child", Label: "Child", Target: "child"})
	parent := herdrhub.StampSnapshot(root, herdrhub.Snapshot{State: herdrhub.StateOnline, Hosts: []herdrhub.Endpoint{{ID: "child", Label: "Child", Target: "child"}}})
	app.hubEndpoints = []herdrhub.Endpoint{root}
	app.applyHubUpdates([]herdrhub.Snapshot{parent})
	generation := app.hubOperationGenerations[child.Key()]
	parent.Hosts = nil
	app.applyHubUpdates([]herdrhub.Snapshot{parent})
	parent.Hosts = []herdrhub.Endpoint{{ID: "child", Label: "Child", Target: "child"}}
	app.applyHubUpdates([]herdrhub.Snapshot{parent})
	late := herdrhub.StampSnapshot(child, herdrhub.Snapshot{State: herdrhub.StateOnline, Workspaces: []herdrhub.WorkspaceSummary{{Name: "old", Path: "/old"}}})
	app.Update(herdrHubInteractiveInventoryMsg{Endpoint: child, OperationGeneration: generation, Snapshot: late})
	for _, s := range app.hubSnapshots {
		if s.EndpointID == child.Key() {
			t.Fatalf("late result revived %+v", s)
		}
	}
}
