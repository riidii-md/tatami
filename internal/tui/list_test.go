package tui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/systemusage"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

func TestListViewShowsCachedRemoteSessionsWithEndpointIdentity(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	view := NewListViewWithHerdrSessions(store, func() ([]shell.HerdrSession, error) { return nil, nil })
	view.SetHerdrHubSnapshots([]herdrhub.Endpoint{{ID: "work", Label: "Workbox", Target: "work"}}, []herdrhub.Snapshot{{EndpointID: "work", Sessions: []herdrhub.Session{{SessionKey: herdrhub.SessionKey{EndpointID: "work", SessionName: "same"}, Running: true}}}})
	view.SetSize(100, 40)
	if !strings.Contains(view.View(), "Herdr · Workbox") || !strings.Contains(view.View(), "same") {
		t.Fatalf("hub not rendered: %s", view.View())
	}
	view.filtering = true
	view.filter.SetValue("workbox")
	view.refreshItems()
	if got := view.Selected(); got == nil || got.Endpoint == nil || got.Endpoint.ID != "work" {
		t.Fatalf("filtered selection = %#v", got)
	}
}

func TestHubEndpointStatusShowsLatencyAndLastSuccessAge(t *testing.T) {
	now := time.Now()
	if got := hubEndpointStatus(herdrhub.Snapshot{State: herdrhub.StateOnline, Latency: 24 * time.Millisecond}); got != "online · 24ms" {
		t.Fatalf("online status = %q", got)
	}
	got := hubEndpointStatus(herdrhub.Snapshot{State: herdrhub.StateStale, LastSuccess: now.Add(-8 * time.Minute)})
	if !strings.Contains(got, "stale · last seen 8m") {
		t.Fatalf("stale status = %q", got)
	}
}

func TestHubAuthenticationNeededShowsPasswordlessSSHSetup(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	view := NewListViewWithHerdrSessions(store, func() ([]shell.HerdrSession, error) { return nil, nil })
	endpoint := herdrhub.Endpoint{ID: "macmini", Label: "Mac Mini", Target: "oles@bmo.local"}
	view.SetHerdrHubSnapshots([]herdrhub.Endpoint{endpoint}, []herdrhub.Snapshot{{EndpointID: endpoint.ID, State: herdrhub.StateAuthenticationNeeded}})
	view.SetSize(100, 40)

	for i, item := range view.items {
		if item.Type == "herdr_endpoint" && item.Endpoint != nil && item.Endpoint.ID == endpoint.ID {
			view.cursor = i
		}
	}
	got := view.View()
	for _, want := range []string{"Passwordless SSH required", "ssh-copy-id oles@bmo.local", "then [r] retry"} {
		if !strings.Contains(got, want) {
			t.Errorf("authentication guidance missing %q:\n%s", want, got)
		}
	}
}

func TestHubAuthenticationGuidanceDoesNotEchoUnsafeTarget(t *testing.T) {
	endpoint := &herdrhub.Endpoint{ID: "macmini", Label: "Mac Mini", Target: "host\nunsafe"}
	got := hubAuthenticationGuidance(endpoint, herdrhub.Snapshot{State: herdrhub.StateAuthenticationNeeded})
	if strings.Contains(got, endpoint.Target) || strings.Contains(got, "ssh-copy-id") {
		t.Fatalf("unsafe target exposed in authentication command: %q", got)
	}
}

func TestHubFilterIncludesLocalAndRemoteSessions(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	view := NewListViewWithHerdrSessions(store, func() ([]shell.HerdrSession, error) { return []shell.HerdrSession{{Name: "local-match"}}, nil })
	view.SetHerdrHubSnapshots([]herdrhub.Endpoint{{ID: "work", Label: "Work", Target: "work"}}, []herdrhub.Snapshot{{EndpointID: "work", Sessions: []herdrhub.Session{{SessionKey: herdrhub.SessionKey{EndpointID: "work", SessionName: "remote-match"}}}}})
	view.filtering = true
	view.filter.SetValue("match")
	view.refreshItems()
	if len(view.items) != 2 {
		t.Fatalf("filter items=%#v", view.items)
	}
}

func TestHubShowsLocalOfflineWhenLocalInventoryFails(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	view := NewListViewWithHerdrSessions(store, func() ([]shell.HerdrSession, error) {
		return nil, errors.New("socket unavailable")
	})
	view.SetHerdrHubSnapshots([]herdrhub.Endpoint{{ID: "work", Label: "Work", Target: "work"}}, nil)
	got := view.View()
	if !strings.Contains(got, "Herdr Hub") || !strings.Contains(got, "This Mac · offline") || !strings.Contains(got, "Work · loading") {
		t.Fatalf("failed local inventory hub=%s", got)
	}
}

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
		"header:Herdr Hub",
		"herdr_endpoint:▾ Herdr · This Mac · online",
		"herdr_session:default",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("home groups = %#v; want %#v", got, want)
	}

	list.SetSize(48, 40)
	view := list.View()
	if strings.Index(view, "Quick Access") > strings.Index(view, "Tatami Projects") ||
		strings.Index(view, "Tatami Projects") > strings.Index(view, "Herdr · This Mac") {
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

func TestHighlightedHerdrSessionRendersResourceSummaryAboveHelp(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "project", Path: "/tmp/project"})
	list := NewListViewWithHerdrSessions(store, func() ([]shell.HerdrSession, error) {
		return []shell.HerdrSession{{Name: "agentic", Running: true}}, nil
	})
	for i, item := range list.items {
		if item.Type == "herdr_session" {
			list.cursor = i
			break
		}
	}

	list.SetHerdrUsageLoading("agentic")
	if view := list.View(); !strings.Contains(view, "Usage  loading") {
		t.Fatalf("loading summary missing:\n%s", view)
	}

	list.SetHerdrUsage("agentic", &systemusage.SessionUsage{
		Name:         "agentic",
		Agents:       []systemusage.AgentUsage{{}, {}},
		CPUPercent:   37.5,
		RSSBytes:     1536 * 1024 * 1024,
		ProcessCount: 12,
		MaxAge:       2*time.Hour + 15*time.Minute,
	}, nil)
	view := list.View()
	for _, want := range []string{"Usage", "CPU 37.5%", "RAM 1.5 GiB", "PROCS 12", "AGENTS 2", "MAX AGE 2h15m"} {
		if !strings.Contains(view, want) {
			t.Errorf("resource summary missing %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "Usage") > strings.Index(view, "[enter]open") {
		t.Fatalf("resource summary is not above help:\n%s", view)
	}

	list.SetHerdrUsage("agentic", nil, fmt.Errorf("pane changed"))
	if view := list.View(); !strings.Contains(view, "Usage  unavailable: pane changed") {
		t.Fatalf("resource error missing:\n%s", view)
	}
}

func TestStoppedHerdrSessionRendersStoppedUsageWithoutLoadedSummary(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "project", Path: "/tmp/project"})
	list := NewListViewWithHerdrSessions(store, func() ([]shell.HerdrSession, error) {
		return []shell.HerdrSession{{Name: "old", Running: false}}, nil
	})
	for i, item := range list.items {
		if item.Type == "herdr_session" {
			list.cursor = i
			break
		}
	}

	if view := list.View(); !strings.Contains(view, "Usage  stopped") {
		t.Fatalf("stopped usage state missing:\n%s", view)
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
