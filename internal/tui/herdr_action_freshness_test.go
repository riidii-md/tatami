package tui

import (
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestCapturedHubWorkspaceRejectedAfterInventoryReplacement(t *testing.T) {
	for _, change := range []string{"removed", "path", "target"} {
		for _, action := range []Action{ActionCD, ActionNewTab, ActionNewPane, ActionWithTemplate} {
			t.Run(change+string(rune('0'+action)), func(t *testing.T) {
				app := NewApp(newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()}), withoutHerdrSessions())
				root, _ := herdrhub.LegacyProfile(herdrhub.Endpoint{ID: "root", Label: "Root", Target: "root"}).Endpoint()
				original := herdrhub.WorkspaceSummary{Name: "project", Path: "/srv/old", Target: "target-a"}
				snapshot := herdrhub.StampSnapshot(root, herdrhub.Snapshot{State: herdrhub.StateOnline, Workspaces: []herdrhub.WorkspaceSummary{original}})
				app.hubEndpoints = []herdrhub.Endpoint{root}
				app.applyHubUpdates([]herdrhub.Snapshot{snapshot})
				app.listView.SetHerdrHubSnapshots(app.hubEndpoints, app.hubSnapshots)
				app.listView.ExpandHerdrEndpoint(root.Key())
				for i, item := range app.listView.items {
					if item.Workspace != nil && item.Endpoint != nil {
						app.listView.cursor = i
					}
				}
				app.Update(tea.KeyMsg{Type: tea.KeyEnter})
				if app.currentView != ViewActions {
					t.Fatal("action menu not opened")
				}
				if app.actionsView.Workspace().Remote == nil || app.actionsView.Workspace().Remote.Source == nil {
					t.Fatal("fixture did not select a hub workspace")
				}
				app.actionsView = NewActionView(app.actionsView.Workspace(), true, false, false)
				for i := 0; i < 8 && app.actionsView.Selected() != action; i++ {
					app.Update(tea.KeyMsg{Type: tea.KeyDown})
				}
				if app.actionsView.Selected() != action {
					t.Fatal("fixture action unavailable")
				}
				if change == "removed" {
					snapshot.Workspaces = nil
				} else {
					replacement := original
					if change == "path" {
						replacement.Path = "/srv/new"
					} else {
						replacement.Target = "target-b"
					}
					snapshot.Workspaces = []herdrhub.WorkspaceSummary{replacement}
				}
				app.Update(herdrHubRefreshResultMsg{Generation: app.herdrHubGeneration, Endpoint: root, OperationGeneration: app.hubOperationGenerations[root.Key()], Snapshots: []herdrhub.Snapshot{snapshot}})
				app.Update(tea.KeyMsg{Type: tea.KeyEnter})
				if app.Result() != nil || app.currentView != ViewList || !strings.Contains(app.listView.hubNotice, "changed") {
					t.Fatalf("stale action accepted: view=%v result=%+v notice=%s", app.currentView, app.Result(), app.listView.hubNotice)
				}
			})
		}
	}
}
