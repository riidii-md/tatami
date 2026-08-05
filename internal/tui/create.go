package tui

import (
	"fmt"
	"strings"

	"github.com/OleksandrBesan/tatami/internal/workspace"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type createField int

const (
	fieldName createField = iota
	fieldPath
	fieldRemoteHost
	fieldSSHKey
	fieldFolder
	fieldLayoutType
)

// CreateView handles workspace creation and editing
type CreateView struct {
	nameInput       textinput.Model
	pathPicker      *PathPicker
	remoteHostInput textinput.Model
	sshKeyInput     textinput.Model
	folderInput     textinput.Model
	layoutType      workspace.LayoutType
	layoutTypes     []workspace.LayoutType
	layoutPanes     []workspace.Pane
	layoutMainCmd   string
	templateName    string
	quickAccess     bool

	activeField createField
	editing     bool
	editingName string
	errorMsg    string
}

// NewCreateView creates a new create view
func NewCreateView() *CreateView {
	nameInput := textinput.New()
	nameInput.Placeholder = "workspace-name"
	nameInput.CharLimit = 50
	nameInput.Width = 30
	nameInput.Focus()

	remoteHostInput := textinput.New()
	remoteHostInput.Placeholder = "user@host (optional, for remote)"
	remoteHostInput.CharLimit = 100
	remoteHostInput.Width = 30

	sshKeyInput := textinput.New()
	sshKeyInput.Placeholder = "~/.ssh/id_rsa (optional)"
	sshKeyInput.CharLimit = 200
	sshKeyInput.Width = 30

	folderInput := textinput.New()
	folderInput.Placeholder = "folder/path (optional)"
	folderInput.CharLimit = 100
	folderInput.Width = 30

	return &CreateView{
		nameInput:       nameInput,
		pathPicker:      NewPathPicker(),
		remoteHostInput: remoteHostInput,
		sshKeyInput:     sshKeyInput,
		folderInput:     folderInput,
		layoutType:      workspace.LayoutNone,
		layoutTypes:     []workspace.LayoutType{workspace.LayoutNone, workspace.LayoutZellij, workspace.LayoutTmux, workspace.LayoutHerdr},
		activeField:     fieldName,
		editing:         false,
	}
}

// Reset clears the form for new creation
func (c *CreateView) Reset() {
	c.nameInput.SetValue("")
	c.pathPicker.SetValue("")
	c.remoteHostInput.SetValue("")
	c.sshKeyInput.SetValue("")
	c.folderInput.SetValue("")
	c.layoutType = workspace.LayoutNone
	c.layoutPanes = nil
	c.layoutMainCmd = ""
	c.templateName = ""
	c.quickAccess = false
	c.activeField = fieldName
	c.editing = false
	c.editingName = ""
	c.errorMsg = ""
	c.nameInput.Focus()
	c.pathPicker.Blur()
	c.remoteHostInput.Blur()
	c.sshKeyInput.Blur()
	c.folderInput.Blur()
}

// SetFolder sets the folder for new workspace
func (c *CreateView) SetFolder(folder string) {
	c.folderInput.SetValue(folder)
}

// EditWorkspace populates the form for editing
func (c *CreateView) EditWorkspace(ws *workspace.Workspace) {
	c.nameInput.SetValue(ws.Name)
	c.pathPicker.SetValue(ws.Path)
	if ws.Remote != nil {
		c.remoteHostInput.SetValue(ws.Remote.Host)
		c.sshKeyInput.SetValue(ws.Remote.Key)
	} else {
		c.remoteHostInput.SetValue("")
		c.sshKeyInput.SetValue("")
	}
	c.folderInput.SetValue(ws.Folder)
	c.layoutType = ws.Layout.Type
	c.layoutPanes = ws.Layout.Panes
	c.layoutMainCmd = ws.Layout.MainCmd
	c.quickAccess = ws.QuickAccess
	c.templateName = ""
	c.activeField = fieldName
	c.editing = true
	c.editingName = ws.Name
	c.errorMsg = ""
	c.nameInput.Focus()
	c.pathPicker.Blur()
	c.remoteHostInput.Blur()
	c.sshKeyInput.Blur()
	c.folderInput.Blur()
}

// ApplyTemplate applies a template to the current workspace
func (c *CreateView) ApplyTemplate(tmpl *workspace.Template) {
	c.layoutPanes = tmpl.Panes
	c.layoutMainCmd = tmpl.MainCmd
	c.templateName = tmpl.Name
	if c.layoutType == workspace.LayoutNone {
		c.layoutType = workspace.LayoutZellij
	}
}

// IsEditing returns whether we're editing an existing workspace
func (c *CreateView) IsEditing() bool {
	return c.editing
}

// EditingName returns the name of the workspace being edited
func (c *CreateView) EditingName() string {
	return c.editingName
}

// SetError sets an error message
func (c *CreateView) SetError(msg string) {
	c.errorMsg = msg
}

// GetWorkspace returns the workspace from current form values
func (c *CreateView) GetWorkspace() *workspace.Workspace {
	ws := workspace.NewWorkspace(
		strings.TrimSpace(c.nameInput.Value()),
		c.pathPicker.Value(),
	)
	ws.Folder = strings.Trim(strings.TrimSpace(c.folderInput.Value()), "/")
	ws.QuickAccess = c.quickAccess
	ws.Layout.Type = c.layoutType
	ws.Layout.MainCmd = c.layoutMainCmd
	ws.Layout.Panes = c.layoutPanes

	// Set remote if host is provided
	remoteHost := strings.TrimSpace(c.remoteHostInput.Value())
	if remoteHost != "" {
		ws.Remote = &workspace.Remote{
			Host: remoteHost,
			Path: c.pathPicker.Value(),
			Key:  strings.TrimSpace(c.sshKeyInput.Value()),
		}
	}
	return ws
}

// Validate checks if form values are valid
func (c *CreateView) Validate() error {
	name := strings.TrimSpace(c.nameInput.Value())
	if name == "" {
		c.errorMsg = "Name is required"
		return nil
	}
	path := c.pathPicker.Value()
	if path == "" {
		c.errorMsg = "Path is required"
		return nil
	}
	c.errorMsg = ""
	return nil
}

// Update handles input for the create view
func (c *CreateView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+j", "ctrl+n":
			c.nextField()
			return nil
		case "ctrl+k":
			c.prevField()
			return nil
		case "left", "right":
			if c.activeField == fieldLayoutType {
				c.cycleLayoutType(msg.String() == "right")
				return nil
			}
		case "tab", "shift+tab":
			// On path field, let tab go to path picker for autocomplete
			if c.activeField == fieldPath {
				return c.pathPicker.Update(msg)
			}
			// On other fields, tab moves to next field
			if msg.String() == "tab" {
				c.nextField()
			} else {
				c.prevField()
			}
			return nil
		}
	}

	// Update active input
	var cmd tea.Cmd
	switch c.activeField {
	case fieldName:
		c.nameInput, cmd = c.nameInput.Update(msg)
	case fieldPath:
		cmd = c.pathPicker.Update(msg)
	case fieldRemoteHost:
		c.remoteHostInput, cmd = c.remoteHostInput.Update(msg)
	case fieldSSHKey:
		c.sshKeyInput, cmd = c.sshKeyInput.Update(msg)
	case fieldFolder:
		c.folderInput, cmd = c.folderInput.Update(msg)
	}

	return cmd
}

