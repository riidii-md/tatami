package tui

import (
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/herdrhub"
	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	"github.com/charmbracelet/lipgloss"
)

func TestMobileHubRendersEndpointAndRemoteSession(t *testing.T) {
	store := newTestStore(t, &workspace.Workspace{Name: "local", Path: t.TempDir()})
	view := NewListViewWithHerdrSessions(store, func() ([]shell.HerdrSession, error) { return nil, nil })
	view.SetHerdrHubSnapshots([]herdrhub.Endpoint{{ID: "work", Label: "Work", Target: "work"}}, []herdrhub.Snapshot{{EndpointID: "work", Sessions: []herdrhub.Session{{SessionKey: herdrhub.SessionKey{EndpointID: "work", SessionName: "same"}, Running: true}}}})
	view.SetMobileMode(true)
	view.SetSize(24, 20)
	got := view.View()
	if !strings.Contains(got, "Work") || !strings.Contains(got, "same") {
		t.Fatalf("mobile hub=%s", got)
	}
}

func TestNumberKeyIndexAcceptsOnlyVisibleOneThroughNineChoices(t *testing.T) {
	tests := []struct {
		key   string
		count int
		index int
		ok    bool
	}{
		{key: "1", count: 3, index: 0, ok: true},
		{key: "3", count: 3, index: 2, ok: true},
		{key: "4", count: 3, ok: false},
		{key: "0", count: 9, ok: false},
		{key: "x", count: 9, ok: false},
		{key: "10", count: 10, ok: false},
	}

	for _, test := range tests {
		index, ok := numberKeyIndex(test.key, test.count)
		if index != test.index || ok != test.ok {
			t.Errorf("numberKeyIndex(%q, %d) = (%d, %v); want (%d, %v)", test.key, test.count, index, ok, test.index, test.ok)
		}
	}
}

func TestMobileChoicePrefixShowsNumberWithoutChangingDesktopPrefix(t *testing.T) {
	if got := choicePrefix(true, 1, true); got != "> [2] " {
		t.Fatalf("mobile selected prefix = %q; want %q", got, "> [2] ")
	}
	if got := choicePrefix(false, 1, true); got != "> " {
		t.Fatalf("desktop selected prefix = %q; want %q", got, "> ")
	}
	if got := choicePrefix(true, 9, false); strings.Contains(got, "[") {
		t.Fatalf("tenth mobile choice unexpectedly received a number: %q", got)
	}
}

func TestMobilePanelDropsDecorativeBorder(t *testing.T) {
	mobile := renderPanel("content", true)
	desktop := renderPanel("content", false)
	if strings.Contains(mobile, "╭") || strings.Contains(mobile, "╰") {
		t.Fatalf("mobile panel still contains a decorative border: %q", mobile)
	}
	if !strings.Contains(desktop, "╭") || !strings.Contains(desktop, "╰") {
		t.Fatalf("desktop panel lost its border: %q", desktop)
	}
}

func TestCompactRemoteUsageFitsNarrowScreen(t *testing.T) {
	session := &shell.HerdrSession{Name: "remote", Running: true}
	endpoint := &herdrhub.Endpoint{ID: "mac", Label: "Mac", Target: "mac"}
	view := &ListView{
		items:      []ListItem{{Type: "herdr_session", Herdr: session, Endpoint: endpoint}},
		width:      24,
		mobileMode: true,
	}

	for _, line := range strings.Split(view.herdrUsageView(), "\n") {
		if got := lipgloss.Width(line); got > 22 {
			t.Fatalf("compact usage line width = %d; want at most 22 columns: %q", got, line)
		}
	}
}
