package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreCreatesUpdatesAndListsNewestFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	store := NewStore(path)

	first := NewSession("claude", []string{"--dangerously-skip-permissions"}, "/repo", Context{Mux: "zellij", ZellijPaneID: "pane-1", TTY: "/dev/pts/1"})
	first.StartedAt = time.Unix(10, 0).UTC()
	first.PID = 111
	if err := store.Create(first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	second := NewSession("codex", nil, "/repo2", Context{Mux: "tmux", TmuxSession: "main", TmuxWindow: "1", TmuxPane: "%7"})
	second.StartedAt = time.Unix(20, 0).UTC()
	second.PID = 222
	if err := store.Create(second); err != nil {
		t.Fatalf("create second: %v", err)
	}

	first.Status = StatusExited
	first.ExitCode = intPtr(0)
	ended := time.Unix(30, 0).UTC()
	first.EndedAt = &ended
	if err := store.Update(first); err != nil {
		t.Fatalf("update first: %v", err)
	}

	got, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(got))
	}
	if got[0].Agent != "codex" || got[1].Agent != "claude" {
		t.Fatalf("expected newest-first order [codex claude], got [%s %s]", got[0].Agent, got[1].Agent)
	}
	if got[1].Status != StatusExited || got[1].ExitCode == nil || *got[1].ExitCode != 0 || got[1].EndedAt == nil {
		t.Fatalf("updated exited session not persisted: %+v", got[1])
	}
}

func TestDetectContextReadsZellijAndTmuxEnvironment(t *testing.T) {
	t.Setenv("ZELLIJ", "1")
	t.Setenv("ZELLIJ_SESSION_NAME", "dev")
	t.Setenv("ZELLIJ_PANE_ID", "terminal_7")
	t.Setenv("TMUX", "")

	ctx := DetectContext()
	if ctx.Mux != "zellij" || ctx.ZellijSession != "dev" || ctx.ZellijPaneID != "terminal_7" {
		t.Fatalf("unexpected zellij context: %+v", ctx)
	}

	t.Setenv("ZELLIJ", "")
	t.Setenv("ZELLIJ_SESSION_NAME", "")
	t.Setenv("ZELLIJ_PANE_ID", "")
	t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
	t.Setenv("TATAMI_TMUX_SESSION", "main")
	t.Setenv("TATAMI_TMUX_WINDOW", "2")
	t.Setenv("TATAMI_TMUX_PANE", "%7")

	ctx = DetectContext()
	if ctx.Mux != "tmux" || ctx.TmuxSession != "main" || ctx.TmuxWindow != "2" || ctx.TmuxPane != "%7" {
		t.Fatalf("unexpected tmux context: %+v", ctx)
	}
}

func TestPruneStaleMarksMissingRunningProcessesStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	store := NewStore(path)

	s := NewSession("claude", nil, "/repo", Context{})
	s.PID = -424242
	if err := store.Create(s); err != nil {
		t.Fatalf("create: %v", err)
	}

	changed, err := store.PruneStale()
	if err != nil {
		t.Fatalf("prune stale: %v", err)
	}
	if changed != 1 {
		t.Fatalf("expected 1 stale session, got %d", changed)
	}
	got, err := store.Get(s.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusStale {
		t.Fatalf("expected stale, got %q", got.Status)
	}
}

func TestRegistryFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "agents.json")
	store := NewStore(path)
	if err := store.Create(NewSession("claude", nil, "/repo", Context{})); err != nil {
		t.Fatalf("create: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("registry permissions = %v, want 0600", info.Mode().Perm())
	}
}

func intPtr(v int) *int { return &v }
