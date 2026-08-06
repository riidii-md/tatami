package tui

import (
	"fmt"

	"github.com/OleksandrBesan/tatami/internal/git"
	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

// View represents the current view state
type View int

const (
	ViewList View = iota
	ViewCreate
	ViewActions
	ViewLayout
	ViewTemplates
	ViewFolderInput
	ViewWorktree
	ViewWorktreeActions
	ViewSessions
)

// Result represents the outcome of the TUI session
type Result struct {
	Action      Action
	Workspace   *workspace.Workspace
	Template    *workspace.Template
	Worktree    *git.Worktree
	SessionName string
}

// AppOption configures optional chooser behavior.
type AppOption func(*App)

// WithNewTabMode adapts workspace actions for a dedicated terminal tab. The
// caller can replace Tatami with a shell rooted in a workspace or worktree.
func WithNewTabMode() AppOption {
	return func(a *App) {
		a.newTabMode = true
	}
}

// App is the main Bubbletea model
type App struct {
	store              *workspace.Store
	zellij             *shell.ZellijRunner
	tmux               *shell.TmuxRunner
	currentView        View
	previousView       View
	listView           *ListView
	createView         *CreateView
	actionsView        *ActionView
	layoutEditor       *LayoutEditor
	templateView       *TemplateView
	folderInput        *FolderInput
	worktreeView       *WorktreeView
	worktreeActionView *WorktreeActionView
	sessionView        *SessionView
	result             *Result
	width              int
	height             int
	err                error
	newTabMode         bool
}

// NewApp creates a new App
func NewApp(store *workspace.Store, options ...AppOption) *App {
	zellij := shell.NewZellijRunner()
	tmux := shell.NewTmuxRunner()

	listView := NewListView(store)
	listView.SetInZellij(zellij.IsInsideSession())

	app := &App{
		store:        store,
		zellij:       zellij,
		tmux:         tmux,
		currentView:  ViewList,
		listView:     listView,
		createView:   NewCreateView(),
		layoutEditor: NewLayoutEditor(),
	}
	for _, option := range options {
		option(app)
	}
	return app
}

// Result returns the result of the TUI session
func (a *App) Result() *Result {
	return a.result
}

// Init implements tea.Model
func (a *App) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.listView.SetSize(msg.Width, msg.Height)
		return a, nil

	case tea.KeyMsg:
		// Global quit
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}

		// View-specific handling
		switch a.currentView {
		case ViewList:
			return a.updateList(msg)
		case ViewCreate:
			return a.updateCreate(msg)
		case ViewActions:
			return a.updateActions(msg)
		case ViewLayout:
			return a.updateLayout(msg)
		case ViewTemplates:
			return a.updateTemplates(msg)
		case ViewFolderInput:
			return a.updateFolderInput(msg)
		case ViewWorktree:
			return a.updateWorktree(msg)
		case ViewWorktreeActions:
			return a.updateWorktreeActions(msg)
		case ViewSessions:
			return a.updateSessions(msg)
		}
	}

	return a, nil
}

func (a *App) updateFolderInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.currentView = ViewList
		return a, nil

	case "enter":
		folderPath := a.folderInput.Value()
		if folderPath != "" {
			// Set current folder to the new path and go to create view
			a.listView.SetCurrentFolder(folderPath)
			a.createView.Reset()
			a.createView.SetFolder(folderPath)
			a.currentView = ViewCreate
			return a, nil
		}
		a.currentView = ViewList
		return a, nil

	default:
		return a, a.folderInput.Update(msg)
	}
}

