package tui

import (
	"strings"

	"github.com/OleksandrBesan/tatami/internal/git"
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
)

// ActionView displays the action menu
type ActionView struct {
	workspace *workspace.Workspace
	actions   []Action
	cursor    int
	inZellij  bool
	inTmux    bool
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

	return &ActionView{
		workspace: ws,
		actions:   actions,
		cursor:    0,
		inZellij:  inZellij,
		inTmux:    inTmux,
	}
}

// Selected returns the currently selected action
func (a *ActionView) Selected() Action {
	return a.actions[a.cursor]
}

// Workspace returns the workspace
func (a *ActionView) Workspace() *workspace.Workspace {
	return a.workspace
}

// Update handles input for the action view
func (a *ActionView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if a.cursor < len(a.actions)-1 {
				a.cursor++
			}
		case "k", "up":
			if a.cursor > 0 {
				a.cursor--
			}
		case "1":
			a.cursor = 0
		case "2":
			if len(a.actions) > 1 {
				a.cursor = 1
			}
		case "3":
			if len(a.actions) > 2 {
				a.cursor = 2
			}
		case "4":
			if len(a.actions) > 3 {
				a.cursor = 3
			}
		}
	}
	return nil
}

// View renders the action view
func (a *ActionView) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Open: " + a.workspace.Name))
	b.WriteString("\n\n")

	actionLabels := map[Action]string{
		ActionCD:           "cd here",
		ActionNewTab:       "new tab",
		ActionNewPane:      "new pane",
		ActionWithTemplate: "with template",
		ActionWithLayout:   "with saved layout",
		ActionWorktree:     "open worktree...",
	}

	for i, action := range a.actions {
		cursor := "  "
		style := normalStyle
		if i == a.cursor {
			cursor = "> "
			style = selectedStyle
		}

		label := actionLabels[action]
		if action == ActionWithLayout && a.workspace.Layout.Type == workspace.LayoutHerdr {
			label = "open in herdr"
		}
		b.WriteString(cursor)
		b.WriteString(style.Render(label))
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("\n[enter]select  [esc]back"))

	return boxStyle.Render(b.String())
}
