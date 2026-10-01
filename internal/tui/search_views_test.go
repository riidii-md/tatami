package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/git"
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	appsearch "github.com/OleksandrBesan/tatami/internal/search"
	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

func TestHomeSearchUsesKnownSnapshotsWithoutQueryTimeProviders(t *testing.T) {
	providerCalls := 0
	store := newTestStore(t, &workspace.Workspace{Name: "Local API", Path: "/srv/local", Folder: "team/backend"})
	view := NewListViewWithHerdrSessions(store, func() ([]shell.HerdrSession, error) {
		providerCalls++
		return []shell.HerdrSession{{Name: "local-agents", Running: true}}, nil
	})
	endpoint := herdrhub.Endpoint{ID: "work", Label: "Workbox", Target: "workbox"}
	view.SetHerdrHubSnapshots([]herdrhub.Endpoint{endpoint}, []herdrhub.Snapshot{{
		EndpointID: endpoint.ID,
		State:      herdrhub.StateOnline,
		Workspaces: []herdrhub.WorkspaceSummary{{Name: "Deep API", Path: "/srv/deep", Folder: "platform", Repository: "github.com/riidii/deep-api"}},
		Sessions:   []herdrhub.Session{{SessionKey: herdrhub.SessionKey{EndpointID: endpoint.ID, SessionName: "remote-agents"}, Running: true}},
	}})
	view.hubCollapsed[endpoint.ID] = true
	view.refreshItems()
	baseline := providerCalls

	typeListQuery(view, "deep-api")
	if providerCalls != baseline {
		t.Fatalf("typing called Herdr provider: before=%d after=%d", baseline, providerCalls)
	}
	selected := view.Selected()
	if selected == nil || selected.Workspace == nil || selected.Workspace.Name != "Deep API" || selected.Endpoint == nil {
		t.Fatalf("repository result = %#v", selected)
	}
	if !strings.Contains(view.View(), "repository: github.com/riidii/deep-api") {
		t.Fatalf("safe matched-field explanation missing:\n%s", view.View())
	}

	view.ClearFilter()
	typeListQuery(view, "workbox")
	selected = view.Selected()
	if selected == nil || selected.Type != "herdr_endpoint" || selected.Endpoint == nil || selected.Endpoint.ID != "work" {
		t.Fatalf("endpoint-only result = %#v", selected)
	}
}

func TestHomeSearchLabelsIncompleteRemoteTruth(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: "/srv/local"})
	view := NewListViewWithHerdrSessions(store, nil)
	view.SetHerdrHubSnapshots([]herdrhub.Endpoint{{ID: "work", Label: "Workbox", Target: "workbox"}}, nil)
	typeListQuery(view, "missing")
	rendered := view.View()
	if !strings.Contains(rendered, "No matches in known data") || !strings.Contains(rendered, "Workbox undiscovered/loading") {
		t.Fatalf("incomplete search state missing:\n%s", rendered)
	}
}

func TestHomeSearchKeepsDistinctWorkspaceNamesSharingAPath(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "primary", Path: "/srv/shared"})
	if err := store.Create(&workspace.Workspace{Name: "alternate", Path: "/srv/shared"}); err != nil {
		t.Fatal(err)
	}
	view := NewListViewWithHerdrSessions(store, nil)
	typeListQuery(view, "alternate")
	selected := view.Selected()
	if selected == nil || selected.Workspace == nil || selected.Workspace.Name != "alternate" {
		t.Fatalf("shared-path search selected %#v", selected)
	}
}

func TestHomeSearchRetainsLastLocalSessionSnapshotOnRefreshFailure(t *testing.T) {
	calls := 0
	view := NewListViewWithHerdrSessions(newTestStore(t, &workspace.Workspace{Name: "project", Path: "/srv/project"}), func() ([]shell.HerdrSession, error) {
		calls++
		if calls == 1 {
			return []shell.HerdrSession{{Name: "retained-session", Running: true}}, nil
		}
		return nil, fmt.Errorf("temporarily unavailable")
	})
	view.refreshSources()
	typeListQuery(view, "retained-session")
	selected := view.Selected()
	if selected == nil || selected.Herdr == nil || selected.Herdr.Name != "retained-session" || view.localSessionsErr == nil {
		t.Fatalf("failed refresh lost snapshot: selected=%#v err=%v", selected, view.localSessionsErr)
	}
}

