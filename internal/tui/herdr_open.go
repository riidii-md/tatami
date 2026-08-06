package tui

import (
	"strings"

	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// HerdrOpenMode chooses whether a target gets a dedicated session or joins an existing one.
type HerdrOpenMode int

const (
	HerdrOpenDedicated HerdrOpenMode = iota
	HerdrOpenExisting
)

// HerdrOpenModeView lets the user choose where a Herdr target is opened.
type HerdrOpenModeView struct {
	cursor int
}

func NewHerdrOpenModeView() *HerdrOpenModeView {
	return &HerdrOpenModeView{}
}

func (v *HerdrOpenModeView) Selected() HerdrOpenMode {
	return HerdrOpenMode(v.cursor)
}

func (v *HerdrOpenModeView) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "j", "down":
		v.cursor = 1
	case "k", "up":
		v.cursor = 0
	}
	return nil
}

func (v *HerdrOpenModeView) View() string {
	labels := []string{
		"new / separate herdr session",
		"existing herdr session...",
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Open in Herdr"))
	b.WriteString("\n\n")
	for i, label := range labels {
		cursor := "  "
		style := normalStyle
		if i == v.cursor {
			cursor = "> "
			style = selectedStyle
		}
		b.WriteString(cursor)
		b.WriteString(style.Render(label))
		b.WriteString("\n")
	}
	b.WriteString(helpStyle.Render("\n[enter]select  [esc]back"))
	return boxStyle.Render(b.String())
}

// HerdrSessionNameView lets the user accept or replace a generated session name.
type HerdrSessionNameView struct {
	input textinput.Model
}

func NewHerdrSessionNameView(defaultName string) *HerdrSessionNameView {
	input := textinput.New()
	input.Placeholder = "session-name"
	input.CharLimit = 80
	input.Width = 40
	input.SetValue(defaultName)
	input.Focus()
	return &HerdrSessionNameView{input: input}
}

func (v *HerdrSessionNameView) Value() string {
	return strings.TrimSpace(v.input.Value())
}

func (v *HerdrSessionNameView) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(msg)
	return cmd
}

func (v *HerdrSessionNameView) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Name New Herdr Session"))
	b.WriteString("\n\n")
	b.WriteString(labelStyle.Render("Session Name"))
	b.WriteString("\n")
	b.WriteString(v.input.View())
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("\n[enter]open  [esc]back"))
	return boxStyle.Render(b.String())
}

// HerdrSessionPickerView lets the user select any known Herdr session.
type HerdrSessionPickerView struct {
	sessions       []shell.HerdrSession
	cursor         int
	currentSession string
	err            error
}

func NewHerdrSessionPickerView(sessions []shell.HerdrSession, currentSession string, err error) *HerdrSessionPickerView {
	ordered := make([]shell.HerdrSession, 0, len(sessions)+1)
	currentFound := false
	for _, session := range sessions {
		if session.Name == currentSession && currentSession != "" {
			ordered = append(ordered, session)
			currentFound = true
		}
	}
	if currentSession != "" && !currentFound {
		ordered = append(ordered, shell.HerdrSession{Name: currentSession, Running: true})
	}
	for _, session := range sessions {
		if session.Name != currentSession {
			ordered = append(ordered, session)
		}
	}
	return &HerdrSessionPickerView{
		sessions:       ordered,
		currentSession: currentSession,
		err:            err,
	}
}

func (v *HerdrSessionPickerView) Selected() string {
	if len(v.sessions) == 0 {
		return ""
	}
	return v.sessions[v.cursor].Name
}

func (v *HerdrSessionPickerView) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "j", "down":
		if v.cursor < len(v.sessions)-1 {
			v.cursor++
		}
	case "k", "up":
		if v.cursor > 0 {
			v.cursor--
		}
	case "g":
		v.cursor = 0
	case "G":
		if len(v.sessions) > 0 {
			v.cursor = len(v.sessions) - 1
		}
	}
	return nil
}

func (v *HerdrSessionPickerView) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Choose Herdr Session"))
	b.WriteString("\n\n")
	if v.err != nil {
		b.WriteString(errorStyle.Render("Could not list Herdr sessions: " + v.err.Error()))
		b.WriteString("\n")
	} else if len(v.sessions) == 0 {
		b.WriteString(mutedStyle.Render("No existing Herdr sessions"))
		b.WriteString("\n")
	} else {
		for i, session := range v.sessions {
			cursor := "  "
			style := normalStyle
			if i == v.cursor {
				cursor = "> "
				style = selectedStyle
			}
			label := session.Name
			if session.Name == v.currentSession {
				label += " (current)"
			} else if session.Running {
				label += " (running)"
			} else {
				label += " (stopped)"
			}
			b.WriteString(cursor)
			b.WriteString(style.Render(label))
			b.WriteString("\n")
		}
	}
	b.WriteString(helpStyle.Render("\n[enter]select  [esc]back"))
	return boxStyle.Render(b.String())
}
