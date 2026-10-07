package tui

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

func focusHostAction(app *App, action int) {
	for i := 0; i < 40 && app.herdrHostView.focus != action; i++ {
		app.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
}
func TestDraftTestDoesNotSaveAndFailureStaysInForm(t *testing.T) {
	saved := 0
	refreshed := 0
	app := NewApp(newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()}), withoutHerdrSessions(),
		WithHerdrHubEndpointSaver(func([]herdrhub.Endpoint) error { saved++; return nil }),
		WithHerdrHubInteractiveInventory(func(context.Context, herdrhub.Endpoint, io.Reader, io.Writer) (herdrhub.Snapshot, error) {
			return herdrhub.Snapshot{}, nil
		}),
		WithHerdrHubRefresh(func(context.Context, []herdrhub.Endpoint, herdrhub.Cache) []herdrhub.Snapshot {
			refreshed++
			return nil
		}, nil),
	)
	app.herdrHostView = NewHerdrHostView(herdrhub.Endpoint{})
	app.currentView = ViewHerdrHost
	app.herdrHostView.inputs[hostLabel].SetValue("Box")
	app.herdrHostView.inputs[hostHostname].SetValue("box")
	focusHostAction(app, hostTest)
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || saved != 0 || refreshed != 0 {
		t.Fatalf("draft Test saved/refreshed %d/%d", saved, refreshed)
	}
	v := app.herdrHostView
	p, _ := v.Profile(nil)
	app.Update(herdrHostTestResultMsg{View: v, Profile: p, Err: errors.New("PRIVATE")})
	if app.currentView != ViewHerdrHost || app.err != nil || v.err == nil {
		t.Fatal("test failure not recoverable")
	}
	app.Update(herdrHostTestResultMsg{View: v, Profile: p, Snapshot: herdrhub.Snapshot{State: herdrhub.StateOnline, Legacy: true}})
	if v.testSnapshot == nil || saved != 0 {
		t.Fatal("Test implicitly saved")
	}
	focusHostAction(app, hostSave)
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || saved != 1 || refreshed != 0 {
		t.Fatal("Save implicitly tested")
	}
	if len(app.hubSnapshots) != 1 || app.hubSnapshots[0].RootRevision == "" {
		t.Fatal("matching Test snapshot was not bound to saved profile")
	}
}
