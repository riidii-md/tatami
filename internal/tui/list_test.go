package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

func TestHomeGroupsTatamiProjectsBeforeSeparateHerdrSessions(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{
		Name:        "pinned-project",
		Path:        "/tmp/pinned-project",
		Folder:      "team",
		QuickAccess: true,
	})
	if err := store.Create(&workspace.Workspace{Name: "root-project", Path: "/tmp/root-project"}); err != nil {
		t.Fatalf("create root workspace: %v", err)
	}

	list := NewListViewWithHerdrSessions(store, func() ([]shell.HerdrSession, error) {
		return []shell.HerdrSession{{Name: "default", Default: true}}, nil
	})

	got := make([]string, 0, len(list.items))
	for _, item := range list.items {
		got = append(got, item.Type+":"+item.Name)
	}
	want := []string{
		"header:Quick Access",
		"workspace:pinned-project",
		"header:Tatami Projects",
		"folder:team",
		"workspace:root-project",
		"header:Herdr Sessions",
		"herdr_session:default",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("home groups = %#v; want %#v", got, want)
	}

	list.SetSize(48, 40)
	view := list.View()
	if strings.Index(view, "Quick Access") > strings.Index(view, "Tatami Projects") ||
		strings.Index(view, "Tatami Projects") > strings.Index(view, "Herdr Sessions") {
		t.Fatalf("rendered home sections are out of order:\n%s", view)
	}
	if !strings.Contains(view, strings.Repeat("─", 44)) {
		t.Fatalf("Herdr section is missing its visual divider:\n%s", view)
	}
}

func TestMobileHomeNumbersSelectVisibleItemsAndUseCompactRows(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{
		Name: "first-project",
		Path: "/tmp/a-very-long-path/first-project",
	})
	if err := store.Create(&workspace.Workspace{
		Name: "second-project",
		Path: "/tmp/a-very-long-path/second-project",
	}); err != nil {
		t.Fatalf("create second workspace: %v", err)
	}

	list := NewListViewWithHerdrSessions(store, nil)
	list.SetMobileMode(true)
	list.SetSize(80, 24)

	view := list.View()
	if !strings.Contains(view, "[1]") || !strings.Contains(view, "[2]") {
		t.Fatalf("mobile list does not show numbered choices:\n%s", view)
	}
	if strings.Contains(view, "/tmp/a-very-long-path") {
		t.Fatalf("mobile list still renders project paths:\n%s", view)
	}

	list.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	selected := list.Selected()
	if selected == nil || selected.Name != "second-project" {
		t.Fatalf("mobile choice 2 selected %#v; want second-project", selected)
	}
}

func TestNarrowHomeAutomaticallyUsesCompactRows(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{
		Name: "project",
		Path: "/tmp/a-very-long-path/project",
	})
	list := NewListViewWithHerdrSessions(store, nil)
	list.SetSize(48, 24)

	view := list.View()
	if strings.Contains(view, "/tmp/a-very-long-path") {
		t.Fatalf("narrow list still renders project paths:\n%s", view)
	}
	if strings.Contains(view, "[1]") {
		t.Fatalf("narrow desktop list unexpectedly enabled mobile number keys:\n%s", view)
	}
}

func TestMobileHomeNumbersReferToCurrentVisiblePage(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "project-01", Path: "/tmp/project-01"})
	for i := 2; i <= 12; i++ {
		name := fmt.Sprintf("project-%02d", i)
		if err := store.Create(&workspace.Workspace{Name: name, Path: "/tmp/" + name}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	list := NewListViewWithHerdrSessions(store, nil)
	list.SetMobileMode(true)
	list.SetSize(80, 24)
	list.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	list.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})

	selected := list.Selected()
	if selected == nil || selected.Name != "project-04" {
		t.Fatalf("first choice on final page selected %#v; want project-04", selected)
	}
}
