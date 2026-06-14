package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/OleksandrBesan/tatami/internal/workspace"
)

// ListItem represents an item in the list (workspace or folder)
type ListItem struct {
	Type      string // "workspace", "folder", "header"
	Name      string
	Workspace *workspace.Workspace
}

// ListView displays the list of workspaces
type ListView struct {
	store         *workspace.Store
	items         []ListItem
	cursor        int
	currentFolder string // Current folder path (empty = root)
	filter        textinput.Model
	filtering     bool
	inZellij      bool
	width         int
	height        int
}

// NewListView creates a new list view
func NewListView(store *workspace.Store) *ListView {
	ti := textinput.New()
	ti.Placeholder = "Filter..."
	ti.CharLimit = 50

	lv := &ListView{
		store:         store,
		cursor:        0,
		currentFolder: "",
		filter:        ti,
		filtering:     false,
	}
	lv.refreshItems()
	return lv
}

// refreshItems rebuilds the item list based on current folder
func (l *ListView) refreshItems() {
	l.items = nil

	if l.filtering && l.filter.Value() != "" {
		// Filter mode - show all matching workspaces
		query := strings.ToLower(l.filter.Value())
		for _, ws := range l.store.List() {
			if strings.Contains(strings.ToLower(ws.Name), query) ||
				strings.Contains(strings.ToLower(ws.Path), query) {
				wsCopy := ws
				l.items = append(l.items, ListItem{Type: "workspace", Name: ws.Name, Workspace: &wsCopy})
			}
		}
		return
	}

	// Normal mode - show structure
	if l.currentFolder == "" {
		// Root view
		// Quick Access section
		quickAccess := l.store.QuickAccess()
		if len(quickAccess) > 0 {
			l.items = append(l.items, ListItem{Type: "header", Name: "Quick Access"})
			for _, ws := range quickAccess {
				wsCopy := ws
				l.items = append(l.items, ListItem{Type: "workspace", Name: ws.Name, Workspace: &wsCopy})
			}
		}

		// Folders section
		subfolders := l.store.ListSubfolders("")
		sort.Strings(subfolders)
		if len(subfolders) > 0 {
			l.items = append(l.items, ListItem{Type: "header", Name: "Folders"})
			for _, f := range subfolders {
				l.items = append(l.items, ListItem{Type: "folder", Name: f})
			}
		}

		// Root workspaces
		rootWs := l.store.ListRootWorkspaces()
		if len(rootWs) > 0 {
			l.items = append(l.items, ListItem{Type: "header", Name: "Workspaces"})
			for _, ws := range rootWs {
				wsCopy := ws
				l.items = append(l.items, ListItem{Type: "workspace", Name: ws.Name, Workspace: &wsCopy})
			}
		}
	} else {
		// Inside a folder
		// Back option
		l.items = append(l.items, ListItem{Type: "folder", Name: ".."})

		// Subfolders
		subfolders := l.store.ListSubfolders(l.currentFolder)
		sort.Strings(subfolders)
		for _, f := range subfolders {
			l.items = append(l.items, ListItem{Type: "folder", Name: f})
		}

		// Workspaces in this folder
		wsInFolder := l.store.ListInFolder(l.currentFolder)
		for _, ws := range wsInFolder {
			wsCopy := ws
			l.items = append(l.items, ListItem{Type: "workspace", Name: ws.Name, Workspace: &wsCopy})
		}
	}

	// Adjust cursor
	if l.cursor >= len(l.items) {
		l.cursor = max(0, len(l.items)-1)
	}
	// Skip headers
	l.skipHeaders(1)
}

func (l *ListView) skipHeaders(direction int) {
	for l.cursor >= 0 && l.cursor < len(l.items) && l.items[l.cursor].Type == "header" {
		l.cursor += direction
	}
	if l.cursor < 0 {
		// find first non-header from start
		for l.cursor = 0; l.cursor < len(l.items); l.cursor++ {
			if l.items[l.cursor].Type != "header" {
				return
			}
		}
		l.cursor = 0
	} else if l.cursor >= len(l.items) {
		// find last non-header from end
		for l.cursor = len(l.items) - 1; l.cursor >= 0; l.cursor-- {
			if l.items[l.cursor].Type != "header" {
				return
			}
		}
		l.cursor = 0
	}
}

// SetSize sets the view dimensions
func (l *ListView) SetSize(width, height int) {
	l.width = width
	l.height = height
}

// Selected returns the currently selected item
func (l *ListView) Selected() *ListItem {
	if len(l.items) == 0 || l.cursor >= len(l.items) {
		return nil
	}
	return &l.items[l.cursor]
}

// CurrentFolder returns the current folder path
func (l *ListView) CurrentFolder() string {
	return l.currentFolder
}

// EnterFolder enters a folder
func (l *ListView) EnterFolder(name string) {
	if name == ".." {
		// Go up
		if l.currentFolder == "" {
			return
		}
		parts := strings.Split(l.currentFolder, "/")
		if len(parts) <= 1 {
			l.currentFolder = ""
		} else {
			l.currentFolder = strings.Join(parts[:len(parts)-1], "/")
		}
	} else {
		// Go into folder
		if l.currentFolder == "" {
			l.currentFolder = name
		} else {
			l.currentFolder = l.currentFolder + "/" + name
		}
	}
	l.refreshItems()
	// Skip ".." and start on first actual item when entering a folder
	if name != ".." && len(l.items) > 1 {
		l.cursor = 1
	} else {
		l.cursor = 0
	}
}