func TestHomeSearchEmptyStoreReportsNoMatchTruth(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "temporary", Path: "/srv/temporary"})
	if err := store.Delete("temporary"); err != nil {
		t.Fatal(err)
	}
	view := NewListViewWithHerdrSessions(store, func() ([]shell.HerdrSession, error) {
		return []shell.HerdrSession{{Name: "known-session", Running: true}}, nil
	})
	typeListQuery(view, "missing")
	rendered := view.View()
	if !strings.Contains(rendered, "No matches in known data") || strings.Contains(rendered, "No workspaces yet") {
		t.Fatalf("incorrect empty-store query truth:\n%s", rendered)
	}
}

func TestHomeSearchBoundsKnownFederatedProjection(t *testing.T) {
	view := NewListViewWithHerdrSessions(newTestStore(t, &workspace.Workspace{Name: "local", Path: "/srv/local"}), nil)
	endpoints := make([]herdrhub.Endpoint, 0, 5)
	snapshots := make([]herdrhub.Snapshot, 0, 5)
	for endpointIndex := range 5 {
		endpoint := herdrhub.Endpoint{ID: fmt.Sprintf("host-%d", endpointIndex), Label: fmt.Sprintf("Host %d", endpointIndex), Target: fmt.Sprintf("host-%d", endpointIndex)}
		workspaces := make([]herdrhub.WorkspaceSummary, herdrhub.MaxInventoryWorkspaces)
		for workspaceIndex := range workspaces {
			name := fmt.Sprintf("remote-%d-%d", endpointIndex, workspaceIndex)
			workspaces[workspaceIndex] = herdrhub.WorkspaceSummary{Name: name, Path: "/srv/" + name}
		}
		endpoints = append(endpoints, endpoint)
		snapshots = append(snapshots, herdrhub.Snapshot{EndpointID: endpoint.ID, State: herdrhub.StateOnline, Workspaces: workspaces})
	}
	view.SetHerdrHubSnapshots(endpoints, snapshots)
	if len(view.searchItems) != appsearch.MaxDocuments || !view.searchSourceTruncated {
		t.Fatalf("search projection size=%d truncated=%v", len(view.searchItems), view.searchSourceTruncated)
	}
	typeListQuery(view, "remote")
	if len(view.search.Rows()) != appsearch.MaxResults || !strings.Contains(view.View(), "Results truncated") {
		t.Fatalf("bounded result rows=%d view missing notice", len(view.search.Rows()))
	}
}

func TestHomeQueryLettersDoNotRunBrowseCommands(t *testing.T) {
	app := NewApp(newTestStore(t, &workspace.Workspace{Name: "project", Path: "/tmp/project"}), withoutHerdrSessions())
	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	updated := model.(*App)
	if updated.listView.search.Query() != "q" || updated.currentView != ViewList || updated.result != nil {
		t.Fatalf("q in query focus changed state: query=%q view=%v result=%#v", updated.listView.search.Query(), updated.currentView, updated.result)
	}
	updated.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if model.(*App).currentView != ViewCreate {
		t.Fatalf("n in browse focus opened view %v, want create", model.(*App).currentView)
	}
}

func TestHomePrintableQueryKeysDoNotCallSourceProviders(t *testing.T) {
	localCalls, repositoryCalls, refreshCalls, agentCalls := 0, 0, 0, 0
	app := NewApp(
		newTestStore(t, &workspace.Workspace{Name: "project", Path: "/srv/project"}),
		WithHerdrSessionLister(func() ([]shell.HerdrSession, error) {
			localCalls++
			return []shell.HerdrSession{{Name: "agents", Running: true}}, nil
		}),
		WithRepositoryIdentityResolver(func(context.Context, string) (git.RepositoryIdentity, error) {
			repositoryCalls++
			return git.RepositoryIdentity{Display: "github.com/riidii/tatami"}, nil
		}),
		WithHerdrHubRefresh(func(context.Context, []herdrhub.Endpoint, herdrhub.Cache) []herdrhub.Snapshot {
			refreshCalls++
			return nil
		}, nil),
		WithHerdrHubAgentQuery(func(context.Context, herdrhub.Endpoint, string) ([]herdrhub.Agent, error) {
			agentCalls++
			return nil, nil
		}),
	)
	baselineLocal := localCalls
	for _, key := range "qdre1" {
		model, cmd := app.Update(runeKey(key))
		app = model.(*App)
		if cmd != nil {
			_ = cmd()
		}
	}
	if app.listView.search.Query() != "qdre1" || localCalls != baselineLocal || repositoryCalls != 0 || refreshCalls != 0 || agentCalls != 0 {
		t.Fatalf("query=%q providers local=%d/%d repository=%d refresh=%d agent=%d", app.listView.search.Query(), localCalls, baselineLocal, repositoryCalls, refreshCalls, agentCalls)
	}
}

