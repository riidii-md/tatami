package tui

import (
	"path/filepath"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/config"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

func TestNewTabModeOpensSelectedWorkspaceDirectly(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{
		Name: "project",
		Path: "/tmp/project",
	})
	app := NewApp(store, WithNewTabMode())

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := model.(*App)

	if updated.currentView != ViewList {
		t.Fatalf("selected workspace opened view %v; want the list to quit directly", updated.currentView)
	}
	if updated.result == nil {
		t.Fatal("selected workspace produced no result")
	}
	if updated.result.Action != ActionCD {
		t.Fatalf("selected workspace action = %v; want ActionCD", updated.result.Action)
	}
	if updated.result.Workspace == nil || updated.result.Workspace.Path != "/tmp/project" {
		t.Fatalf("selected workspace = %#v; want /tmp/project", updated.result.Workspace)
	}
	if cmd == nil {
		t.Fatal("selected workspace did not quit the chooser")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("selection command = %T; want tea.QuitMsg", cmd())
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
