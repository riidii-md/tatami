package tui

import (
	"sort"
	"strings"

	"github.com/OleksandrBesan/tatami/internal/docker"
	"github.com/OleksandrBesan/tatami/internal/git"
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
	filter          textinput.Model
	filtering       bool
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
	filter := textinput.New()
	filter.Placeholder = "Filter worktrees..."
	filter.CharLimit = 100
	filter.Width = 40

	branchInput := textinput.New()
	branchInput.Placeholder = "branch-name"
	branchInput.CharLimit = 100
	branchInput.Width = 40
	branchInput.Focus()

	v := &WorktreeView{
		repoPath:    repoPath,
		filter:      filter,
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
	return w.filtering
}

func (w *WorktreeView) clearFilter() {
	w.filtering = false
	w.filter.Blur()
	w.filter.SetValue("")
	w.cursor = 0
}

func (w *WorktreeView) filteredWorktreeIndexes() []int {
	query := strings.ToLower(strings.TrimSpace(w.filter.Value()))
	indexes := make([]int, 0, len(w.worktrees))
	for i, wt := range w.worktrees {
		if query == "" || strings.Contains(strings.ToLower(wt.Branch), query) || strings.Contains(strings.ToLower(wt.Path), query) {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func (w *WorktreeView) clampListCursor(worktreeCount int) {
	if w.cursor < 0 {
		w.cursor = 0
	}
	if w.cursor > worktreeCount {
		w.cursor = worktreeCount
	}
}

func (w *WorktreeView) beginCreate(branch string) {
	w.filtering = false
	w.filter.Blur()
	w.filter.SetValue("")
	w.mode = WorktreeModeCreate
	w.branchInput.SetValue(branch)
	w.branchInput.CursorEnd()
	w.branchInput.Focus()
	w.updateSuggestions()
}

func (w *WorktreeView) selectListItem(indexes []int) tea.Cmd {
	if w.cursor >= len(indexes) {
		branch := ""
		if w.filtering {
			branch = strings.TrimSpace(w.filter.Value())
		}
		w.beginCreate(branch)
		return nil
	}

	wt := w.worktrees[indexes[w.cursor]]
	w.selected = &wt
	return tea.Quit
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
	indexes := w.filteredWorktreeIndexes()
	if w.filtering {
		switch msg.String() {
		case "esc":
			w.clearFilter()
			return nil
		case "down", "ctrl+n":
			if w.cursor < len(indexes) {
				w.cursor++
			}
			return nil
		case "up", "ctrl+p":
			if w.cursor > 0 {
				w.cursor--
			}
			return nil
		case "enter":
			return w.selectListItem(indexes)
		}

		previous := w.filter.Value()
		var cmd tea.Cmd
		w.filter, cmd = w.filter.Update(msg)
		if w.filter.Value() != previous {
			w.cursor = 0
		}
		w.clampListCursor(len(w.filteredWorktreeIndexes()))
		return cmd
	}

	if w.mobileMode {
		if index, ok := numberKeyIndex(msg.String(), len(indexes)+1); ok {
			w.cursor = index
			return nil
		}
	}
	switch msg.String() {
	case "j", "down":
		// +1 for "Create new" option
		if w.cursor < len(indexes) {
			w.cursor++
		}
	case "k", "up":
		if w.cursor > 0 {
			w.cursor--
		}
	case "g":
		w.cursor = 0
	case "G":
		w.cursor = len(indexes) // "Create new" option
	case "/":
		w.filtering = true
		w.filter.SetValue("")
		w.filter.Focus()
		w.cursor = 0
	case "d":
		// Delete only non-main worktrees
		if w.cursor < len(indexes) && !w.worktrees[indexes[w.cursor]].IsMain {
			w.deleteIndex = indexes[w.cursor]
			w.dockerResources = docker.FindResources(w.worktrees[w.deleteIndex].Path)
			w.mode = WorktreeModeConfirmDelete
		}
	case "enter":
		return w.selectListItem(indexes)
	}
	return nil
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
	indexes := w.filteredWorktreeIndexes()
	w.clampListCursor(len(indexes))

	b.WriteString(titleStyle.Render("Git Worktrees"))
	b.WriteString("\n\n")
	if w.filtering {
		b.WriteString(w.filter.View())
		b.WriteString("\n\n")
	}

	if len(w.worktrees) == 0 && w.errorMsg != "" {
		b.WriteString(errorStyle.Render(w.errorMsg))
		b.WriteString("\n\n")
	}
	if w.filtering && strings.TrimSpace(w.filter.Value()) != "" && len(indexes) == 0 {
		b.WriteString(mutedStyle.Render("No matching worktrees."))
		b.WriteString("\n")
	}

	// List existing worktrees
	for i, worktreeIndex := range indexes {
		wt := w.worktrees[worktreeIndex]
		cursor := choicePrefix(w.mobileMode, i, i == w.cursor)
		style := normalStyle
		if i == w.cursor {
			style = selectedStyle
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
	}

	// "Create new" option
	cursor := choicePrefix(w.mobileMode, len(indexes), w.cursor == len(indexes))
	style := normalStyle
	if w.cursor == len(indexes) {
		style = selectedStyle
	}
	b.WriteString(cursor + style.Render("+ Create new worktree") + "\n")

	help := "\n[enter]select  [d]delete  [/]filter  [esc]back"
	if w.filtering {
		help = "\n[↑↓]select  [enter]open/create  [esc]clear"
	} else if w.mobileMode {
		help = "\n[↑↓/1-9]select  [enter]open  [d]delete  [/]filter  [b]back"
	}
	b.WriteString(helpStyle.Render(help))

	return renderPanel(b.String(), w.mobileMode)
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
