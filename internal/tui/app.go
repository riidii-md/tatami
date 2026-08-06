package tui

import (
	"fmt"
	"os"
	"strings"

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
	ViewHerdrOpenMode
	ViewHerdrSessionName
	ViewHerdrSessionPicker
)

// Result represents the outcome of the TUI session
type Result struct {
	Action      Action
	Workspace   *workspace.Workspace
	Template    *workspace.Template
	Worktree    *git.Worktree
	SessionName string
	HerdrMode   HerdrOpenMode
	// HerdrSessionName is the explicitly selected destination for HerdrOpenExisting.
	HerdrSessionName string
}

type herdrSessionLister func() ([]shell.HerdrSession, error)
type herdrSessionStopper func(string) error

// AppOption configures optional chooser behavior.
type AppOption func(*App)

// WithNewTabMode adapts workspace actions for a dedicated terminal tab. The
// caller can replace Tatami with a shell rooted in a workspace or worktree.
func WithNewTabMode() AppOption {
	return func(a *App) {
		a.newTabMode = true
	}
}

// WithHerdrSessionLister injects the Herdr session source used by the home page and session picker.
func WithHerdrSessionLister(lister func() ([]shell.HerdrSession, error)) AppOption {
	return func(a *App) {
		a.herdrSessionLister = lister
	}
}

// WithHerdrSessionStopper injects the Herdr session stop operation.
func WithHerdrSessionStopper(stopper func(string) error) AppOption {
	return func(a *App) {
		a.herdrSessionStopper = stopper
	}
}

// App is the main Bubbletea model
type App struct {
	store                  *workspace.Store
	zellij                 *shell.ZellijRunner
	tmux                   *shell.TmuxRunner
	currentView            View
	previousView           View
	listView               *ListView
	createView             *CreateView
	actionsView            *ActionView
	layoutEditor           *LayoutEditor
	templateView           *TemplateView
	folderInput            *FolderInput
	worktreeView           *WorktreeView
	worktreeActionView     *WorktreeActionView
	sessionView            *SessionView
	herdrOpenModeView      *HerdrOpenModeView
	herdrSessionNameView   *HerdrSessionNameView
	herdrSessionPickerView *HerdrSessionPickerView
	pendingHerdrResult     *Result
	herdrOpenBackView      View
	result                 *Result
	width                  int
	height                 int
	err                    error
	newTabMode             bool
	herdrSessionLister     herdrSessionLister
	herdrSessionStopper    herdrSessionStopper
}

