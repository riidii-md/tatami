package tui

import (
	"errors"
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	"testing"
	"time"
)

func TestCacheFailureCannotReviveDelayedChildMessages(t *testing.T) {
	app := NewApp(newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()}), withoutHerdrSessions(), WithHerdrHubRefresh(nil, func(herdrhub.Cache) error { return errors.New("PRIVATE raw failure") }))
	root, _ := herdrhub.LegacyProfile(herdrhub.Endpoint{ID: "root", Label: "Root", Target: "root"}).Endpoint()
	host := herdrhub.Endpoint{ID: "child", Label: "Child", Target: "child"}
	child, _ := herdrhub.DescendantEndpoint(root, host)
	parent := herdrhub.StampSnapshot(root, herdrhub.Snapshot{State: herdrhub.StateOnline, LastSuccess: time.Now(), Hosts: []herdrhub.Endpoint{host}})
	late := herdrhub.StampSnapshot(child, herdrhub.Snapshot{State: herdrhub.StateOnline, LastSuccess: time.Now(), Workspaces: []herdrhub.WorkspaceSummary{{Name: "late", Path: "/late"}}, Sessions: []herdrhub.Session{{SessionKey: herdrhub.SessionKey{EndpointID: child.Key(), SessionName: "agents"}}}})
	app.hubEndpoints = []herdrhub.Endpoint{root}
	app.applyHubUpdates([]herdrhub.Snapshot{parent, late})
	op := app.hubOperationGenerations[child.Key()]
	agentGeneration := app.herdrHubAgentGeneration
	parent.Hosts = nil
	app.Update(herdrHubRefreshResultMsg{Generation: app.herdrHubGeneration, Endpoint: root, OperationGeneration: app.hubOperationGenerations[root.Key()], Snapshots: []herdrhub.Snapshot{parent}})
	if app.err != nil || app.listView.hubNotice == "" {
		t.Fatal("cache failure not endpoint-recoverable")
	}
	parent.Hosts = []herdrhub.Endpoint{host}
	app.Update(herdrHubRefreshResultMsg{Generation: app.herdrHubGeneration, Endpoint: root, OperationGeneration: app.hubOperationGenerations[root.Key()], Snapshots: []herdrhub.Snapshot{parent}})
	fresh := late
	fresh.Workspaces = nil
	app.applyHubUpdates([]herdrhub.Snapshot{fresh})
	app.listView.SetHerdrHubSnapshots(app.hubEndpoints, app.hubSnapshots)
	app.listView.ExpandHerdrEndpoint(root.Key())
	app.listView.ExpandHerdrEndpoint(child.Key())
	for i, item := range app.listView.items {
		if item.Type == "herdr_session" && item.Endpoint.Key() == child.Key() {
			app.listView.cursor = i
		}
	}
	selected := app.listView.Selected()
	if selected == nil || selected.Herdr == nil || selected.Endpoint.Key() != child.Key() {
		t.Fatal("fixture did not select the current child session")
	}
	app.Update(herdrHubRefreshResultMsg{Generation: app.herdrHubGeneration, Endpoint: child, OperationGeneration: op, Snapshots: []herdrhub.Snapshot{late}})
	app.Update(herdrHubInteractiveInventoryMsg{Endpoint: child, OperationGeneration: op, Snapshot: late})
	app.Update(herdrHubAgentsResultMsg{Generation: agentGeneration, Endpoint: child, EndpointID: child.Key(), Session: "agents", Agents: []herdrhub.Agent{{Kind: "late-private-agent"}}})
	for _, snapshot := range app.hubSnapshots {
		if snapshot.EndpointID == child.Key() && len(snapshot.Workspaces) != 0 {
			t.Fatal("delayed result replaced fresh child after failed cache write")
		}
	}
	if len(app.listView.hubAgents[hubSessionKey(child.Key(), "agents")]) != 0 {
		t.Fatal("delayed agents applied")
	}
}
