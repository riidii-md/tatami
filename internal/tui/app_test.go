package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/OleksandrBesan/tatami/internal/config"
	"github.com/OleksandrBesan/tatami/internal/git"
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/systemusage"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

func TestWindowResizeRequestsFullScreenRepaint(t *testing.T) {
	app := NewApp(newTestStore(t, &workspace.Workspace{Name: "project", Path: t.TempDir()}))

	if _, cmd := app.Update(tea.WindowSizeMsg{Width: 76, Height: 40}); cmd != nil {
		t.Fatal("initial window size unexpectedly requested a second repaint")
	}
	if _, cmd := app.Update(tea.WindowSizeMsg{Width: 145, Height: 59}); cmd == nil {
		t.Fatal("window growth did not request a full-screen repaint")
	}
	if _, cmd := app.Update(tea.WindowSizeMsg{Width: 145, Height: 59}); cmd != nil {
		t.Fatal("unchanged window size unexpectedly requested a full-screen repaint")
	}
	if _, cmd := app.Update(tea.WindowSizeMsg{Width: 76, Height: 40}); cmd == nil {
		t.Fatal("window shrink did not request a full-screen repaint")
	}
}

func TestMobileMouseTapOpensExactHomeRowAndAction(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "project-01", Path: t.TempDir()})
	for i := 2; i <= 12; i++ {
		name := fmt.Sprintf("project-%02d", i)
		if err := store.Create(&workspace.Workspace{Name: name, Path: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
	}
	app := NewApp(store, WithMobileMode(), WithHerdrSessionLister(nil))
	app.Update(tea.WindowSizeMsg{Width: 76, Height: 40})

	homeRow := renderedRowContaining(t, app.View(), "project-12")
	model, _ := app.Update(tea.MouseMsg{X: 8, Y: homeRow, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	app = model.(*App)
	if app.currentView != ViewActions || app.actionsView == nil || app.actionsView.Workspace().Name != "project-12" {
		t.Fatalf("tap opened view=%v workspace=%v; want actions for project-12", app.currentView, app.actionsView)
	}

	actionRow := renderedRowContaining(t, app.View(), "cd here")
	model, _ = app.Update(tea.MouseMsg{X: 8, Y: actionRow, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	app = model.(*App)
	if app.result == nil || app.result.Action != ActionCD || app.result.Workspace == nil || app.result.Workspace.Name != "project-12" {
		t.Fatalf("action tap result = %#v; want cd for project-12", app.result)
	}
}

func TestMobileMouseWheelNavigatesHomeRows(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "first", Path: t.TempDir()})
	if err := store.Create(&workspace.Workspace{Name: "second", Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	app := NewApp(store, WithMobileMode(), WithHerdrSessionLister(nil))
	app.Update(tea.WindowSizeMsg{Width: 76, Height: 40})

	model, _ := app.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	app = model.(*App)
	if selected := app.listView.Selected(); selected == nil || selected.Name != "second" {
		t.Fatalf("wheel selected %#v; want second", selected)
	}
}

func TestRemoteAgentDetailUsesCompositeSelectionAndFailureKeepsAttachable(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	endpoint := herdrhub.Endpoint{ID: "work", Label: "Work", Target: "work"}
	app := NewApp(store, withoutHerdrSessions(), WithHerdrHubSnapshots([]herdrhub.Endpoint{herdrhub.LocalEndpoint(), endpoint}, []herdrhub.Snapshot{{EndpointID: "work", State: herdrhub.StateOnline, Sessions: []herdrhub.Session{{SessionKey: herdrhub.SessionKey{EndpointID: "work", SessionName: "same"}, Running: true}}}}), WithHerdrHubAgentQuery(func(context.Context, herdrhub.Endpoint, string) ([]herdrhub.Agent, error) {
		return []herdrhub.Agent{{Kind: "codex", Status: "working", CWD: "/repo"}}, nil
	}))
	for i, item := range app.listView.items {
		if item.Herdr != nil && item.Endpoint != nil && item.Endpoint.ID == "work" {
			app.listView.cursor = i
			break
		}
	}
	cmd := app.scheduleSelectedHubAgents()
	if !strings.Contains(app.listView.View(), "agents loading") {
		t.Fatalf("remote detail did not enter loading state: %s", app.listView.View())
	}
	msg := cmd()
	app.Update(msg)
	if !strings.Contains(app.listView.View(), "1 agents") || !strings.Contains(app.listView.View(), "working") {
		t.Fatalf("remote detail missing: %s", app.listView.View())
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.result == nil || app.result.HerdrEndpointID != "work" || app.result.SessionName != "same" {
		t.Fatalf("result=%#v", app.result)
	}
}

func TestLocalHubCollapseAndRefresh(t *testing.T) {
	calls := 0
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	app := NewApp(store, WithHerdrSessionLister(func() ([]shell.HerdrSession, error) { calls++; return []shell.HerdrSession{{Name: "local"}}, nil }))
	for i, item := range app.listView.items {
		if item.Type == "herdr_endpoint" && item.Endpoint.ID == herdrhub.LocalEndpointID {
			app.listView.cursor = i
		}
	}
	app.Update(tea.KeyMsg{Type: tea.KeySpace})
	if strings.Contains(app.listView.View(), "local stopped") {
		t.Fatal("local children remain after collapse")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if calls < 2 {
		t.Fatalf("local r calls=%d", calls)
	}
}

func TestLocalSessionRefreshDoesNotQueryRemoteEndpoints(t *testing.T) {
	localCalls := 0
	remoteCalls := 0
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	remote := herdrhub.Endpoint{ID: "work", Label: "Work", Target: "work"}
	app := NewApp(store,
		WithHerdrSessionLister(func() ([]shell.HerdrSession, error) {
			localCalls++
			return []shell.HerdrSession{{Name: "local", Running: true}}, nil
		}),
		WithHerdrHubSnapshots([]herdrhub.Endpoint{herdrhub.LocalEndpoint(), remote}, nil),
		WithHerdrHubRefresh(func(context.Context, []herdrhub.Endpoint, herdrhub.Cache) []herdrhub.Snapshot {
			remoteCalls++
			return nil
		}, nil),
	)
	for i, item := range app.listView.items {
		if item.Herdr != nil && item.Endpoint == nil {
			app.listView.cursor = i
			break
		}
	}
	before := localCalls
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd != nil || remoteCalls != 0 || localCalls <= before {
		t.Fatalf("local refresh cmd=%v local=%d/%d remote=%d", cmd, localCalls, before, remoteCalls)
	}
}

func TestRemoteRefreshKeysUseSelectedAndAllEndpointsAndCancel(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	work := herdrhub.Endpoint{ID: "work", Label: "Work", Target: "work"}
	gpu := herdrhub.Endpoint{ID: "gpu", Label: "GPU", Target: "gpu"}
	var calls [][]herdrhub.Endpoint
	var first context.Context
	localCalls := 0
	app := NewApp(store, WithHerdrSessionLister(func() ([]shell.HerdrSession, error) {
		localCalls++
		return nil, nil
	}), WithHerdrHubSnapshots([]herdrhub.Endpoint{herdrhub.LocalEndpoint(), work, gpu}, []herdrhub.Snapshot{
		{EndpointID: "work", Sessions: []herdrhub.Session{{SessionKey: herdrhub.SessionKey{EndpointID: "work", SessionName: "run"}, Running: true}}},
		{EndpointID: "gpu", Sessions: []herdrhub.Session{{SessionKey: herdrhub.SessionKey{EndpointID: "gpu", SessionName: "other"}, Running: true}}},
	}), WithHerdrHubRefresh(func(ctx context.Context, eps []herdrhub.Endpoint, _ herdrhub.Cache) []herdrhub.Snapshot {
		if first == nil {
			first = ctx
		}
		calls = append(calls, eps)
		return []herdrhub.Snapshot{{EndpointID: eps[0].ID, State: herdrhub.StateOnline}}
	}, nil))
	for i, item := range app.listView.items {
		if item.Herdr != nil && item.Endpoint != nil && item.Endpoint.ID == "work" {
			app.listView.cursor = i
			break
		}
	}
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	msg := cmd()
	app.Update(msg)
	if len(calls) != 1 || len(calls[0]) != 1 || calls[0][0].ID != "work" {
		t.Fatalf("selected refresh=%#v", calls)
	}
	if len(app.hubSnapshots) != 2 || app.hubSnapshots[1].EndpointID != "gpu" {
		t.Fatalf("selected refresh discarded other endpoint: %#v", app.hubSnapshots)
	}
	beforeAllLocalCalls := localCalls
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	batchMsg := cmd()
	batch, ok := batchMsg.(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("R command = %T, want two endpoint commands", batchMsg)
	}
	for _, endpointCmd := range batch {
		app.Update(endpointCmd())
	}
	if len(calls) != 3 || len(calls[1]) != 1 || len(calls[2]) != 1 {
		t.Fatalf("all refresh=%#v", calls)
	}
	if localCalls <= beforeAllLocalCalls {
		t.Fatal("R did not refresh visible local sessions")
	}
	if first.Err() == nil {
		t.Fatal("older refresh was not cancelled")
	}
}

func TestEnterDiscoversRemoteTatamiAndKeepsUserOnMainScreen(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	remote := herdrhub.Endpoint{ID: "work", Label: "Work", Target: "work"}
	app := NewApp(store,
		withoutHerdrSessions(),
		WithHerdrHubSnapshots([]herdrhub.Endpoint{herdrhub.LocalEndpoint(), remote}, []herdrhub.Snapshot{{EndpointID: remote.ID, State: herdrhub.StateAuthenticationNeeded}}),
		WithHerdrHubInteractiveInventory(func(context.Context, herdrhub.Endpoint, io.Reader, io.Writer) (herdrhub.Snapshot, error) {
			return herdrhub.Snapshot{}, nil
		}),
	)
	for i, item := range app.listView.items {
		if item.Type == "herdr_endpoint" && item.Endpoint != nil && item.Endpoint.ID == remote.ID {
			app.listView.cursor = i
		}
	}

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.listView.hubCollapsed[remote.ID] {
		t.Fatal("enter on remote endpoint collapsed it instead of opening it")
	}
	if app.result != nil {
		t.Fatalf("endpoint discovery attached immediately: %#v", app.result)
	}
	if cmd == nil {
		t.Fatal("opening remote endpoint did not schedule interactive discovery")
	}

	app.Update(herdrHubInteractiveInventoryMsg{Endpoint: remote, Snapshot: herdrhub.Snapshot{
		EndpointID: remote.ID,
		State:      herdrhub.StateOnline,
		Host:       "work-host",
		Workspaces: []herdrhub.WorkspaceSummary{{Name: "API", Path: "/srv/api", Folder: "team", QuickAccess: true}},
		Sessions:   []herdrhub.Session{{SessionKey: herdrhub.SessionKey{EndpointID: remote.ID, SessionName: "agents"}, Running: true}},
		Hosts:      []herdrhub.Endpoint{{ID: "macmini", Label: "Mac Mini", Target: "macmini"}},
	}})
	if app.currentView != ViewList {
		t.Fatalf("view=%v", app.currentView)
	}
	app.listView.SetSize(120, 60)
	if got := app.listView.View(); !strings.Contains(got, "Quick Access") || !strings.Contains(got, "API") || !strings.Contains(got, "agents") || !strings.Contains(got, "Mac Mini") {
		t.Fatalf("federated main screen missing remote inventory: %s", got)
	}
	for i, item := range app.listView.items {
		if item.Herdr != nil && item.Endpoint != nil && item.Endpoint.Key() == remote.Key() {
			app.listView.cursor = i
			break
		}
	}
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.result == nil || app.result.Action != ActionAttachHerdrSession || app.result.HerdrEndpointID != remote.ID || app.result.HerdrTarget != remote.Target || app.result.SessionName != "agents" || len(app.result.HerdrVia) != 0 {
		t.Fatalf("remote session result = %#v", app.result)
	}
	if cmd == nil {
		t.Fatal("choosing remote session did not quit the chooser")
	}
}

func TestInteractiveRemoteInventoryCommandPassesTerminalIO(t *testing.T) {
	remote := herdrhub.Endpoint{ID: "work", Label: "Work", Target: "work"}
	stdin := strings.NewReader("terminal input")
	stderr := io.Discard
	called := false
	command := &herdrHubInteractiveInventoryCommand{
		endpoint: remote,
		query: func(_ context.Context, got herdrhub.Endpoint, gotStdin io.Reader, gotStderr io.Writer) (herdrhub.Snapshot, error) {
			called = true
			if !reflect.DeepEqual(got, remote) || gotStdin != stdin || gotStderr != stderr {
				t.Fatalf("interactive call endpoint=%#v stdin=%v stderr=%v", got, gotStdin == stdin, gotStderr == stderr)
			}
			return herdrhub.Snapshot{EndpointID: remote.ID, State: herdrhub.StateOnline, Sessions: []herdrhub.Session{{SessionKey: herdrhub.SessionKey{EndpointID: remote.ID, SessionName: "agents"}}}}, nil
		},
	}
	command.SetStdin(stdin)
	command.SetStdout(io.Discard)
	command.SetStderr(stderr)
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	if !called || len(command.snapshot.Sessions) != 1 || command.snapshot.Sessions[0].SessionName != "agents" {
		t.Fatalf("interactive command result = %#v", command.snapshot)
	}
}

func TestEnterOnlineRemoteEndpointStaysExpandedOnMainScreen(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	remote := herdrhub.Endpoint{ID: "work", Label: "Work", Target: "work"}
	app := NewApp(store,
		withoutHerdrSessions(),
		WithHerdrHubSnapshots([]herdrhub.Endpoint{herdrhub.LocalEndpoint(), remote}, []herdrhub.Snapshot{{
			EndpointID: remote.ID,
			State:      herdrhub.StateOnline,
			Sessions:   []herdrhub.Session{{SessionKey: herdrhub.SessionKey{EndpointID: remote.ID, SessionName: "named"}, Running: true}},
		}}),
	)
	for i, item := range app.listView.items {
		if item.Type == "herdr_endpoint" && item.Endpoint != nil && item.Endpoint.ID == remote.ID {
			app.listView.cursor = i
		}
	}

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || app.currentView != ViewList || app.listView.hubCollapsed[remote.Key()] {
		t.Fatalf("online endpoint: cmd=%v view=%v collapsed=%v", cmd, app.currentView, app.listView.hubCollapsed[remote.Key()])
	}
}

func TestHealthyEndpointResultDoesNotWaitForSlowEndpoint(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	slow := herdrhub.Endpoint{ID: "slow", Label: "Slow", Target: "slow"}
	fast := herdrhub.Endpoint{ID: "fast", Label: "Fast", Target: "fast"}
	releaseSlow := make(chan struct{})
	app := NewApp(store,
		withoutHerdrSessions(),
		WithHerdrHubSnapshots([]herdrhub.Endpoint{herdrhub.LocalEndpoint(), slow, fast}, nil),
		WithHerdrHubRefresh(func(_ context.Context, endpoints []herdrhub.Endpoint, _ herdrhub.Cache) []herdrhub.Snapshot {
			endpoint := endpoints[0]
			if endpoint.ID == "slow" {
				<-releaseSlow
			}
			return []herdrhub.Snapshot{{
				EndpointID: endpoint.ID,
				State:      herdrhub.StateOnline,
				Sessions:   []herdrhub.Session{{SessionKey: herdrhub.SessionKey{EndpointID: endpoint.ID, SessionName: endpoint.ID + "-session"}, Running: true}},
			}}
		}, nil),
	)
	batchMsg := app.scheduleHubRefresh([]herdrhub.Endpoint{slow, fast})()
	batch, ok := batchMsg.(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("refresh command = %T", batchMsg)
	}
	results := make(chan tea.Msg, 2)
	for _, cmd := range batch {
		cmd := cmd
		go func() { results <- cmd() }()
	}
	select {
	case msg := <-results:
		app.Update(msg)
		found := false
		for _, item := range app.listView.items {
			if item.Herdr != nil && item.Herdr.Name == "fast-session" {
				found = true
			}
		}
		if !found {
			t.Fatalf("healthy endpoint was not applied first: %#v", app.listView.items)
		}
	case <-time.After(time.Second):
		t.Fatal("healthy endpoint waited for slow endpoint")
	}
	close(releaseSlow)
	select {
	case msg := <-results:
		app.Update(msg)
	case <-time.After(time.Second):
		t.Fatal("slow endpoint did not finish after release")
	}
}

func TestRemoteAgentStaleAndErrorDoNotCrossApply(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	endpoint := herdrhub.Endpoint{ID: "work", Label: "Work", Target: "work"}
	app := NewApp(store, withoutHerdrSessions(), WithHerdrHubSnapshots([]herdrhub.Endpoint{herdrhub.LocalEndpoint(), endpoint}, []herdrhub.Snapshot{{EndpointID: "work", Sessions: []herdrhub.Session{{SessionKey: herdrhub.SessionKey{EndpointID: "work", SessionName: "run"}, Running: true}}}}))
	for i, item := range app.listView.items {
		if item.Herdr != nil && item.Endpoint != nil {
			app.listView.cursor = i
		}
	}
	app.herdrHubAgentGeneration = 2
	app.Update(herdrHubAgentsResultMsg{EndpointID: "work", Session: "run", Generation: 1, Agents: []herdrhub.Agent{{Status: "wrong"}}})
	if strings.Contains(app.listView.View(), "wrong") {
		t.Fatal("stale result applied")
	}
	app.Update(herdrHubAgentsResultMsg{EndpointID: "work", Session: "run", Generation: 2, Err: errors.New("offline")})
	if !strings.Contains(app.listView.View(), "agents unavailable") {
		t.Fatal("error not rendered")
	}
}

func TestRemoteAgentQueryIsCanceledWhenSelectionMoves(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	endpoint := herdrhub.Endpoint{ID: "work", Label: "Work", Target: "work"}
	started := make(chan struct{})
	app := NewApp(store,
		WithHerdrSessionLister(func() ([]shell.HerdrSession, error) {
			return []shell.HerdrSession{{Name: "local", Running: true}}, nil
		}),
		WithHerdrHubSnapshots([]herdrhub.Endpoint{herdrhub.LocalEndpoint(), endpoint}, []herdrhub.Snapshot{{EndpointID: "work", Sessions: []herdrhub.Session{{SessionKey: herdrhub.SessionKey{EndpointID: "work", SessionName: "run"}, Running: true}}}}),
		WithHerdrHubAgentQuery(func(ctx context.Context, _ herdrhub.Endpoint, _ string) ([]herdrhub.Agent, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}),
	)
	for i, item := range app.listView.items {
		if item.Herdr != nil && item.Endpoint != nil {
			app.listView.cursor = i
			break
		}
	}
	cmd := app.scheduleSelectedHubAgents()
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("remote agent query did not start")
	}
	for i, item := range app.listView.items {
		if item.Herdr != nil && item.Endpoint == nil {
			app.listView.cursor = i
			break
		}
	}
	app.scheduleSelectedHubAgents()
	select {
	case msg := <-result:
		got := msg.(herdrHubAgentsResultMsg)
		if !errors.Is(got.Err, context.Canceled) {
			t.Fatalf("canceled query error = %v", got.Err)
		}
	case <-time.After(time.Second):
		t.Fatal("remote agent query was not canceled")
	}
}

func TestStoppedRemoteSessionRestoresThroughExactEndpoint(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	endpoint := herdrhub.Endpoint{ID: "work", Label: "Work", Target: "work"}
	app := NewApp(store, withoutHerdrSessions(), WithHerdrHubSnapshots([]herdrhub.Endpoint{herdrhub.LocalEndpoint(), endpoint}, []herdrhub.Snapshot{{EndpointID: "work", Sessions: []herdrhub.Session{{SessionKey: herdrhub.SessionKey{EndpointID: "work", SessionName: "old"}, Running: false}}}}))
	for i, item := range app.listView.items {
		if item.Herdr != nil && item.Endpoint != nil {
			app.listView.cursor = i
		}
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.result == nil || app.result.HerdrEndpointID != "work" || app.result.SessionName != "old" {
		t.Fatalf("stopped remote result = %#v", app.result)
	}
}

func TestHerdrHostAddEditAndConfirmedRemove(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	work := herdrhub.Endpoint{ID: "work", Label: "Work", Target: "work"}
	var saved [][]herdrhub.Endpoint
	var tested []herdrhub.Endpoint
	app := NewApp(store,
		withoutHerdrSessions(),
		WithHerdrHubSnapshots([]herdrhub.Endpoint{herdrhub.LocalEndpoint(), work}, nil),
		WithHerdrHubEndpointSaver(func(endpoints []herdrhub.Endpoint) error {
			saved = append(saved, append([]herdrhub.Endpoint(nil), endpoints...))
			return nil
		}),
		WithHerdrHubRefresh(func(_ context.Context, endpoints []herdrhub.Endpoint, _ herdrhub.Cache) []herdrhub.Snapshot {
			tested = append(tested, endpoints...)
			return nil
		}, nil),
	)

	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if app.currentView != ViewHerdrHost {
		t.Fatalf("add opened view %v", app.currentView)
	}
	app.herdrHostView.inputs[0].SetValue("gpu")
	app.herdrHostView.inputs[1].SetValue("GPU Box")
	app.herdrHostView.inputs[2].SetValue("gpu")
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(saved) != 1 || len(saved[0]) != 3 || saved[0][2].ID != "gpu" || cmd == nil {
		t.Fatalf("add saved=%#v cmd=%v", saved, cmd)
	}
	cmd()
	if len(tested) != 1 || tested[0].ID != "gpu" {
		t.Fatalf("save did not test added host: %#v", tested)
	}

	for i, item := range app.listView.items {
		if item.Type == "herdr_endpoint" && item.Endpoint != nil && item.Endpoint.ID == "gpu" {
			app.listView.cursor = i
		}
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	app.herdrHostView.inputs[1].SetValue("GPU Edited")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(saved) != 2 || saved[1][2].Label != "GPU Edited" {
		t.Fatalf("edit saved=%#v", saved)
	}

	for i, item := range app.listView.items {
		if item.Type == "herdr_endpoint" && item.Endpoint != nil && item.Endpoint.ID == "gpu" {
			app.listView.cursor = i
		}
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if app.currentView != ViewHerdrHostDelete || len(saved) != 2 {
		t.Fatalf("remove was not gated: view=%v saved=%#v", app.currentView, saved)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(saved) != 3 || len(saved[2]) != 2 || saved[2][1].ID != "work" {
		t.Fatalf("confirmed remove saved=%#v", saved)
	}
}

func TestHerdrHostRemoveDefaultsToCancel(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	work := herdrhub.Endpoint{ID: "work", Label: "Work", Target: "work"}
	saves := 0
	app := NewApp(store, withoutHerdrSessions(), WithHerdrHubSnapshots([]herdrhub.Endpoint{herdrhub.LocalEndpoint(), work}, nil), WithHerdrHubEndpointSaver(func([]herdrhub.Endpoint) error {
		saves++
		return nil
	}))
	for i, item := range app.listView.items {
		if item.Type == "herdr_endpoint" && item.Endpoint != nil && item.Endpoint.ID == "work" {
			app.listView.cursor = i
		}
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if saves != 0 || app.currentView != ViewList {
		t.Fatalf("default remove selection was destructive: saves=%d view=%v", saves, app.currentView)
	}
}

func TestNewTabModeShowsWorkspaceActions(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	t.Setenv("TMUX", "")
	repoPath := newGitRepo(t)
	store := newTestStore(t, &workspace.Workspace{
		Name: "project",
		Path: repoPath,
	})
	app := NewApp(store, WithNewTabMode(), withoutHerdrSessions())

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := model.(*App)

	if updated.currentView != ViewActions {
		t.Fatalf("selected workspace opened view %v; want ViewActions", updated.currentView)
	}
	if updated.result != nil {
		t.Fatalf("new-tab mode produced result %#v before an action was selected", updated.result)
	}
	if cmd != nil {
		t.Fatalf("new-tab mode unexpectedly returned command %T", cmd)
	}
	wantActions := []Action{ActionWorktree, ActionCD}
	if !reflect.DeepEqual(updated.actionsView.actions, wantActions) {
		t.Fatalf("new-tab actions = %#v; want %#v", updated.actionsView.actions, wantActions)
	}
}

func TestNewTabModeOpensSelectedWorktreeDirectly(t *testing.T) {
	ws := &workspace.Workspace{Name: "project", Path: newGitRepo(t)}
	store := newTestStore(t, ws)
	app := NewApp(store, WithNewTabMode(), withoutHerdrSessions())
	app.actionsView = NewActionView(ws, false, false, true)
	app.worktreeView = &WorktreeView{
		selected: &git.Worktree{Path: "/tmp/project-feature", Branch: "feature"},
	}
	app.currentView = ViewWorktree

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := model.(*App)

	if updated.result == nil || updated.result.Action != ActionWorktree {
		t.Fatalf("worktree selection result = %#v; want ActionWorktree", updated.result)
	}
	if updated.result.Worktree == nil || updated.result.Worktree.Path != "/tmp/project-feature" {
		t.Fatalf("selected worktree = %#v; want /tmp/project-feature", updated.result.Worktree)
	}
	if updated.currentView == ViewWorktreeActions {
		t.Fatal("new-tab mode unexpectedly opened the multiplexer-only worktree action menu")
	}
	if cmd == nil {
		t.Fatal("worktree selection did not quit the chooser")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("selection command = %T; want tea.QuitMsg", cmd())
	}
}

func TestHerdrWorkspaceActionsStayOnHerdrBackend(t *testing.T) {
	ws := &workspace.Workspace{
		Name: "agents",
		Path: newGitRepo(t),
		Layout: workspace.Layout{
			Type:    workspace.LayoutHerdr,
			MainCmd: "claude",
		},
	}

	view := NewActionView(ws, false, false, true)
	wantActions := []Action{ActionWithLayout, ActionWorktree, ActionWithTemplate}
	if !reflect.DeepEqual(view.actions, wantActions) {
		t.Fatalf("Herdr actions = %#v; want %#v", view.actions, wantActions)
	}
}

func TestHerdrWorkspaceWithoutCommandsCanStillOpenInHerdr(t *testing.T) {
	ws := &workspace.Workspace{
		Name:   "agents",
		Path:   t.TempDir(),
		Layout: workspace.Layout{Type: workspace.LayoutHerdr},
	}

	view := NewActionView(ws, false, false, false)
	wantActions := []Action{ActionWithLayout, ActionWithTemplate}
	if !reflect.DeepEqual(view.actions, wantActions) {
		t.Fatalf("empty Herdr actions = %#v; want %#v", view.actions, wantActions)
	}
}

func TestHerdrWorktreeSelectionOffersSessionMode(t *testing.T) {
	ws := &workspace.Workspace{
		Name:   "agents",
		Path:   newGitRepo(t),
		Layout: workspace.Layout{Type: workspace.LayoutHerdr, MainCmd: "claude"},
	}
	store := newTestStore(t, ws)
	app := NewApp(store, withoutHerdrSessions())
	app.actionsView = NewActionView(ws, false, false, false)
	app.worktreeView = &WorktreeView{
		selected: &git.Worktree{Path: "/tmp/agents-feature", Branch: "feature"},
	}
	app.currentView = ViewWorktree

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := model.(*App)

	if updated.currentView != ViewHerdrOpenMode {
		t.Fatalf("Herdr worktree selection opened view %v; want ViewHerdrOpenMode", updated.currentView)
	}
	if updated.result != nil {
		t.Fatalf("Herdr worktree selection produced result before choosing session mode: %#v", updated.result)
	}
	if cmd != nil {
		t.Fatalf("Herdr worktree selection unexpectedly quit before choosing session mode")
	}

	model, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = model.(*App)
	if updated.currentView != ViewHerdrSessionName {
		t.Fatalf("new-session mode opened view %v; want ViewHerdrSessionName", updated.currentView)
	}
	if updated.herdrSessionNameView.Value() != "tatami-feature" {
		t.Fatalf("default worktree session name = %q; want tatami-feature", updated.herdrSessionNameView.Value())
	}
	if updated.result != nil || cmd != nil {
		t.Fatalf("new-session mode completed before naming: result=%#v cmd=%T", updated.result, cmd)
	}

	updated.herdrSessionNameView.input.SetValue("feature-review")
	model, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = model.(*App)
	if updated.result == nil || updated.result.Action != ActionWorktree {
		t.Fatalf("dedicated Herdr result = %#v; want ActionWorktree", updated.result)
	}
	if updated.result.HerdrMode != HerdrOpenDedicated {
		t.Fatalf("Herdr mode = %v; want HerdrOpenDedicated", updated.result.HerdrMode)
	}
	if updated.result.HerdrSessionName != "feature-review" {
		t.Fatalf("custom worktree session name = %q; want feature-review", updated.result.HerdrSessionName)
	}
	if cmd == nil {
		t.Fatal("dedicated Herdr selection did not quit")
	}
}

func TestHerdrDestinationBackReturnsToReusableWorktreeList(t *testing.T) {
	ws := &workspace.Workspace{
		Name:   "agents",
		Path:   newGitRepo(t),
		Layout: workspace.Layout{Type: workspace.LayoutHerdr},
	}
	app := NewApp(newTestStore(t, ws), withoutHerdrSessions())
	app.actionsView = NewActionView(ws, false, false, false)
	app.worktreeView = &WorktreeView{
		selected: &git.Worktree{Path: "/tmp/agents-feature", Branch: "feature"},
	}
	app.currentView = ViewWorktree

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := model.(*App)
	model, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated = model.(*App)

	if updated.currentView != ViewWorktree {
		t.Fatalf("destination back opened view %v; want ViewWorktree", updated.currentView)
	}
	if updated.worktreeView.Selected() != nil {
		t.Fatalf("destination back retained selected worktree: %#v", updated.worktreeView.Selected())
	}
	if cmd != nil {
		t.Fatalf("destination back unexpectedly returned command %T", cmd)
	}
}

func TestHerdrWorktreeCanOpenInChosenExistingSession(t *testing.T) {
	t.Setenv("HERDR_ENV", "")
	t.Setenv("HERDR_SESSION", "")
	ws := &workspace.Workspace{
		Name:   "agents",
		Path:   newGitRepo(t),
		Layout: workspace.Layout{Type: workspace.LayoutHerdr, MainCmd: "claude"},
	}
	app := NewApp(newTestStore(t, ws), WithHerdrSessionLister(func() ([]shell.HerdrSession, error) {
		return []shell.HerdrSession{{Name: "team-session", Running: true}}, nil
	}))
	app.actionsView = NewActionView(ws, false, false, false)
	app.worktreeView = &WorktreeView{
		selected: &git.Worktree{Path: "/tmp/agents-feature", Branch: "feature"},
	}
	app.currentView = ViewWorktree

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := model.(*App)
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = model.(*App)
	model, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = model.(*App)

	if updated.currentView != ViewHerdrSessionPicker {
		t.Fatalf("existing-session mode opened view %v; want ViewHerdrSessionPicker", updated.currentView)
	}
	if updated.result != nil || cmd != nil {
		t.Fatalf("existing-session mode completed before a session was selected: result=%#v cmd=%T", updated.result, cmd)
	}

	model, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = model.(*App)

	if updated.result == nil || updated.result.HerdrMode != HerdrOpenExisting {
		t.Fatalf("existing-session Herdr result = %#v; want HerdrOpenExisting", updated.result)
	}
	if updated.result.HerdrSessionName != "team-session" {
		t.Fatalf("selected Herdr session = %q; want team-session", updated.result.HerdrSessionName)
	}
	if updated.result.Worktree == nil || updated.result.Worktree.Path != "/tmp/agents-feature" {
		t.Fatalf("existing-session Herdr worktree = %#v", updated.result.Worktree)
	}
	if cmd == nil {
		t.Fatal("existing-session Herdr selection did not quit")
	}
}

func TestHerdrProjectOffersSessionDestination(t *testing.T) {
	ws := &workspace.Workspace{
		Name:   "agents",
		Path:   newGitRepo(t),
		Layout: workspace.Layout{Type: workspace.LayoutHerdr, MainCmd: "claude"},
	}
	app := NewApp(newTestStore(t, ws), withoutHerdrSessions())
	app.actionsView = NewActionView(ws, false, false, false)
	app.currentView = ViewActions

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := model.(*App)

	if updated.currentView != ViewHerdrOpenMode {
		t.Fatalf("Herdr project opened view %v; want ViewHerdrOpenMode", updated.currentView)
	}
	if updated.result != nil || cmd != nil {
		t.Fatalf("Herdr project completed before destination choice: result=%#v cmd=%T", updated.result, cmd)
	}

	model, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = model.(*App)
	if updated.currentView != ViewHerdrSessionName {
		t.Fatalf("new-session project opened view %v; want ViewHerdrSessionName", updated.currentView)
	}
	updated.herdrSessionNameView.input.SetValue("project-review")
	model, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = model.(*App)
	if updated.result == nil || updated.result.Action != ActionWithLayout || updated.result.HerdrMode != HerdrOpenDedicated {
		t.Fatalf("dedicated Herdr project result = %#v", updated.result)
	}
	if updated.result.HerdrSessionName != "project-review" {
		t.Fatalf("custom project session name = %q; want project-review", updated.result.HerdrSessionName)
	}
	if cmd == nil {
		t.Fatal("dedicated Herdr project selection did not quit")
	}
}

func TestHerdrTemplateOffersSessionDestination(t *testing.T) {
	ws := &workspace.Workspace{
		Name:   "agents",
		Path:   newGitRepo(t),
		Layout: workspace.Layout{Type: workspace.LayoutHerdr},
	}
	app := NewApp(newTestStore(t, ws), withoutHerdrSessions())
	app.actionsView = NewActionView(ws, false, false, false)
	app.templateView = NewTemplateView()
	app.previousView = ViewActions
	app.currentView = ViewTemplates

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := model.(*App)

	if updated.currentView != ViewHerdrOpenMode {
		t.Fatalf("Herdr template opened view %v; want ViewHerdrOpenMode", updated.currentView)
	}
	if updated.pendingHerdrResult == nil || updated.pendingHerdrResult.Template == nil {
		t.Fatalf("pending Herdr template result = %#v", updated.pendingHerdrResult)
	}
	if updated.result != nil || cmd != nil {
		t.Fatalf("Herdr template completed before destination choice: result=%#v cmd=%T", updated.result, cmd)
	}
}

func TestHerdrExistingSessionPickerPrefersCurrentSession(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SESSION", "current-session")
	ws := &workspace.Workspace{
		Name:   "agents",
		Path:   newGitRepo(t),
		Layout: workspace.Layout{Type: workspace.LayoutHerdr},
	}
	app := NewApp(newTestStore(t, ws), WithHerdrSessionLister(func() ([]shell.HerdrSession, error) {
		return []shell.HerdrSession{
			{Name: "other-session", Running: true},
			{Name: "current-session", Running: true},
			{Name: "stopped-session", Running: false},
		}, nil
	}))
	app.actionsView = NewActionView(ws, false, false, false)
	app.currentView = ViewActions

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := model.(*App)
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = model.(*App)
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = model.(*App)

	if updated.currentView != ViewHerdrSessionPicker {
		t.Fatalf("existing-session choice opened view %v; want ViewHerdrSessionPicker", updated.currentView)
	}
	if got := updated.herdrSessionPickerView.Selected(); got != "current-session" {
		t.Fatalf("initial existing session = %q; want current-session", got)
	}
	if !strings.Contains(updated.View(), "current-session (current)") {
		t.Fatalf("session picker does not mark current session:\n%s", updated.View())
	}
}

func TestHomePageHerdrSessionCanBeOpened(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "project", Path: "/tmp/project"})
	app := NewApp(store, WithHerdrSessionLister(func() ([]shell.HerdrSession, error) {
		return []shell.HerdrSession{{Name: "tatami-project", Running: true}}, nil
	}))
	for i, item := range app.listView.items {
		if item.Type == "herdr_session" {
			app.listView.cursor = i
			break
		}
	}

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := model.(*App)
	if updated.result == nil || updated.result.Action != ActionAttachHerdrSession {
		t.Fatalf("session result = %#v; want ActionAttachHerdrSession", updated.result)
	}
	if updated.result.SessionName != "tatami-project" {
		t.Fatalf("session name = %q; want tatami-project", updated.result.SessionName)
	}
	if cmd == nil {
		t.Fatal("Herdr session selection did not quit")
	}
}

func TestHighlightedHerdrUsageIgnoresStaleResults(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "project", Path: "/tmp/project"})
	collected := make([]string, 0)
	app := NewApp(store,
		WithHerdrSessionLister(func() ([]shell.HerdrSession, error) {
			return []shell.HerdrSession{
				{Name: "alpha", Running: true},
				{Name: "beta", Running: true},
			}, nil
		}),
		WithHerdrSessionUsageCollector(func(session string) (systemusage.Report, error) {
			collected = append(collected, session)
			return systemusage.Report{Sessions: []systemusage.SessionUsage{{
				Name: session, CPUPercent: 22, RSSBytes: 256 * 1024 * 1024, ProcessCount: 4,
				Agents: []systemusage.AgentUsage{{}}, MaxAge: 45 * time.Minute,
			}}}, nil
		}),
	)
	for i, item := range app.listView.items {
		if item.Type == "herdr_session" && item.Name == "alpha" {
			app.listView.cursor = i
			break
		}
	}

	firstRequest := app.scheduleSelectedHerdrUsage()
	firstGeneration := app.herdrUsageGeneration
	if firstRequest == nil || !strings.Contains(app.View(), "Usage  loading") {
		t.Fatalf("initial usage request was not scheduled:\n%s", app.View())
	}

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := model.(*App)
	if selected := updated.listView.Selected(); selected == nil || selected.Name != "beta" {
		t.Fatalf("selected session = %#v; want beta", selected)
	}
	if updated.herdrUsageGeneration == firstGeneration {
		t.Fatal("selection change did not invalidate the first request")
	}

	model, staleCmd := updated.Update(herdrUsageRequestMsg{Session: "alpha", Generation: firstGeneration})
	updated = model.(*App)
	if staleCmd != nil || len(collected) != 0 {
		t.Fatalf("stale request collected usage: cmd=%T calls=%#v", staleCmd, collected)
	}

	currentGeneration := updated.herdrUsageGeneration
	model, collectCmd := updated.Update(herdrUsageRequestMsg{Session: "beta", Generation: currentGeneration})
	updated = model.(*App)
	if collectCmd == nil {
		t.Fatal("current request did not start collection")
	}
	result := collectCmd()
	model, _ = updated.Update(result)
	updated = model.(*App)
	if !reflect.DeepEqual(collected, []string{"beta"}) {
		t.Fatalf("collected sessions = %#v; want beta", collected)
	}
	for _, want := range []string{"CPU 22.0%", "RAM 256 MiB", "MAX AGE 45m"} {
		if !strings.Contains(updated.View(), want) {
			t.Errorf("beta summary missing %q:\n%s", want, updated.View())
		}
	}

	model, _ = updated.Update(herdrUsageResultMsg{
		Session: "alpha", Generation: firstGeneration,
		Report: systemusage.Report{Sessions: []systemusage.SessionUsage{{Name: "alpha", CPUPercent: 99}}},
	})
	if strings.Contains(model.(*App).View(), "CPU 99.0%") {
		t.Fatalf("stale result replaced current summary:\n%s", model.(*App).View())
	}
}

func TestHomePageRunningHerdrSessionCanBeStopped(t *testing.T) {
	running := true
	store := newTestStore(t, &workspace.Workspace{Name: "project", Path: "/tmp/project"})
	app := NewApp(store,
		WithHerdrSessionLister(func() ([]shell.HerdrSession, error) {
			return []shell.HerdrSession{{Name: "team-session", Running: running}}, nil
		}),
		WithHerdrSessionStopper(func(name string) error {
			if name != "team-session" {
				t.Fatalf("stopped session = %q; want team-session", name)
			}
			running = false
			return nil
		}),
	)
	for i, item := range app.listView.items {
		if item.Type == "herdr_session" {
			app.listView.cursor = i
			break
		}
	}

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	updated := model.(*App)
	selected := updated.listView.Selected()

	if running {
		t.Fatal("session stopper was not called")
	}
	if selected == nil || selected.Herdr == nil || selected.Herdr.Running {
		t.Fatalf("refreshed stopped session = %#v", selected)
	}
	if cmd != nil || updated.result != nil {
		t.Fatalf("stopping session unexpectedly exited: result=%#v cmd=%T", updated.result, cmd)
	}
	if !strings.Contains(updated.View(), "[x]delete") {
		t.Fatalf("stopped-session help does not advertise delete action:\n%s", updated.View())
	}
}

func TestHomePageStoppedHerdrSessionCanBeDeletedAfterConfirmation(t *testing.T) {
	deleted := false
	store := newTestStore(t, &workspace.Workspace{Name: "project", Path: "/tmp/project"})
	app := NewApp(store,
		WithHerdrSessionLister(func() ([]shell.HerdrSession, error) {
			if deleted {
				return nil, nil
			}
			return []shell.HerdrSession{{Name: "old-session", Running: false}}, nil
		}),
		WithHerdrSessionDeleter(func(name string) error {
			if name != "old-session" {
				t.Fatalf("deleted session = %q; want old-session", name)
			}
			deleted = true
			return nil
		}),
	)
	for i, item := range app.listView.items {
		if item.Type == "herdr_session" {
			app.listView.cursor = i
			break
		}
	}

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	updated := model.(*App)
	if updated.currentView != ViewHerdrSessionDelete {
		t.Fatalf("delete request opened view %v; want ViewHerdrSessionDelete", updated.currentView)
	}
	if deleted || cmd != nil {
		t.Fatalf("session deleted before confirmation: deleted=%v cmd=%T", deleted, cmd)
	}
	if !strings.Contains(updated.View(), "old-session") {
		t.Fatalf("delete confirmation does not name the session:\n%s", updated.View())
	}

	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = model.(*App)
	model, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = model.(*App)
	if !deleted {
		t.Fatal("confirmed stopped session was not deleted")
	}
	if updated.currentView != ViewList {
		t.Fatalf("confirmed deletion opened view %v; want ViewList", updated.currentView)
	}
	if cmd != nil || updated.result != nil {
		t.Fatalf("deleting session unexpectedly exited: result=%#v cmd=%T", updated.result, cmd)
	}
	for _, item := range updated.listView.items {
		if item.Type == "herdr_session" && item.Name == "old-session" {
			t.Fatalf("deleted session remains on home page: %#v", item)
		}
	}
}

func TestHomePageDefaultHerdrSessionCannotBeDeleted(t *testing.T) {
	deleteCalled := false
	store := newTestStore(t, &workspace.Workspace{Name: "project", Path: "/tmp/project"})
	app := NewApp(store,
		WithHerdrSessionLister(func() ([]shell.HerdrSession, error) {
			return []shell.HerdrSession{{Name: "default", Running: false, Default: true}}, nil
		}),
		WithHerdrSessionDeleter(func(string) error {
			deleteCalled = true
			return nil
		}),
	)
	for i, item := range app.listView.items {
		if item.Type == "herdr_session" {
			app.listView.cursor = i
			break
		}
	}

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	updated := model.(*App)
	if deleteCalled {
		t.Fatal("default Herdr session was sent to the deleter")
	}
	if updated.currentView != ViewList || cmd != nil {
		t.Fatalf("default delete changed view=%v cmd=%T", updated.currentView, cmd)
	}
	if !strings.Contains(updated.View(), "built-in") {
		t.Fatalf("default-session help does not explain why it cannot be deleted:\n%s", updated.View())
	}
}

func TestDefaultModeStillShowsWorkspaceActions(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{
		Name: "project",
		Path: "/tmp/project",
	})
	app := NewApp(store, withoutHerdrSessions())

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := model.(*App)

	if updated.currentView != ViewActions {
		t.Fatalf("selected workspace opened view %v; want ViewActions", updated.currentView)
	}
	if updated.result != nil {
		t.Fatalf("default mode produced result %#v before an action was selected", updated.result)
	}
	if cmd != nil {
		t.Fatalf("default mode unexpectedly returned command %T", cmd)
	}
}

func TestMobileModeNumberSelectionRequiresEnterToOpen(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "first", Path: "/tmp/first"})
	if err := store.Create(&workspace.Workspace{Name: "second", Path: "/tmp/second"}); err != nil {
		t.Fatalf("create second workspace: %v", err)
	}
	app := NewApp(store, withoutHerdrSessions(), WithMobileMode())

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	updated := model.(*App)
	if updated.currentView != ViewList || cmd != nil || updated.result != nil {
		t.Fatalf("number selection opened immediately: view=%v result=%#v cmd=%T", updated.currentView, updated.result, cmd)
	}
	if selected := updated.listView.Selected(); selected == nil || selected.Name != "second" {
		t.Fatalf("number selection chose %#v; want second", selected)
	}

	model, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = model.(*App)
	if updated.currentView != ViewActions || cmd != nil {
		t.Fatalf("enter after number selection opened view=%v cmd=%T; want actions", updated.currentView, cmd)
	}
	if !strings.Contains(updated.View(), "[1]") {
		t.Fatalf("mobile action menu does not show numbered choices:\n%s", updated.View())
	}
}

func TestMobileActionNumberSelectionStillRequiresEnter(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{
		Name:   "project",
		Path:   "/tmp/project",
		Layout: workspace.Layout{Type: workspace.LayoutHerdr},
	})
	app := NewApp(store, withoutHerdrSessions(), WithMobileMode())

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := model.(*App)
	model, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	updated = model.(*App)
	if updated.currentView != ViewActions || cmd != nil || updated.result != nil {
		t.Fatalf("numbered action executed immediately: view=%v result=%#v cmd=%T", updated.currentView, updated.result, cmd)
	}
	if got := updated.actionsView.Selected(); got != ActionWithTemplate {
		t.Fatalf("numbered action selected %v; want ActionWithTemplate", got)
	}

	model, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = model.(*App)
	if updated.currentView != ViewTemplates || cmd != nil || updated.result != nil {
		t.Fatalf("enter after numbered action produced view=%v result=%#v cmd=%T", updated.currentView, updated.result, cmd)
	}
}

func TestMobileBackReturnsFromMenuAndDoesNotQuitAtRoot(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "project", Path: "/tmp/project"})
	app := NewApp(store, withoutHerdrSessions(), WithMobileMode())

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := model.(*App)
	model, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	updated = model.(*App)
	if updated.currentView != ViewList || cmd != nil {
		t.Fatalf("mobile back from action menu produced view=%v cmd=%T", updated.currentView, cmd)
	}

	model, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	updated = model.(*App)
	if updated.currentView != ViewList || cmd != nil || updated.result != nil {
		t.Fatalf("mobile back at root quit or changed state: view=%v result=%#v cmd=%T", updated.currentView, updated.result, cmd)
	}
}

func TestMobileBackKeyRemainsTextInsideCreateForm(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "project", Path: "/tmp/project"})
	app := NewApp(store, withoutHerdrSessions(), WithMobileMode())

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	updated := model.(*App)
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	updated = model.(*App)
	if updated.currentView != ViewCreate {
		t.Fatalf("typing b in mobile create form navigated away: view=%v", updated.currentView)
	}
	if got := updated.createView.nameInput.Value(); got != "b" {
		t.Fatalf("mobile create name = %q; want b", got)
	}
}

