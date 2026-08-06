package tui

import (
	"strings"

	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

// TemplateView displays the template picker
type TemplateView struct {
	templates  []workspace.Template
	cursor     int
	mobileMode bool
}

// SetMobileMode enables numbered, compact menu rendering.
func (t *TemplateView) SetMobileMode(enabled bool) {
	t.mobileMode = enabled
}

// NewTemplateView creates a new template view
func NewTemplateView() *TemplateView {
	return &TemplateView{
		templates: workspace.GetTemplates(),
		cursor:    0,
	}
}

// Selected returns the currently selected template
func (t *TemplateView) Selected() *workspace.Template {
	if len(t.templates) == 0 {
		return nil
	}
	return &t.templates[t.cursor]
}

// Update handles input for the template view
func (t *TemplateView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if t.mobileMode {
			if index, ok := numberKeyIndex(msg.String(), len(t.templates)); ok {
				t.cursor = index
				return nil
			}
		}
		switch msg.String() {
		case "j", "down":
			if t.cursor < len(t.templates)-1 {
				t.cursor++
			}
		case "k", "up":
			if t.cursor > 0 {
				t.cursor--
			}
		case "g":
			t.cursor = 0
		case "G":
			if len(t.templates) > 0 {
				t.cursor = len(t.templates) - 1
			}
		}
	}
	return nil
}

// View renders the template view
func (t *TemplateView) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Choose Layout Template"))
	b.WriteString("\n\n")

	for i, tmpl := range t.templates {
		cursor := choicePrefix(t.mobileMode, i, i == t.cursor)
		style := normalStyle
		if i == t.cursor {
			style = selectedStyle
		}

		name := style.Render(tmpl.Name)
		desc := ""
		if !t.mobileMode {
			desc = mutedStyle.Render(" - " + tmpl.Description)
		}
		b.WriteString(cursor + name + desc + "\n")
	}

	help := "\n[enter]select  [esc]cancel"
	if t.mobileMode {
		help = "\n[↑↓/1-9]select  [enter]apply  [b]back"
	}
	b.WriteString(helpStyle.Render(help))

	return renderPanel(b.String(), t.mobileMode)
}
