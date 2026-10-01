package tui

import (
	"sort"
	"strings"
	"unicode"

	"github.com/OleksandrBesan/tatami/internal/docker"
	"github.com/OleksandrBesan/tatami/internal/git"
	appsearch "github.com/OleksandrBesan/tatami/internal/search"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// WorktreeMode represents the current mode of the worktree view
type WorktreeMode int

const (
	WorktreeModeList WorktreeMode = iota
	WorktreeModeCreate
	WorktreeModeConfirmDelete
)

// WorktreeView displays the worktree picker
type WorktreeView struct {
	repoPath        string
	worktrees       []git.Worktree
	branches        []string
	cursor          int
	mode            WorktreeMode
	search          searchController
	repository      string
	branchInput     textinput.Model
	suggestions     []string
	suggCursor      int
	deleteIndex     int
	dockerResources *docker.Resources
	errorMsg        string
	selected        *git.Worktree
	mobileMode      bool
}

// SetMobileMode enables numbered, compact menu rendering.
func (w *WorktreeView) SetMobileMode(enabled bool) {
	w.mobileMode = enabled
}

// NewWorktreeView creates a new worktree view
func NewWorktreeView(repoPath string) *WorktreeView {
	branchInput := textinput.New()
	branchInput.Placeholder = "branch-name"
	branchInput.CharLimit = 100
	branchInput.Width = 40
	branchInput.Focus()

	v := &WorktreeView{
		repoPath:    repoPath,
		search:      newSearchController("branches, paths, commits, repository"),
		branchInput: branchInput,
		mode:        WorktreeModeList,
	}
	v.refresh()
	return v
}

// refresh reloads worktrees and branches
func (w *WorktreeView) refresh() {
	worktrees, err := git.ListWorktrees(w.repoPath)
	if err != nil {
		w.errorMsg = err.Error()
		return
	}
	w.worktrees = worktrees

	branches, err := git.ListBranches(w.repoPath)
	if err == nil {
		w.branches = branches
	}
	w.rebuildSearch()
}

// Selected returns the selected worktree (nil if creating new)
func (w *WorktreeView) Selected() *git.Worktree {
	return w.selected
}

// Mode returns the current mode
func (w *WorktreeView) Mode() WorktreeMode {
	return w.mode
}

// IsFiltering reports whether the worktree filter is active.
func (w *WorktreeView) IsFiltering() bool {
	return w.search.InQueryFocus()
}

func (w *WorktreeView) clearFilter() {
	w.search.Clear()
	w.cursor = 0
}

func (w *WorktreeView) beginCreate(branch string) {
	w.search.Clear()
	w.mode = WorktreeModeCreate
	w.branchInput.SetValue(branch)
	w.branchInput.CursorEnd()
	w.branchInput.Focus()
	w.updateSuggestions()
}

func (w *WorktreeView) activateSearchSelection() tea.Cmd {
	id, ok := w.search.ActiveDocumentID()
	if !ok {
		return nil
	}
	if id == worktreeCreateSearchID {
		w.beginCreate(strings.TrimSpace(w.search.Query()))
		return nil
	}
	for index := range w.worktrees {
		if worktreeSearchID(w.worktrees[index]) == id {
			selected := w.worktrees[index]
			w.selected = &selected
			return tea.Quit
		}
	}
	return nil
}

// Update handles input for the worktree view
func (w *WorktreeView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch w.mode {
		case WorktreeModeList:
			return w.updateList(msg)
		case WorktreeModeCreate:
			return w.updateCreate(msg)
		case WorktreeModeConfirmDelete:
			return w.updateConfirmDelete(msg)
		}
	}
	return nil
}