func TestMobileBackLeavesFolderAndRemainsTextWhileFiltering(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{
		Name:   "nested-project",
		Path:   "/tmp/nested-project",
		Folder: "team",
	})
	app := NewApp(store, withoutHerdrSessions(), WithMobileMode())

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := model.(*App)
	if updated.listView.CurrentFolder() != "team" {
		t.Fatalf("enter selected folder %q; want team", updated.listView.CurrentFolder())
	}
	model, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	updated = model.(*App)
	if updated.listView.CurrentFolder() != "" || cmd != nil {
		t.Fatalf("mobile back left folder=%q cmd=%T; want root", updated.listView.CurrentFolder(), cmd)
	}
	if selected := updated.listView.Selected(); selected == nil || selected.Type == "header" {
		t.Fatalf("mobile back selected non-actionable row %#v", selected)
	}

	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated = model.(*App)
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	updated = model.(*App)
	if !updated.listView.IsFiltering() || updated.listView.filter.Value() != "b" {
		t.Fatalf("mobile filter after b: filtering=%v value=%q", updated.listView.IsFiltering(), updated.listView.filter.Value())
	}
}

func newTestStore(t *testing.T, ws *workspace.Workspace) *workspace.Store {
	t.Helper()

	dir := t.TempDir()
	store, err := workspace.NewStore(&config.Paths{
		ConfigDir:      dir,
		WorkspacesFile: filepath.Join(dir, "workspaces.json"),
	})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.Create(ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return store
}

func withoutHerdrSessions() AppOption {
	return WithHerdrSessionLister(func() ([]shell.HerdrSession, error) {
		return nil, nil
	})
}

func renderedRowContaining(t *testing.T, view, text string) int {
	t.Helper()
	for row, line := range strings.Split(view, "\n") {
		if strings.Contains(line, text) {
			return row
		}
	}
	t.Fatalf("rendered view does not contain %q:\n%s", text, view)
	return -1
}

func newGitRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if output, err := exec.Command("git", "init", "--quiet", dir).CombinedOutput(); err != nil {
		t.Fatalf("initialize git repository: %v\n%s", err, output)
	}
	return dir
}
