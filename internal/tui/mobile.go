package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const narrowTerminalWidth = 60

type mobileModeSetter interface {
	SetMobileMode(bool)
}

func numberKeyIndex(key string, count int) (int, bool) {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return 0, false
	}
	index := int(key[0] - '1')
	if index >= count {
		return 0, false
	}
	return index, true
}

func choicePrefix(mobile bool, ordinal int, selected bool) string {
	cursor := "  "
	if selected {
		cursor = "> "
	}
	if mobile && ordinal >= 0 && ordinal < 9 {
		return fmt.Sprintf("%s[%d] ", cursor, ordinal+1)
	}
	return cursor
}

func renderPanel(content string, mobile bool) string {
	if mobile {
		return lipgloss.NewStyle().Padding(0, 1).Render(content)
	}
	return boxStyle.Render(content)
}

func renderScreenWithFooter(body, footer string, height int) string {
	body = strings.TrimRight(body, "\n")
	view := body + "\n" + footer
	if height <= 0 || lipgloss.Height(view) >= height {
		return view
	}

	bodyHeight := height - lipgloss.Height(footer)
	body = lipgloss.NewStyle().Height(bodyHeight).Render(body)
	return body + "\n" + footer
}

func numberedChoiceAtRow(view string, row int) (rune, bool) {
	lines := strings.Split(view, "\n")
	if row < 0 || row >= len(lines) {
		return 0, false
	}
	for choice := '1'; choice <= '9'; choice++ {
		if strings.Contains(lines[row], fmt.Sprintf("[%c]", choice)) {
			return choice, true
		}
	}
	return 0, false
}
