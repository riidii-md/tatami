package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/OleksandrBesan/tatami/internal/git"
	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/systemusage"
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
	ViewHerdrSessionDelete
	ViewHerdrHost
	ViewHerdrHostDelete
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
	// HerdrEndpointID and HerdrTarget preserve the composite identity for a
	// session selected from the federated hub. They are empty for local rows.
	HerdrEndpointID string
	HerdrTarget     string
	HerdrVia        []string
}

type herdrSessionLister func() ([]shell.HerdrSession, error)
type herdrSessionStopper func(string) error
type herdrSessionDeleter func(string) error
type herdrSessionUsageCollector func(string) (systemusage.Report, error)

const herdrUsageDebounce = 120 * time.Millisecond

type herdrUsageRequestMsg struct {
	Session    string
	Generation uint64
}

type herdrUsageResultMsg struct {
	Session    string
	Generation uint64
	Report     systemusage.Report
	Err        error
}

// AppOption configures optional chooser behavior.
type AppOption func(*App)

type herdrHubRefresher func(context.Context, []herdrhub.Endpoint, herdrhub.Cache) []herdrhub.Snapshot
type herdrHubCacheSaver func(herdrhub.Cache) error
type herdrHubEndpointSaver func([]herdrhub.Endpoint) error
type herdrHubAgentQuery func(context.Context, herdrhub.Endpoint, string) ([]herdrhub.Agent, error)
type herdrHubInteractiveInventory func(context.Context, herdrhub.Endpoint, io.Reader, io.Writer) (herdrhub.Snapshot, error)

// WithHerdrHubSnapshots renders safe cached remote inventory immediately.
func WithHerdrHubSnapshots(endpoints []herdrhub.Endpoint, snapshots []herdrhub.Snapshot) AppOption {
	return func(a *App) { a.hubEndpoints = endpoints; a.hubSnapshots = snapshots }
}

// WithHerdrHubRefresh enables background, endpoint-local inventory refresh.
func WithHerdrHubRefresh(refresh herdrHubRefresher, save herdrHubCacheSaver) AppOption {
	return func(a *App) { a.herdrHubRefresher = refresh; a.herdrHubCacheSaver = save }
}
func WithHerdrHubEndpointSaver(save herdrHubEndpointSaver) AppOption {
	return func(a *App) { a.herdrHubEndpointSaver = save }
}
func WithHerdrHubAgentQuery(query herdrHubAgentQuery) AppOption {
	return func(a *App) { a.herdrHubAgentQuery = query }
}
func WithHerdrHubInteractiveInventory(query herdrHubInteractiveInventory) AppOption {
	return func(a *App) { a.herdrHubInteractiveInventory = query }
}

// WithNewTabMode adapts workspace actions for a dedicated terminal tab. The
// caller can replace Tatami with a shell rooted in a workspace or worktree.
func WithNewTabMode() AppOption {
	return func(a *App) {
		a.newTabMode = true
	}
}

