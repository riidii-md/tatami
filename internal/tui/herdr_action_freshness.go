package tui

import (
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	"slices"
)

func (a *App) checkHubWorkspace(ws *workspace.Workspace) bool {
	if ws == nil || ws.Remote == nil || ws.Remote.Source == nil {
		return true
	}
	source := ws.Remote.Source
	for _, e := range herdrhub.ReachableEndpoints(a.hubEndpoints, a.hubSnapshots) {
		if e.Key() == source.EndpointKey && e.RootID == source.RootID && e.RootRevision == source.RootRevision && herdrhub.RouteFingerprint(e) == source.RouteFingerprint {
			for _, snapshot := range a.hubSnapshots {
				if snapshot.EndpointID != e.Key() || !herdrhub.SnapshotMatches(e, snapshot) {
					continue
				}
				for _, summary := range snapshot.Workspaces {
					current, ok := remoteWorkspaceListItem(e, summary)
					if ok && current.Workspace.Name == ws.Name && current.Workspace.Folder == ws.Folder &&
						current.Workspace.Path == ws.Path && current.Workspace.Remote.Host == ws.Remote.Host &&
						current.Workspace.Remote.Path == ws.Remote.Path && slices.Equal(current.Workspace.Remote.Jump, ws.Remote.Jump) {
						return true
					}
				}
			}
			break
		}
	}
	a.listView.hubNotice = "The selected remote route changed or was removed. Choose a current item."
	a.currentView = ViewList
	return false
}