func (w *WorktreeView) updateList(msg tea.KeyMsg) tea.Cmd {
	previousQuery := w.search.Query()
	event, cmd := w.search.Update(msg)
	if w.search.Query() != previousQuery {
		w.rebuildSearch()
	}
	w.cursor = w.search.ActiveIndex()
	if event.Consumed {
		if event.Activate {
			return w.activateSearchSelection()
		}
		return cmd
	}

	if w.mobileMode {
		rowHeight := 1
		if w.search.Query() != "" {
			rowHeight = 2
		}
		if event := w.search.SelectVisibleChoice(msg.String(), 9, rowHeight); event.Consumed {
			w.cursor = w.search.ActiveIndex()
			return nil
		}
	}
	switch msg.String() {
	case "j":
		w.search.Move(1)
	case "k":
		w.search.Move(-1)
	case "g":
		w.search.SelectFirst()
	case "G":
		w.search.SelectLast()
	case "d":
		if id, ok := w.search.ActiveDocumentID(); ok && id != worktreeCreateSearchID {
			for index := range w.worktrees {
				if worktreeSearchID(w.worktrees[index]) == id && !w.worktrees[index].IsMain {
					w.deleteIndex = index
					w.dockerResources = docker.FindResources(w.worktrees[index].Path)
					w.mode = WorktreeModeConfirmDelete
					break
				}
			}
		}
	}
	w.cursor = w.search.ActiveIndex()
	return nil
}

const worktreeCreateSearchID appsearch.ID = "worktree:create"

func worktreeSearchID(worktree git.Worktree) appsearch.ID {
	return appsearch.ID("worktree:" + worktree.Path)
}

func (w *WorktreeView) rebuildSearch() {
	documents := make([]appsearch.Document, 0, len(w.worktrees)+1)
	emptyRows := make([]searchRow, 0, len(w.worktrees)+1)
	for index, worktree := range w.worktrees {
		id := worktreeSearchID(worktree)
		branch := worktree.Branch
		if branch == "" {
			branch = "(detached)"
		}
		fields := []appsearch.Field{
			{Name: "path", Value: worktree.Path, Class: appsearch.SecondaryField},
			{Name: "commit", Value: shortCommit(worktree.Commit), Class: appsearch.MetadataField},
			{Name: "repository", Value: w.repository, Class: appsearch.MetadataField},
		}
		if worktree.IsMain {
			fields = append(fields, appsearch.Field{Name: "status", Value: "main current", Class: appsearch.MetadataField})
		}
		documents = append(documents, appsearch.Document{ID: id, Kind: "worktree", Primary: branch, Secondary: worktree.Path, Fields: fields, Ordinal: index})
		emptyRows = append(emptyRows, searchRow{RowID: id, DocumentID: id})
	}
	emptyRows = append(emptyRows, searchRow{RowID: worktreeCreateSearchID, DocumentID: worktreeCreateSearchID})
	query := strings.TrimSpace(w.search.Query())
	if validWorktreeBranchQuery(query) && !w.existingBranch(query) {
		documents = append(documents, appsearch.Document{
			ID:      worktreeCreateSearchID,
			Kind:    "worktree-create",
			Primary: "+ Create new worktree",
			Fields:  []appsearch.Field{{Name: "branch", Value: query, Class: appsearch.MetadataField}},
			Ordinal: len(documents),
		})
	}
	_ = w.search.ReplaceDocuments(w.search.generation+1, documents, emptyRows)
	w.cursor = w.search.ActiveIndex()
}

func (w *WorktreeView) existingBranch(branch string) bool {
	for _, worktree := range w.worktrees {
		if worktree.Branch == branch {
			return true
		}
	}
	return false
}

func validWorktreeBranchQuery(branch string) bool {
	if branch == "" || branch == "@" || branch == "HEAD" || strings.HasPrefix(branch, "-") || strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".") {
		return false
	}
	if strings.Contains(branch, "..") || strings.Contains(branch, "//") || strings.Contains(branch, "@{") || strings.ContainsAny(branch, "~^:?*[\\") {
		return false
	}
	if strings.IndexFunc(branch, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return false
	}
	for _, component := range strings.Split(branch, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

func (w *WorktreeView) SetRepositoryIdentity(repository string) {
	w.repository = repository
	w.rebuildSearch()
}

func (w *WorktreeView) updateCreate(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		w.mode = WorktreeModeList
		w.errorMsg = ""
		return nil
	case "tab", "down":
		// Cycle through suggestions
		if len(w.suggestions) > 0 {
			w.suggCursor = (w.suggCursor + 1) % len(w.suggestions)
			w.branchInput.SetValue(w.suggestions[w.suggCursor])
			w.branchInput.CursorEnd()
		}
		return nil
	case "shift+tab", "up":
		// Cycle through suggestions backwards
		if len(w.suggestions) > 0 {
			w.suggCursor = (w.suggCursor - 1 + len(w.suggestions)) % len(w.suggestions)
			w.branchInput.SetValue(w.suggestions[w.suggCursor])
			w.branchInput.CursorEnd()
		}
		return nil
	case "enter":
		branch := strings.TrimSpace(w.branchInput.Value())
		if branch == "" {
			w.errorMsg = "Branch name is required"
			return nil
		}

		// Check if worktree already exists for this branch
		for _, wt := range w.worktrees {
			if wt.Branch == branch {
				w.errorMsg = "Worktree for this branch already exists"
				return nil
			}
		}

		// Create the worktree
		wt, err := git.CreateWorktree(w.repoPath, branch)
		if err != nil {
			w.errorMsg = "Failed to create worktree: " + err.Error()
			return nil
		}

		w.selected = &wt
		return tea.Quit
	}

	// Update text input
	var cmd tea.Cmd
	w.branchInput, cmd = w.branchInput.Update(msg)
	w.updateSuggestions()
	return cmd
}

