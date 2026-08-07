package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// HerdrSessionDeleteView confirms removal of a stopped session and its saved spaces.
type HerdrSessionDeleteView struct {
	sessionName string
	cursor      int
	mobileMode  bool
}

// SetMobileMode enables numbered, compact confirmation rendering.
func (v *HerdrSessionDeleteView) SetMobileMode(enabled bool) {
	v.mobileMode = enabled
}

func NewHerdrSessionDeleteView(sessionName string) *HerdrSessionDeleteView {
	return &HerdrSessionDeleteView{sessionName: sessionName}
}

func (v *HerdrSessionDeleteView) Confirmed() bool {
	return v.cursor == 1
}

func (v *HerdrSessionDeleteView) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "j", "down":
		v.cursor = 1
	case "k", "up":
		v.cursor = 0
	default:
		if v.mobileMode {
			if index, ok := numberKeyIndex(key.String(), 2); ok {
				v.cursor = index
			}
		}
	}
	return nil
}

func (v *HerdrSessionDeleteView) View() string {
	labels := []string{"cancel", "delete saved session"}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Delete Herdr Session"))
	b.WriteString("\n\n")
	b.WriteString(normalStyle.Render(fmt.Sprintf("Delete stopped session %q?", v.sessionName)))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("Its saved spaces will be removed."))
	b.WriteString("\n\n")
	for i, label := range labels {
		cursor := choicePrefix(v.mobileMode, i, i == v.cursor)
		style := normalStyle
		if i == v.cursor {
			style = selectedStyle
		}
		b.WriteString(cursor)
		b.WriteString(style.Render(label))
		b.WriteString("\n")
	}
	help := "\n[enter]select  [esc]back"
	if v.mobileMode {
		help = "\n[↑↓/1-9]select  [enter]confirm  [b]back"
	}
	b.WriteString(helpStyle.Render(help))
	return renderPanel(b.String(), v.mobileMode)
}
