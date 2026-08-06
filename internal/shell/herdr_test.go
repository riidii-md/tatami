package shell

import (
	"reflect"
	"strings"
	"testing"

	"github.com/OleksandrBesan/tatami/internal/workspace"
)

type recordedHerdrCommand struct {
	args []string
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

func TestHerdrLayoutTypeIsPersistedAsHerdr(t *testing.T) {
	ws := workspace.NewWorkspace("agents", "/tmp/agents")
	ws.Layout.Type = workspace.LayoutHerdr

	if ws.Layout.Type != "herdr" {
		t.Fatalf("LayoutHerdr = %q, want herdr", ws.Layout.Type)
	}
}