// WithMobileMode enables phone-friendly navigation and compact rendering.
func WithMobileMode() AppOption {
	return func(a *App) {
		a.mobileMode = true
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

// WithHerdrSessionDeleter injects the Herdr session deletion operation.
func WithHerdrSessionDeleter(deleter func(string) error) AppOption {
	return func(a *App) {
		a.herdrSessionDeleter = deleter
	}
}

// WithHerdrSessionUsageCollector injects highlighted-session resource collection.
func WithHerdrSessionUsageCollector(collector func(string) (systemusage.Report, error)) AppOption {
	return func(a *App) {
		a.herdrSessionUsageCollector = collector
	}
}

// App is the main Bubbletea model
type App struct {
	store                        *workspace.Store
	zellij                       *shell.ZellijRunner
	tmux                         *shell.TmuxRunner
	currentView                  View
	previousView                 View
	listView                     *ListView
	createView                   *CreateView
	actionsView                  *ActionView
	layoutEditor                 *LayoutEditor
	templateView                 *TemplateView
	folderInput                  *FolderInput
	worktreeView                 *WorktreeView
	worktreeActionView           *WorktreeActionView
	sessionView                  *SessionView
	herdrOpenModeView            *HerdrOpenModeView
	herdrSessionNameView         *HerdrSessionNameView
	herdrSessionPickerView       *HerdrSessionPickerView
	herdrSessionDeleteView       *HerdrSessionDeleteView
	pendingHerdrResult           *Result
	herdrOpenBackView            View
	result                       *Result
	width                        int
	height                       int
	err                          error
	newTabMode                   bool
	mobileMode                   bool
	herdrSessionLister           herdrSessionLister
	herdrSessionStopper          herdrSessionStopper
	herdrSessionDeleter          herdrSessionDeleter
	herdrSessionUsageCollector   herdrSessionUsageCollector
	herdrUsageGeneration         uint64
	hubEndpoints                 []herdrhub.Endpoint
	hubSnapshots                 []herdrhub.Snapshot
	herdrHubRefresher            herdrHubRefresher
	herdrHubCacheSaver           herdrHubCacheSaver
	herdrHubGeneration           uint64
	herdrHubCancel               context.CancelFunc
	herdrHubEndpointSaver        herdrHubEndpointSaver
	herdrHostView                *HerdrHostView
	herdrHostEditingID           string
	herdrHubAgentQuery           herdrHubAgentQuery
	herdrHubInteractiveInventory herdrHubInteractiveInventory
	herdrHubAgentGeneration      uint64
	herdrHubAgentCancel          context.CancelFunc
	herdrHostDeleteView          *HerdrHostDeleteView
}

// NewApp creates a new App
func NewApp(store *workspace.Store, options ...AppOption) *App {
	zellij := shell.NewZellijRunner()
	tmux := shell.NewTmuxRunner()

	herdrRunner := shell.NewHerdrRunner()
	app := &App{
		store:                      store,
		zellij:                     zellij,
		tmux:                       tmux,
		currentView:                ViewList,
		createView:                 NewCreateView(),
		layoutEditor:               NewLayoutEditor(),
		herdrSessionLister:         shell.ListHerdrSessions,
		herdrSessionStopper:        herdrRunner.StopSession,
		herdrSessionDeleter:        herdrRunner.DeleteSession,
		herdrSessionUsageCollector: systemusage.CollectHerdrSession,
	}
	for _, option := range options {
		option(app)
	}
	app.listView = NewListViewWithHerdrSessions(store, app.herdrSessionLister)
	app.listView.SetHerdrHubSnapshots(app.hubEndpoints, app.hubSnapshots)
	app.listView.SetInZellij(zellij.IsInsideSession())
	app.applyMobileMode(app.listView)
	app.applyMobileMode(app.createView)
	return app
}

func (a *App) applyMobileMode(view mobileModeSetter) {
	view.SetMobileMode(a.mobileMode)
}

// Result returns the result of the TUI session
func (a *App) Result() *Result {
	return a.result
}

// Init implements tea.Model
func (a *App) Init() tea.Cmd {
	return batchCommands(a.scheduleSelectedHerdrUsage(), a.scheduleHubRefresh(a.remoteHubEndpoints()), a.scheduleSelectedHubAgents())
}

type herdrHubAgentsResultMsg struct {
	EndpointID, Session string
	Generation          uint64
	Agents              []herdrhub.Agent
	Err                 error
}

func (a *App) scheduleSelectedHubAgents() tea.Cmd {
	if a.herdrHubAgentCancel != nil {
		a.herdrHubAgentCancel()
		a.herdrHubAgentCancel = nil
	}
	a.herdrHubAgentGeneration++
	s := a.listView.Selected()
	if a.herdrHubAgentQuery == nil || s == nil || s.Endpoint == nil || s.Herdr == nil || !s.Herdr.Running {
		return nil
	}
	generation := a.herdrHubAgentGeneration
	endpoint := *s.Endpoint
	session := s.Herdr.Name
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	a.herdrHubAgentCancel = cancel
	a.listView.SetHerdrHubAgentsLoading(endpoint.Key(), session)
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		agents, err := a.herdrHubAgentQuery(ctx, endpoint, session)
		return herdrHubAgentsResultMsg{EndpointID: endpoint.Key(), Session: session, Generation: generation, Agents: agents, Err: err}
	})
}

func (a *App) remoteHubEndpoints() []herdrhub.Endpoint {
	remote := make([]herdrhub.Endpoint, 0, len(a.hubEndpoints))
	for _, endpoint := range a.hubEndpoints {
		if endpoint.ID != herdrhub.LocalEndpointID {
			remote = append(remote, endpoint)
		}
	}
	return remote
}

