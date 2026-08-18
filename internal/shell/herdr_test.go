package shell

import (
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/workspace"
)

type recordedHerdrCommand struct {
	args []string
}

func TestHerdrCommandOutputPreservesStructuredStderr(t *testing.T) {
	cmd := exec.Command("sh", "-c", `printf '%s' '{"error":{"code":"agent_pane_busy"}}' >&2; exit 1`)
	_, err := herdrCommandOutput(cmd)
	if err == nil || !strings.Contains(err.Error(), "agent_pane_busy") {
		t.Fatalf("herdrCommandOutput error = %v; want structured stderr", err)
	}
}

func TestHerdrInteractiveCommandsIncludeRemoteAttach(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{args: []string{"--session", "local"}, want: true},
		{args: []string{"--remote", "workbox"}, want: true},
		{args: []string{"--remote", "workbox", "--session", "agents"}, want: true},
		{args: []string{"--session", "local", "agent", "list"}, want: false},
		{args: []string{"session", "list", "--json"}, want: false},
	}
	for _, test := range tests {
		if got := herdrCommandIsInteractive(test.args); got != test.want {
			t.Errorf("herdrCommandIsInteractive(%q) = %t; want %t", test.args, got, test.want)
		}
	}
}

func TestHerdrRunWithLayoutStartsSessionCreatesWorkspaceAndAttaches(t *testing.T) {
	var commands []recordedHerdrCommand
	running := false
	var startedSessions []string
	runner := NewHerdrRunnerWithRuntime(func(args ...string) ([]byte, error) {
		commands = append(commands, recordedHerdrCommand{args: append([]string(nil), args...)})
		switch len(commands) {
		case 1:
			return []byte(`{"running":false}`), nil
		case 2:
			return []byte(`{"running":true}`), nil
		case 3:
			return []byte(`{"result":{"workspaces":[]}}`), nil
		case 4:
			return []byte(`{"result":{"root_pane":{"pane_id":"w1:p1"}}}`), nil
		case 5:
			return []byte(`{"result":{"pane":{"pane_id":"w1:p2"}}}`), nil
		case 6:
			return []byte(`{"result":{"agent":{"pane_id":"w1:p2"}}}`), nil
		case 7:
			return []byte(`{"result":{"pane_id":"w1:p1"}}`), nil
		case 8:
			return []byte(``), nil
		default:
			return nil, nil
		}
	}, func(session string) error {
		startedSessions = append(startedSessions, session)
		running = true
		return nil
	})

	ws := &workspace.Workspace{
		Name: "minato",
		Path: "/home/oles/work/minato",
		Layout: workspace.Layout{
			Type:    workspace.LayoutHerdr,
			MainCmd: "nvim",
			Panes: []workspace.Pane{
				{Command: "claude", Direction: "right"},
			},
		},
	}

	if err := runner.RunWithLayout(ws); err != nil {
		t.Fatalf("RunWithLayout returned error: %v", err)
	}

	got := make([][]string, 0, len(commands))
	for _, command := range commands {
		got = append(got, command.args)
	}
	want := [][]string{
		{"--session", "tatami-minato", "status", "server", "--json"},
		{"--session", "tatami-minato", "status", "server", "--json"},
		{"--session", "tatami-minato", "workspace", "list"},
		{"--session", "tatami-minato", "workspace", "create", "--cwd", "/home/oles/work/minato", "--label", "minato", "--focus"},
		{"--session", "tatami-minato", "pane", "split", "w1:p1", "--direction", "right", "--cwd", "/home/oles/work/minato", "--no-focus"},
		{"--session", "tatami-minato", "agent", "start", "claude", "--kind", "claude", "--pane", "w1:p2"},
		{"--session", "tatami-minato", "pane", "run", "w1:p1", "nvim"},
		{"--session", "tatami-minato"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
	if !running || !reflect.DeepEqual(startedSessions, []string{"tatami-minato"}) {
		t.Fatalf("started sessions = %#v; want tatami-minato", startedSessions)
	}
}

func TestHerdrStopSessionUsesNamedSessionCommand(t *testing.T) {
	var commands [][]string
	runner := NewHerdrRunnerWithExecutor(func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string(nil), args...))
		return nil, nil
	})

	if err := runner.StopSession("team-session"); err != nil {
		t.Fatalf("StopSession returned error: %v", err)
	}
	want := [][]string{{"session", "stop", "team-session"}}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v; want %#v", commands, want)
	}
}