func (w *WorktreeView) updateConfirmDelete(msg tea.KeyMsg) tea.Cmd {
	hasDocker := w.dockerResources != nil && w.dockerResources.HasResources()

	switch msg.String() {
	case "y", "Y":
		wt := w.worktrees[w.deleteIndex]
		// When Docker resources exist, [y] deletes worktree + cleans Docker
		if hasDocker {
			docker.Cleanup(wt.Path, w.dockerResources)
		}
		if err := git.RemoveWorktree(w.repoPath, wt.Path); err != nil {
			w.errorMsg = "Failed to remove worktree: " + err.Error()
		}
		w.dockerResources = nil
		w.refresh()
		if w.cursor >= len(w.worktrees) {
			w.cursor = len(w.worktrees)
		}
		w.mode = WorktreeModeList
	case "w", "W":
		// Worktree only (skip Docker cleanup)
		if hasDocker {
			wt := w.worktrees[w.deleteIndex]
			if err := git.RemoveWorktree(w.repoPath, wt.Path); err != nil {
				w.errorMsg = "Failed to remove worktree: " + err.Error()
			}
			w.dockerResources = nil
			w.refresh()
			if w.cursor >= len(w.worktrees) {
				w.cursor = len(w.worktrees)
			}
			w.mode = WorktreeModeList
		}
	case "n", "N", "esc":
		w.dockerResources = nil
		w.mode = WorktreeModeList
	}
	return nil
}

func (w *WorktreeView) updateSuggestions() {
	input := strings.ToLower(w.branchInput.Value())
	w.suggestions = nil
	w.suggCursor = 0

	if input == "" {
		// Show all branches when input is empty
		w.suggestions = w.branches
		return
	}

	// Filter branches that contain the input
	var matches []string
	for _, branch := range w.branches {
		if strings.Contains(strings.ToLower(branch), input) {
			matches = append(matches, branch)
		}
	}

	// Sort: exact prefix matches first
	sort.Slice(matches, func(i, j int) bool {
		iPre := strings.HasPrefix(strings.ToLower(matches[i]), input)
		jPre := strings.HasPrefix(strings.ToLower(matches[j]), input)
		if iPre != jPre {
			return iPre
		}
		return matches[i] < matches[j]
	})

	w.suggestions = matches
}

// View renders the worktree view
func (w *WorktreeView) View() string {
	switch w.mode {
	case WorktreeModeCreate:
		return w.viewCreate()
	case WorktreeModeConfirmDelete:
		return w.viewConfirmDelete()
	default:
		return w.viewList()
	}
}

