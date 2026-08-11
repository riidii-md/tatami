package tui

import (
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/git"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func TestWorktreeFilterNarrowsExistingWorktreesAndSelectsMatch(t *testing.T) {
	view := newFilterableWorktreeView([]git.Worktree{
		{Branch: "develop", Path: "/repo", IsMain: true},
		{Branch: "SA-2007-coa-file-separate-entity", Path: "/repo/.worktrees/sa-2007"},
		{Branch: "SA-2094-coa-raw-ql-dataset", Path: "/repo/.worktrees/sa-2094"},
		{Branch: "codex-support", Path: "/repo/.worktrees/codex-support"},
	})

	view.Update(keyMsg("/"))
	view.Update(textMsg("SA-2"))

	if !view.IsFiltering() {
		t.Fatal("slash did not activate worktree filtering")
	}
	rendered := view.View()
	for _, want := range []string{"SA-2007-coa-file-separate-entity", "SA-2094-coa-raw-ql-dataset", "+ Create new worktree"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("filtered view missing %q:\n%s", want, rendered)
		}
	}
	for _, unwanted := range []string{"develop", "codex-support"} {
		if strings.Contains(rendered, unwanted) {
			t.Fatalf("filtered view still contains %q:\n%s", unwanted, rendered)
		}
	}

	view.Update(keyMsg("down"))
	cmd := view.Update(keyMsg("enter"))
	if selected := view.Selected(); selected == nil || selected.Branch != "SA-2094-coa-raw-ql-dataset" {
		t.Fatalf("selected worktree = %#v, want SA-2094", selected)
	}
	if cmd == nil {
		t.Fatal("selecting a filtered worktree did not quit the picker")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("selection command = %T, want tea.QuitMsg", cmd())
	}
}

func TestWorktreeFilterMatchesPathCaseInsensitively(t *testing.T) {
	view := newFilterableWorktreeView([]git.Worktree{
		{Branch: "feature-one", Path: "/repo/.worktrees/SA-2294-COA-Assembly"},
		{Branch: "feature-two", Path: "/repo/.worktrees/unrelated"},
	})

	view.Update(keyMsg("/"))
	view.Update(textMsg("coa-assembly"))
	rendered := view.View()
	if !strings.Contains(rendered, "feature-one") || strings.Contains(rendered, "feature-two") {
		t.Fatalf("path-filtered view is incorrect:\n%s", rendered)
	}
}

func TestWorktreeFilterNoMatchOffersCreateWithQuery(t *testing.T) {
	view := newFilterableWorktreeView([]git.Worktree{{Branch: "develop", Path: "/repo", IsMain: true}})

	view.Update(keyMsg("/"))
	view.Update(textMsg("SA-2600-new-work"))
	rendered := view.View()
	for _, want := range []string{"No matching worktrees", "+ Create new worktree"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("empty filtered view missing %q:\n%s", want, rendered)
		}
	}

	view.Update(keyMsg("enter"))
	if view.Mode() != WorktreeModeCreate {
		t.Fatalf("mode = %v, want WorktreeModeCreate", view.Mode())
	}
	if got := view.branchInput.Value(); got != "SA-2600-new-work" {
		t.Fatalf("create branch input = %q, want filter query", got)
	}
}

func TestWorktreeFilterEscapeClearsBeforeLeavingPicker(t *testing.T) {
	view := newFilterableWorktreeView([]git.Worktree{
		{Branch: "develop", Path: "/repo", IsMain: true},
		{Branch: "codex-support", Path: "/repo/.worktrees/codex-support"},
	})
	view.Update(keyMsg("/"))
	view.Update(textMsg("codex"))
	app := &App{currentView: ViewWorktree, worktreeView: view}

	model, _ := app.updateWorktree(keyMsg("esc"))
	updated := model.(*App)
	if updated.currentView != ViewWorktree {
		t.Fatalf("first escape opened view %v, want ViewWorktree", updated.currentView)
	}
	if updated.worktreeView.IsFiltering() {
		t.Fatal("first escape did not clear worktree filter")
	}
	if rendered := updated.worktreeView.View(); !strings.Contains(rendered, "develop") || !strings.Contains(rendered, "codex-support") {
		t.Fatalf("cleared filter did not restore worktrees:\n%s", rendered)
	}

	model, _ = updated.updateWorktree(keyMsg("esc"))
	if got := model.(*App).currentView; got != ViewActions {
		t.Fatalf("second escape opened view %v, want ViewActions", got)
	}
}

func TestWorktreeFilterQueryChangeResetsSelection(t *testing.T) {
	view := newFilterableWorktreeView([]git.Worktree{
		{Branch: "SA-2007-first", Path: "/repo/one"},
		{Branch: "SA-2094-second", Path: "/repo/two"},
		{Branch: "SA-2294-third", Path: "/repo/three"},
	})
	view.Update(keyMsg("/"))
	view.Update(textMsg("SA-2"))
	view.Update(keyMsg("down"))
	view.Update(textMsg("294"))

	view.Update(keyMsg("enter"))
	if selected := view.Selected(); selected == nil || selected.Branch != "SA-2294-third" {
		t.Fatalf("selected worktree after narrowing = %#v, want SA-2294-third", selected)
	}
}

func TestMobileBackKeyRemainsTextInsideWorktreeFilter(t *testing.T) {
	view := newFilterableWorktreeView([]git.Worktree{{Branch: "develop", Path: "/repo", IsMain: true}})
	view.SetMobileMode(true)
	view.Update(keyMsg("/"))
	app := &App{currentView: ViewWorktree, worktreeView: view, mobileMode: true}

	model, _ := app.Update(keyMsg("b"))
	updated := model.(*App)
	if updated.currentView != ViewWorktree {
		t.Fatalf("typing b in mobile worktree filter navigated to view %v", updated.currentView)
	}
	if got := updated.worktreeView.filter.Value(); got != "b" {
		t.Fatalf("mobile worktree filter = %q, want b", got)
	}
}

func newFilterableWorktreeView(worktrees []git.Worktree) *WorktreeView {
	filter := textinput.New()
	filter.Placeholder = "Filter worktrees..."
	branchInput := textinput.New()
	return &WorktreeView{
		worktrees:   worktrees,
		mode:        WorktreeModeList,
		filter:      filter,
		branchInput: branchInput,
	}
}

func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

func textMsg(value string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}
