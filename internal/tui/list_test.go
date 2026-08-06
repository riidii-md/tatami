package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/workspace"
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