func TestHerdrDeleteSessionUsesNamedSessionCommand(t *testing.T) {
	var commands [][]string
	runner := NewHerdrRunnerWithExecutor(func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string(nil), args...))
		return nil, nil
	})

	if err := runner.DeleteSession("old-session"); err != nil {
		t.Fatalf("DeleteSession returned error: %v", err)
	}
	want := [][]string{{"session", "delete", "old-session"}}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v; want %#v", commands, want)
	}
}

func TestHerdrAttachRemoteSessionUsesExactArguments(t *testing.T) {
	var got []string
	runner := NewHerdrRunnerWithExecutor(func(args ...string) ([]byte, error) {
		got = append([]string(nil), args...)
		return nil, nil
	})
	if err := runner.AttachRemoteSession("workbox", "same-name"); err != nil {
		t.Fatal(err)
	}
	want := []string{"--remote", "workbox", "--session", "same-name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("arguments = %#v, want %#v", got, want)
	}
}

func TestHerdrAttachRemoteUsesExactTargetArgument(t *testing.T) {
	var got []string
	runner := NewHerdrRunnerWithExecutor(func(args ...string) ([]byte, error) {
		got = append([]string(nil), args...)
		return nil, nil
	})
	if err := runner.AttachRemote("oles@bmo.local"); err != nil {
		t.Fatal(err)
	}
	want := []string{"--remote", "oles@bmo.local"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("arguments = %#v, want %#v", got, want)
	}
}

func TestHerdrAttachRemoteRejectsEmptyTarget(t *testing.T) {
	runner := NewHerdrRunnerWithExecutor(func(args ...string) ([]byte, error) {
		t.Fatalf("executor called with %q", args)
		return nil, nil
	})
	if err := runner.AttachRemote("  "); err == nil || !strings.Contains(err.Error(), "target is required") {
		t.Fatalf("empty target error = %v", err)
	}
}

func TestHerdrRunWithLayoutReusesRunningSession(t *testing.T) {
	var started bool
	var commands [][]string
	runner := NewHerdrRunnerWithRuntime(func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string(nil), args...))
		switch len(commands) {
		case 1:
			return []byte(`{"running":true}`), nil
		case 2:
			return []byte(`{"result":{"workspaces":[{"workspace_id":"w1","label":"existing"}]}}`), nil
		case 3:
			return []byte(`{"result":{"panes":[{"pane_id":"w1:p1","cwd":"/tmp/existing"}]}}`), nil
		default:
			return nil, nil
		}
	}, func(string) error {
		started = true
		return nil
	})

	ws := &workspace.Workspace{Name: "existing", Path: "/tmp/existing"}
	if err := runner.RunWithLayout(ws); err != nil {
		t.Fatalf("RunWithLayout returned error: %v", err)
	}
	if started {
		t.Fatal("running Herdr session was started again")
	}
	want := [][]string{
		{"--session", "tatami-existing", "status", "server", "--json"},
		{"--session", "tatami-existing", "workspace", "list"},
		{"--session", "tatami-existing", "pane", "list", "--workspace", "w1"},
		{"--session", "tatami-existing", "workspace", "focus", "w1"},
		{"--session", "tatami-existing"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands mismatch\ngot:  %#v\nwant: %#v", commands, want)
	}
}

