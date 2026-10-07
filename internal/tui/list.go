package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	appsearch "github.com/OleksandrBesan/tatami/internal/search"
	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/sshconn"
	"github.com/OleksandrBesan/tatami/internal/systemusage"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ListItem represents an item on the Tatami home list.
type ListItem struct {
	Type       string // "workspace", "folder", "header"
	Name       string
	SearchID   appsearch.ID
	RowID      appsearch.ID
	FolderPath string
	Breadcrumb string
	MatchedBy  string
	Repository string
	Workspace  *workspace.Workspace
	Herdr      *shell.HerdrSession
	Endpoint   *herdrhub.Endpoint
}

// ListView displays the list of workspaces
type ListView struct {
	hubNotice             string
	store                 *workspace.Store
	items                 []ListItem
	normalItems           []ListItem
	searchItems           map[appsearch.ID]ListItem
	repositories          map[string]string
	search                searchController
	searchGeneration      uint64
	searchSourceTruncated bool
	selectionChanged      bool
	cursor                int
	currentFolder         string // Current folder path (empty = root)
	inZellij              bool
	width                 int
	height                int
	mobileMode            bool
	herdrSessions         herdrSessionLister
	localSessions         []shell.HerdrSession
	localSessionsErr      error
	herdrUsage            *systemusage.SessionUsage
	herdrUsageFor         string
	herdrUsageErr         error
	herdrLoading          bool
	hubSnapshots          []herdrhub.Snapshot
	hubEndpoints          map[string]herdrhub.Endpoint
	hubEndpointOrder      []herdrhub.Endpoint
	hubCollapsed          map[string]bool
	hubCollapseKnown      map[string]bool
	hubAgents             map[string][]herdrhub.Agent
	hubAgentErr           map[string]error
	hubAgentLoading       map[string]bool
	mouseRows             map[int]appsearch.ID
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
			selectedEndpoint = selected.Endpoint.Key()
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
	if l.hubCollapseKnown == nil {
		l.hubCollapseKnown = make(map[string]bool)
	}
	for _, endpoint := range endpoints {
		key := endpoint.Key()
		if !l.hubCollapseKnown[key] {
			l.hubCollapseKnown[key] = true
			l.hubCollapsed[key] = false
		}
	}
	l.hubSnapshots = herdrhub.ReconcileSnapshots(endpoints, snapshots, false)
	l.refreshItems()
	if selectedSession != "" {
		for i, item := range l.items {
			if item.Herdr != nil && item.Herdr.Name == selectedSession {
				endpoint := herdrhub.LocalEndpointID
				if item.Endpoint != nil {
					endpoint = item.Endpoint.Key()
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
	lv := &ListView{
		store:         store,
		cursor:        0,
		currentFolder: "",
		search:        newSearchController("workspaces, sessions, repositories"),
		searchItems:   make(map[appsearch.ID]ListItem),
		repositories:  make(map[string]string),
		herdrSessions: lister,
	}
	lv.refreshSources()
	return lv
}

func (l *ListView) refreshSources() {
	if l.herdrSessions != nil {
		sessions, err := l.herdrSessions()
		l.localSessionsErr = err
		if err == nil {
			l.localSessions = sessions
		}
	}
	l.refreshItems()
}

// refreshItems rebuilds the item list based on current folder
func (l *ListView) refreshItems() {
	if l.cursor >= 0 && l.cursor < len(l.items) && !listItemIsHeader(l.items[l.cursor]) {
		rows := l.search.Rows()
		active := l.search.ActiveIndex()
		if active < 0 || active >= len(rows) || rows[active].RowID != l.items[l.cursor].RowID {
			l.selectSearchRow(l.items[l.cursor].RowID)
		}
	}
	l.items = nil

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
			sessions, err := l.localSessions, l.localSessionsErr
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
	l.prepareSearchProjection()
}

func (l *ListView) prepareSearchProjection() {
	rowOccurrences := make(map[appsearch.ID]int)
	emptyRows := make([]searchRow, 0, len(l.items))
	for index := range l.items {
		item := &l.items[index]
		if listItemIsHeader(*item) {
			continue
		}
		item.SearchID = l.listItemSearchID(*item)
		if item.SearchID == "" {
			item.SearchID = appsearch.ID(fmt.Sprintf("presentation:%d", index))
		}
		occurrence := rowOccurrences[item.SearchID]
		rowOccurrences[item.SearchID] = occurrence + 1
		item.RowID = appsearch.ID(fmt.Sprintf("%s#%d", item.SearchID, occurrence))
		emptyRows = append(emptyRows, searchRow{RowID: item.RowID, DocumentID: item.SearchID})
	}
	l.normalItems = append(l.normalItems[:0], l.items...)

	searchItems, sourceTruncated := l.collectSearchItems()
	l.searchSourceTruncated = sourceTruncated
	documents := make([]appsearch.Document, 0, len(searchItems))
	l.searchItems = make(map[appsearch.ID]ListItem, len(searchItems))
	for ordinal, item := range searchItems {
		if _, exists := l.searchItems[item.SearchID]; exists || item.SearchID == "" {
			continue
		}
		l.searchItems[item.SearchID] = item
		documents = append(documents, l.searchDocument(item, ordinal))
	}
	l.searchGeneration++
	if err := l.search.ReplaceDocuments(l.searchGeneration, documents, emptyRows); err != nil {
		l.search.Clear()
	}
	l.applySearchProjection()
}

func (l *ListView) collectSearchItems() ([]ListItem, bool) {
	capacity := min(len(l.store.List())+len(l.localSessions)+len(l.hubSnapshots)*8, appsearch.MaxDocuments)
	items := make([]ListItem, 0, capacity)
	truncated := false
	appendItem := func(item ListItem) bool {
		if len(items) >= appsearch.MaxDocuments {
			truncated = true
			return false
		}
		items = append(items, item)
		return true
	}
	folders := make(map[string]struct{})
	for _, saved := range l.store.List() {
		ws := saved
		item := ListItem{Type: "workspace", Name: ws.Name, Workspace: &ws, Breadcrumb: ws.Folder, Repository: l.repositories[ws.Path]}
		item.SearchID = l.listItemSearchID(item)
		if !appendItem(item) {
			return items, truncated
		}
		parts := strings.Split(strings.Trim(ws.Folder, "/"), "/")
		for i := range parts {
			if parts[i] == "" {
				continue
			}
			folders[strings.Join(parts[:i+1], "/")] = struct{}{}
		}
	}
	folderPaths := make([]string, 0, len(folders))
	for folder := range folders {
		folderPaths = append(folderPaths, folder)
	}
	sort.Strings(folderPaths)
	for _, folder := range folderPaths {
		item := ListItem{Type: "folder", Name: folder, FolderPath: folder, Breadcrumb: "Tatami Projects"}
		item.SearchID = l.listItemSearchID(item)
		if !appendItem(item) {
			return items, truncated
		}
	}

	if l.herdrSessions != nil {
		local := herdrhub.LocalEndpoint()
		state := herdrhub.StateOnline
		if l.localSessionsErr != nil {
			state = herdrhub.StateOffline
		}
		endpoint := ListItem{Type: "herdr_endpoint", Name: "Herdr · This Mac · " + string(state), Endpoint: &local, Breadcrumb: "Herdr Hub"}
		endpoint.SearchID = l.listItemSearchID(endpoint)
		if !appendItem(endpoint) {
			return items, truncated
		}
		for _, session := range l.localSessions {
			copy := session
			item := ListItem{Type: "herdr_session", Name: session.Name, Herdr: &copy, Breadcrumb: "Herdr · This Mac"}
			item.SearchID = l.listItemSearchID(item)
			if !appendItem(item) {
				return items, truncated
			}
		}
	}

	snapshots := make(map[string]herdrhub.Snapshot, len(l.hubSnapshots))
	for _, snapshot := range l.hubSnapshots {
		snapshots[snapshot.EndpointID] = snapshot
	}
	seen := make(map[string]bool)
	for _, endpoint := range l.hubEndpointOrder {
		if endpoint.ID != herdrhub.LocalEndpointID {
			if l.collectHubSearchItems(&items, endpoint, snapshots, seen) {
				truncated = true
				break
			}
		}
	}
	return items, truncated
}

func (l *ListView) collectHubSearchItems(items *[]ListItem, endpoint herdrhub.Endpoint, snapshots map[string]herdrhub.Snapshot, seen map[string]bool) bool {
	if len(*items) >= appsearch.MaxDocuments {
		return true
	}
	key := endpoint.Key()
	if seen[key] {
		return false
	}
	seen[key] = true
	snapshot, ok := snapshots[key]
	if ok && !herdrhub.SnapshotMatches(endpoint, snapshot) {
		ok = false
	}
	if !ok {
		snapshot = herdrhub.Snapshot{EndpointID: key, State: herdrhub.StateLoading}
	}
	endpointCopy := endpoint
	endpointItem := ListItem{
		Type:       "herdr_endpoint",
		Name:       endpoint.Label,
		Breadcrumb: "Tatami · " + key + " · " + hubEndpointStatus(snapshot),
		Endpoint:   &endpointCopy,
	}
	endpointItem.SearchID = l.listItemSearchID(endpointItem)
	*items = append(*items, endpointItem)
	for _, summary := range snapshot.Workspaces {
		if len(*items) >= appsearch.MaxDocuments {
			return true
		}
		workspaceItem, ok := remoteWorkspaceListItem(endpoint, summary)
		if !ok {
			continue
		}
		workspaceItem.Breadcrumb = endpoint.Label
		workspaceItem.SearchID = l.listItemSearchID(workspaceItem)
		*items = append(*items, workspaceItem)
	}
	for _, session := range snapshot.Sessions {
		if len(*items) >= appsearch.MaxDocuments {
			return true
		}
		copy := shell.HerdrSession{Name: session.SessionName, Running: session.Running, Default: session.Default}
		item := ListItem{Type: "herdr_session", Name: session.SessionName, Herdr: &copy, Endpoint: &endpointCopy, Breadcrumb: endpoint.Label + " · " + key}
		item.SearchID = l.listItemSearchID(item)
		*items = append(*items, item)
	}
	for _, savedChild := range snapshot.Hosts {
		if len(*items) >= appsearch.MaxDocuments {
			return true
		}
		child, err := herdrhub.DescendantEndpoint(endpoint, savedChild)
		if err == nil {
			if l.collectHubSearchItems(items, child, snapshots, seen) {
				return true
			}
		}
	}
	return false
}

func remoteWorkspaceListItem(endpoint herdrhub.Endpoint, summary herdrhub.WorkspaceSummary) (ListItem, bool) {
	target := endpoint.Target
	jump := append([]string(nil), endpoint.Via...)
	if summary.Target != "" {
		hop, err := herdrhub.JumpDestination(endpoint)
		if err != nil {
			return ListItem{}, false
		}
		route := append(append([]string(nil), jump...), hop)
		route = append(route, summary.Jump...)
		route = append(route, summary.Target)
		if len(route) > herdrhub.MaxRouteDepth {
			return ListItem{}, false
		}
		seen := make(map[string]bool, len(route))
		for _, hop := range route {
			if seen[hop] {
				return ListItem{}, false
			}
			seen[hop] = true
		}
		jump = append([]string(nil), route[:len(route)-1]...)
		target = summary.Target
	}
	displayName := summary.Name
	if summary.Folder != "" {
		displayName = summary.Folder + "/" + summary.Name
	}
	endpointCopy := endpoint
	connection := herdrhub.EndpointConnection(endpoint)
	if target != endpoint.Target || !slices.Equal(jump, endpoint.Via) {
		connection = sshconn.Connection{Destination: target, Jump: jump}
	}
	if err := sshconn.Validate(connection); err != nil {
		return ListItem{}, false
	}
	return ListItem{
		Type:       "workspace",
		Name:       displayName + " · " + endpoint.Label,
		Repository: summary.Repository,
		Workspace: &workspace.Workspace{
			Name:        summary.Name,
			Path:        summary.Path,
			Folder:      summary.Folder,
			QuickAccess: summary.QuickAccess,
			Remote:      &workspace.Remote{Host: target, Path: summary.Path, Jump: jump, Connection: &connection, Source: herdrhub.EndpointOrigin(endpoint)},
			Layout:      workspace.Layout{Type: workspace.LayoutNone},
		},
		Endpoint: &endpointCopy,
	}, true
}

func (l *ListView) listItemSearchID(item ListItem) appsearch.ID {
	switch item.Type {
	case "workspace":
		if item.Workspace == nil {
			return ""
		}
		if item.Endpoint != nil {
			return compositeSearchID("workspace:remote", item.Endpoint.Key(), item.Workspace.Name, item.Workspace.Folder, item.Workspace.Path)
		}
		return compositeSearchID("workspace:local", item.Workspace.Name, item.Workspace.Path)
	case "folder":
		path := item.FolderPath
		if path == "" && item.Name != ".." {
			path = strings.Trim(strings.Trim(l.currentFolder, "/")+"/"+item.Name, "/")
		}
		if path == "" {
			return appsearch.ID("folder:back:" + l.currentFolder)
		}
		return appsearch.ID("folder:" + path)
	case "herdr_endpoint":
		if item.Endpoint != nil {
			return appsearch.ID("endpoint:" + item.Endpoint.Key())
		}
	case "herdr_session":
		if item.Herdr == nil {
			return ""
		}
		endpoint := herdrhub.LocalEndpointID
		if item.Endpoint != nil {
			endpoint = item.Endpoint.Key()
		}
		return appsearch.ID("session:" + endpoint + ":" + item.Herdr.Name)
	}
	return ""
}

func compositeSearchID(kind string, parts ...string) appsearch.ID {
	var result strings.Builder
	result.WriteString(kind)
	for _, part := range parts {
		fmt.Fprintf(&result, ":%d:%s", len(part), part)
	}
	return appsearch.ID(result.String())
}

func (l *ListView) searchDocument(item ListItem, ordinal int) appsearch.Document {
	document := appsearch.Document{ID: item.SearchID, Kind: appsearch.Kind(item.Type), Primary: item.Name, Secondary: item.Breadcrumb, Ordinal: ordinal}
	add := func(name, value string) {
		if value != "" {
			document.Fields = append(document.Fields, appsearch.Field{Name: name, Value: value, Class: appsearch.MetadataField})
		}
	}
	add("type", strings.ReplaceAll(item.Type, "_", " "))
	if item.Endpoint != nil {
		add("group", item.Endpoint.Group)
		add("tags", strings.Join(item.Endpoint.Tags, " "))
	}
	switch item.Type {
	case "workspace":
		if item.Workspace != nil {
			document.Primary = item.Workspace.Name
			document.Secondary = item.Workspace.Path
			add("folder", item.Workspace.Folder)
			add("repository", item.Repository)
			if item.Workspace.QuickAccess {
				document.Boost = 3
				add("status", "quick access")
			}
		}
		if item.Endpoint != nil {
			add("endpoint", item.Endpoint.Label+" "+item.Endpoint.Key())
		}
	case "folder":
		document.Primary = item.FolderPath
	case "herdr_endpoint":
		if item.Endpoint != nil {
			document.Primary = item.Endpoint.Label
			if item.Endpoint.ID == herdrhub.LocalEndpointID {
				document.Primary = "Herdr This Mac"
			}
			add("route", item.Endpoint.Key()+" "+item.Endpoint.Target)
		}
	case "herdr_session":
		if item.Herdr != nil {
			document.Primary = item.Herdr.Name
			if item.Herdr.Running {
				document.Boost = 2
				add("status", "running")
			} else {
				add("status", "stopped")
			}
		}
		if item.Endpoint != nil && item.Herdr != nil {
			add("endpoint", item.Endpoint.Label+" "+item.Endpoint.Key())
			for _, agent := range l.hubAgents[hubSessionKey(item.Endpoint.Key(), item.Herdr.Name)] {
				add("agent", agent.Kind+" "+agent.Status+" "+agent.CWD)
			}
		}
	}
	return document
}

func (l *ListView) SetRepositoryIdentities(repositories map[string]string) {
	l.repositories = make(map[string]string, len(repositories))
	for path, repository := range repositories {
		if path != "" && repository != "" {
			l.repositories[path] = repository
		}
	}
	l.refreshItems()
}

func (l *ListView) applySearchProjection() {
	if l.search.Query() == "" {
		l.items = append(l.items[:0], l.normalItems...)
		l.syncCursorFromSearch()
		return
	}
	l.items = l.items[:0]
	for _, row := range l.search.Rows() {
		item, ok := l.searchItems[row.DocumentID]
		if !ok {
			continue
		}
		item.RowID = row.RowID
		if match, ok := l.search.MatchFor(row.DocumentID); ok && match.MatchedField != "" {
			item.MatchedBy = match.MatchedField + ": " + match.MatchedValue
		}
		l.items = append(l.items, item)
	}
	l.cursor = clampInt(l.search.ActiveIndex(), 0, max(0, len(l.items)-1))
}

func (l *ListView) syncCursorFromSearch() {
	rows := l.search.Rows()
	active := l.search.ActiveIndex()
	if active < 0 || active >= len(rows) {
		l.cursor = 0
		return
	}
	for index := range l.items {
		if l.items[index].RowID == rows[active].RowID {
			l.cursor = index
			return
		}
	}
	l.cursor = 0
	l.skipHeaders(1)
}

func (l *ListView) appendHubItems(query string, flat bool) {
	snapshots := make(map[string]herdrhub.Snapshot, len(l.hubSnapshots))
	for _, snapshot := range l.hubSnapshots {
		snapshots[snapshot.EndpointID] = snapshot
	}
	groups := map[string][]herdrhub.Endpoint{}
	labels := map[string]string{}
	for _, endpoint := range l.hubEndpointOrder {
		if endpoint.ID == herdrhub.LocalEndpointID {
			continue
		}
		if endpoint.Group == "" {
			l.appendHubEndpoint(endpoint, 0, query, flat, snapshots)
			continue
		}
		key := strings.ToLower(endpoint.Group)
		if _, ok := labels[key]; !ok {
			labels[key] = endpoint.Group
		}
		groups[key] = append(groups[key], endpoint)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !flat {
			l.items = append(l.items, ListItem{Type: "remote_header", Name: "  " + labels[key]})
		}
		for _, endpoint := range groups[key] {
			l.appendHubEndpoint(endpoint, 0, query, flat, snapshots)
		}
	}
}

func (l *ListView) appendHubEndpoint(endpoint herdrhub.Endpoint, depth int, query string, flat bool, snapshots map[string]herdrhub.Snapshot) {
	key := endpoint.Key()
	snapshot, ok := snapshots[key]
	if !ok {
		snapshot = herdrhub.Snapshot{EndpointID: key, State: herdrhub.StateLoading}
	}
	indent := strings.Repeat("  ", depth)
	if !flat {
		if !l.hubCollapseKnown[key] {
			l.hubCollapseKnown[key] = true
			l.hubCollapsed[key] = depth > 0
		}
		prefix := "▾ "
		if l.hubCollapsed[key] {
			prefix = "▸ "
		}
		endpointCopy := endpoint
		l.items = append(l.items, ListItem{Type: "herdr_endpoint", Name: indent + prefix + "Tatami · " + endpoint.Label + " · " + hubEndpointStatus(snapshot), Endpoint: &endpointCopy})
		if l.hubCollapsed[key] {
			return
		}
	}
	sectionIndent := strings.Repeat("  ", depth+1)
	quick := make([]herdrhub.WorkspaceSummary, 0)
	for _, summary := range snapshot.Workspaces {
		if summary.QuickAccess {
			quick = append(quick, summary)
		}
	}
	if !flat && len(quick) > 0 {
		l.items = append(l.items, ListItem{Type: "remote_header", Name: sectionIndent + "Quick Access"})
	}
	for _, summary := range quick {
		l.appendRemoteWorkspace(endpoint, summary, depth+2, query, flat)
	}
	if !flat && len(snapshot.Workspaces) > 0 {
		l.items = append(l.items, ListItem{Type: "remote_header", Name: sectionIndent + "Tatami Projects"})
	}
	for _, summary := range snapshot.Workspaces {
		l.appendRemoteWorkspace(endpoint, summary, depth+2, query, flat)
	}
	if !flat && len(snapshot.Sessions) > 0 {
		l.items = append(l.items, ListItem{Type: "remote_header", Name: sectionIndent + "Herdr Sessions"})
	}
	for _, session := range snapshot.Sessions {
		agentText := ""
		for _, agent := range l.hubAgents[hubSessionKey(key, session.SessionName)] {
			agentText += " " + agent.Kind + " " + agent.Status + " " + agent.CWD
		}
		if query != "" && !strings.Contains(strings.ToLower(endpoint.Label+" "+key+" "+session.SessionName+agentText), query) {
			continue
		}
		copy := shell.HerdrSession{Name: session.SessionName, Running: session.Running, Default: session.Default}
		endpointCopy := endpoint
		name := strings.Repeat("  ", depth+2) + session.SessionName
		if flat {
			name = session.SessionName + " · " + endpoint.Label
		}
		l.items = append(l.items, ListItem{Type: "herdr_session", Name: name, Herdr: &copy, Endpoint: &endpointCopy})
	}
	if !flat && len(snapshot.Hosts) > 0 {
		l.items = append(l.items, ListItem{Type: "remote_header", Name: sectionIndent + "Remote Hosts"})
	}
	for _, savedChild := range snapshot.Hosts {
		child, err := herdrhub.DescendantEndpoint(endpoint, savedChild)
		if err != nil {
			if !flat {
				l.items = append(l.items, ListItem{Type: "remote_header", Name: sectionIndent + "Configure a jump alias to open credential-bearing descendants, or check the route."})
			}
			continue
		}
		l.appendHubEndpoint(child, depth+1, query, flat, snapshots)
	}
}

func (l *ListView) appendRemoteWorkspace(endpoint herdrhub.Endpoint, summary herdrhub.WorkspaceSummary, depth int, query string, flat bool) {
	displayName := summary.Name
	if summary.Folder != "" {
		displayName = summary.Folder + "/" + summary.Name
	}
	if query != "" && !strings.Contains(strings.ToLower(endpoint.Label+" "+endpoint.Group+" "+strings.Join(endpoint.Tags, " ")+" "+displayName+" "+summary.Path), query) {
		return
	}
	item, ok := remoteWorkspaceListItem(endpoint, summary)
	if !ok {
		return
	}
	if !flat {
		item.Name = strings.Repeat("  ", depth) + displayName
	}
	l.items = append(l.items, item)
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
	jumpOption := ""
	if len(endpoint.Via) > 0 {
		route := strings.Join(endpoint.Via, ",")
		jumpOption = "-o ProxyJump=" + route + " "
	}
	command, err := sshconn.Build(herdrhub.EndpointConnection(*endpoint), sshconn.Background, "true")
	if err != nil {
		return "SSH settings are invalid. Edit this host before retrying."
	}
	authHint := ""
	if herdrhub.EndpointConnection(*endpoint).Auth == sshconn.PasswordPrompt {
		authHint = "\nFor background refresh, change Authentication to an identity or SSH config/agent; password-prompt mode always requires a prompt."
	}
	return "[enter]open/authenticate — OpenSSH will ask for password or key passphrase\n" +
		"Background refresh needs non-interactive SSH\n" +
		"Encrypted key: ssh-add ~/.ssh/<private-key>\n" +
		"Install key: ssh-copy-id " + jumpOption + endpoint.Target + "\n" +
		"Verify refresh: " + sshconn.RenderPOSIX(command) + authHint
}

func hubRouteGuidance(endpoint *herdrhub.Endpoint, snapshot herdrhub.Snapshot) string {
	if endpoint == nil {
		return ""
	}
	needsHop := len(snapshot.Hosts) > 0
	for _, ws := range snapshot.Workspaces {
		if ws.Target != "" {
			needsHop = true
		}
	}
	if needsHop {
		if _, err := herdrhub.JumpDestination(*endpoint); err != nil {
			return err.Error()
		}
	}
	return ""
}

func (l *ListView) herdrEndpointGuidanceView() string {
	selected := l.Selected()
	if selected == nil || selected.Type != "herdr_endpoint" || selected.Endpoint == nil {
		return ""
	}
	for _, snapshot := range l.hubSnapshots {
		if snapshot.EndpointID == selected.Endpoint.Key() {
			guidance := hubAuthenticationGuidance(selected.Endpoint, snapshot)
			if routeGuidance := hubRouteGuidance(selected.Endpoint, snapshot); routeGuidance != "" {
				guidance = routeGuidance + "\n" + guidance
			}
			if snapshot.Error != "" {
				guidance = snapshot.Error + "\n" + guidance
			}
			if selected.Endpoint.Group != "" {
				guidance += "\nGroup: " + selected.Endpoint.Group
			}
			if len(selected.Endpoint.Tags) > 0 {
				guidance += "\nTags: " + strings.Join(selected.Endpoint.Tags, ", ")
			}
			return guidance
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
	if l.hubCollapseKnown == nil {
		l.hubCollapseKnown = make(map[string]bool)
	}
	l.hubCollapseKnown[id] = true
	l.hubCollapsed[id] = !l.hubCollapsed[id]
	l.refreshItems()
}

func (l *ListView) ExpandHerdrEndpoint(id string) {
	if id == "" {
		return
	}
	if l.hubCollapsed == nil {
		l.hubCollapsed = make(map[string]bool)
	}
	if l.hubCollapseKnown == nil {
		l.hubCollapseKnown = make(map[string]bool)
	}
	l.hubCollapseKnown[id] = true
	l.hubCollapsed[id] = false
	l.refreshItems()
}

func listItemIsHeader(item ListItem) bool {
	return item.Type == "header" || item.Type == "remote_header"
}

func (l *ListView) skipHeaders(direction int) {
	for l.cursor >= 0 && l.cursor < len(l.items) && listItemIsHeader(l.items[l.cursor]) {
		l.cursor += direction
	}
	if l.cursor < 0 {
		// find first non-header from start
		for l.cursor = 0; l.cursor < len(l.items); l.cursor++ {
			if !listItemIsHeader(l.items[l.cursor]) {
				return
			}
		}
		l.cursor = 0
	} else if l.cursor >= len(l.items) {
		// find last non-header from end
		for l.cursor = len(l.items) - 1; l.cursor >= 0; l.cursor-- {
			if !listItemIsHeader(l.items[l.cursor]) {
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
	listHeight := l.height - 12
	if l.compact() {
		listHeight = l.height - 8
	}
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
	return start, end
}

func (l *ListView) visibleOrdinal(itemIndex, start int) int {
	ordinal := 0
	for i := start; i <= itemIndex && i < len(l.items); i++ {
		if listItemIsHeader(l.items[i]) {
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
	if l.search.InQueryFocus() {
		return false
	}
	start, end := l.visibleRange()
	selectable := make([]int, 0, end-start)
	for i := start; i < end && len(selectable) < 9; i++ {
		if !listItemIsHeader(l.items[i]) {
			selectable = append(selectable, i)
		}
	}
	index, ok := numberKeyIndex(key, len(selectable))
	if !ok {
		return false
	}
	l.cursor = selectable[index]
	l.selectSearchRow(l.items[l.cursor].RowID)
	l.selectionChanged = true
	return true
}

func (l *ListView) selectMouseRow(row int) bool {
	rowID, ok := l.mouseRows[row]
	if !ok {
		return false
	}
	for index := range l.items {
		if l.items[index].RowID == rowID && !listItemIsHeader(l.items[index]) {
			l.cursor = index
			l.selectSearchRow(rowID)
			l.selectionChanged = true
			return true
		}
	}
	return false
}

func (l *ListView) selectSearchRow(rowID appsearch.ID) {
	for index, row := range l.search.Rows() {
		if row.RowID == rowID {
			l.search.SelectIndex(index)
			return
		}
	}
}

func (l *ListView) recordMouseRow(rendered string, index int) {
	row := strings.Count(rendered, "\n")
	if !l.compact() {
		row++ // desktop rendering has one row of outer vertical padding
	}
	l.mouseRows[row] = l.items[index].RowID
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
	l.search.Clear()
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
	l.refreshSources()
}

// SetCurrentFolder sets the current folder path
func (l *ListView) SetCurrentFolder(folder string) {
	l.currentFolder = folder
	l.cursor = 0
	l.search.Clear()
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
	switch msg := msg.(type) {
	case tea.KeyMsg:
		event, cmd := l.handleKey(msg)
		if event.Consumed {
			return cmd
		}
		if l.mobileMode && l.selectVisibleNumber(msg.String()) {
			return nil
		}
		switch msg.String() {
		case "j":
			if l.search.Move(1).BrowseSelectionChanged {
				l.selectionChanged = true
			}
			l.applySearchProjection()
		case "k":
			if l.search.Move(-1).BrowseSelectionChanged {
				l.selectionChanged = true
			}
			l.applySearchProjection()
		case "g":
			l.search.SelectFirst()
			l.applySearchProjection()
		case "G":
			l.search.SelectLast()
			l.applySearchProjection()
		case "backspace", "h":
			// Go back if in a folder
			if l.currentFolder != "" {
				l.EnterFolder("..")
			}
		}
	}
	return nil
}

func (l *ListView) handleKey(msg tea.KeyMsg) (searchEvent, tea.Cmd) {
	if l.cursor >= 0 && l.cursor < len(l.items) && !listItemIsHeader(l.items[l.cursor]) {
		rows := l.search.Rows()
		active := l.search.ActiveIndex()
		if active < 0 || active >= len(rows) || rows[active].RowID != l.items[l.cursor].RowID {
			l.selectSearchRow(l.items[l.cursor].RowID)
		}
	}
	event, cmd := l.search.Update(msg)
	if event.Consumed {
		if event.BrowseSelectionChanged {
			l.selectionChanged = true
		}
		l.applySearchProjection()
	}
	return event, cmd
}

func (l *ListView) consumeBrowseSelectionChanged() bool {
	changed := l.selectionChanged
	l.selectionChanged = false
	return changed
}

// StopFiltering exits filter mode
func (l *ListView) StopFiltering() {
	l.search.SelectIndex(l.search.ActiveIndex())
	l.applySearchProjection()
}

// ClearFilter resets the filter
func (l *ListView) ClearFilter() {
	l.search.Clear()
	l.applySearchProjection()
}

// IsFiltering returns whether filter mode is active
func (l *ListView) IsFiltering() bool {
	return l.search.InQueryFocus()
}

// View renders the list view
func (l *ListView) View() string {
	var b strings.Builder
	l.mouseRows = make(map[int]appsearch.ID)

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

	b.WriteString(l.search.QueryView())
	b.WriteString("\n\n")

	// Item list
	if len(l.items) == 0 {
		if l.search.Query() != "" {
			b.WriteString(mutedStyle.Render("No matches in known data."))
		} else if l.store.List() == nil || len(l.store.List()) == 0 {
			b.WriteString(mutedStyle.Render("No workspaces yet. Press 'n' to create one."))
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
			case "remote_header":
				b.WriteString(mutedStyle.Render(item.Name))
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
				l.recordMouseRow(b.String(), i)
				b.WriteString(style.Render(item.Name))
				b.WriteString("\n")
				l.renderSearchContext(&b, item)

			case "folder":
				l.recordMouseRow(b.String(), i)
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
				l.renderSearchContext(&b, item)

			case "workspace":
				l.recordMouseRow(b.String(), i)
				cursor := choicePrefix(l.mobileMode, l.visibleOrdinal(i, start), i == l.cursor)
				style := normalStyle
				if i == l.cursor {
					style = selectedStyle
				}
				ws := item.Workspace
				name := style.Render(item.Name)

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
				l.renderSearchContext(&b, item)

			case "herdr_session":
				l.recordMouseRow(b.String(), i)
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
				l.renderSearchContext(&b, item)
			}
		}

		if start > 0 || end < len(l.items) {
			scrollInfo := fmt.Sprintf(" (%d/%d)", l.cursor+1, len(l.items))
			b.WriteString(mutedStyle.Render(scrollInfo))
			b.WriteString("\n")
		}
	}
	if l.search.Truncated() || l.searchSourceTruncated {
		b.WriteString(mutedStyle.Render("Results truncated to the configured search limit."))
		b.WriteString("\n")
	}
	if notice := l.searchSourceNotice(); notice != "" {
		b.WriteString(mutedStyle.Render(notice))
		b.WriteString("\n")
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
	if l.mobileMode && !l.search.InQueryFocus() {
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
	} else if l.search.InQueryFocus() {
		help = "[type]search  [↓]browse  [enter]open  [esc]clear/back"
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
	padding := lipgloss.NewStyle().Padding(1, 2)
	contentHeight := l.height - 2
	if l.compact() {
		padding = lipgloss.NewStyle().Padding(0, 1)
		contentHeight = l.height
	}
	if l.hubNotice != "" {
		help = l.hubNotice + "\n" + help
	}
	content := renderScreenWithFooter(b.String(), helpStyle.Render(help), contentHeight)
	return padding.Render(content)
}

func (l *ListView) searchSourceNotice() string {
	if l.search.Query() == "" {
		return ""
	}
	states := make([]string, 0)
	if l.herdrSessions != nil && l.localSessionsErr != nil {
		states = append(states, "This Mac offline")
	}
	byEndpoint := make(map[string]herdrhub.Snapshot, len(l.hubSnapshots))
	for _, snapshot := range l.hubSnapshots {
		byEndpoint[snapshot.EndpointID] = snapshot
	}
	for _, endpoint := range l.hubEndpointOrder {
		if endpoint.ID == herdrhub.LocalEndpointID {
			continue
		}
		snapshot, ok := byEndpoint[endpoint.Key()]
		if !ok {
			states = append(states, endpoint.Label+" undiscovered/loading")
			continue
		}
		if snapshot.State != herdrhub.StateOnline {
			state := snapshot.State
			if state == "" {
				state = herdrhub.StateLoading
			}
			states = append(states, endpoint.Label+" "+string(state))
		}
	}
	if len(states) == 0 {
		return ""
	}
	return "Known data incomplete: " + strings.Join(states, "; ")
}

func (l *ListView) renderSearchContext(builder *strings.Builder, item ListItem) {
	if l.search.Query() == "" {
		return
	}
	context := item.MatchedBy
	if context == "" {
		context = item.Breadcrumb
	}
	if context != "" {
		builder.WriteString("    ")
		builder.WriteString(mutedStyle.Render(context))
		builder.WriteString("\n")
	}
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
		if l.compact() {
			agentSummary := "loading…"
			if known && !l.hubAgentLoading[key] {
				agentSummary = fmt.Sprintf("%d", len(agents))
			}
			if l.hubAgentErr[key] != nil {
				agentSummary = "unavailable"
			}
			return labelStyle.Render("Usage") + mutedStyle.Render("  CPU/RAM/age n/a") + "\n" +
				labelStyle.Render("Agents") + mutedStyle.Render("  "+agentSummary)
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
