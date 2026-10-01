package tui

import (
	"fmt"
	"strings"

	"github.com/OleksandrBesan/tatami/internal/git"
	appsearch "github.com/OleksandrBesan/tatami/internal/search"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

// WorktreeAction represents an action for opening a worktree
type WorktreeAction int

const (
	WorktreeActionPlain WorktreeAction = iota
	WorktreeActionWithLayout
	WorktreeActionWithTemplate
)

// WorktreeActionView displays actions for opening a worktree
type WorktreeActionView struct {
	worktree   *git.Worktree
	workspace  *workspace.Workspace
	actions    []WorktreeAction
	cursor     int
	search     searchController
	mobileMode bool
}

// SetMobileMode enables numbered, compact menu rendering.
func (v *WorktreeActionView) SetMobileMode(enabled bool) {
	v.mobileMode = enabled
}

// NewWorktreeActionView creates a new worktree action view
func NewWorktreeActionView(wt *git.Worktree, ws *workspace.Workspace) *WorktreeActionView {
	var actions []WorktreeAction

	// Saved layout first (if available)
	if len(ws.Layout.Panes) > 0 {
		actions = append(actions, WorktreeActionWithLayout)
	}

	// Then template option
	actions = append(actions, WorktreeActionWithTemplate)

	// Plain last
	actions = append(actions, WorktreeActionPlain)

	view := &WorktreeActionView{
		worktree:  wt,
		workspace: ws,
		actions:   actions,
		cursor:    0,
		search:    newSearchController("worktree actions"),
	}
	view.rebuildSearch()
	return view
}

// Selected returns the currently selected action
func (v *WorktreeActionView) Selected() WorktreeAction {
	id, ok := v.search.ActiveDocumentID()
	if ok {
		for _, action := range v.actions {
			if worktreeActionSearchID(action) == id {
				return action
			}
		}
	}
	return WorktreeActionPlain
}

// Worktree returns the worktree
func (v *WorktreeActionView) Worktree() *git.Worktree {
	return v.worktree
}

// Workspace returns the workspace
func (v *WorktreeActionView) Workspace() *workspace.Workspace {
	return v.workspace
}

// Update handles input
func (v *WorktreeActionView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		event, cmd := v.handleKey(msg)
		if event.Consumed {
			return cmd
		}
		if v.mobileMode {
			if event := v.search.SelectVisibleChoice(msg.String(), 7, 1); event.Consumed {
				v.cursor = v.search.ActiveIndex()
				return nil
			}
		}
		switch msg.String() {
		case "j":
			v.search.Move(1)
		case "k":
			v.search.Move(-1)
		}
		v.cursor = v.search.ActiveIndex()
	}
	return nil
}

func (v *WorktreeActionView) handleKey(msg tea.KeyMsg) (searchEvent, tea.Cmd) {
	event, cmd := v.search.Update(msg)
	v.cursor = v.search.ActiveIndex()
	return event, cmd
}

func worktreeActionSearchID(action WorktreeAction) appsearch.ID {
	return appsearch.ID(fmt.Sprintf("worktree-action:%d", action))
}

func (action WorktreeAction) Label() string {
	return map[WorktreeAction]string{
		WorktreeActionPlain:        "plain (no layout)",
		WorktreeActionWithLayout:   "with saved layout",
		WorktreeActionWithTemplate: "with template...",
	}[action]
}

func (v *WorktreeActionView) rebuildSearch() {
	documents := make([]appsearch.Document, 0, len(v.actions))
	rows := make([]searchRow, 0, len(v.actions))
	for index, action := range v.actions {
		id := worktreeActionSearchID(action)
		documents = append(documents, appsearch.Document{ID: id, Kind: "worktree-action", Primary: action.Label(), Secondary: v.worktree.Branch, Ordinal: index})
		rows = append(rows, searchRow{RowID: id, DocumentID: id})
	}
	_ = v.search.ReplaceDocuments(1, documents, rows)
}

// View renders the view
func (v *WorktreeActionView) View() string {
	var b strings.Builder

	branch := v.worktree.Branch
	if branch == "" {
		branch = "worktree"
	}
	b.WriteString(titleStyle.Render("Open: " + branch))
	b.WriteString("\n")
	b.WriteString(v.search.QueryView())
	b.WriteString("\n\n")

	start, rows := v.search.VisibleRows(7, 1)
	for visibleIndex, row := range rows {
		i := start + visibleIndex
		action, ok := v.actionByID(row.DocumentID)
		if !ok {
			continue
		}
		cursor := choicePrefix(v.mobileMode, visibleIndex, i == v.cursor)
		style := normalStyle
		if i == v.cursor {
			style = selectedStyle
		}

		label := action.Label()
		b.WriteString(cursor)
		b.WriteString(style.Render(label))
		b.WriteString("\n")
	}
	if len(v.search.Rows()) == 0 {
		b.WriteString(mutedStyle.Render("No matching actions."))
		b.WriteString("\n")
	}

	help := "\n[type]search  [↓]browse  [enter]select  [esc]clear/back"
	if v.mobileMode {
		help = "\n[type]search  [↓]browse  [1-9]select in browse  [enter]open"
	}
	b.WriteString(helpStyle.Render(help))

	return renderPanel(b.String(), v.mobileMode)
}

func (v *WorktreeActionView) actionByID(id appsearch.ID) (WorktreeAction, bool) {
	for _, action := range v.actions {
		if worktreeActionSearchID(action) == id {
			return action, true
		}
	}
	return 0, false
}
