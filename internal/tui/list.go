package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/systemusage"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ListItem represents an item on the Tatami home list.
type ListItem struct {
	Type      string // "workspace", "folder", "header"
	Name      string
	Workspace *workspace.Workspace
	Herdr     *shell.HerdrSession
	Endpoint  *herdrhub.Endpoint
}

// ListView displays the list of workspaces
type ListView struct {
	store            *workspace.Store
	items            []ListItem
	cursor           int
	currentFolder    string // Current folder path (empty = root)
	filter           textinput.Model
	filtering        bool
	inZellij         bool
	width            int
	height           int
	mobileMode       bool
	herdrSessions    herdrSessionLister
	herdrUsage       *systemusage.SessionUsage
	herdrUsageFor    string
	herdrUsageErr    error
	herdrLoading     bool
	hubSnapshots     []herdrhub.Snapshot
	hubEndpoints     map[string]herdrhub.Endpoint
	hubEndpointOrder []herdrhub.Endpoint
	hubCollapsed     map[string]bool
	hubAgents        map[string][]herdrhub.Agent
	hubAgentErr      map[string]error
	hubAgentLoading  map[string]bool
}

func hubSessionKey(endpoint, session string) string { return endpoint + "\x00" + session }
func (l *ListView) SetHerdrHubAgents(endpoint, session string, agents []herdrhub.Agent, err error) {
	if l.hubAgents == nil {
		l.hubAgents = map[string][]herdrhub.Agent{}
		l.hubAgentErr = map[string]error{}
		l.hubAgentLoading = map[string]bool{}
	}
	key := hubSessionKey(endpoint, session)
	l.hubAgents[key] = agents
	l.hubAgentErr[key] = err
	l.hubAgentLoading[key] = false
	l.refreshItems()
}

func (l *ListView) SetHerdrHubAgentsLoading(endpoint, session string) {
	if l.hubAgents == nil {
		l.hubAgents = map[string][]herdrhub.Agent{}
		l.hubAgentErr = map[string]error{}
		l.hubAgentLoading = map[string]bool{}
	}
	key := hubSessionKey(endpoint, session)
	delete(l.hubAgents, key)
	delete(l.hubAgentErr, key)
	l.hubAgentLoading[key] = true
	l.refreshItems()
}

// SetHerdrHubSnapshots supplies cached endpoint inventory. Local sessions keep
// their existing source and controls; remote rows are attach-only.
func (l *ListView) SetHerdrHubSnapshots(endpoints []herdrhub.Endpoint, snapshots []herdrhub.Snapshot) {
	selectedEndpoint, selectedSession := "", ""
	if selected := l.Selected(); selected != nil && selected.Herdr != nil {
		selectedSession = selected.Herdr.Name
		if selected.Endpoint != nil {
			selectedEndpoint = selected.Endpoint.ID
		}
	}
	l.hubEndpoints = make(map[string]herdrhub.Endpoint, len(endpoints))
	l.hubEndpointOrder = append([]herdrhub.Endpoint(nil), endpoints...)
	for _, endpoint := range endpoints {
		l.hubEndpoints[endpoint.ID] = endpoint
	}
	if l.hubCollapsed == nil {
		l.hubCollapsed = make(map[string]bool)
	}
	l.hubSnapshots = append([]herdrhub.Snapshot(nil), snapshots...)
	l.refreshItems()
	if selectedSession != "" {
		for i, item := range l.items {
			if item.Herdr != nil && item.Herdr.Name == selectedSession {
				endpoint := herdrhub.LocalEndpointID
				if item.Endpoint != nil {
					endpoint = item.Endpoint.ID
				}
				if endpoint == selectedEndpoint {
					l.cursor = i
					break
				}
			}
		}
	}
}

// NewListView creates a new list view
func NewListView(store *workspace.Store) *ListView {
	return NewListViewWithHerdrSessions(store, shell.ListHerdrSessions)
}

