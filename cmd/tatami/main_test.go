package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/agent"
	"github.com/OleksandrBesan/tatami/internal/config"
	"github.com/OleksandrBesan/tatami/internal/workspace"
)

func TestHandleCommandRunTracksProcessLifecycle(t *testing.T) {
	dir := t.TempDir()
	paths := testPaths(t, dir)
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(binDir, "fakeagent")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho fake-agent-ran\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ZELLIJ", "1")
	t.Setenv("ZELLIJ_PANE_ID", "terminal_9")

	var out, errOut bytes.Buffer
	if handled, err := handleTopLevelCommand([]string{"run", "fakeagent", "--hello"}, paths, &out, &errOut); !handled || err != nil {
		t.Fatalf("handled=%v err=%v stderr=%q", handled, err, errOut.String())
	}

	sessions, err := agent.NewStore(paths.AgentsFile).List()
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one tracked session, got %d", len(sessions))
	}
	got := sessions[0]
	if got.Agent != "fakeagent" || got.Status != agent.StatusExited || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("unexpected tracked session: %+v", got)
	}
	if got.Context.Mux != "zellij" || got.Context.ZellijPaneID != "terminal_9" {
		t.Fatalf("missing zellij context: %+v", got.Context)
	}
}

func TestHandleCommandAgentsListShowsTrackedSessions(t *testing.T) {
	dir := t.TempDir()
	paths := testPaths(t, dir)
	store := agent.NewStore(paths.AgentsFile)
	s := agent.NewSession("claude", []string{"--model", "sonnet"}, "/repo", agent.Context{Mux: "zellij", ZellijPaneID: "terminal_7"})
	s.PID = os.Getpid()
	if err := store.Create(s); err != nil {
		t.Fatalf("create session: %v", err)
	}

	var out, errOut bytes.Buffer
	if handled, err := handleTopLevelCommand([]string{"agents", "list"}, paths, &out, &errOut); !handled || err != nil {
		t.Fatalf("handled=%v err=%v stderr=%q", handled, err, errOut.String())
	}
	text := out.String()
	for _, want := range []string{"claude", "running", "zellij", "terminal_7", "/repo"} {
		if !strings.Contains(text, want) {
			t.Fatalf("agents list missing %q in:\n%s", want, text)
		}
	}
}

func TestHandleCommandDashboardRendersSections(t *testing.T) {
	dir := t.TempDir()
	paths := testPaths(t, dir)
	store := agent.NewStore(paths.AgentsFile)
	if err := store.Create(agent.NewSession("codex", nil, "/repo", agent.Context{Mux: "tmux", TmuxSession: "main", TmuxWindow: "1", TmuxPane: "%7"})); err != nil {
		t.Fatalf("create session: %v", err)
	}

	var out, errOut bytes.Buffer
	if handled, err := handleTopLevelCommand([]string{"dashboard"}, paths, &out, &errOut); !handled || err != nil {
		t.Fatalf("handled=%v err=%v stderr=%q", handled, err, errOut.String())
	}
	text := out.String()
	for _, want := range []string{"Tatami Dashboard", "AI Agents", "Workspaces", "Zellij Sessions", "Tmux Sessions", "Notifications / Agent Detail", "codex"} {
		if !strings.Contains(text, want) {
			t.Fatalf("dashboard missing %q in:\n%s", want, text)
		}
	}
}

func TestHandleCommandAgentsPruneMarksStale(t *testing.T) {
	dir := t.TempDir()
	paths := testPaths(t, dir)
	store := agent.NewStore(paths.AgentsFile)
	s := agent.NewSession("claude", nil, "/repo", agent.Context{})
	s.PID = -9
	if err := store.Create(s); err != nil {
		t.Fatalf("create session: %v", err)
	}

	var out, errOut bytes.Buffer
	if handled, err := handleTopLevelCommand([]string{"agents", "prune"}, paths, &out, &errOut); !handled || err != nil {
		t.Fatalf("handled=%v err=%v stderr=%q", handled, err, errOut.String())
	}
	if !strings.Contains(out.String(), "marked 1 stale") {
		t.Fatalf("unexpected prune output: %q", out.String())
	}
	got, err := store.Get(s.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.Status != agent.StatusStale {
		t.Fatalf("status=%q, want stale", got.Status)
	}
}

func TestTemplatesWrapKnownAgentCommands(t *testing.T) {
	for _, tmpl := range workspace.GetTemplates() {
		if isKnownAgentCommand(tmpl.MainCmd) && !strings.HasPrefix(tmpl.MainCmd, "tatami run ") {
			t.Fatalf("template %s main command is not wrapped: %q", tmpl.Name, tmpl.MainCmd)
		}
		for _, pane := range tmpl.Panes {
			if isKnownAgentCommand(pane.Command) && !strings.HasPrefix(pane.Command, "tatami run ") {
				t.Fatalf("template %s pane command is not wrapped: %q", tmpl.Name, pane.Command)
			}
		}
	}
}

func testPaths(t *testing.T, dir string) *config.Paths {
	t.Helper()
	paths := &config.Paths{
		ConfigDir:      filepath.Join(dir, "tatami"),
		WorkspacesFile: filepath.Join(dir, "tatami", "workspaces.json"),
		AgentsFile:     filepath.Join(dir, "tatami", "agents.json"),
	}
	if err := os.MkdirAll(paths.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"workspaces": []any{}})
	if err := os.WriteFile(paths.WorkspacesFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	return paths
}