type herdrHubRefreshResultMsg struct {
	Generation uint64
	Snapshots  []herdrhub.Snapshot
}

type herdrHubInteractiveInventoryMsg struct {
	Endpoint herdrhub.Endpoint
	Snapshot herdrhub.Snapshot
	Err      error
}

type herdrHubInteractiveInventoryCommand struct {
	endpoint herdrhub.Endpoint
	query    herdrHubInteractiveInventory
	stdin    io.Reader
	stderr   io.Writer
	snapshot herdrhub.Snapshot
}

func (c *herdrHubInteractiveInventoryCommand) SetStdin(stdin io.Reader)   { c.stdin = stdin }
func (c *herdrHubInteractiveInventoryCommand) SetStdout(io.Writer)        {}
func (c *herdrHubInteractiveInventoryCommand) SetStderr(stderr io.Writer) { c.stderr = stderr }
func (c *herdrHubInteractiveInventoryCommand) Run() error {
	snapshot, err := c.query(context.Background(), c.endpoint, c.stdin, c.stderr)
	c.snapshot = snapshot
	return err
}

func (a *App) scheduleInteractiveHerdrInventory(endpoint herdrhub.Endpoint) tea.Cmd {
	if a.herdrHubInteractiveInventory == nil {
		a.err = fmt.Errorf("interactive remote Tatami discovery is unavailable")
		return nil
	}
	command := &herdrHubInteractiveInventoryCommand{endpoint: endpoint, query: a.herdrHubInteractiveInventory}
	return tea.Exec(command, func(err error) tea.Msg {
		return herdrHubInteractiveInventoryMsg{Endpoint: endpoint, Snapshot: command.snapshot, Err: err}
	})
}

func (a *App) scheduleHubRefresh(endpoints []herdrhub.Endpoint) tea.Cmd {
	if a.herdrHubRefresher == nil || len(endpoints) == 0 {
		return nil
	}
	if a.herdrHubCancel != nil {
		a.herdrHubCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.herdrHubCancel = cancel
	a.herdrHubGeneration++
	generation := a.herdrHubGeneration
	previous := herdrhub.Cache{Snapshots: append([]herdrhub.Snapshot(nil), a.hubSnapshots...)}
	endpointCopy := append([]herdrhub.Endpoint(nil), endpoints...)
	semaphore := make(chan struct{}, 3)
	commands := make([]tea.Cmd, 0, len(endpointCopy))
	for _, endpoint := range endpointCopy {
		endpoint := endpoint
		commands = append(commands, func() tea.Msg {
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return herdrHubRefreshResultMsg{Generation: generation}
			}
			return herdrHubRefreshResultMsg{
				Generation: generation,
				Snapshots:  a.herdrHubRefresher(ctx, []herdrhub.Endpoint{endpoint}, previous),
			}
		})
	}
	return batchCommands(commands...)
}

