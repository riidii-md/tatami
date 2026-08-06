package tui

import (
	"fmt"
	"strings"

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
	visible := s.visibleSessions()
	if len(visible) == 0 || s.cursor >= len(visible) {
		return ""
	}
	return visible[s.cursor].Name
}

// IsCurrentSession returns true if the selected session is the current one
func (s *SessionView) IsCurrentSession() bool {
	visible := s.visibleSessions()
	if len(visible) == 0 || s.cursor >= len(visible) {
		return false
	}
	return visible[s.cursor].IsCurrent
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
	visible := s.visibleSessions()
	if s.mobileMode {
		if index, ok := numberKeyIndex(msg.String(), len(visible)); ok {
			s.cursor = index
			return nil
		}
	}

	switch msg.String() {
	case "j", "down":
		if s.cursor < len(visible)-1 {
			s.cursor++
		}
	case "k", "up":
		if s.cursor > 0 {
			s.cursor--
		}
	case "g":
		s.cursor = 0
	case "G":
		s.cursor = max(0, len(visible)-1)
	case "e":
		// Toggle show exited sessions
		s.showExited = !s.showExited
		visible = s.visibleSessions()
		if s.cursor >= len(visible) {
			s.cursor = max(0, len(visible)-1)
		}
	case "r":
		// Refresh
		s.refresh()
	case "d":
		// Delete - show confirmation
		if len(visible) > 0 && s.cursor < len(visible) {
			s.deleteTarget = visible[s.cursor].Name
			s.mode = SessionModeConfirmDelete
		}
	}
	return nil
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
	b.WriteString("\n\n")

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
	if len(visible) == 0 {
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
	for i, session := range visible {

		cursor := choicePrefix(s.mobileMode, i, i == s.cursor)
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
		help = "[enter]attach  [d]elete  [e]xited  [r]efresh  [esc]back"
		if s.showExited {
			help = "[enter]attach  [d]elete  [e]hide  [r]efresh  [esc]back"
		}
	} else {
		help = "[d]elete  [e]xited  [r]efresh  [esc]back"
		if s.showExited {
			help = "[d]elete  [e]hide  [r]efresh  [esc]back"
		}
		b.WriteString(mutedStyle.Render("Tip: Use Ctrl+o w for Zellij session switcher\n"))
	}
	if s.mobileMode {
		if s.canAttach {
			help = "[↑↓/1-9]select  [enter]attach\n[d]delete [e]exited [r]refresh [b]back"
		} else {
			help = "[↑↓/1-9]select\n[d]delete [e]exited [r]refresh [b]back"
		}
	}
	b.WriteString(helpStyle.Render(help))

	if s.mobileMode {
		return renderPanel(b.String(), true)
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
}