func TestRepositoryEnrichmentIsGenerationCheckedAndSearchable(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "project", Path: "/repo/project"})
	values := []string{"github.com/riidii/old", "github.com/riidii/tatami"}
	call := 0
	app := NewApp(store, withoutHerdrSessions(), WithRepositoryIdentityResolver(func(context.Context, string) (git.RepositoryIdentity, error) {
		value := values[call]
		call++
		return git.RepositoryIdentity{CommonDir: "/repo/.git", Display: value}, nil
	}))
	first := app.scheduleRepositoryIdentities()
	second := app.scheduleRepositoryIdentities()
	app.Update(first())
	if len(app.repositoryIdentities) != 0 {
		t.Fatalf("stale repository generation applied: %#v", app.repositoryIdentities)
	}
	app.Update(second())
	if app.repositoryIdentities["/repo/project"] != "github.com/riidii/tatami" {
		t.Fatalf("repository identities = %#v", app.repositoryIdentities)
	}
	typeListQuery(app.listView, "riidii/tatami")
	if selected := app.listView.Selected(); selected == nil || selected.Workspace == nil || selected.Workspace.Name != "project" {
		t.Fatalf("repository query selected %#v", selected)
	}
}

func TestSearchAdaptersUseAllowListedFields(t *testing.T) {
	t.Run("template commands excluded", func(t *testing.T) {
		view := NewTemplateView()
		view.templates = []workspace.Template{{Name: "safe-layout", Description: "visible description", MainCmd: "credential-sentinel", Panes: []workspace.Pane{{Command: "pane-secret"}}}}
		view.rebuildSearch()
		typeKey(&view.search, "credential-sentinel")
		if len(view.search.Rows()) != 0 {
			t.Fatalf("template command matched: %#v", view.search.Rows())
		}
	})

	t.Run("layout commands excluded", func(t *testing.T) {
		editor := NewLayoutEditor()
		editor.SetPanes([]workspace.Pane{{Command: "credential-sentinel", Direction: "right"}})
		typeKey(&editor.search, "credential-sentinel")
		if len(editor.search.Rows()) != 0 {
			t.Fatalf("layout command matched: %#v", editor.search.Rows())
		}
		editor.search.Clear()
		typeKey(&editor.search, "right")
		if len(editor.search.Rows()) != 1 {
			t.Fatalf("layout direction did not match: %#v", editor.search.Rows())
		}
	})

	t.Run("session status included", func(t *testing.T) {
		view := &SessionView{sessions: []shell.ZellijSession{{Name: "agents", IsExited: true}}, showExited: true, search: newSearchController("sessions")}
		view.rebuildSearch()
		typeKey(&view.search, "exited")
		if len(view.search.Rows()) != 1 || view.Selected() != "agents" {
			t.Fatalf("session status result rows=%#v selected=%q", view.search.Rows(), view.Selected())
		}
	})

	t.Run("Herdr status included", func(t *testing.T) {
		view := NewHerdrSessionPickerView([]shell.HerdrSession{{Name: "agents", Running: false}}, "", nil)
		typeKey(&view.search, "stopped")
		if len(view.search.Rows()) != 1 || view.Selected() != "agents" {
			t.Fatalf("Herdr status result rows=%#v selected=%q", view.search.Rows(), view.Selected())
		}
	})
}

func TestSearchableViewsPublicUpdatesSelectFilteredDomainValues(t *testing.T) {
	workspaceValue := &workspace.Workspace{Name: "project", Path: t.TempDir()}
	actions := NewActionView(workspaceValue, true, false, false)
	actions.Update(textMsg("template"))
	actions.Update(keyMsg("enter"))
	if actions.Selected() != ActionWithTemplate {
		t.Fatalf("filtered action selected %v", actions.Selected())
	}

	worktreeActions := NewWorktreeActionView(&git.Worktree{Branch: "feature"}, workspaceValue)
	worktreeActions.Update(textMsg("plain"))
	worktreeActions.Update(keyMsg("enter"))
	if worktreeActions.Selected() != WorktreeActionPlain {
		t.Fatalf("filtered worktree action selected %v", worktreeActions.Selected())
	}

	picker := NewHerdrSessionPickerView([]shell.HerdrSession{{Name: "alpha"}, {Name: "beta"}}, "", nil)
	picker.Update(textMsg("beta"))
	picker.Update(keyMsg("enter"))
	if picker.Selected() != "beta" {
		t.Fatalf("filtered Herdr session selected %q", picker.Selected())
	}

	sessions := &SessionView{sessions: []shell.ZellijSession{{Name: "alpha"}, {Name: "delta"}}, showExited: true, mode: SessionModeList, search: newSearchController("sessions")}
	sessions.rebuildSearch()
	sessions.Update(runeKey('d'))
	if sessions.Mode() != SessionModeList || sessions.search.Query() != "d" || sessions.Selected() != "delta" {
		t.Fatalf("query mnemonic mode=%v query=%q selected=%q", sessions.Mode(), sessions.search.Query(), sessions.Selected())
	}
}