// Update implements tea.Model
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		resized := a.width > 0 && (a.width != msg.Width || a.height != msg.Height)
		a.width = msg.Width
		a.height = msg.Height
		a.listView.SetSize(msg.Width, msg.Height)
		if resized {
			return a, tea.ClearScreen
		}
		return a, nil
	case herdrHubRefreshResultMsg:
		if msg.Generation != a.herdrHubGeneration {
			return a, nil
		}
		a.hubSnapshots = mergeHubSnapshots(a.hubEndpoints, a.hubSnapshots, msg.Snapshots)
		a.listView.SetHerdrHubSnapshots(a.hubEndpoints, a.hubSnapshots)
		if a.herdrHubCacheSaver != nil {
			if err := a.herdrHubCacheSaver(herdrhub.Cache{Snapshots: a.hubSnapshots}); err != nil {
				a.err = err
			}
		}
		return a, nil
	case herdrHubAgentsResultMsg:
		if msg.Generation != a.herdrHubAgentGeneration {
			return a, nil
		}
		s := a.listView.Selected()
		if s == nil || s.Endpoint == nil || s.Herdr == nil || s.Endpoint.Key() != msg.EndpointID || s.Herdr.Name != msg.Session {
			return a, nil
		}
		a.listView.SetHerdrHubAgents(msg.EndpointID, msg.Session, msg.Agents, msg.Err)
		if a.herdrHubAgentCancel != nil {
			a.herdrHubAgentCancel()
			a.herdrHubAgentCancel = nil
		}
		return a, nil
	case herdrHubInteractiveInventoryMsg:
		if msg.Err != nil {
			a.err = msg.Err
			return a, nil
		}
		a.hubSnapshots = mergeHubSnapshots(a.hubEndpoints, a.hubSnapshots, []herdrhub.Snapshot{msg.Snapshot})
		a.listView.SetHerdrHubSnapshots(a.hubEndpoints, a.hubSnapshots)
		a.listView.ExpandHerdrEndpoint(msg.Endpoint.Key())
		if a.herdrHubCacheSaver != nil {
			if err := a.herdrHubCacheSaver(herdrhub.Cache{Snapshots: a.hubSnapshots}); err != nil {
				a.err = err
			}
		}
		a.currentView = ViewList
		return a, nil

	case herdrUsageRequestMsg:
		if !a.currentHerdrUsageRequest(msg.Session, msg.Generation) {
			return a, nil
		}
		return a, func() tea.Msg {
			report, err := a.herdrSessionUsageCollector(msg.Session)
			return herdrUsageResultMsg{
				Session: msg.Session, Generation: msg.Generation, Report: report, Err: err,
			}
		}

	case herdrUsageResultMsg:
		if !a.currentHerdrUsageRequest(msg.Session, msg.Generation) {
			return a, nil
		}
		var usage *systemusage.SessionUsage
		for i := range msg.Report.Sessions {
			if msg.Report.Sessions[i].Name == msg.Session {
				usage = &msg.Report.Sessions[i]
				break
			}
		}
		if usage == nil && msg.Err == nil {
			usage = &systemusage.SessionUsage{Name: msg.Session}
		}
		a.listView.SetHerdrUsage(msg.Session, usage, msg.Err)
		return a, nil
	case tea.MouseMsg:
		return a.updateMouse(msg)

	case tea.KeyMsg:
		// Global quit
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}
		if handled, model, cmd := a.handleMobileBack(msg); handled {
			return model, cmd
		}

		// View-specific handling
		switch a.currentView {
		case ViewList:
			before := a.selectedHerdrUsageKey()
			model, cmd := a.updateList(msg)
			if before != a.selectedHerdrUsageKey() {
				cmd = batchCommands(cmd, a.scheduleSelectedHerdrUsage(), a.scheduleSelectedHubAgents())
			}
			return model, cmd
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
		case ViewHerdrSessionDelete:
			return a.updateHerdrSessionDelete(msg)
		case ViewHerdrHost:
			return a.updateHerdrHost(msg)
		case ViewHerdrHostDelete:
			return a.updateHerdrHostDelete(msg)
		}
	}

	return a, nil
}

func (a *App) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !a.mobileMode {
		return a, nil
	}

	event := tea.MouseEvent(msg)
	switch event.Button {
	case tea.MouseButtonWheelUp:
		return a.Update(tea.KeyMsg{Type: tea.KeyUp})
	case tea.MouseButtonWheelDown:
		return a.Update(tea.KeyMsg{Type: tea.KeyDown})
	case tea.MouseButtonWheelLeft:
		return a.Update(tea.KeyMsg{Type: tea.KeyLeft})
	case tea.MouseButtonWheelRight:
		return a.Update(tea.KeyMsg{Type: tea.KeyRight})
	case tea.MouseButtonLeft:
		if event.Action != tea.MouseActionPress {
			return a, nil
		}
	default:
		return a, nil
	}

	if a.currentView == ViewList {
		if !a.listView.selectMouseRow(event.Y) {
			return a, nil
		}
		return a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if !a.currentViewSupportsNumberedTap() {
		return a, nil
	}

	choice, ok := numberedChoiceAtRow(a.View(), event.Y)
	if !ok {
		return a, nil
	}
	model, selectCmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{choice}})
	updated, ok := model.(*App)
	if !ok {
		return model, selectCmd
	}
	model, enterCmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return model, batchCommands(selectCmd, enterCmd)
}