func TestHerdrRunWithLayoutRejectsExistingTargetAtDifferentPath(t *testing.T) {
	var commands [][]string
	runner := NewHerdrRunnerWithRuntime(func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string(nil), args...))
		switch len(commands) {
		case 1:
			return []byte(`{"running":true}`), nil
		case 2:
			return []byte(`{"result":{"workspaces":[{"workspace_id":"w1","label":"existing"}]}}`), nil
		case 3:
			return []byte(`{"result":{"panes":[{"pane_id":"w1:p1","cwd":"/tmp/old"}]}}`), nil
		default:
			t.Fatalf("unexpected command: %#v", args)
			return nil, nil
		}
	}, func(string) error {
		t.Fatal("running Herdr session was started again")
		return nil
	})

	err := runner.RunWithLayout(&workspace.Workspace{Name: "existing", Path: "/tmp/new"})
	if err == nil || !strings.Contains(err.Error(), "different working directory") {
		t.Fatalf("RunWithLayout error = %v; want different working directory", err)
	}
}

func TestHerdrRunWithLayoutReusesWorkspaceByCwdWhenLabelDiffers(t *testing.T) {
	var commands [][]string
	runner := NewHerdrRunnerWithRuntime(func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string(nil), args...))
		switch len(commands) {
		case 1:
			return []byte(`{"running":true}`), nil
		case 2:
			return []byte(`{"result":{"workspaces":[{"workspace_id":"w1","label":"project"}]}}`), nil
		case 3:
			return []byte(`{"result":{"panes":[{"pane_id":"w1:p1","cwd":"/tmp/project"}]}}`), nil
		default:
			return nil, nil
		}
	}, func(string) error {
		t.Fatal("running Herdr session was started again")
		return nil
	})

	ws := &workspace.Workspace{Name: "main", Path: "/tmp/project"}
	if err := runner.RunWithLayoutInSession(ws, "tatami-project"); err != nil {
		t.Fatalf("RunWithLayoutInSession returned error: %v", err)
	}
	want := [][]string{
		{"--session", "tatami-project", "status", "server", "--json"},
		{"--session", "tatami-project", "workspace", "list"},
		{"--session", "tatami-project", "pane", "list", "--workspace", "w1"},
		{"--session", "tatami-project", "workspace", "focus", "w1"},
		{"--session", "tatami-project"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands mismatch\ngot:  %#v\nwant: %#v", commands, want)
	}
}

func TestHerdrRunWithLayoutInSessionUsesProjectSession(t *testing.T) {
	var commands [][]string
	runner := NewHerdrRunnerWithRuntime(func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string(nil), args...))
		switch len(commands) {
		case 1:
			return []byte(`{"running":true}`), nil
		case 2:
			return []byte(`{"result":{"workspaces":[]}}`), nil
		case 3:
			return []byte(`{"result":{"root_pane":{"pane_id":"w1:p1"}}}`), nil
		default:
			return nil, nil
		}
	}, func(string) error {
		t.Fatal("running Herdr session was started again")
		return nil
	})

	ws := &workspace.Workspace{Name: "feature", Path: "/tmp/project-feature"}
	if err := runner.RunWithLayoutInSession(ws, "tatami-project"); err != nil {
		t.Fatalf("RunWithLayoutInSession returned error: %v", err)
	}

	want := [][]string{
		{"--session", "tatami-project", "status", "server", "--json"},
		{"--session", "tatami-project", "workspace", "list"},
		{"--session", "tatami-project", "workspace", "create", "--cwd", "/tmp/project-feature", "--label", "feature", "--focus"},
		{"--session", "tatami-project"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands mismatch\ngot:  %#v\nwant: %#v", commands, want)
	}
}

