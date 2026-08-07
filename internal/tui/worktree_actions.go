package tui

import (
	"strings"

	"github.com/OleksandrBesan/tatami/internal/git"
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

	return &WorktreeActionView{
		worktree:  wt,
		workspace: ws,
		actions:   actions,
		cursor:    0,
	}
}

// Selected returns the currently selected action
func (v *WorktreeActionView) Selected() WorktreeAction {
	return v.actions[v.cursor]
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
		if v.mobileMode {
			if index, ok := numberKeyIndex(msg.String(), len(v.actions)); ok {
				v.cursor = index
				return nil
			}
		}
		switch msg.String() {
		case "j", "down":
			if v.cursor < len(v.actions)-1 {
				v.cursor++
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
		}
	}
	return nil
}

// View renders the view
func (v *WorktreeActionView) View() string {
	var b strings.Builder

	branch := v.worktree.Branch
	if branch == "" {
		branch = "worktree"
	}
	b.WriteString(titleStyle.Render("Open: " + branch))
	b.WriteString("\n\n")

	actionLabels := map[WorktreeAction]string{
		WorktreeActionPlain:        "plain (no layout)",
		WorktreeActionWithLayout:   "with saved layout",
		WorktreeActionWithTemplate: "with template...",
	}

	for i, action := range v.actions {
		cursor := choicePrefix(v.mobileMode, i, i == v.cursor)
		style := normalStyle
		if i == v.cursor {
			style = selectedStyle
		}

		label := actionLabels[action]
		b.WriteString(cursor)
		b.WriteString(style.Render(label))
		b.WriteString("\n")
	}

	help := "\n[enter]select  [esc]back"
	if v.mobileMode {
		help = "\n[↑↓/1-9]select  [enter]open  [b]back"
	}
	b.WriteString(helpStyle.Render(help))

	return renderPanel(b.String(), v.mobileMode)
}