func (w *WorktreeView) viewList() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Git Worktrees"))
	b.WriteString("\n")
	b.WriteString(w.search.QueryView())
	b.WriteString("\n\n")

	if len(w.worktrees) == 0 && w.errorMsg != "" {
		b.WriteString(errorStyle.Render(w.errorMsg))
		b.WriteString("\n\n")
	}
	if strings.TrimSpace(w.search.Query()) != "" && len(w.search.Rows()) == 0 {
		b.WriteString(mutedStyle.Render("No matching worktrees."))
		b.WriteString("\n")
	}

	rowHeight := 1
	if w.search.Query() != "" {
		rowHeight = 2
	}
	start, rows := w.search.VisibleRows(9, rowHeight)
	for visibleIndex, row := range rows {
		i := start + visibleIndex
		cursor := choicePrefix(w.mobileMode, visibleIndex, i == w.cursor)
		style := normalStyle
		if i == w.cursor {
			style = selectedStyle
		}
		if row.DocumentID == worktreeCreateSearchID {
			label := "+ Create new worktree"
			if query := strings.TrimSpace(w.search.Query()); query != "" {
				label += " · " + query
			}
			b.WriteString(cursor + style.Render(label) + "\n")
			continue
		}
		wt := w.worktreeByID(row.DocumentID)
		if wt == nil {
			continue
		}

		branch := wt.Branch
		if branch == "" {
			branch = "(detached)"
		}

		label := style.Render(branch)
		if wt.IsMain {
			label += mutedStyle.Render(" (main)")
		}
		b.WriteString(cursor + label + "\n")
		if w.search.Query() != "" {
			if match, ok := w.search.MatchFor(row.DocumentID); ok && match.MatchedField != "" {
				b.WriteString("    " + mutedStyle.Render(match.MatchedField+": "+match.MatchedValue) + "\n")
			}
		}
	}
	if w.search.Truncated() {
		b.WriteString(mutedStyle.Render("Results truncated."))
		b.WriteString("\n")
	}

	help := "\n[type]search  [↓]browse  [enter]open/create  [esc]clear/back"
	if !w.search.InQueryFocus() {
		help = "\n[↑↓]select  [enter]open/create  [d]delete  [/]search  [esc]back"
	} else if w.mobileMode {
		help = "\n[type]search  [↓]browse  [enter]open/create  [esc]clear/back"
	}
	b.WriteString(helpStyle.Render(help))

	return renderPanel(b.String(), w.mobileMode)
}

func (w *WorktreeView) worktreeByID(id appsearch.ID) *git.Worktree {
	for index := range w.worktrees {
		if worktreeSearchID(w.worktrees[index]) == id {
			return &w.worktrees[index]
		}
	}
	return nil
}

func (w *WorktreeView) viewCreate() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Create Worktree"))
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("Branch Name"))
	b.WriteString("\n")
	b.WriteString(w.branchInput.View())
	b.WriteString("\n")

	// Show suggestions
	if len(w.suggestions) > 0 {
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("Suggestions:"))
		b.WriteString("\n")
		maxShow := 5
		for i, sugg := range w.suggestions {
			if i >= maxShow {
				b.WriteString(mutedStyle.Render("  ..."))
				break
			}
			prefix := "  "
			style := mutedStyle
			if i == w.suggCursor {
				prefix = "> "
				style = normalStyle
			}
			b.WriteString(prefix + style.Render(sugg) + "\n")
		}
	}

	if w.errorMsg != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(w.errorMsg))
		b.WriteString("\n")
	}

	help := "\n[tab/arrows]suggestions  [enter]create  [esc]cancel"
	b.WriteString(helpStyle.Render(help))

	return renderPanel(b.String(), w.mobileMode)
}

func (w *WorktreeView) viewConfirmDelete() string {
	var b strings.Builder

	wt := w.worktrees[w.deleteIndex]
	b.WriteString(titleStyle.Render("Delete Worktree"))
	b.WriteString("\n\n")
	b.WriteString("Delete worktree for branch ")
	b.WriteString(selectedStyle.Render(wt.Branch))
	b.WriteString("?\n\n")
	b.WriteString(mutedStyle.Render("Path: " + wt.Path))

	hasDocker := w.dockerResources != nil && w.dockerResources.HasResources()
	if hasDocker {
		b.WriteString("\n\n")
		b.WriteString(labelStyle.Render("Docker resources found:"))
		b.WriteString("\n")
		if len(w.dockerResources.Containers) > 0 {
			b.WriteString("  Containers: ")
			b.WriteString(normalStyle.Render(strings.Join(w.dockerResources.Containers, ", ")))
			b.WriteString("\n")
		}
		if len(w.dockerResources.Volumes) > 0 {
			b.WriteString("  Volumes:    ")
			b.WriteString(normalStyle.Render(strings.Join(w.dockerResources.Volumes, ", ")))
			b.WriteString("\n")
		}
		if len(w.dockerResources.Networks) > 0 {
			b.WriteString("  Networks:   ")
			b.WriteString(normalStyle.Render(strings.Join(w.dockerResources.Networks, ", ")))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString("[y]es (worktree + docker)  [w]orktree only  [n]o")
	} else {
		b.WriteString("\n\n")
		b.WriteString("[y]es  [n]o")
	}

	return renderPanel(b.String(), w.mobileMode)
}
