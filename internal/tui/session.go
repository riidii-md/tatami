package tui

import (
	"fmt"
	"strings"

	appsearch "github.com/OleksandrBesan/tatami/internal/search"
	"github.com/OleksandrBesan/tatami/internal/shell"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SessionMode represents the current mode of the session view
type SessionMode int

const (
	SessionModeList SessionMode = iota
	SessionModeConfirmDelete
)

// SessionView displays Zellij sessions
type SessionView struct {
	sessions     []shell.ZellijSession
	cursor       int
	search       searchController
	mode         SessionMode
	showExited   bool // Show exited sessions
	canAttach    bool // Can attach (false when inside Zellij)
	deleteTarget string
	width        int
	height       int
	err          error
	mobileMode   bool
}

// SetMobileMode enables numbered, compact session rendering.
func (s *SessionView) SetMobileMode(enabled bool) {
	s.mobileMode = enabled
}

// NewSessionView creates a new session view
// canAttach should be false when inside a Zellij session (nested attach doesn't work)
func NewSessionView(canAttach bool) *SessionView {
	sv := &SessionView{
		cursor:     0,
		search:     newSearchController("Zellij sessions"),
		mode:       SessionModeList,
		showExited: false,
		canAttach:  canAttach,
	}
	sv.refresh()
	return sv
}

// CanAttach returns whether attach is allowed
func (s *SessionView) CanAttach() bool {
	return s.canAttach
}

// refresh reloads the session list
func (s *SessionView) refresh() {
	sessions, err := shell.ListSessions()
	if err != nil {
		s.err = err
		return
	}
	s.sessions = sessions
	s.err = nil

	// Adjust cursor if needed
	if s.cursor >= len(s.visibleSessions()) {
		s.cursor = max(0, len(s.visibleSessions())-1)
	}
	s.rebuildSearch()
}

// visibleSessions returns sessions filtered by showExited flag
func (s *SessionView) visibleSessions() []shell.ZellijSession {
	if s.showExited {
		return s.sessions
	}
	var visible []shell.ZellijSession
	for _, session := range s.sessions {
		if !session.IsExited {
			visible = append(visible, session)
		}
	}
	return visible
}

// SetSize sets the view dimensions
func (s *SessionView) SetSize(width, height int) {
	s.width = width
	s.height = height
}

// Selected returns the currently selected session name, or empty if none
func (s *SessionView) Selected() string {
	id, ok := s.search.ActiveDocumentID()
	if ok {
		if session, ok := s.sessionByID(id); ok {
			return session.Name
		}
	}
	return ""
}

// IsCurrentSession returns true if the selected session is the current one
func (s *SessionView) IsCurrentSession() bool {
	id, ok := s.search.ActiveDocumentID()
	if ok {
		if session, ok := s.sessionByID(id); ok {
			return session.IsCurrent
		}
	}
	return false
}

// Mode returns the current mode
func (s *SessionView) Mode() SessionMode {
	return s.mode
}

// Update handles input for the session view
func (s *SessionView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if s.mode == SessionModeConfirmDelete {
			return s.updateConfirmDelete(msg)
		}
		return s.updateList(msg)
	}
	return nil
}

func (s *SessionView) updateList(msg tea.KeyMsg) tea.Cmd {
	event, cmd := s.handleKey(msg)
	if event.Consumed {
		return cmd
	}
	if s.mobileMode {
		if event := s.search.SelectVisibleChoice(msg.String(), 10, 1); event.Consumed {
			s.cursor = s.search.ActiveIndex()
			return nil
		}
	}

	switch msg.String() {
	case "j":
		s.search.Move(1)
	case "k":
		s.search.Move(-1)
	case "g":
		s.search.SelectFirst()
	case "G":
		s.search.SelectLast()
	case "e":
		// Toggle show exited sessions
		s.showExited = !s.showExited
		s.rebuildSearch()
	case "r":
		// Refresh
		s.refresh()
	case "d":
		// Delete - show confirmation
		if selected := s.Selected(); selected != "" {
			s.deleteTarget = selected
			s.mode = SessionModeConfirmDelete
		}
	}
	s.cursor = s.search.ActiveIndex()
	return nil
}

func (s *SessionView) handleKey(msg tea.KeyMsg) (searchEvent, tea.Cmd) {
	if s.mode != SessionModeList {
		return searchEvent{}, nil
	}
	event, cmd := s.search.Update(msg)
	s.cursor = s.search.ActiveIndex()
	return event, cmd
}

func zellijSessionSearchID(name string) appsearch.ID { return appsearch.ID("zellij-session:" + name) }

func (s *SessionView) rebuildSearch() {
	visible := s.visibleSessions()
	documents := make([]appsearch.Document, 0, len(visible))
	rows := make([]searchRow, 0, len(visible))
	for index, session := range visible {
		id := zellijSessionSearchID(session.Name)
		state := "running"
		boost := 0
		if session.IsCurrent {
			state, boost = "current running", 3
		} else if session.IsExited {
			state = "exited"
		}
		documents = append(documents, appsearch.Document{ID: id, Kind: "zellij-session", Primary: session.Name, Secondary: session.CreatedAt, Fields: []appsearch.Field{{Name: "status", Value: state, Class: appsearch.MetadataField}}, Ordinal: index, Boost: boost})
		rows = append(rows, searchRow{RowID: id, DocumentID: id})
	}
	_ = s.search.ReplaceDocuments(s.search.generation+1, documents, rows)
	s.cursor = s.search.ActiveIndex()
}

