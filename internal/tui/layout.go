package tui

import (
	"fmt"
	"strings"

	appsearch "github.com/OleksandrBesan/tatami/internal/search"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type layoutField int

const (
	layoutFieldCommand layoutField = iota
	layoutFieldDirection
)

// LayoutEditor handles editing workspace layouts
type LayoutEditor struct {
	panes         []workspace.Pane
	cursor        int
	editing       bool
	activeField   layoutField
	commandInput  textinput.Model
	directionOpts []string
	directionIdx  int
	search        searchController
	generation    uint64
	mobileMode    bool
}

// NewLayoutEditor creates a new layout editor
func NewLayoutEditor() *LayoutEditor {
	cmdInput := textinput.New()
	cmdInput.Placeholder = "nvim"
	cmdInput.CharLimit = 100
	cmdInput.Width = 30

	return &LayoutEditor{
		panes:         nil,
		cursor:        0,
		editing:       false,
		activeField:   layoutFieldCommand,
		commandInput:  cmdInput,
		directionOpts: []string{"down", "right"},
		directionIdx:  0,
		search:        newSearchController("pane number or direction"),
	}
}

// SetPanes sets the panes to edit
func (l *LayoutEditor) SetPanes(panes []workspace.Pane) {
	l.panes = make([]workspace.Pane, len(panes))
	copy(l.panes, panes)
	l.cursor = 0
	l.editing = false
	l.search.Clear()
	l.rebuildSearch()
}

// GetPanes returns the current panes
func (l *LayoutEditor) GetPanes() []workspace.Pane {
	return l.panes
}

// IsEditing returns whether currently editing a pane
func (l *LayoutEditor) IsEditing() bool {
	return l.editing
}

func (l *LayoutEditor) SetMobileMode(enabled bool) {
	l.mobileMode = enabled
}

func (l *LayoutEditor) startEdit() {
	if len(l.panes) == 0 {
		return
	}
	l.editing = true
	l.activeField = layoutFieldCommand
	l.commandInput.SetValue(l.panes[l.cursor].Command)
	l.commandInput.Focus()

	// Set direction index
	for i, dir := range l.directionOpts {
		if dir == l.panes[l.cursor].Direction {
			l.directionIdx = i
			break
		}
	}
}

func (l *LayoutEditor) stopEdit(save bool) {
	if save && len(l.panes) > l.cursor {
		l.panes[l.cursor].Command = l.commandInput.Value()
		l.panes[l.cursor].Direction = l.directionOpts[l.directionIdx]
	}
	l.editing = false
	l.commandInput.Blur()
	l.rebuildSearch()
}

func (l *LayoutEditor) addPane() {
	l.panes = append(l.panes, workspace.Pane{
		Command:   "",
		Direction: "down",
	})
	l.cursor = len(l.panes) - 1
	l.rebuildSearch()
	l.search.SelectLast()
	l.startEdit()
}

func (l *LayoutEditor) deletePane() {
	if len(l.panes) == 0 {
		return
	}
	l.panes = append(l.panes[:l.cursor], l.panes[l.cursor+1:]...)
	if l.cursor >= len(l.panes) && l.cursor > 0 {
		l.cursor--
	}
	l.rebuildSearch()
}

// Update handles input for the layout editor
func (l *LayoutEditor) Update(msg tea.Msg) tea.Cmd {
	if l.editing {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				l.stopEdit(true)
				return nil
			case "esc":
				l.stopEdit(false)
				return nil
			case "tab":
				l.activeField = (l.activeField + 1) % 2
				if l.activeField == layoutFieldCommand {
					l.commandInput.Focus()
				} else {
					l.commandInput.Blur()
				}
				return nil
			case "left", "right":
				if l.activeField == layoutFieldDirection {
					if msg.String() == "right" {
						l.directionIdx = (l.directionIdx + 1) % len(l.directionOpts)
					} else {
						l.directionIdx = (l.directionIdx + len(l.directionOpts) - 1) % len(l.directionOpts)
					}
					return nil
				}
			}
		}

		if l.activeField == layoutFieldCommand {
			var cmd tea.Cmd
			l.commandInput, cmd = l.commandInput.Update(msg)
			return cmd
		}
		return nil
	}

	// Not editing - handle list navigation
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+n" {
			l.addPane()
			return nil
		}
		event, cmd := l.handleKey(msg)
		if event.Consumed {
			if event.Activate {
				l.startEdit()
			}
			return cmd
		}
		switch msg.String() {
		case "j":
			l.search.Move(1)
		case "k":
			l.search.Move(-1)
		case "e":
			l.startEdit()
		case "a":
			l.addPane()
		case "d", "x":
			l.deletePane()
		default:
			if l.mobileMode {
				l.search.SelectVisibleChoice(msg.String(), 10, 1)
			}
		}
		l.syncCursorFromSearch()
	}
	return nil
}

