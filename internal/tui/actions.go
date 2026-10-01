package tui

import (
	"fmt"
	"strings"

	"github.com/OleksandrBesan/tatami/internal/git"
	appsearch "github.com/OleksandrBesan/tatami/internal/search"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

// Action represents a workspace action
type Action int

const (
	ActionCD Action = iota
	ActionNewTab
	ActionNewPane
	ActionWithLayout
	ActionWithTemplate
	ActionWorktree
	ActionAttachSession
	ActionAttachHerdrSession
	ActionAttachHerdrEndpoint
)

// ActionView displays the action menu
type ActionView struct {
	workspace  *workspace.Workspace
	actions    []Action
	cursor     int
	search     searchController
	inZellij   bool
	inTmux     bool
	mobileMode bool
}

// SetMobileMode enables numbered, compact menu rendering.
func (a *ActionView) SetMobileMode(enabled bool) {
	a.mobileMode = enabled
}

// NewActionView creates a new action view
func NewActionView(ws *workspace.Workspace, inZellij, inTmux, inNewTab bool) *ActionView {
	var actions []Action

	// Check if workspace is a git repo (only for local workspaces)
	isGitRepo := !ws.IsRemote() && git.IsGitRepo(ws.Path)
	hasSavedLayout := len(ws.Layout.Panes) > 0 || ws.Layout.MainCmd != ""

	if ws.Layout.Type == workspace.LayoutHerdr {
		// Herdr is the workspace backend, so every open path must stay on Herdr.
		actions = append(actions, ActionWithLayout)
		if isGitRepo {
			actions = append(actions, ActionWorktree)
		}
		actions = append(actions, ActionWithTemplate)
	} else if inZellij {
		// Saved layout first (if available)
		if ws.Layout.Type == workspace.LayoutZellij && hasSavedLayout {
			actions = append(actions, ActionWithLayout)
		}
		// Git worktree option (only for local git repos)
		if isGitRepo {
			actions = append(actions, ActionWorktree)
		}
		actions = append(actions, ActionWithTemplate, ActionNewPane, ActionNewTab, ActionCD)
	} else if inTmux {
		// Saved layout first (if available)
		if ws.Layout.Type == workspace.LayoutTmux && hasSavedLayout {
			actions = append(actions, ActionWithLayout)
		}
		// Git worktree option (only for local git repos)
		if isGitRepo {
			actions = append(actions, ActionWorktree)
		}
		actions = append(actions, ActionWithTemplate, ActionNewPane, ActionNewTab, ActionCD)
	} else if inNewTab {
		// A dedicated Kitty tab can open a local Git worktree or hand the
		// current tab over to a shell in the selected workspace.
		if isGitRepo {
			actions = append(actions, ActionWorktree)
		}
		actions = append(actions, ActionCD)
	} else {
		// Outside multiplexer - only CD
		actions = append(actions, ActionCD)
	}

	view := &ActionView{
		workspace: ws,
		actions:   actions,
		cursor:    0,
		search:    newSearchController("actions"),
		inZellij:  inZellij,
		inTmux:    inTmux,
	}
	view.rebuildSearch()
	return view
}

// Selected returns the currently selected action
func (a *ActionView) Selected() Action {
	id, ok := a.search.ActiveDocumentID()
	if !ok {
		return ActionCD
	}
	for _, action := range a.actions {
		if actionSearchID(action) == id {
			return action
		}
	}
	return ActionCD
}

// Workspace returns the workspace
func (a *ActionView) Workspace() *workspace.Workspace {
	return a.workspace
}

// Update handles input for the action view
func (a *ActionView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		event, cmd := a.handleKey(msg)
		if event.Consumed {
			return cmd
		}
		if a.mobileMode {
			if event := a.search.SelectVisibleChoice(msg.String(), 7, 1); event.Consumed {
				a.cursor = a.search.ActiveIndex()
				return nil
			}
		}
		switch msg.String() {
		case "j":
			a.search.Move(1)
		case "k":
			a.search.Move(-1)
		case "1":
			a.search.SelectIndex(0)
		case "2":
			a.search.SelectIndex(1)
		case "3":
			a.search.SelectIndex(2)
		case "4":
			a.search.SelectIndex(3)
		}
		a.cursor = a.search.ActiveIndex()
	}
	return nil
}

func (a *ActionView) handleKey(msg tea.KeyMsg) (searchEvent, tea.Cmd) {
	event, cmd := a.search.Update(msg)
	a.cursor = a.search.ActiveIndex()
	return event, cmd
}

func actionSearchID(action Action) appsearch.ID {
	return appsearch.ID(fmt.Sprintf("action:%d", action))
}

func (a Action) Label(ws *workspace.Workspace) string {
	labels := map[Action]string{
		ActionCD:           "cd here",
		ActionNewTab:       "new tab",
		ActionNewPane:      "new pane",
		ActionWithTemplate: "with template",
		ActionWithLayout:   "with saved layout",
		ActionWorktree:     "open worktree...",
	}
	if a == ActionWithLayout && ws != nil && ws.Layout.Type == workspace.LayoutHerdr {
		return "open in herdr"
	}
	return labels[a]
}

func (a *ActionView) rebuildSearch() {
	documents := make([]appsearch.Document, 0, len(a.actions))
	rows := make([]searchRow, 0, len(a.actions))
	for index, action := range a.actions {
		id := actionSearchID(action)
		documents = append(documents, appsearch.Document{ID: id, Kind: "action", Primary: action.Label(a.workspace), Secondary: a.workspace.Name, Ordinal: index})
		rows = append(rows, searchRow{RowID: id, DocumentID: id})
	}
	_ = a.search.ReplaceDocuments(1, documents, rows)
}

// View renders the action view
func (a *ActionView) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Open: " + a.workspace.Name))
	b.WriteString("\n")
	b.WriteString(a.search.QueryView())
	b.WriteString("\n\n")

	start, rows := a.search.VisibleRows(7, 1)
	for visibleIndex, row := range rows {
		i := start + visibleIndex
		action, ok := a.actionByID(row.DocumentID)
		if !ok {
			continue
		}
		cursor := choicePrefix(a.mobileMode, visibleIndex, i == a.cursor)
		style := normalStyle
		if i == a.cursor {
			style = selectedStyle
		}

		label := action.Label(a.workspace)
		b.WriteString(cursor)
		b.WriteString(style.Render(label))
		b.WriteString("\n")
	}
	if len(a.search.Rows()) == 0 {
		b.WriteString(mutedStyle.Render("No matching actions."))
		b.WriteString("\n")
	}

	help := "\n[type]search  [↓]browse  [enter]select  [esc]clear/back"
	if a.mobileMode {
		help = "\n[type]search  [↓]browse  [1-9]select in browse  [enter]open"
	}
	b.WriteString(helpStyle.Render(help))

	return renderPanel(b.String(), a.mobileMode)
}

func (a *ActionView) actionByID(id appsearch.ID) (Action, bool) {
	for _, action := range a.actions {
		if actionSearchID(action) == id {
			return action, true
		}
	}
	return 0, false
}
