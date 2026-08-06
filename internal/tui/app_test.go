package tui

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/config"
	"github.com/OleksandrBesan/tatami/internal/git"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

func TestNewTabModeShowsWorkspaceActions(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	t.Setenv("TMUX", "")
	repoPath := newGitRepo(t)
	store := newTestStore(t, &workspace.Workspace{
		Name: "project",
		Path: repoPath,
	})
	app := NewApp(store, WithNewTabMode())

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
	app := NewApp(store, WithNewTabMode())
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

func TestHerdrWorktreeSelectionDoesNotOpenMultiplexerActions(t *testing.T) {
	ws := &workspace.Workspace{
		Name:   "agents",
		Path:   newGitRepo(t),
		Layout: workspace.Layout{Type: workspace.LayoutHerdr, MainCmd: "claude"},
	}
	store := newTestStore(t, ws)
	app := NewApp(store)
	app.actionsView = NewActionView(ws, false, false, false)
	app.worktreeView = &WorktreeView{
		selected: &git.Worktree{Path: "/tmp/agents-feature", Branch: "feature"},
	}
	app.currentView = ViewWorktree

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := model.(*App)

	if updated.result == nil || updated.result.Action != ActionWorktree {
		t.Fatalf("Herdr worktree result = %#v; want ActionWorktree", updated.result)
	}
	if updated.currentView == ViewWorktreeActions {
		t.Fatal("Herdr worktree selection opened the multiplexer action menu")
	}
	if cmd == nil {
		t.Fatal("Herdr worktree selection did not quit the chooser")
	}
}

func TestDefaultModeStillShowsWorkspaceActions(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{
		Name: "project",
		Path: "/tmp/project",
	})
	app := NewApp(store)

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

func newGitRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if output, err := exec.Command("git", "init", "--quiet", dir).CombinedOutput(); err != nil {
		t.Fatalf("initialize git repository: %v\n%s", err, output)
	}
	return dir
}