func TestHerdrRunWithLayoutRetriesAgentStartUntilNewPaneIsReady(t *testing.T) {
	agentAttempts := 0
	runner := NewHerdrRunnerWithRuntime(func(args ...string) ([]byte, error) {
		switch {
		case reflect.DeepEqual(args, []string{"--session", "team", "status", "server", "--json"}):
			return []byte(`{"running":true}`), nil
		case reflect.DeepEqual(args, []string{"--session", "team", "workspace", "list"}):
			return []byte(`{"result":{"workspaces":[]}}`), nil
		case reflect.DeepEqual(args, []string{"--session", "team", "workspace", "create", "--cwd", "/tmp/feature", "--label", "feature", "--focus"}):
			return []byte(`{"result":{"root_pane":{"pane_id":"w2:p1"}}}`), nil
		case reflect.DeepEqual(args, []string{"--session", "team", "agent", "start", "claude", "--kind", "claude", "--pane", "w2:p1"}):
			agentAttempts++
			if agentAttempts == 1 {
				return nil, errors.New(`{"error":{"code":"agent_pane_busy","message":"agent target pane w2:p1 is not an available shell"}}`)
			}
			return []byte(`{"result":{"agent":{"pane_id":"w2:p1"}}}`), nil
		case reflect.DeepEqual(args, []string{"--session", "team"}):
			return nil, nil
		default:
			t.Fatalf("unexpected command: %#v", args)
			return nil, nil
		}
	}, func(string) error {
		t.Fatal("running Herdr session was started again")
		return nil
	})

	ws := &workspace.Workspace{
		Name: "feature",
		Path: "/tmp/feature",
		Layout: workspace.Layout{
			Type:    workspace.LayoutHerdr,
			MainCmd: "claude",
		},
	}
	if err := runner.RunWithLayoutInSession(ws, "team"); err != nil {
		t.Fatalf("RunWithLayoutInSession returned error: %v", err)
	}
	if agentAttempts != 2 {
		t.Fatalf("agent start attempts = %d; want 2", agentAttempts)
	}
}

func TestHerdrRunCommandDoesNotRetryNonTransientAgentError(t *testing.T) {
	attempts := 0
	runner := NewHerdrRunnerWithExecutor(func(args ...string) ([]byte, error) {
		attempts++
		return nil, errors.New("agent executable was not found")
	})

	err := runner.runCommand("team", "w2:p1", "claude", 0)
	if err == nil || !strings.Contains(err.Error(), "agent executable was not found") {
		t.Fatalf("runCommand error = %v; want original agent error", err)
	}
	if attempts != 1 {
		t.Fatalf("agent start attempts = %d; want 1", attempts)
	}
}

func TestHerdrRunWithLayoutInCurrentSessionDoesNotAttachNestedClient(t *testing.T) {
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_SESSION", "tatami-project")
	var commands [][]string
	runner := NewHerdrRunnerWithRuntime(func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string(nil), args...))
		switch len(commands) {
		case 1:
			return []byte(`{"running":true}`), nil
		case 2:
			return []byte(`{"result":{"workspaces":[]}}`), nil
		case 3:
			return []byte(`{"result":{"root_pane":{"pane_id":"w1:p1"}}}`), nil
		default:
			t.Fatalf("unexpected nested attach command: %#v", args)
			return nil, nil
		}
	}, func(string) error {
		t.Fatal("running Herdr session was started again")
		return nil
	})

	ws := &workspace.Workspace{Name: "feature", Path: "/tmp/project-feature"}
	if err := runner.RunWithLayoutInSession(ws, "tatami-project"); err != nil {
		t.Fatalf("RunWithLayoutInSession returned error: %v", err)
	}
	want := [][]string{
		{"--session", "tatami-project", "status", "server", "--json"},
		{"--session", "tatami-project", "workspace", "list"},
		{"--session", "tatami-project", "workspace", "create", "--cwd", "/tmp/project-feature", "--label", "feature", "--focus"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands mismatch\ngot:  %#v\nwant: %#v", commands, want)
	}
}