func TestLayoutAddPaneEditsNewPane(t *testing.T) {
	editor := NewLayoutEditor()
	editor.SetPanes([]workspace.Pane{{Command: "first", Direction: "down"}, {Command: "second", Direction: "right"}})
	editor.addPane()
	if !editor.IsEditing() || editor.cursor != 2 {
		t.Fatalf("added pane editing=%v cursor=%d, want third pane", editor.IsEditing(), editor.cursor)
	}
	editor.commandInput.SetValue("third")
	editor.stopEdit(true)
	panes := editor.GetPanes()
	if panes[0].Command != "first" || panes[1].Command != "second" || panes[2].Command != "third" {
		t.Fatalf("pane commands after edit = %#v", panes)
	}
}

func TestScalableSearchViewsRenderViewportAndTruncation(t *testing.T) {
	sessions := make([]shell.ZellijSession, 250)
	for index := range sessions {
		sessions[index] = shell.ZellijSession{Name: fmt.Sprintf("session-%03d", index)}
	}
	sessionView := &SessionView{sessions: sessions, showExited: true, search: newSearchController("sessions")}
	sessionView.rebuildSearch()
	sessionView.search.SetHeight(12)
	typeKey(&sessionView.search, "session")
	sessionView.search.SelectLast()
	rendered := sessionView.View()
	if !strings.Contains(rendered, "session-199") || strings.Contains(rendered, "session-000") || !strings.Contains(rendered, "Results truncated") {
		t.Fatalf("session viewport/truncation incorrect:\n%s", rendered)
	}

	editor := NewLayoutEditor()
	panes := make([]workspace.Pane, 250)
	for index := range panes {
		panes[index] = workspace.Pane{Command: fmt.Sprintf("command-%03d", index), Direction: "down"}
	}
	editor.SetPanes(panes)
	editor.search.SetHeight(12)
	typeKey(&editor.search, "down")
	editor.search.SelectLast()
	rendered = editor.View()
	if !strings.Contains(rendered, "command-199") || strings.Contains(rendered, "command-000") || !strings.Contains(rendered, "Results truncated") {
		t.Fatalf("layout viewport/truncation incorrect:\n%s", rendered)
	}
}

func TestMobileDigitsAreTextUntilBrowseFocus(t *testing.T) {
	view := NewTemplateView()
	view.SetMobileMode(true)
	view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if view.search.Query() != "2" || !view.search.InQueryFocus() {
		t.Fatalf("digit in query focus: query=%q focus=%v", view.search.Query(), view.search.focus)
	}
	view.search.Clear()
	view.Update(tea.KeyMsg{Type: tea.KeyDown})
	view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if id, _ := view.search.ActiveDocumentID(); id != templateSearchID(view.templates[1]) {
		t.Fatalf("digit in browse focus selected %q", id)
	}
}

func TestMobileNumberSelectsViewportRelativeRow(t *testing.T) {
	view := NewTemplateView()
	view.SetMobileMode(true)
	view.search.SetHeight(10)
	view.search.SelectLast()
	view.View()
	want := view.search.Rows()[view.search.visibleStart].DocumentID
	view.Update(runeKey('1'))
	if id, _ := view.search.ActiveDocumentID(); id != want {
		t.Fatalf("viewport [1] selected %q, want %q", id, want)
	}

	editor := NewLayoutEditor()
	editor.SetMobileMode(true)
	panes := make([]workspace.Pane, 20)
	for index := range panes {
		panes[index] = workspace.Pane{Command: fmt.Sprintf("pane-%02d", index), Direction: "down"}
	}
	editor.SetPanes(panes)
	editor.search.SetHeight(12)
	editor.search.SelectLast()
	editor.syncCursorFromSearch()
	editor.View()
	wantIndex := editor.search.visibleStart
	editor.Update(runeKey('1'))
	if editor.cursor != wantIndex {
		t.Fatalf("layout viewport [1] selected %d, want %d", editor.cursor, wantIndex)
	}
}