func (l *LayoutEditor) handleKey(msg tea.KeyMsg) (searchEvent, tea.Cmd) {
	if l.editing {
		return searchEvent{}, nil
	}
	event, cmd := l.search.Update(msg)
	l.syncCursorFromSearch()
	return event, cmd
}

func (l *LayoutEditor) rebuildSearch() {
	l.generation++
	documents := make([]appsearch.Document, 0, len(l.panes))
	rows := make([]searchRow, 0, len(l.panes))
	for index, pane := range l.panes {
		id := appsearch.ID(fmt.Sprintf("layout:%d:%d", l.generation, index))
		documents = append(documents, appsearch.Document{ID: id, Kind: "layout-pane", Primary: fmt.Sprintf("Pane %d", index+1), Secondary: pane.Direction, Ordinal: index})
		rows = append(rows, searchRow{RowID: id, DocumentID: id})
	}
	_ = l.search.ReplaceDocuments(l.generation, documents, rows)
	l.search.setActiveIndex(l.cursor)
	l.syncCursorFromSearch()
}

func (l *LayoutEditor) syncCursorFromSearch() {
	id, ok := l.search.ActiveDocumentID()
	if !ok {
		l.cursor = 0
		return
	}
	for index := range l.panes {
		if id == appsearch.ID(fmt.Sprintf("layout:%d:%d", l.generation, index)) {
			l.cursor = index
			return
		}
	}
}

func (l *LayoutEditor) paneIndexByID(id appsearch.ID) (int, bool) {
	for index := range l.panes {
		if id == appsearch.ID(fmt.Sprintf("layout:%d:%d", l.generation, index)) {
			return index, true
		}
	}
	return 0, false
}

// View renders the layout editor
func (l *LayoutEditor) View() string {
	var b strings.Builder

	b.WriteString(labelStyle.Render("Layout Panes"))
	b.WriteString("\n")
	if !l.editing {
		b.WriteString(l.search.QueryView())
	}
	b.WriteString("\n\n")

	if len(l.panes) == 0 {
		b.WriteString(mutedStyle.Render("  No panes. Press Ctrl+N to add one."))
		b.WriteString("\n")
	} else {
		start, rows := l.search.VisibleRows(10, 1)
		for visibleIndex, row := range rows {
			searchIndex := start + visibleIndex
			i, ok := l.paneIndexByID(row.DocumentID)
			if !ok {
				continue
			}
			pane := l.panes[i]
			cursor := choicePrefix(l.mobileMode, visibleIndex, searchIndex == l.search.ActiveIndex())
			style := normalStyle
			if searchIndex == l.search.ActiveIndex() {
				style = selectedStyle
			}

			cmd := pane.Command
			if cmd == "" {
				cmd = "(empty)"
			}
			line := fmt.Sprintf("%s%s [%s]", cursor, cmd, pane.Direction)
			b.WriteString(style.Render(line))
			b.WriteString("\n")
		}
		if l.search.Truncated() {
			b.WriteString(mutedStyle.Render("Results truncated."))
			b.WriteString("\n")
		}
	}

	// Edit form
	if l.editing && len(l.panes) > 0 {
		b.WriteString("\n")
		b.WriteString(labelStyle.Render("Edit Pane"))
		b.WriteString("\n")

		cmdLabel := "  Command"
		if l.activeField == layoutFieldCommand {
			cmdLabel = "> Command"
		}
		b.WriteString(mutedStyle.Render(cmdLabel))
		b.WriteString("\n  ")
		b.WriteString(l.commandInput.View())
		b.WriteString("\n")

		dirLabel := "  Direction"
		if l.activeField == layoutFieldDirection {
			dirLabel = "> Direction"
		}
		b.WriteString(mutedStyle.Render(dirLabel))
		b.WriteString("\n  ")

		for i, dir := range l.directionOpts {
			style := mutedStyle
			if i == l.directionIdx {
				style = selectedStyle
			}
			b.WriteString(style.Render("[" + dir + "]"))
			b.WriteString("  ")
		}
		b.WriteString("\n")
	}

	// Help
	help := "[type]search  [↓]browse  [enter]edit  [ctrl+n]add  [esc]clear/done"
	if l.editing {
		help = "[tab]next  [enter]save  [esc]cancel"
	}
	b.WriteString(helpStyle.Render("\n" + help))

	return b.String()
}
