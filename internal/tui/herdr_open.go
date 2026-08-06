package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// HerdrOpenMode chooses whether a worktree gets a dedicated session or joins its project session.
type HerdrOpenMode int

const (
	HerdrOpenDedicated HerdrOpenMode = iota
	HerdrOpenShared
)

// HerdrOpenModeView lets the user choose how a Herdr worktree is grouped.
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
		"same project session",
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Open worktree in Herdr"))
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