// NewListViewWithHerdrSessions creates a list view with an injected Herdr session source.
func NewListViewWithHerdrSessions(store *workspace.Store, lister herdrSessionLister) *ListView {
	ti := textinput.New()
	ti.Placeholder = "Filter..."
	ti.CharLimit = 50

	lv := &ListView{
		store:         store,
		cursor:        0,
		currentFolder: "",
		filter:        ti,
		filtering:     false,
		herdrSessions: lister,
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
		if l.herdrSessions != nil {
			if sessions, err := l.herdrSessions(); err == nil {
				for _, session := range sessions {
					if strings.Contains(strings.ToLower(session.Name), query) {
						copy := session
						l.items = append(l.items, ListItem{Type: "herdr_session", Name: session.Name, Herdr: &copy})
					}
				}
			}
		}
		l.appendHubItems(query, true)
		if l.cursor >= len(l.items) {
			l.cursor = max(0, len(l.items)-1)
		}
		l.skipHeaders(1)
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

		// Tatami projects include both folders and projects saved directly at root.
		subfolders := l.store.ListSubfolders("")
		sort.Strings(subfolders)
		rootWs := l.store.ListRootWorkspaces()
		if len(subfolders) > 0 || len(rootWs) > 0 {
			l.items = append(l.items, ListItem{Type: "header", Name: "Tatami Projects"})
			for _, f := range subfolders {
				l.items = append(l.items, ListItem{Type: "folder", Name: f})
			}
			for _, ws := range rootWs {
				wsCopy := ws
				l.items = append(l.items, ListItem{Type: "workspace", Name: ws.Name, Workspace: &wsCopy})
			}
		}

		// Herdr is a separate runtime/session group after Tatami's saved projects.
		if l.herdrSessions != nil {
			sessions, err := l.herdrSessions()
			l.items = append(l.items, ListItem{Type: "header", Name: "Herdr Hub"})
			local := herdrhub.LocalEndpoint()
			prefix := "▾ "
			if l.hubCollapsed[herdrhub.LocalEndpointID] {
				prefix = "▸ "
			}
			state := herdrhub.StateOnline
			if err != nil {
				state = herdrhub.StateOffline
			}
			l.items = append(l.items, ListItem{Type: "herdr_endpoint", Name: prefix + "Herdr · This Mac · " + string(state), Endpoint: &local})
			if err == nil && !l.hubCollapsed[herdrhub.LocalEndpointID] {
				for _, session := range sessions {
					sessionCopy := session
					l.items = append(l.items, ListItem{Type: "herdr_session", Name: session.Name, Herdr: &sessionCopy})
				}
			}
		}
		l.appendHubItems("", false)
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

func (l *ListView) appendHubItems(query string, flat bool) {
	snapshots := make(map[string]herdrhub.Snapshot, len(l.hubSnapshots))
	for _, snapshot := range l.hubSnapshots {
		snapshots[snapshot.EndpointID] = snapshot
	}
	for _, endpoint := range l.hubEndpointOrder {
		if endpoint.ID == herdrhub.LocalEndpointID {
			continue
		}
		snapshot, ok := snapshots[endpoint.ID]
		if !ok {
			snapshot = herdrhub.Snapshot{EndpointID: endpoint.ID, State: herdrhub.StateLoading}
		}
		if !flat {
			prefix := "▾ "
			if l.hubCollapsed[endpoint.ID] {
				prefix = "▸ "
			}
			endpointCopy := endpoint
			l.items = append(l.items, ListItem{Type: "herdr_endpoint", Name: prefix + "Herdr · " + endpoint.Label + " · " + hubEndpointStatus(snapshot), Endpoint: &endpointCopy})
			if l.hubCollapsed[endpoint.ID] {
				continue
			}
		}
		for _, session := range snapshot.Sessions {
			agentText := ""
			for _, agent := range l.hubAgents[hubSessionKey(endpoint.ID, session.SessionName)] {
				agentText += " " + agent.Kind + " " + agent.Status + " " + agent.CWD
			}
			if query != "" && !strings.Contains(strings.ToLower(endpoint.Label+" "+endpoint.ID+" "+session.SessionName+agentText), query) {
				continue
			}
			copy := shell.HerdrSession{Name: session.SessionName, Running: session.Running, Default: session.Default}
			endpointCopy := endpoint
			name := session.SessionName
			if flat {
				name += " · " + endpoint.Label
			}
			l.items = append(l.items, ListItem{Type: "herdr_session", Name: name, Herdr: &copy, Endpoint: &endpointCopy})
		}
	}
}

func hubEndpointStatus(snapshot herdrhub.Snapshot) string {
	status := string(snapshot.State)
	if snapshot.State == herdrhub.StateOnline && snapshot.Latency > 0 {
		status += " · " + snapshot.Latency.Round(time.Millisecond).String()
	}
	if (snapshot.State == herdrhub.StateStale || snapshot.State == herdrhub.StateOffline) && !snapshot.LastSuccess.IsZero() {
		age := time.Since(snapshot.LastSuccess)
		if age < 0 {
			age = 0
		}
		status += " · last seen " + formatUsageAge(age)
	}
	return status
}

func hubAuthenticationGuidance(endpoint *herdrhub.Endpoint, snapshot herdrhub.Snapshot) string {
	if endpoint == nil || snapshot.State != herdrhub.StateAuthenticationNeeded {
		return ""
	}
	if err := herdrhub.ValidateEndpoint(*endpoint); err != nil {
		return "SSH authentication required. Edit this host and enter a valid destination."
	}
	return "[enter]open/authenticate — OpenSSH will ask for password or key passphrase\n" +
		"Background refresh needs non-interactive SSH\n" +
		"Encrypted key: ssh-add ~/.ssh/<private-key>\n" +
		"Install key: ssh-copy-id " + endpoint.Target + "\n" +
		"Verify refresh: ssh -o BatchMode=yes " + endpoint.Target + " true"
}

func (l *ListView) herdrEndpointGuidanceView() string {
	selected := l.Selected()
	if selected == nil || selected.Type != "herdr_endpoint" || selected.Endpoint == nil {
		return ""
	}
	for _, snapshot := range l.hubSnapshots {
		if snapshot.EndpointID == selected.Endpoint.ID {
			return hubAuthenticationGuidance(selected.Endpoint, snapshot)
		}
	}
	return ""
}

func (l *ListView) ToggleHerdrEndpoint(id string) {
	if id == "" {
		return
	}
	if l.hubCollapsed == nil {
		l.hubCollapsed = make(map[string]bool)
	}
	l.hubCollapsed[id] = !l.hubCollapsed[id]
	l.refreshItems()
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

// SetMobileMode enables numbered choices and compact phone rendering.
func (l *ListView) SetMobileMode(enabled bool) {
	l.mobileMode = enabled
}

func (l *ListView) compact() bool {
	return l.mobileMode || (l.width > 0 && l.width <= narrowTerminalWidth)
}

func (l *ListView) visibleRange() (int, int) {
	listHeight := l.height - 10
	if l.compact() {
		listHeight = l.height - 6
	}
	if listHeight < 5 {
		listHeight = 5
	}
	if l.mobileMode && listHeight > 9 {
		listHeight = 9
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
	return start, end
}

func (l *ListView) visibleOrdinal(itemIndex, start int) int {
	ordinal := 0
	for i := start; i <= itemIndex && i < len(l.items); i++ {
		if l.items[i].Type == "header" {
			continue
		}
		if i == itemIndex {
			return ordinal
		}
		ordinal++
	}
	return -1
}

func (l *ListView) selectVisibleNumber(key string) bool {
	start, end := l.visibleRange()
	selectable := make([]int, 0, end-start)
	for i := start; i < end && len(selectable) < 9; i++ {
		if l.items[i].Type != "header" {
			selectable = append(selectable, i)
		}
	}
	index, ok := numberKeyIndex(key, len(selectable))
	if !ok {
		return false
	}
	l.cursor = selectable[index]
	return true
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
		l.skipHeaders(1)
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

// SetHerdrUsageLoading shows a pending resource snapshot for a highlighted session.
func (l *ListView) SetHerdrUsageLoading(session string) {
	l.herdrUsageFor = session
	l.herdrUsage = nil
	l.herdrUsageErr = nil
	l.herdrLoading = true
}

// SetHerdrUsage stores the latest resource snapshot for a highlighted session.
func (l *ListView) SetHerdrUsage(session string, usage *systemusage.SessionUsage, err error) {
	l.herdrUsageFor = session
	l.herdrUsage = usage
	l.herdrUsageErr = err
	l.herdrLoading = false
}

// ClearHerdrUsage removes resource state when the selection leaves Herdr sessions.
func (l *ListView) ClearHerdrUsage() {
	l.herdrUsageFor = ""
	l.herdrUsage = nil
	l.herdrUsageErr = nil
	l.herdrLoading = false
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
		if l.mobileMode && l.selectVisibleNumber(msg.String()) {
			return nil
		}
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
	if l.compact() {
		b.WriteString("\n")
	} else {
		b.WriteString("\n\n")
	}

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
		start, end := l.visibleRange()

		for i := start; i < end; i++ {
			item := l.items[i]

			switch item.Type {
			case "header":
				// Section header
				b.WriteString("\n")
				if item.Name == "Herdr Sessions" {
					dividerWidth := l.width - 4
					if dividerWidth < 24 {
						dividerWidth = 40
					}
					if dividerWidth > 52 {
						dividerWidth = 52
					}
					b.WriteString(mutedStyle.Render(strings.Repeat("─", dividerWidth)))
					b.WriteString("\n")
				}
				b.WriteString(labelStyle.Render(item.Name))
				b.WriteString("\n")
			case "herdr_endpoint":
				if strings.Contains(item.Name, "This Mac") {
					dividerWidth := l.width - 4
					if dividerWidth < 24 {
						dividerWidth = 40
					}
					if dividerWidth > 52 {
						dividerWidth = 52
					}
					b.WriteString(mutedStyle.Render(strings.Repeat("─", dividerWidth)))
					b.WriteString("\n")
				}
				style := normalStyle
				if i == l.cursor {
					style = selectedStyle
				}
				b.WriteString(style.Render(item.Name))
				b.WriteString("\n")

			case "folder":
				cursor := choicePrefix(l.mobileMode, l.visibleOrdinal(i, start), i == l.cursor)
				style := normalStyle
				if i == l.cursor {
					style = selectedStyle
				}
				icon := "📁 "
				if item.Name == ".." {
					icon = "⬅ "
				}
				b.WriteString(fmt.Sprintf("%s%s%s/\n", cursor, icon, style.Render(item.Name)))

			case "workspace":
				cursor := choicePrefix(l.mobileMode, l.visibleOrdinal(i, start), i == l.cursor)
				style := normalStyle
				if i == l.cursor {
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

				line := fmt.Sprintf("%s%s%s", cursor, star, name)
				if !l.compact() {
					line = fmt.Sprintf("%s%s%-20s %s", cursor, star, name, path)
				}
				b.WriteString(line + "\n")

			case "herdr_session":
				cursor := choicePrefix(l.mobileMode, l.visibleOrdinal(i, start), i == l.cursor)
				style := normalStyle
				if i == l.cursor {
					style = selectedStyle
				}
				status := "○"
				statusText := "stopped"
				if item.Herdr != nil && item.Herdr.Running {
					status = "●"
					statusText = "running"
				}
				name := style.Render(item.Name)
				if l.compact() {
					b.WriteString(fmt.Sprintf("%s%s %s %s\n", cursor, status, name, mutedStyle.Render(statusText)))
				} else {
					b.WriteString(fmt.Sprintf("%s%s %-20s %s\n", cursor, status, name, mutedStyle.Render(statusText)))
				}
			}
		}

		if start > 0 || end < len(l.items) {
			scrollInfo := fmt.Sprintf(" (%d/%d)", l.cursor+1, len(l.items))
			b.WriteString(mutedStyle.Render(scrollInfo))
			b.WriteString("\n")
		}
	}

	if usage := l.herdrUsageView(); usage != "" {
		b.WriteString("\n")
		b.WriteString(usage)
		b.WriteString("\n")
	}
	if guidance := l.herdrEndpointGuidanceView(); guidance != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(guidance))
		b.WriteString("\n")
	}

	// Help text
	var help string
	if l.mobileMode && !l.filtering {
		help = "[↑↓/1-9]select  [enter]open"
		if l.currentFolder != "" {
			help += "  [b]back"
		}
		if selected := l.Selected(); selected != nil && selected.Type == "herdr_endpoint" && selected.Endpoint != nil {
			help = "[↑↓/1-9]select  [enter/space]collapse  [r]refresh"
			if selected.Endpoint.ID != herdrhub.LocalEndpointID {
				help = "[↑↓/1-9]select  [enter]open  [space]collapse  [r]refresh"
				help += "\n[e]edit [d]remove [a]add [/]filter [q]uit"
			} else {
				help += "\n[a]add [/]filter [q]uit"
			}
		} else if selected != nil && selected.Type == "herdr_session" && selected.Endpoint == nil {
			if selected.Herdr != nil && selected.Herdr.Running {
				help += "\n[x]stop  [q]uit"
			} else if selected.Herdr != nil && !selected.Herdr.Default && selected.Name != "default" {
				help += "\n[x]delete  [q]uit"
			}
		} else if selected != nil && selected.Type == "herdr_session" {
			if selected.Herdr != nil && selected.Herdr.Running {
				help = "[↑↓/1-9]select  [enter]open remote session\n[r]refresh [/]filter [q]uit"
			} else {
				help = "[↑↓/1-9]select  [enter]restore remote session\n[r]refresh [/]filter [q]uit"
			}
		} else {
			help += "\n[n]ew [e]dit [d]elete [*]star [/]filter [q]uit"
		}
	} else if l.filtering {
		help = "[enter]confirm  [esc]cancel"
	} else if selected := l.Selected(); selected != nil && selected.Type == "herdr_endpoint" && selected.Endpoint != nil {
		help = "[enter/space]collapse  [r]refresh  [R]refresh all  [a]add"
		if selected.Endpoint.ID != herdrhub.LocalEndpointID {
			help = "[enter]open/authenticate  [space]collapse  [r]refresh  [R]refresh all  [a]add"
			help += "  [e]edit  [d]remove"
		}
		help += "  [/]filter  [q]uit"
	} else if selected := l.Selected(); selected != nil && selected.Type == "herdr_session" && selected.Endpoint == nil {
		// local-only controls below
		switch {
		case selected.Herdr != nil && selected.Herdr.Running:
			help = "[enter]open  [x]stop  [q]uit"
		case selected.Herdr != nil && (selected.Herdr.Default || selected.Name == "default"):
			help = "[enter]open  built-in session  [q]uit"
		default:
			help = "[enter]open  [x]delete  [q]uit"
		}
	} else if selected := l.Selected(); selected != nil && selected.Type == "herdr_session" {
		if selected.Herdr != nil && selected.Herdr.Running {
			help = "[enter]open remote session  [r]refresh  [q]uit"
		} else {
			help = "[enter]restore remote session  [r]refresh  [q]uit"
		}
	} else if l.currentFolder != "" {
		help = "[h/←]back  [n]ew  [e]dit  [d]elete  [*]star  [q]uit"
	} else if l.inZellij {
		help = "[n]ew  [e]dit  [d]elete  [*]star  [f]older  [z]ellij  [/]filter  [q]uit"
	} else {
		help = "[n]ew  [e]dit  [d]elete  [*]star  [f]older  [/]filter  [q]uit"
	}
	b.WriteString(helpStyle.Render(help))

	padding := lipgloss.NewStyle().Padding(1, 2)
	if l.compact() {
		padding = lipgloss.NewStyle().Padding(0, 1)
	}
	return padding.Render(b.String())
}

func (l *ListView) herdrUsageView() string {
	selected := l.Selected()
	if selected == nil || selected.Type != "herdr_session" || selected.Herdr == nil {
		return ""
	}
	if selected.Endpoint != nil {
		if !selected.Herdr.Running {
			return labelStyle.Render("Usage") + mutedStyle.Render("  stopped")
		}
		key := hubSessionKey(selected.Endpoint.ID, selected.Herdr.Name)
		agents, known := l.hubAgents[key]
		summary := "  agents loading…"
		if known && !l.hubAgentLoading[key] {
			summary = fmt.Sprintf("  %d agents", len(agents))
		}
		if len(agents) > 0 {
			summary += " · " + agents[0].Status
		}
		if l.hubAgentErr[key] != nil {
			summary = "  agents unavailable"
		}
		return labelStyle.Render("Usage") + mutedStyle.Render("  CPU unavailable  RAM unavailable  MAX AGE unavailable"+summary)
	}
	if !selected.Herdr.Running {
		return labelStyle.Render("Usage") + mutedStyle.Render("  stopped")
	}
	if l.herdrUsageFor != selected.Name {
		return ""
	}
	if l.herdrLoading {
		return labelStyle.Render("Usage") + mutedStyle.Render("  loading…")
	}
	if l.herdrUsageErr != nil {
		reason := strings.Join(strings.Fields(l.herdrUsageErr.Error()), " ")
		return labelStyle.Render("Usage") + errorStyle.Render("  unavailable: "+reason)
	}
	if l.herdrUsage == nil {
		return ""
	}

	usage := l.herdrUsage
	line := fmt.Sprintf(
		"  CPU %.1f%%  RAM %s  PROCS %d  AGENTS %d  MAX AGE %s",
		usage.CPUPercent,
		formatUsageBytes(usage.RSSBytes),
		usage.ProcessCount,
		len(usage.Agents),
		formatUsageAge(usage.MaxAge),
	)
	return labelStyle.Render("Usage") + normalStyle.Render(line)
}

func formatUsageBytes(bytes uint64) string {
	const unit = uint64(1024)
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	divisor := unit
	unitName := "KiB"
	for _, candidate := range []string{"MiB", "GiB", "TiB", "PiB"} {
		if bytes < divisor*unit {
			break
		}
		divisor *= unit
		unitName = candidate
	}
	value := float64(bytes) / float64(divisor)
	if value >= 10 || value == float64(uint64(value)) {
		return fmt.Sprintf("%.0f %s", value, unitName)
	}
	return fmt.Sprintf("%.1f %s", value, unitName)
}

func formatUsageAge(age time.Duration) string {
	if age <= 0 {
		return "0s"
	}
	if age < time.Minute {
		return age.Round(time.Second).String()
	}
	return strings.TrimSuffix(age.Truncate(time.Minute).String(), "0s")
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