func (a *App) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle filter mode
	if a.listView.IsFiltering() {
		switch msg.String() {
		case "enter":
			a.listView.StopFiltering()
			return a, nil
		case "esc":
			a.listView.ClearFilter()
			return a, nil
		default:
			return a, a.listView.Update(msg)
		}
	}

	switch msg.String() {
	case "q":
		return a, tea.Quit

	case "esc":
		// Go back if in folder, otherwise quit
		if a.listView.CurrentFolder() != "" {
			a.listView.EnterFolder("..")
			return a, nil
		}
		return a, tea.Quit

	case "enter", "l":
		item := a.listView.Selected()
		if item == nil {
			return a, nil
		}
		switch item.Type {
		case "folder":
			a.listView.EnterFolder(item.Name)
		case "workspace":
			a.actionsView = NewActionView(item.Workspace, a.zellij.IsInsideSession(), a.tmux.IsInsideSession(), a.newTabMode)
			a.currentView = ViewActions
		}
		return a, nil

	case "n":
		a.createView.Reset()
		// Set current folder for new workspace
		a.createView.SetFolder(a.listView.CurrentFolder())
		a.currentView = ViewCreate
		return a, nil

	case "e":
		item := a.listView.Selected()
		if item != nil && item.Type == "workspace" {
			a.createView.EditWorkspace(item.Workspace)
			a.currentView = ViewCreate
		}
		return a, nil

	case "d":
		item := a.listView.Selected()
		if item != nil && item.Type == "workspace" {
			if err := a.store.Delete(item.Workspace.Name); err == nil {
				a.listView.Refresh()
			}
		}
		return a, nil

	case "*", "s":
		// Toggle quick access
		item := a.listView.Selected()
		if item != nil && item.Type == "workspace" {
			a.store.ToggleQuickAccess(item.Workspace.Name)
			a.listView.Refresh()
		}
		return a, nil

	case "f":
		// Create folder
		a.folderInput = NewFolderInput(a.listView.CurrentFolder())
		a.currentView = ViewFolderInput
		return a, nil

	case "z":
		// Open Zellij session browser
		// canAttach is false when inside Zellij (nested attach doesn't work)
		a.sessionView = NewSessionView(!a.zellij.IsInsideSession())
		a.currentView = ViewSessions
		return a, nil

	default:
		return a, a.listView.Update(msg)
	}
}

func (a *App) updateCreate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// If in layout editor, go back to create form
		if a.currentView == ViewLayout {
			a.currentView = ViewCreate
			return a, nil
		}
		a.currentView = ViewList
		return a, nil

	case "enter":
		// Don't submit if we're in layout mode
		if a.currentView == ViewLayout && a.layoutEditor.IsEditing() {
			return a, a.layoutEditor.Update(msg)
		}

		a.createView.Validate()
		if a.createView.errorMsg != "" {
			return a, nil
		}

		ws := a.createView.GetWorkspace()

		var err error
		if a.createView.IsEditing() {
			err = a.store.Update(a.createView.EditingName(), ws)
		} else {
			err = a.store.Create(ws)
		}

		if err != nil {
			a.createView.SetError(err.Error())
			return a, nil
		}

		a.listView.Refresh()
		a.currentView = ViewList
		return a, nil

	case "ctrl+l":
		// Open layout editor
		ws := a.createView.GetWorkspace()
		a.layoutEditor.SetPanes(ws.Layout.Panes)
		a.currentView = ViewLayout
		return a, nil

	case "f2":
		// Open template picker
		a.templateView = NewTemplateView()
		a.previousView = ViewCreate
		a.currentView = ViewTemplates
		return a, nil

	default:
		return a, a.createView.Update(msg)
	}
}

func (a *App) updateActions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		a.currentView = ViewList
		return a, nil

	case "enter":
		action := a.actionsView.Selected()
		ws := a.actionsView.Workspace()

		// If template action, show template picker
		if action == ActionWithTemplate {
			a.templateView = NewTemplateView()
			a.previousView = ViewActions
			a.currentView = ViewTemplates
			return a, nil
		}

		// If worktree action, show worktree picker
		if action == ActionWorktree {
			a.worktreeView = NewWorktreeView(ws.Path)
			a.currentView = ViewWorktree
			return a, nil
		}

		a.result = &Result{
			Action:    action,
			Workspace: ws,
		}
		return a, tea.Quit

	default:
		return a, a.actionsView.Update(msg)
	}
}

func (a *App) updateLayout(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if a.layoutEditor.IsEditing() {
			// Cancel pane edit
			return a, a.layoutEditor.Update(msg)
		}
		// Save panes and go back
		ws := a.createView.GetWorkspace()
		ws.Layout.Panes = a.layoutEditor.GetPanes()
		a.createView.EditWorkspace(ws)
		a.createView.editing = true // Keep edit mode
		a.currentView = ViewCreate
		return a, nil

	default:
		return a, a.layoutEditor.Update(msg)
	}
}

