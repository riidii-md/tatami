package shell

import (
	"reflect"
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
			return []byte(`{"result":{"root_pane":{"pane_id":"w1:p1"}}}`), nil
		case 4:
			return []byte(`{"result":{"pane":{"pane_id":"w1:p2"}}}`), nil
		case 5:
			return []byte(`{"result":{"agent":{"pane_id":"w1:p2"}}}`), nil
		case 6:
			return []byte(`{"result":{"pane_id":"w1:p1"}}`), nil
		case 7:
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

func TestHerdrRunWithLayoutReusesRunningSession(t *testing.T) {
	var started bool
	var commands [][]string
	runner := NewHerdrRunnerWithRuntime(func(args ...string) ([]byte, error) {
		commands = append(commands, append([]string(nil), args...))
		switch len(commands) {
		case 1:
			return []byte(`{"running":true}`), nil
		case 2:
			return []byte(`{"result":{"root_pane":{"pane_id":"w1:p1"}}}`), nil
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
}

func TestHerdrLayoutTypeIsPersistedAsHerdr(t *testing.T) {
	ws := workspace.NewWorkspace("agents", "/tmp/agents")
	ws.Layout.Type = workspace.LayoutHerdr

	if ws.Layout.Type != "herdr" {
		t.Fatalf("LayoutHerdr = %q, want herdr", ws.Layout.Type)
	}
}