// Refresh reloads items from store
func (l *ListView) Refresh() {
	l.refreshItems()
}

// SetCurrentFolder sets the current folder path
func (l *ListView) SetCurrentFolder(folder string) {
	l.currentFolder = folder
	l.cursor = 0
	l.refreshItems()
}

// SetInZellij sets whether we're inside a Zellij session
func (l *ListView) SetInZellij(inZellij bool) {
	l.inZellij = inZellij
}

// Update handles input for the list view
func (l *ListView) Update(msg tea.Msg) tea.Cmd {
	if l.filtering {
		var cmd tea.Cmd
		l.filter, cmd = l.filter.Update(msg)
		l.refreshItems()
		return cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if l.cursor < len(l.items)-1 {
				l.cursor++
				l.skipHeaders(1)
			}
		case "k", "up":
			if l.cursor > 0 {
				l.cursor--
				l.skipHeaders(-1)
			}
		case "g":
			l.cursor = 0
			l.skipHeaders(1)
		case "G":
			l.cursor = max(0, len(l.items)-1)
			l.skipHeaders(-1)
		case "/":
			l.filtering = true
			l.filter.Focus()
			return nil
		case "backspace", "h":
			// Go back if in a folder
			if l.currentFolder != "" && !l.filtering {
				l.EnterFolder("..")
			}
		}
	}
	return nil
}

// StopFiltering exits filter mode
func (l *ListView) StopFiltering() {
	l.filtering = false
	l.filter.Blur()
	l.refreshItems()
}

// ClearFilter resets the filter
func (l *ListView) ClearFilter() {
	l.filter.SetValue("")
	l.StopFiltering()
}

// IsFiltering returns whether filter mode is active
func (l *ListView) IsFiltering() bool {
	return l.filtering
}

// View renders the list view
func (l *ListView) View() string {
	var b strings.Builder

	// Title
	title := "TATAMI"
	if l.currentFolder != "" {
		title = "TATAMI - " + l.currentFolder
	}
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	// Filter input (if active)
	if l.filtering {
		b.WriteString(l.filter.View())
		b.WriteString("\n\n")
	}

	// Item list
	if len(l.items) == 0 {
		if l.store.List() == nil || len(l.store.List()) == 0 {
			b.WriteString(mutedStyle.Render("No workspaces yet. Press 'n' to create one."))
		} else if l.filtering {
			b.WriteString(mutedStyle.Render("No matching workspaces."))
		} else {
			b.WriteString(mutedStyle.Render("Empty folder. Press 'n' to create a workspace."))
		}
	} else {
		listHeight := l.height - 10
		if listHeight < 5 {
			listHeight = 5
		}

		start := 0
		end := len(l.items)
		if end > listHeight {
			if l.cursor >= listHeight {
				start = l.cursor - listHeight + 1
			}
			end = start + listHeight
			if end > len(l.items) {
				end = len(l.items)
				start = end - listHeight
			}
		}

		for i := start; i < end; i++ {
			item := l.items[i]

			switch item.Type {
			case "header":
				// Section header
				b.WriteString("\n")
				b.WriteString(labelStyle.Render(item.Name))
				b.WriteString("\n")

			case "folder":
				cursor := "  "
				style := normalStyle
				if i == l.cursor {
					cursor = "> "
					style = selectedStyle
				}
				icon := "📁 "
				if item.Name == ".." {
					icon = "⬅ "
				}
				b.WriteString(fmt.Sprintf("%s%s%s/\n", cursor, icon, style.Render(item.Name)))

			case "workspace":
				cursor := "  "
				style := normalStyle
				if i == l.cursor {
					cursor = "> "
					style = selectedStyle
				}
				ws := item.Workspace
				name := style.Render(ws.Name)

				// Show path - for remote show host:path
				var pathStr string
				if ws.IsRemote() {
					pathStr = fmt.Sprintf("%s:%s", ws.Remote.Host, shortenPath(ws.Remote.Path, 30))
				} else {
					pathStr = shortenPath(ws.Path, 40)
				}
				path := mutedStyle.Render(pathStr)

				star := "  "
				if ws.QuickAccess {
					star = "★ "
				}

				line := fmt.Sprintf("%s%s%-20s %s", cursor, star, name, path)
				b.WriteString(line + "\n")
			}
		}

		if len(l.items) > listHeight {
			scrollInfo := fmt.Sprintf(" (%d/%d)", l.cursor+1, len(l.items))
			b.WriteString(mutedStyle.Render(scrollInfo))
			b.WriteString("\n")
		}
	}

	// Help text
	var help string
	if l.filtering {
		help = "[enter]confirm  [esc]cancel"
	} else if l.currentFolder != "" {
		help = "[h/←]back  [n]ew  [e]dit  [d]elete  [*]star  [q]uit"
	} else if l.inZellij {
		help = "[n]ew  [e]dit  [d]elete  [*]star  [f]older  [z]ellij  [/]filter  [q]uit"
	} else {
		help = "[n]ew  [e]dit  [d]elete  [*]star  [f]older  [/]filter  [q]uit"
	}
	b.WriteString(helpStyle.Render(help))

	return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
}

func shortenPath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}

	home, _ := strings.CutPrefix(path, "/Users/")
	if home != path {
		parts := strings.SplitN(home, "/", 2)
		if len(parts) == 2 {
			path = "~/" + parts[1]
		}
	}

	if len(path) <= maxLen {
		return path
	}

	return "..." + path[len(path)-maxLen+3:]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