func (s *SessionView) sessionByID(id appsearch.ID) (shell.ZellijSession, bool) {
	for _, session := range s.visibleSessions() {
		if zellijSessionSearchID(session.Name) == id {
			return session, true
		}
	}
	return shell.ZellijSession{}, false
}

func (s *SessionView) updateConfirmDelete(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "y", "Y":
		// Confirm delete
		if s.deleteTarget != "" {
			if err := shell.DeleteSession(s.deleteTarget); err != nil {
				s.err = err
			}
			s.deleteTarget = ""
			s.mode = SessionModeList
			s.refresh()
		}
	case "n", "N", "esc":
		// Cancel delete
		s.deleteTarget = ""
		s.mode = SessionModeList
	}
	return nil
}

// View renders the session view
func (s *SessionView) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Zellij Sessions"))
	b.WriteString("\n")
	if s.mode == SessionModeList {
		b.WriteString(s.search.QueryView())
		b.WriteString("\n\n")
	} else {
		b.WriteString("\n")
	}

	if s.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", s.err)))
		b.WriteString("\n\n")
	}

	if s.mode == SessionModeConfirmDelete {
		b.WriteString(fmt.Sprintf("Delete session '%s'? ", s.deleteTarget))
		help := "[y]es  [n]o"
		if s.mobileMode {
			help += "  [b]back"
		}
		b.WriteString(helpStyle.Render(help))
		return renderPanel(b.String(), s.mobileMode)
	}

	visible := s.visibleSessions()
	if len(s.search.Rows()) == 0 {
		if len(s.sessions) == 0 {
			b.WriteString(mutedStyle.Render("No Zellij sessions found."))
		} else {
			b.WriteString(mutedStyle.Render("No active sessions. Press 'e' to show exited."))
		}
		b.WriteString("\n\n")
		help := "[e]xited  [r]efresh  [esc]back"
		if s.mobileMode {
			help = "[e]xited  [r]efresh  [b]back"
		}
		b.WriteString(helpStyle.Render(help))
		if s.mobileMode {
			return renderPanel(b.String(), true)
		}
		return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
	}

	// Show sessions
	start, rows := s.search.VisibleRows(10, 1)
	for visibleIndex, row := range rows {
		i := start + visibleIndex
		session, ok := s.sessionByID(row.DocumentID)
		if !ok {
			continue
		}

		cursor := choicePrefix(s.mobileMode, visibleIndex, i == s.cursor)
		style := normalStyle
		if i == s.cursor {
			style = selectedStyle
		}

		// Status indicator
		var status string
		var statusStyle lipgloss.Style
		if session.IsCurrent {
			status = "◆"
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("33")) // blue
		} else if session.IsExited {
			status = "○"
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241")) // gray
		} else {
			status = "●"
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // green
		}

		name := style.Render(session.Name)
		created := mutedStyle.Render(session.CreatedAt)
		statusStr := statusStyle.Render(status)

		line := fmt.Sprintf("%s%s %s  %s", cursor, statusStr, name, created)
		if session.IsCurrent {
			line += mutedStyle.Render(" (current)")
		} else if session.IsExited {
			line += mutedStyle.Render(" (exited)")
		}
		b.WriteString(line + "\n")
	}
	if s.search.Truncated() {
		b.WriteString(mutedStyle.Render("Results truncated."))
		b.WriteString("\n")
	}

	// Show exited count if hidden
	if !s.showExited {
		exitedCount := len(s.sessions) - len(visible)
		if exitedCount > 0 {
			b.WriteString(mutedStyle.Render(fmt.Sprintf("\n  +%d exited (press 'e' to show)", exitedCount)))
			b.WriteString("\n")
		}
	}

	// Help text
	b.WriteString("\n")
	var help string
	if s.canAttach {
		help = "[type]search  [↓]browse  [enter]attach  [esc]clear/back"
		if s.showExited {
			help = "[type]search  [↓]browse  [enter]attach  [esc]clear/back"
		}
	} else {
		help = "[type]search  [↓]browse  [esc]clear/back"
		if s.showExited {
			help = "[d]elete  [e]hide  [r]efresh  [esc]back"
		}
		b.WriteString(mutedStyle.Render("Tip: Use Ctrl+o w for Zellij session switcher\n"))
	}
	if s.mobileMode {
		if s.canAttach {
			help = "[type]search [↓]browse [enter]attach\n[d/e/r] commands in browse"
		} else {
			help = "[type]search [↓]browse\n[d/e/r] commands in browse"
		}
	}
	b.WriteString(helpStyle.Render(help))

	if s.mobileMode {
		return renderPanel(b.String(), true)
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
}
