package tui

import (
	"errors"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/workspace"
)

func TestFailedInteractiveDiscoveryRemainsRecoverable(t *testing.T) {
	app := NewApp(newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()}), withoutHerdrSessions())
	endpoint := herdrhub.Endpoint{ID: "box", Label: "Box", Target: "box"}
	app.hubEndpoints = []herdrhub.Endpoint{herdrhub.LocalEndpoint(), endpoint}
	app.listView.SetHerdrHubSnapshots(app.hubEndpoints, nil)
	app.Update(herdrHubInteractiveInventoryMsg{Endpoint: endpoint, Err: errors.New("PRIVATE raw execution failure")})
	if app.err != nil || app.currentView != ViewList {
		t.Fatalf("global failure: %v, %v", app.err, app.currentView)
	}
	if len(app.hubSnapshots) != 1 || app.hubSnapshots[0].Error == "" {
		t.Fatalf("no recoverable endpoint guidance: %+v", app.hubSnapshots)
	}
	if app.hubSnapshots[0].Error == "PRIVATE raw execution failure" {
		t.Fatal("raw error exposed")
	}
}