func (a *App) currentViewSupportsNumberedTap() bool {
	switch a.currentView {
	case ViewActions,
		ViewTemplates,
		ViewWorktree,
		ViewWorktreeActions,
		ViewSessions,
		ViewHerdrOpenMode,
		ViewHerdrSessionPicker,
		ViewHerdrSessionDelete,
		ViewHerdrHostDelete:
		return true
	default:
		return false
	}
}

func mergeHubSnapshots(endpoints []herdrhub.Endpoint, current, updates []herdrhub.Snapshot) []herdrhub.Snapshot {
	byID := make(map[string]herdrhub.Snapshot, len(current)+len(updates))
	for _, snapshot := range current {
		byID[snapshot.EndpointID] = snapshot
	}
	for _, snapshot := range updates {
		byID[snapshot.EndpointID] = snapshot
	}
	merged := make([]herdrhub.Snapshot, 0, len(byID))
	added := make(map[string]bool, len(byID))
	for _, endpoint := range endpoints {
		if endpoint.ID == herdrhub.LocalEndpointID {
			continue
		}
		if snapshot, ok := byID[endpoint.Key()]; ok {
			merged = append(merged, snapshot)
			added[snapshot.EndpointID] = true
		}
	}
	for _, snapshot := range current {
		if !added[snapshot.EndpointID] {
			merged = append(merged, byID[snapshot.EndpointID])
			added[snapshot.EndpointID] = true
		}
	}
	for _, snapshot := range updates {
		if !added[snapshot.EndpointID] {
			merged = append(merged, snapshot)
			added[snapshot.EndpointID] = true
		}
	}
	return merged
}

func (a *App) selectedHerdrUsageKey() string {
	selected := a.listView.Selected()
	if selected == nil || selected.Type != "herdr_session" || selected.Herdr == nil {
		return ""
	}
	endpointID := herdrhub.LocalEndpointID
	if selected.Endpoint != nil {
		endpointID = selected.Endpoint.Key()
	}
	return fmt.Sprintf("%s:%s:%t", endpointID, selected.Herdr.Name, selected.Herdr.Running)
}

func (a *App) scheduleSelectedHerdrUsage() tea.Cmd {
	a.herdrUsageGeneration++
	selected := a.listView.Selected()
	if selected == nil || selected.Type != "herdr_session" || selected.Herdr == nil || !selected.Herdr.Running || selected.Endpoint != nil {
		a.listView.ClearHerdrUsage()
		return nil
	}

	session := selected.Herdr.Name
	generation := a.herdrUsageGeneration
	a.listView.SetHerdrUsageLoading(session)
	return tea.Tick(herdrUsageDebounce, func(time.Time) tea.Msg {
		return herdrUsageRequestMsg{Session: session, Generation: generation}
	})
}

func (a *App) currentHerdrUsageRequest(session string, generation uint64) bool {
	if generation != a.herdrUsageGeneration {
		return false
	}
	selected := a.listView.Selected()
	return selected != nil && selected.Type == "herdr_session" && selected.Herdr != nil &&
		selected.Herdr.Running && selected.Endpoint == nil && selected.Herdr.Name == session
}

func batchCommands(commands ...tea.Cmd) tea.Cmd {
	valid := make([]tea.Cmd, 0, len(commands))
	for _, command := range commands {
		if command != nil {
			valid = append(valid, command)
		}
	}
	switch len(valid) {
	case 0:
		return nil
	case 1:
		return valid[0]
	default:
		return tea.Batch(valid...)
	}
}