func TestHerdrAttachSession(t *testing.T) {
	var commands [][]string
	runner := NewHerdrRunnerWithRuntime(func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string(nil), args...))
		return nil, nil
	}, nil)

	if err := runner.AttachSession("tatami-project"); err != nil {
		t.Fatalf("AttachSession returned error: %v", err)
	}
	if want := [][]string{{"--session", "tatami-project"}}; !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v; want %#v", commands, want)
	}
}

func TestParseHerdrSessions(t *testing.T) {
	input := []byte(`{"sessions":[{"default":true,"name":"default","running":true,"session_dir":"/tmp/default","socket_path":"/tmp/default.sock"},{"default":false,"name":"tatami-project","running":false,"session_dir":"/tmp/project","socket_path":"/tmp/project.sock"}]}`)

	sessions, err := parseHerdrSessions(input)
	if err != nil {
		t.Fatalf("parseHerdrSessions returned error: %v", err)
	}
	want := []HerdrSession{
		{Name: "default", Running: true, Default: true},
		{Name: "tatami-project", Running: false},
	}
	if !reflect.DeepEqual(sessions, want) {
		t.Fatalf("sessions = %#v; want %#v", sessions, want)
	}
}

func TestParseHerdrAgents(t *testing.T) {
	input := []byte(`{"id":"cli:agent:list","result":{"agents":[{"agent":"claude","agent_status":"idle","cwd":"/repo","pane_id":"w2:p1","terminal_id":"term-1","workspace_id":"w2","agent_session":{"value":"claude-session"}},{"agent":"codex","agent_status":"blocked","cwd":"/repo/worktree","pane_id":"w3:p2","terminal_id":"term-2","workspace_id":"w3","agent_session":{"value":"codex-session"}}]}}`)

	agents, err := parseHerdrAgents(input)
	if err != nil {
		t.Fatalf("parseHerdrAgents returned error: %v", err)
	}
	want := []HerdrAgent{
		{Kind: "claude", Status: "idle", CWD: "/repo", PaneID: "w2:p1", TerminalID: "term-1", WorkspaceID: "w2", AgentSessionID: "claude-session"},
		{Kind: "codex", Status: "blocked", CWD: "/repo/worktree", PaneID: "w3:p2", TerminalID: "term-2", WorkspaceID: "w3", AgentSessionID: "codex-session"},
	}
	if !reflect.DeepEqual(agents, want) {
		t.Fatalf("agents = %#v; want %#v", agents, want)
	}
}

func TestParseHerdrPaneProcessInfo(t *testing.T) {
	input := []byte(`{"id":"cli:pane:process_info","result":{"process_info":{"foreground_process_group_id":43828,"foreground_processes":[{"pid":43828,"name":"claude.exe"},{"pid":45006,"name":"node"}],"pane_id":"w2:p1","shell_pid":38785}}}`)

	info, err := parseHerdrPaneProcessInfo(input)
	if err != nil {
		t.Fatalf("parseHerdrPaneProcessInfo returned error: %v", err)
	}
	want := HerdrPaneProcessInfo{
		PaneID:                   "w2:p1",
		ShellPID:                 38785,
		ForegroundProcessGroupID: 43828,
		ForegroundPIDs:           []int32{43828, 45006},
	}
	if !reflect.DeepEqual(info, want) {
		t.Fatalf("process info = %#v; want %#v", info, want)
	}
}

func TestParseHerdrPaneProcessInfoRejectsMissingForegroundProcesses(t *testing.T) {
	input := []byte(`{"result":{"process_info":{"pane_id":"w2:p1","shell_pid":38785}}}`)

	if _, err := parseHerdrPaneProcessInfo(input); err == nil {
		t.Fatal("missing foreground process information was accepted")
	}
}

func TestHerdrLayoutTypeIsPersistedAsHerdr(t *testing.T) {
	ws := workspace.NewWorkspace("agents", "/tmp/agents")
	ws.Layout.Type = workspace.LayoutHerdr

	if ws.Layout.Type != "herdr" {
		t.Fatalf("LayoutHerdr = %q, want herdr", ws.Layout.Type)
	}
}