func (c *CreateView) nextField() {
	c.activeField = (c.activeField + 1) % 6
	c.updateFocus()
}

func (c *CreateView) prevField() {
	c.activeField = (c.activeField + 5) % 6
	c.updateFocus()
}

func (c *CreateView) updateFocus() {
	c.nameInput.Blur()
	c.pathPicker.Blur()
	c.remoteHostInput.Blur()
	c.sshKeyInput.Blur()
	c.folderInput.Blur()

	switch c.activeField {
	case fieldName:
		c.nameInput.Focus()
	case fieldPath:
		c.pathPicker.Focus()
	case fieldRemoteHost:
		c.remoteHostInput.Focus()
	case fieldSSHKey:
		c.sshKeyInput.Focus()
	case fieldFolder:
		c.folderInput.Focus()
	}
}

func (c *CreateView) cycleLayoutType(forward bool) {
	for i, lt := range c.layoutTypes {
		if lt == c.layoutType {
			if forward {
				c.layoutType = c.layoutTypes[(i+1)%len(c.layoutTypes)]
			} else {
				c.layoutType = c.layoutTypes[(i+len(c.layoutTypes)-1)%len(c.layoutTypes)]
			}
			return
		}
	}
}

// View renders the create view
func (c *CreateView) View() string {
	var b strings.Builder

	title := "Create Workspace"
	if c.editing {
		title = "Edit Workspace"
	}
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	// Name field
	nameLabel := "Name"
	if c.activeField == fieldName {
		nameLabel = "> " + nameLabel
	} else {
		nameLabel = "  " + nameLabel
	}
	b.WriteString(labelStyle.Render(nameLabel))
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(c.nameInput.View())
	b.WriteString("\n\n")

	// Path field
	pathLabel := "Path"
	if c.remoteHostInput.Value() != "" {
		pathLabel = "Remote Path"
	}
	if c.activeField == fieldPath {
		pathLabel = "> " + pathLabel
	} else {
		pathLabel = "  " + pathLabel
	}
	b.WriteString(labelStyle.Render(pathLabel))
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(c.pathPicker.View())
	b.WriteString("\n\n")

	// Remote host field
	remoteLabel := "Remote Host"
	if c.activeField == fieldRemoteHost {
		remoteLabel = "> " + remoteLabel
	} else {
		remoteLabel = "  " + remoteLabel
	}
	b.WriteString(labelStyle.Render(remoteLabel))
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(c.remoteHostInput.View())
	b.WriteString("\n\n")

	// SSH Key field
	sshKeyLabel := "SSH Key"
	if c.activeField == fieldSSHKey {
		sshKeyLabel = "> " + sshKeyLabel
	} else {
		sshKeyLabel = "  " + sshKeyLabel
	}
	b.WriteString(labelStyle.Render(sshKeyLabel))
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(c.sshKeyInput.View())
	b.WriteString("\n\n")

	// Folder field
	folderLabel := "Folder"
	if c.activeField == fieldFolder {
		folderLabel = "> " + folderLabel
	} else {
		folderLabel = "  " + folderLabel
	}
	b.WriteString(labelStyle.Render(folderLabel))
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(c.folderInput.View())
	b.WriteString("\n\n")

	// Layout type
	layoutLabel := "Layout"
	if c.activeField == fieldLayoutType {
		layoutLabel = "> " + layoutLabel
	} else {
		layoutLabel = "  " + layoutLabel
	}
	b.WriteString(labelStyle.Render(layoutLabel))
	b.WriteString("\n")
	b.WriteString("  ")

	// Show layout options
	for i, lt := range c.layoutTypes {
		style := mutedStyle
		if lt == c.layoutType {
			style = selectedStyle
		}
		b.WriteString(style.Render("[" + string(lt) + "]"))
		if i < len(c.layoutTypes)-1 {
			b.WriteString("  ")
		}
	}
	b.WriteString("\n")

	// Show template/panes info
	if c.templateName != "" {
		b.WriteString("  ")
		b.WriteString(mutedStyle.Render("Template: "))
		b.WriteString(selectedStyle.Render(c.templateName))
		b.WriteString("\n")
	} else if len(c.layoutPanes) > 0 {
		b.WriteString("  ")
		b.WriteString(mutedStyle.Render("Panes: "))
		b.WriteString(selectedStyle.Render(fmt.Sprintf("%d configured", len(c.layoutPanes))))
		b.WriteString("\n")
	}

	// Error message
	if c.errorMsg != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(c.errorMsg))
		b.WriteString("\n")
	}

	// Help text
	help := "[ctrl+j/k]nav  [F2]template  [enter]save  [esc]cancel"
	if c.activeField == fieldPath {
		help = "[tab]autocomplete  [ctrl+j/k]nav  [enter]save  [esc]cancel"
	} else if c.activeField == fieldLayoutType {
		help = "[←/→]change  [F2]template  [enter]save  [esc]cancel"
	}
	b.WriteString(helpStyle.Render(help))

	return boxStyle.Render(b.String())
}