func (a *App) handleMobileBack(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	if !a.mobileMode || msg.String() != "b" {
		return false, a, nil
	}

	switch a.currentView {
	case ViewList:
		if a.listView.IsFiltering() {
			return false, a, nil
		}
		if a.listView.CurrentFolder() == "" {
			return true, a, nil
		}
	case ViewActions, ViewTemplates, ViewWorktreeActions, ViewSessions,
		ViewHerdrOpenMode, ViewHerdrSessionPicker, ViewHerdrSessionDelete, ViewHerdrHostDelete:
	case ViewWorktree:
		if a.worktreeView == nil || a.worktreeView.Mode() == WorktreeModeCreate || a.worktreeView.IsFiltering() {
			return false, a, nil
		}
	default:
		return false, a, nil
	}

	model, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	return true, model, cmd
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
	case " ":
		item := a.listView.Selected()
		if item != nil && item.Type == "herdr_endpoint" && item.Endpoint != nil {
			a.listView.ToggleHerdrEndpoint(item.Endpoint.Key())
		}
		return a, nil
	case "r":
		selected := a.listView.Selected()
		if selected != nil && selected.Type == "herdr_session" && selected.Endpoint == nil {
			a.listView.Refresh()
			return a, nil
		}
		if selected != nil && selected.Endpoint != nil {
			if selected.Endpoint.ID == herdrhub.LocalEndpointID {
				a.listView.Refresh()
				return a, nil
			}
			return a, a.scheduleHubRefresh([]herdrhub.Endpoint{*selected.Endpoint})
		}
		a.listView.Refresh()
		return a, a.scheduleHubRefresh(a.remoteHubEndpoints())
	case "R":
		a.listView.Refresh()
		return a, a.scheduleHubRefresh(a.remoteHubEndpoints())

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
		case "herdr_endpoint":
			if item.Endpoint != nil {
				if item.Endpoint.ID != herdrhub.LocalEndpointID {
					for _, snapshot := range a.hubSnapshots {
						if snapshot.EndpointID == item.Endpoint.Key() && snapshot.State == herdrhub.StateOnline {
							a.listView.ExpandHerdrEndpoint(item.Endpoint.Key())
							return a, nil
						}
					}
					return a, a.scheduleInteractiveHerdrInventory(*item.Endpoint)
				}
				a.listView.ToggleHerdrEndpoint(item.Endpoint.Key())
			}
		case "folder":
			a.listView.EnterFolder(item.Name)
		case "workspace":
			a.actionsView = NewActionView(item.Workspace, a.zellij.IsInsideSession(), a.tmux.IsInsideSession(), a.newTabMode)
			a.applyMobileMode(a.actionsView)
			a.currentView = ViewActions
		case "herdr_session":
			a.result = &Result{
				Action:      ActionAttachHerdrSession,
				SessionName: item.Herdr.Name,
			}
			if item.Endpoint != nil {
				a.result.HerdrEndpointID = item.Endpoint.Key()
				a.result.HerdrTarget = item.Endpoint.Target
				a.result.HerdrVia = append([]string(nil), item.Endpoint.Via...)
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
	case "a":
		a.herdrHostView = NewHerdrHostView(herdrhub.Endpoint{})
		a.applyMobileMode(a.herdrHostView)
		a.herdrHostEditingID = ""
		a.currentView = ViewHerdrHost
		return a, nil

	case "e":
		item := a.listView.Selected()
		if item != nil && item.Type == "workspace" && item.Endpoint == nil {
			a.createView.EditWorkspace(item.Workspace)
			a.currentView = ViewCreate
		}
		if item != nil && item.Type == "herdr_endpoint" && item.Endpoint != nil && item.Endpoint.ID != herdrhub.LocalEndpointID && item.Endpoint.NodeID == "" {
			a.herdrHostView = NewHerdrHostView(*item.Endpoint)
			a.applyMobileMode(a.herdrHostView)
			a.herdrHostEditingID = item.Endpoint.ID
			a.currentView = ViewHerdrHost
		}
		return a, nil

	case "d":
		item := a.listView.Selected()
		if item != nil && item.Type == "workspace" && item.Endpoint == nil {
			if err := a.store.Delete(item.Workspace.Name); err == nil {
				a.listView.Refresh()
			}
		}
		if item != nil && item.Type == "herdr_endpoint" && item.Endpoint != nil && item.Endpoint.ID != herdrhub.LocalEndpointID && item.Endpoint.NodeID == "" {
			a.herdrHostDeleteView = NewHerdrHostDeleteView(*item.Endpoint)
			a.applyMobileMode(a.herdrHostDeleteView)
			a.currentView = ViewHerdrHostDelete
		}
		return a, nil

	case "x":
		item := a.listView.Selected()
		if item == nil || item.Type != "herdr_session" || item.Herdr == nil {
			return a, nil
		}
		if item.Endpoint != nil {
			return a, nil
		}
		if item.Herdr.Running {
			if err := a.herdrSessionStopper(item.Name); err != nil {
				a.err = err
				return a, nil
			}
			a.listView.Refresh()
			return a, nil
		}
		if item.Herdr.Default || item.Name == "default" {
			return a, nil
		}
		a.herdrSessionDeleteView = NewHerdrSessionDeleteView(item.Name)
		a.applyMobileMode(a.herdrSessionDeleteView)
		a.currentView = ViewHerdrSessionDelete
		return a, nil

	case "*", "s":
		// Toggle quick access
		item := a.listView.Selected()
		if item != nil && item.Type == "workspace" && item.Endpoint == nil {
			a.store.ToggleQuickAccess(item.Workspace.Name)
			a.listView.Refresh()
		}
		return a, nil

	case "f":
		// Create folder
		a.folderInput = NewFolderInput(a.listView.CurrentFolder())
		a.applyMobileMode(a.folderInput)
		a.currentView = ViewFolderInput
		return a, nil

	case "z":
		// Open Zellij session browser
		// canAttach is false when inside Zellij (nested attach doesn't work)
		a.sessionView = NewSessionView(!a.zellij.IsInsideSession())
		a.applyMobileMode(a.sessionView)
		a.currentView = ViewSessions
		return a, nil

	default:
		return a, a.listView.Update(msg)
	}
}

