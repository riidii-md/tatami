package tui

import (
	"fmt"

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
