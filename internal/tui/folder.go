package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// FolderInput handles folder creation
type FolderInput struct {
	input      textinput.Model
	parentPath string
	mobileMode bool
}

// SetMobileMode enables compact input rendering.
func (f *FolderInput) SetMobileMode(enabled bool) {
	f.mobileMode = enabled
}

// NewFolderInput creates a new folder input
func NewFolderInput(parentPath string) *FolderInput {
	ti := textinput.New()
	ti.Placeholder = "folder-name"
	ti.CharLimit = 50
	ti.Width = 30
	ti.Focus()

	return &FolderInput{
		input:      ti,
		parentPath: parentPath,
	}
}

// Value returns the full folder path
func (f *FolderInput) Value() string {
	name := strings.Trim(strings.TrimSpace(f.input.Value()), "/")
	if name == "" {
		return ""
	}
	if f.parentPath == "" {
		return name
	}
	return f.parentPath + "/" + name
}

// Update handles input
func (f *FolderInput) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	return cmd
}

// View renders the folder input
func (f *FolderInput) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Create Folder"))
	b.WriteString("\n\n")

	if f.parentPath != "" {
		b.WriteString(mutedStyle.Render("In: " + f.parentPath + "/"))
		b.WriteString("\n\n")
	}

	b.WriteString(labelStyle.Render("Folder Name"))
	b.WriteString("\n")
	b.WriteString(f.input.View())
	b.WriteString("\n\n")

	b.WriteString(helpStyle.Render("[enter]create  [esc]cancel"))

	return renderPanel(b.String(), f.mobileMode)
}