func (a *App) updateHerdrHost(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.herdrHostView == nil {
		a.currentView = ViewList
		return a, nil
	}
	switch msg.String() {
	case "esc":
		a.currentView = ViewList
		return a, nil
	case "enter":
		endpoint := a.herdrHostView.Endpoint()
		if err := herdrhub.ValidateEndpoint(endpoint); err != nil {
			a.herdrHostView.err = err
			return a, nil
		}
		out := make([]herdrhub.Endpoint, 0, len(a.hubEndpoints)+1)
		replaced := false
		for _, old := range a.hubEndpoints {
			if old.ID == a.herdrHostEditingID {
				out = append(out, endpoint)
				replaced = true
			} else {
				out = append(out, old)
			}
		}
		if !replaced {
			out = append(out, endpoint)
		}
		if a.herdrHubEndpointSaver != nil {
			if err := a.herdrHubEndpointSaver(out); err != nil {
				a.herdrHostView.err = err
				return a, nil
			}
		}
		a.hubEndpoints = out
		a.listView.SetHerdrHubSnapshots(out, a.hubSnapshots)
		a.currentView = ViewList
		return a, a.scheduleHubRefresh([]herdrhub.Endpoint{endpoint})
	default:
		return a, a.herdrHostView.Update(msg)
	}
}

func (a *App) updateHerdrHostDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		a.herdrHostDeleteView = nil
		a.currentView = ViewList
		return a, nil
	case "enter":
		if a.herdrHostDeleteView == nil {
			a.err = fmt.Errorf("no Herdr host selected for deletion")
			return a, nil
		}
		if !a.herdrHostDeleteView.Confirmed() {
			a.herdrHostDeleteView = nil
			a.currentView = ViewList
			return a, nil
		}
		id := a.herdrHostDeleteView.endpoint.ID
		out := make([]herdrhub.Endpoint, 0, len(a.hubEndpoints))
		for _, endpoint := range a.hubEndpoints {
			if endpoint.ID != id {
				out = append(out, endpoint)
			}
		}
		if a.herdrHubEndpointSaver != nil {
			if err := a.herdrHubEndpointSaver(out); err != nil {
				a.err = err
				return a, nil
			}
		}
		a.herdrHostDeleteView = nil
		a.hubEndpoints = out
		a.listView.SetHerdrHubSnapshots(out, a.hubSnapshots)
		a.currentView = ViewList
		return a, nil
	default:
		return a, a.herdrHostDeleteView.Update(msg)
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
		a.applyMobileMode(a.templateView)
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
			a.applyMobileMode(a.templateView)
			a.previousView = ViewActions
			a.currentView = ViewTemplates
			return a, nil
		}

		// If worktree action, show worktree picker
		if action == ActionWorktree {
			a.worktreeView = NewWorktreeView(ws.Path)
			a.applyMobileMode(a.worktreeView)
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
		if a.worktreeView.Mode() == WorktreeModeList && !a.worktreeView.IsFiltering() {
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
		a.applyMobileMode(a.worktreeActionView)
		a.currentView = ViewWorktreeActions
		return a, nil
	}

	return a, cmd
}

