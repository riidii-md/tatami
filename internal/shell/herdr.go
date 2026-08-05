package shell

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/OleksandrBesan/tatami/internal/workspace"
)

// HerdrRunner executes Herdr commands as a Tatami layout backend.
type HerdrRunner struct {
	exec herdrExecutor
}

type herdrExecutor func(args ...string) ([]byte, error)

// NewHerdrRunner creates a new Herdr runner.
func NewHerdrRunner() *HerdrRunner {
	return NewHerdrRunnerWithExecutor(func(args ...string) ([]byte, error) {
		cmd := exec.Command("herdr", args...)
		cmd.Stdin = os.Stdin
		cmd.Stderr = os.Stderr
		if len(args) == 2 && args[0] == "--session" {
			cmd.Stdout = os.Stdout
			return nil, cmd.Run()
		}
		return cmd.Output()
	})
}

// NewHerdrRunnerWithExecutor creates a Herdr runner with an injected executor for tests.
func NewHerdrRunnerWithExecutor(exec herdrExecutor) *HerdrRunner {
	return &HerdrRunner{exec: exec}
}

// IsAvailable checks if Herdr is installed.
func (h *HerdrRunner) IsAvailable() bool {
	_, err := exec.LookPath("herdr")
	return err == nil
}

// RunWithLayout creates a Herdr workspace from a Tatami layout, then attaches to it.
func (h *HerdrRunner) RunWithLayout(ws *workspace.Workspace) error {
	session := SessionName(ws.Name)
	rootPane, err := h.createWorkspace(session, ws)
	if err != nil {
		return err
	}

	for i, pane := range ws.Layout.Panes {
		paneID, err := h.splitPane(session, rootPane, ws.Path, pane.Direction)
		if err != nil {
			return err
		}
		if err := h.runCommand(session, paneID, pane.Command, i+1); err != nil {
			return err
		}
	}

	if ws.Layout.MainCmd != "" {
		if err := h.runCommand(session, rootPane, ws.Layout.MainCmd, 0); err != nil {
			return err
		}
	}

	_, err = h.exec("--session", session)
	return err
}

// SessionName returns the default Herdr session name for a Tatami workspace.
func SessionName(workspaceName string) string {
	return "tatami-" + sanitizeName(workspaceName)
}

func (h *HerdrRunner) createWorkspace(session string, ws *workspace.Workspace) (string, error) {
	out, err := h.exec("--session", session, "workspace", "create", "--cwd", ws.Path, "--label", ws.Name, "--focus")
	if err != nil {
		return "", fmt.Errorf("failed to create herdr workspace: %w", err)
	}
	paneID := jsonString(out, "result", "root_pane", "pane_id")
	if paneID == "" {
		return "", fmt.Errorf("herdr workspace create did not return root pane id")
	}
	return paneID, nil
}

func (h *HerdrRunner) splitPane(session, targetPane, cwd, direction string) (string, error) {
	if direction == "" || direction == "stack" {
		direction = "right"
	}
	out, err := h.exec("--session", session, "pane", "split", targetPane, "--direction", direction, "--cwd", cwd, "--no-focus")
	if err != nil {
		return "", fmt.Errorf("failed to split herdr pane: %w", err)
	}
	paneID := jsonString(out, "result", "pane", "pane_id")
	if paneID == "" {
		return "", fmt.Errorf("herdr pane split did not return pane id")
	}
	return paneID, nil
}

func (h *HerdrRunner) runCommand(session, paneID, command string, index int) error {
	if command == "" {
		return nil
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil
	}
	kind := fields[0]
	if isHerdrAgentKind(kind) {
		name := kind
		if index > 1 {
			name = fmt.Sprintf("%s-%d", kind, index)
		}
		args := []string{"--session", session, "agent", "start", name, "--kind", kind, "--pane", paneID}
		if len(fields) > 1 {
			args = append(args, "--")
			args = append(args, fields[1:]...)
		}
		_, err := h.exec(args...)
		if err != nil {
			return fmt.Errorf("failed to start herdr agent %s: %w", kind, err)
		}
		return nil
	}

	_, err := h.exec("--session", session, "pane", "run", paneID, command)
	if err != nil {
		return fmt.Errorf("failed to run herdr pane command %q: %w", command, err)
	}
	return nil
}

func isHerdrAgentKind(command string) bool {
	switch command {
	case "pi", "claude", "codex", "gemini", "cursor", "devin", "agy", "cline", "omp", "mastracode", "opencode", "copilot", "kimi", "kiro", "droid", "amp", "grok", "hermes", "kilo", "qodercli", "maki":
		return true
	default:
		return false
	}
}

func sanitizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	re := regexp.MustCompile(`[^a-z0-9_-]+`)
	name = re.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		return "workspace"
	}
	return name
}

func jsonString(data []byte, path ...string) string {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return ""
	}
	current := value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[key]
	}
	text, _ := current.(string)
	return text
}