func (a *App) updateTemplates(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		a.currentView = a.previousView
		return a, nil

	case "enter":
		tmpl := a.templateView.Selected()

		// If came from create view, apply template and go back
		if a.previousView == ViewCreate {
			a.createView.ApplyTemplate(tmpl)
			a.currentView = ViewCreate
			return a, nil
		}

		// If came from worktree actions view, execute worktree with template
		if a.previousView == ViewWorktreeActions {
			ws := a.worktreeActionView.Workspace()
			wt := a.worktreeActionView.Worktree()
			a.result = &Result{
				Action:    ActionWorktree,
				Workspace: ws,
				Worktree:  wt,
				Template:  tmpl,
			}
			return a, tea.Quit
		}

		// If came from actions view, execute with template
		ws := a.actionsView.Workspace()
		a.result = &Result{
			Action:    ActionWithTemplate,
			Workspace: ws,
			Template:  tmpl,
		}
		return a, tea.Quit

	default:
		return a, a.templateView.Update(msg)
	}
}

func (a *App) updateWorktree(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		// Only go back if in list mode
		if a.worktreeView.Mode() == WorktreeModeList {
			a.currentView = ViewActions
			return a, nil
		}
	}

	// Let worktree view handle the input
	cmd := a.worktreeView.Update(msg)

	// Check if a worktree was selected - show worktree actions
	if wt := a.worktreeView.Selected(); wt != nil {
		ws := a.actionsView.Workspace()
		if a.newTabMode || ws.Layout.Type == workspace.LayoutHerdr {
			a.result = &Result{
				Action:    ActionWorktree,
				Workspace: ws,
				Worktree:  wt,
				Template:  &workspace.Template{},
			}
			return a, tea.Quit
		}
		a.worktreeActionView = NewWorktreeActionView(wt, ws)
		a.currentView = ViewWorktreeActions
		return a, nil
	}

	return a, cmd
}

func (a *App) updateWorktreeActions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		// Reset worktree selection and go back to worktree list
		a.worktreeView = NewWorktreeView(a.actionsView.Workspace().Path)
		a.currentView = ViewWorktree
		return a, nil

	case "enter":
		action := a.worktreeActionView.Selected()
		ws := a.worktreeActionView.Workspace()
		wt := a.worktreeActionView.Worktree()

		switch action {
		case WorktreeActionWithTemplate:
			a.templateView = NewTemplateView()
			a.previousView = ViewWorktreeActions
			a.currentView = ViewTemplates
			return a, nil

		case WorktreeActionWithLayout:
			a.result = &Result{
				Action:    ActionWorktree,
				Workspace: ws,
				Worktree:  wt,
			}
			return a, tea.Quit

		case WorktreeActionPlain:
			a.result = &Result{
				Action:    ActionWorktree,
				Workspace: ws,
				Worktree:  wt,
				Template:  &workspace.Template{}, // Empty template = plain
			}
			return a, tea.Quit
		}

	default:
		return a, a.worktreeActionView.Update(msg)
	}

	return a, nil
}

func (a *App) updateSessions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		// Only go back if in list mode
		if a.sessionView.Mode() == SessionModeList {
			a.currentView = ViewList
			return a, nil
		}

	case "enter":
		// Attach to selected session (only if allowed and not current)
		if a.sessionView.Mode() == SessionModeList && a.sessionView.CanAttach() {
			sessionName := a.sessionView.Selected()
			if sessionName != "" && !a.sessionView.IsCurrentSession() {
				a.result = &Result{
					Action:      ActionAttachSession,
					SessionName: sessionName,
				}
				return a, tea.Quit
			}
		}
	}

	// Let session view handle the input
	return a, a.sessionView.Update(msg)
}

// View implements tea.Model
func (a *App) View() string {
	if a.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v", a.err))
	}

	switch a.currentView {
	case ViewList:
		return a.listView.View()
	case ViewCreate:
		return a.createView.View()
	case ViewActions:
		return a.actionsView.View()
	case ViewLayout:
		return boxStyle.Render(a.layoutEditor.View())
	case ViewTemplates:
		return a.templateView.View()
	case ViewFolderInput:
		return a.folderInput.View()
	case ViewWorktree:
		return a.worktreeView.View()
	case ViewWorktreeActions:
		return a.worktreeActionView.View()
	case ViewSessions:
		return a.sessionView.View()
	default:
		return ""
	}
}
