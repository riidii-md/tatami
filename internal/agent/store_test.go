package agent

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStoreCreatesUpdatesAndListsNewestFirst(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")
	store := NewStore(dir)

	first := NewSession("claude", "/repo", Context{Mux: "zellij", ZellijPaneID: "pane-1", TTY: "/dev/pts/1"})
	first.StartedAt = time.Unix(10, 0).UTC()
	first.PID = 111
	if err := store.Create(first); err != nil {
		t.Fatalf("create first: %v", err)
	}
	second := NewSession("codex", "/repo2", Context{Mux: "tmux", TmuxSession: "main", TmuxWindow: "1", TmuxPane: "%7"})
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

func TestStorePreservesConcurrentCreates(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "agents"))
	const count = 32

	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session := NewSession("codex", "/repo", Context{Mux: "none"})
			session.PID = os.Getpid()
			errs <- store.Create(session)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent create: %v", err)
		}
	}

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != count {
		t.Fatalf("session count = %d, want %d", len(sessions), count)
	}
}

func TestStoreRejectsAmbiguousIDPrefix(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "agents"))
	for _, id := range []string{"shared-prefix-one", "shared-prefix-two"} {
		session := NewSession("claude", "/repo", Context{})
		session.ID = id
		if err := store.Create(session); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	if _, err := store.Get("shared-prefix"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("Get ambiguous prefix error = %v, want ambiguous error", err)
	}
	exact, err := store.Get("shared-prefix-one")
	if err != nil || exact.ID != "shared-prefix-one" {
		t.Fatalf("Get exact = %#v, %v", exact, err)
	}
}

func TestDetectContextReadsSupportedTerminalEnvironments(t *testing.T) {
	clearTerminalEnv(t)
	t.Setenv("ZELLIJ", "1")
	t.Setenv("ZELLIJ_SESSION_NAME", "dev")
	t.Setenv("ZELLIJ_PANE_ID", "terminal_7")

	ctx := DetectContext()
	if ctx.Mux != "zellij" || ctx.ZellijSession != "dev" || ctx.ZellijPaneID != "terminal_7" {
		t.Fatalf("unexpected zellij context: %+v", ctx)
	}

	clearTerminalEnv(t)
	t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
	t.Setenv("TATAMI_TMUX_SESSION", "main")
	t.Setenv("TATAMI_TMUX_WINDOW", "2")
	t.Setenv("TATAMI_TMUX_PANE", "%7")

	ctx = DetectContext()
	if ctx.Mux != "tmux" || ctx.TmuxSession != "main" || ctx.TmuxWindow != "2" || ctx.TmuxPane != "%7" {
		t.Fatalf("unexpected tmux context: %+v", ctx)
	}

	clearTerminalEnv(t)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SESSION", "tatami-project")
	ctx = DetectContext()
	if ctx.Mux != "herdr" || ctx.HerdrSession != "tatami-project" {
		t.Fatalf("unexpected Herdr context: %+v", ctx)
	}

	clearTerminalEnv(t)
	t.Setenv("KITTY_WINDOW_ID", "42")
	ctx = DetectContext()
	if ctx.Mux != "kitty" || ctx.KittyWindowID != "42" {
		t.Fatalf("unexpected Kitty context: %+v", ctx)
	}
}

func TestPruneStaleMarksMissingRunningProcessesStale(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "agents"))

	s := NewSession("claude", "/repo", Context{})
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

func TestCompletedExitWinsRaceWithStaleReconciliation(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "agents"))
	session := NewSession("claude", "/repo", Context{})
	session.PID = 4242
	if err := store.Create(session); err != nil {
		t.Fatalf("create: %v", err)
	}

	processCheckStarted := make(chan struct{})
	releaseProcessCheck := make(chan struct{})
	store.isProcessAlive = func(int) bool {
		close(processCheckStarted)
		<-releaseProcessCheck
		return false
	}
	pruneDone := make(chan error, 1)
	go func() {
		_, err := store.PruneStale()
		pruneDone <- err
	}()
	<-processCheckStarted

	exitCode := 0
	session.Status = StatusExited
	session.ExitCode = &exitCode
	ended := time.Now().UTC()
	session.EndedAt = &ended
	updateDone := make(chan error, 1)
	go func() { updateDone <- store.Update(session) }()
	close(releaseProcessCheck)
	if err := <-pruneDone; err != nil {
		t.Fatalf("prune: %v", err)
	}
	if err := <-updateDone; err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := store.Get(session.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusExited || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("final session = %+v, want completed exit", got)
	}
}

func TestRegistryDirectoryAndFilesArePrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "agents")
	store := NewStore(dir)
	session := NewSession("claude", "/repo", Context{})
	if err := store.Create(session); err != nil {
		t.Fatalf("create: %v", err)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat directory: %v", err)
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Fatalf("registry directory permissions = %v, want 0700", dirInfo.Mode().Perm())
	}
	fileInfo, err := os.Stat(filepath.Join(dir, session.ID+".json"))
	if err != nil {
		t.Fatalf("stat session: %v", err)
	}
	if fileInfo.Mode().Perm() != 0600 {
		t.Fatalf("session permissions = %v, want 0600", fileInfo.Mode().Perm())
	}
}

func TestSessionMetadataDoesNotContainCommandArguments(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")
	store := NewStore(dir)
	session := NewSession("claude", "/repo", Context{})
	if err := store.Create(session); err != nil {
		t.Fatalf("create: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, session.ID+".json"))
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	for _, field := range []string{"\"args\"", "\"command\""} {
		if strings.Contains(string(data), field) {
			t.Fatalf("session metadata contains sensitive command field %s: %s", field, data)
		}
	}
}

func TestStoreReportsInvalidAndMissingSessions(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "agents"))
	if store.Path() == "" {
		t.Fatal("store path is empty")
	}
	if err := store.Create(nil); err == nil {
		t.Fatal("Create(nil) succeeded")
	}
	if err := store.Update(nil); err == nil {
		t.Fatal("Update(nil) succeeded")
	}
	if _, err := store.Get(""); err == nil {
		t.Fatal("Get with empty id succeeded")
	}
	if _, err := store.Get("missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Get missing error = %v", err)
	}
	autoID := &Session{Agent: "claude", Cwd: "/repo", Status: StatusRunning, StartedAt: time.Now().UTC(), Context: Context{Mux: "none"}}
	if err := store.Create(autoID); err != nil {
		t.Fatalf("Create with generated id: %v", err)
	}
	if autoID.ID == "" {
		t.Fatal("Create did not generate a missing session id")
	}

	session := NewSession("claude", "/repo", Context{})
	if err := store.Create(session); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.Create(session); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate Create error = %v", err)
	}
	missing := NewSession("codex", "/repo", Context{})
	if err := store.Update(missing); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Update missing error = %v", err)
	}
	invalid := NewSession("codex", "/repo", Context{})
	invalid.ID = "../escape"
	if err := store.Create(invalid); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("Create invalid id error = %v", err)
	}
}

func TestStoreRejectsMalformedOrMismatchedSessionFiles(t *testing.T) {
	malformedDir := filepath.Join(t.TempDir(), "agents")
	if err := os.MkdirAll(malformedDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(malformedDir, "broken.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(malformedDir).List(); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("malformed List error = %v", err)
	}

	mismatchDir := filepath.Join(t.TempDir(), "agents")
	if err := os.MkdirAll(mismatchDir, 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"id":"different","agent":"claude","cwd":"/repo","pid":1,"status":"running","started_at":"2026-01-01T00:00:00Z","context":{"mux":"none"}}`)
	if err := os.WriteFile(filepath.Join(mismatchDir, "expected.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(mismatchDir).List(); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched List error = %v", err)
	}
}

func TestSessionIDFallbackNameAndProcessLiveness(t *testing.T) {
	session := NewSession("💥", "/repo", Context{})
	if !strings.Contains(session.ID, "-agent") {
		t.Fatalf("session ID = %q, want sanitized fallback name", session.ID)
	}
	if !processAlive(os.Getpid()) {
		t.Fatal("current process reported dead")
	}
	if processAlive(-1) {
		t.Fatal("negative process id reported alive")
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Fatalf("firstNonEmpty = %q", got)
	}
}

func clearTerminalEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"ZELLIJ", "ZELLIJ_SESSION_NAME", "ZELLIJ_PANE_ID",
		"TMUX", "TATAMI_TMUX_SESSION", "TATAMI_TMUX_WINDOW", "TATAMI_TMUX_PANE",
		"HERDR_ENV", "HERDR_SESSION", "KITTY_WINDOW_ID",
	} {
		t.Setenv(name, "")
	}
}

func intPtr(v int) *int { return &v }