func (a *App) beginHerdrOpen(result *Result, backView View) {
	a.pendingHerdrResult = result
	a.herdrOpenBackView = backView
	a.herdrOpenModeView = NewHerdrOpenModeView()
	a.applyMobileMode(a.herdrOpenModeView)
	a.currentView = ViewHerdrOpenMode
}

func (a *App) openRemoteHerdrSessionPicker(endpoint herdrhub.Endpoint, sessions []herdrhub.Session, err error) {
	choices := make([]shell.HerdrSession, 0, len(sessions))
	for _, session := range sessions {
		choices = append(choices, shell.HerdrSession{Name: session.SessionName, Running: session.Running})
	}
	a.pendingHerdrResult = &Result{
		Action:          ActionAttachHerdrSession,
		HerdrEndpointID: endpoint.ID,
		HerdrTarget:     endpoint.Target,
	}
	a.herdrOpenBackView = ViewList
	a.herdrSessionPickerView = NewHerdrSessionPickerView(choices, "", err)
	a.applyMobileMode(a.herdrSessionPickerView)
	a.currentView = ViewHerdrSessionPicker
}

func (a *App) updateHerdrOpenMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		if a.herdrOpenBackView == ViewWorktree && a.actionsView != nil {
			a.worktreeView = NewWorktreeView(a.actionsView.Workspace().Path)
			a.applyMobileMode(a.worktreeView)
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
			a.applyMobileMode(a.herdrSessionPickerView)
			a.currentView = ViewHerdrSessionPicker
			return a, nil
		}
		a.herdrSessionNameView = NewHerdrSessionNameView(defaultHerdrSessionName(a.pendingHerdrResult))
		a.applyMobileMode(a.herdrSessionNameView)
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
		if a.pendingHerdrResult != nil && a.pendingHerdrResult.Action == ActionAttachHerdrSession && a.pendingHerdrResult.HerdrEndpointID != "" {
			a.pendingHerdrResult = nil
			a.herdrSessionPickerView = nil
			a.currentView = ViewList
			return a, nil
		}
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
		if a.pendingHerdrResult.Action == ActionAttachHerdrSession && a.pendingHerdrResult.HerdrEndpointID != "" {
			a.pendingHerdrResult.SessionName = session
			a.result = a.pendingHerdrResult
			return a, tea.Quit
		}
		a.pendingHerdrResult.HerdrMode = HerdrOpenExisting
		a.pendingHerdrResult.HerdrSessionName = session
		a.result = a.pendingHerdrResult
		return a, tea.Quit
	default:
		return a, a.herdrSessionPickerView.Update(msg)
	}
}

func (a *App) updateHerdrSessionDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		a.herdrSessionDeleteView = nil
		a.currentView = ViewList
		return a, nil
	case "enter":
		if a.herdrSessionDeleteView == nil {
			a.err = fmt.Errorf("no Herdr session selected for deletion")
			return a, nil
		}
		if !a.herdrSessionDeleteView.Confirmed() {
			a.herdrSessionDeleteView = nil
			a.currentView = ViewList
			return a, nil
		}
		if err := a.herdrSessionDeleter(a.herdrSessionDeleteView.sessionName); err != nil {
			a.err = err
			return a, nil
		}
		a.herdrSessionDeleteView = nil
		a.listView.Refresh()
		a.currentView = ViewList
		return a, nil
	default:
		return a, a.herdrSessionDeleteView.Update(msg)
	}
}

func (a *App) updateWorktreeActions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		// Reset worktree selection and go back to worktree list
		a.worktreeView = NewWorktreeView(a.actionsView.Workspace().Path)
		a.applyMobileMode(a.worktreeView)
		a.currentView = ViewWorktree
		return a, nil

	case "enter":
		action := a.worktreeActionView.Selected()
		ws := a.worktreeActionView.Workspace()
		wt := a.worktreeActionView.Worktree()

		switch action {
		case WorktreeActionWithTemplate:
			a.templateView = NewTemplateView()
			a.applyMobileMode(a.templateView)
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
	case ViewHerdrSessionDelete:
		return a.herdrSessionDeleteView.View()
	case ViewHerdrHost:
		return a.herdrHostView.View()
	case ViewHerdrHostDelete:
		return a.herdrHostDeleteView.View()
	default:
		return ""
	}
}