// NewApp creates a new App
func NewApp(store *workspace.Store, options ...AppOption) *App {
	zellij := shell.NewZellijRunner()
	tmux := shell.NewTmuxRunner()

	herdrRunner := shell.NewHerdrRunner()
	app := &App{
		store:               store,
		zellij:              zellij,
		tmux:                tmux,
		currentView:         ViewList,
		createView:          NewCreateView(),
		layoutEditor:        NewLayoutEditor(),
		herdrSessionLister:  shell.ListHerdrSessions,
		herdrSessionStopper: herdrRunner.StopSession,
	}
	for _, option := range options {
		option(app)
	}
	app.listView = NewListViewWithHerdrSessions(store, app.herdrSessionLister)
	app.listView.SetInZellij(zellij.IsInsideSession())
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
		case ViewHerdrOpenMode:
			return a.updateHerdrOpenMode(msg)
		case ViewHerdrSessionName:
			return a.updateHerdrSessionName(msg)
		case ViewHerdrSessionPicker:
			return a.updateHerdrSessionPicker(msg)
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
		case "herdr_session":
			a.result = &Result{
				Action:      ActionAttachHerdrSession,
				SessionName: item.Name,
			}
			return a, tea.Quit
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

	case "x":
		item := a.listView.Selected()
		if item != nil && item.Type == "herdr_session" && item.Herdr != nil && item.Herdr.Running {
			if err := a.herdrSessionStopper(item.Name); err != nil {
				a.err = err
				return a, nil
			}
			a.listView.Refresh()
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

		result := &Result{
			Action:    action,
			Workspace: ws,
		}
		if ws.Layout.Type == workspace.LayoutHerdr {
			a.beginHerdrOpen(result, ViewActions)
			return a, nil
		}
		a.result = result
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
			result := &Result{
				Action:    ActionWorktree,
				Workspace: ws,
				Worktree:  wt,
				Template:  tmpl,
			}
			if ws.Layout.Type == workspace.LayoutHerdr {
				a.beginHerdrOpen(result, ViewTemplates)
				return a, nil
			}
			a.result = result
			return a, tea.Quit
		}

		// If came from actions view, execute with template
		ws := a.actionsView.Workspace()
		result := &Result{
			Action:    ActionWithTemplate,
			Workspace: ws,
			Template:  tmpl,
		}
		if ws.Layout.Type == workspace.LayoutHerdr {
			a.beginHerdrOpen(result, ViewTemplates)
			return a, nil
		}
		a.result = result
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
		if ws.Layout.Type == workspace.LayoutHerdr {
			a.beginHerdrOpen(&Result{
				Action:    ActionWorktree,
				Workspace: ws,
				Worktree:  wt,
				Template:  &workspace.Template{},
			}, ViewWorktree)
			return a, nil
		}
		if a.newTabMode {
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

func (a *App) beginHerdrOpen(result *Result, backView View) {
	a.pendingHerdrResult = result
	a.herdrOpenBackView = backView
	a.herdrOpenModeView = NewHerdrOpenModeView()
	a.currentView = ViewHerdrOpenMode
}

func (a *App) updateHerdrOpenMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		if a.herdrOpenBackView == ViewWorktree && a.actionsView != nil {
			a.worktreeView = NewWorktreeView(a.actionsView.Workspace().Path)
		}
		a.pendingHerdrResult = nil
		a.currentView = a.herdrOpenBackView
		return a, nil
	case "enter":
		if a.pendingHerdrResult == nil {
			a.err = fmt.Errorf("no pending Herdr workspace")
			return a, nil
		}
		mode := a.herdrOpenModeView.Selected()
		a.pendingHerdrResult.HerdrMode = mode
		if mode == HerdrOpenExisting {
			sessions, err := a.herdrSessionLister()
			currentSession := ""
			if os.Getenv("HERDR_ENV") == "1" {
				currentSession = strings.TrimSpace(os.Getenv("HERDR_SESSION"))
			}
			a.herdrSessionPickerView = NewHerdrSessionPickerView(sessions, currentSession, err)
			a.currentView = ViewHerdrSessionPicker
			return a, nil
		}
		a.herdrSessionNameView = NewHerdrSessionNameView(defaultHerdrSessionName(a.pendingHerdrResult))
		a.currentView = ViewHerdrSessionName
		return a, nil
	default:
		return a, a.herdrOpenModeView.Update(msg)
	}
}

func defaultHerdrSessionName(result *Result) string {
	if result == nil {
		return ""
	}
	name := "workspace"
	if result.Workspace != nil && result.Workspace.Name != "" {
		name = result.Workspace.Name
	}
	if result.Worktree != nil {
		if result.Worktree.Branch != "" {
			name = result.Worktree.Branch
		} else {
			name = "worktree"
		}
	}
	return shell.SessionName(name)
}

func (a *App) updateHerdrSessionName(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.currentView = ViewHerdrOpenMode
		return a, nil
	case "enter":
		if a.pendingHerdrResult == nil {
			a.err = fmt.Errorf("no pending Herdr workspace")
			return a, nil
		}
		name := a.herdrSessionNameView.Value()
		if name == "" {
			return a, nil
		}
		a.pendingHerdrResult.HerdrMode = HerdrOpenDedicated
		a.pendingHerdrResult.HerdrSessionName = name
		a.result = a.pendingHerdrResult
		return a, tea.Quit
	default:
		return a, a.herdrSessionNameView.Update(msg)
	}
}

func (a *App) updateHerdrSessionPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		a.currentView = ViewHerdrOpenMode
		return a, nil
	case "enter":
		if a.pendingHerdrResult == nil {
			a.err = fmt.Errorf("no pending Herdr workspace")
			return a, nil
		}
		session := a.herdrSessionPickerView.Selected()
		if session == "" {
			return a, nil
		}
		a.pendingHerdrResult.HerdrMode = HerdrOpenExisting
		a.pendingHerdrResult.HerdrSessionName = session
		a.result = a.pendingHerdrResult
		return a, tea.Quit
	default:
		return a, a.herdrSessionPickerView.Update(msg)
	}
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
	case ViewHerdrOpenMode:
		return a.herdrOpenModeView.View()
	case ViewHerdrSessionName:
		return a.herdrSessionNameView.View()
	case ViewHerdrSessionPicker:
		return a.herdrSessionPickerView.View()
	default:
		return ""
	}
}
