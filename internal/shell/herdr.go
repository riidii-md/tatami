package shell

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/OleksandrBesan/tatami/internal/workspace"
)

// HerdrRunner executes Herdr commands as a Tatami layout backend.
type HerdrRunner struct {
	exec        herdrExecutor
	startServer herdrServerStarter
}

type herdrExecutor func(args ...string) ([]byte, error)
type herdrServerStarter func(session string) error

// HerdrSession is a named Herdr session known to the local Herdr installation.
type HerdrSession struct {
	Name    string
	Running bool
	Default bool
}

// NewHerdrRunner creates a new Herdr runner.
func NewHerdrRunner() *HerdrRunner {
	return NewHerdrRunnerWithRuntime(func(args ...string) ([]byte, error) {
		cmd := exec.Command("herdr", args...)
		cmd.Stdin = os.Stdin
		cmd.Stderr = os.Stderr
		if len(args) == 2 && args[0] == "--session" {
			cmd.Stdout = os.Stdout
			return nil, cmd.Run()
		}
		return cmd.Output()
	}, func(session string) error {
		cmd := exec.Command("herdr", "--session", session, "server")
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start herdr session %s: %w", session, err)
		}
		if err := cmd.Process.Release(); err != nil {
			return fmt.Errorf("failed to release herdr session %s: %w", session, err)
		}
		return nil
	})
}

// NewHerdrRunnerWithExecutor creates a Herdr runner with an injected executor for tests.
func NewHerdrRunnerWithExecutor(exec herdrExecutor) *HerdrRunner {
	return NewHerdrRunnerWithRuntime(exec, func(string) error { return nil })
}

// NewHerdrRunnerWithRuntime creates a Herdr runner with injected command and server runtimes.
func NewHerdrRunnerWithRuntime(exec herdrExecutor, startServer herdrServerStarter) *HerdrRunner {
	return &HerdrRunner{exec: exec, startServer: startServer}
}

// IsAvailable checks if Herdr is installed.
func (h *HerdrRunner) IsAvailable() bool {
	_, err := exec.LookPath("herdr")
	return err == nil
}

// RunWithLayout creates a Herdr workspace from a Tatami layout, then attaches to it.
func (h *HerdrRunner) RunWithLayout(ws *workspace.Workspace) error {
	return h.RunWithLayoutInSession(ws, SessionName(ws.Name))
}

// RunWithLayoutInSession creates or focuses a workspace in the selected Herdr session, then attaches.
func (h *HerdrRunner) RunWithLayoutInSession(ws *workspace.Workspace, session string) error {
	if strings.TrimSpace(session) == "" {
		return fmt.Errorf("herdr session name is required")
	}
	if err := h.ensureSession(session); err != nil {
		return err
	}
	existing, err := h.existingWorkspace(session, ws)
	if err != nil {
		return err
	}
	if existing != "" {
		if _, err := h.exec("--session", session, "workspace", "focus", existing); err != nil {
			return fmt.Errorf("failed to focus herdr workspace %s: %w", existing, err)
		}
		return h.AttachSession(session)
	}
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

	return h.AttachSession(session)
}

// AttachSession attaches to a named Herdr session, starting it when necessary.
func (h *HerdrRunner) AttachSession(session string) error {
	if strings.TrimSpace(session) == "" {
		return fmt.Errorf("herdr session name is required")
	}
	if os.Getenv("HERDR_ENV") == "1" && os.Getenv("HERDR_SESSION") == session {
		return nil
	}
	_, err := h.exec("--session", session)
	return err
}

// StopSession stops a running named Herdr session.
func (h *HerdrRunner) StopSession(session string) error {
	if strings.TrimSpace(session) == "" {
		return fmt.Errorf("herdr session name is required")
	}
	if _, err := h.exec("session", "stop", session); err != nil {
		return fmt.Errorf("failed to stop herdr session %s: %w", session, err)
	}
	return nil
}

// ListHerdrSessions lists all named Herdr sessions, including stopped sessions.
func ListHerdrSessions() ([]HerdrSession, error) {
	if _, err := exec.LookPath("herdr"); err != nil {
		return nil, nil
	}
	out, err := exec.Command("herdr", "session", "list", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list herdr sessions: %w", err)
	}
	return parseHerdrSessions(out)
}

func parseHerdrSessions(data []byte) ([]HerdrSession, error) {
	var response struct {
		Sessions []struct {
			Name    string `json:"name"`
			Running bool   `json:"running"`
			Default bool   `json:"default"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("failed to parse herdr sessions: %w", err)
	}
	sessions := make([]HerdrSession, 0, len(response.Sessions))
	for _, session := range response.Sessions {
		if session.Name == "" {
			continue
		}
		sessions = append(sessions, HerdrSession{
			Name:    session.Name,
			Running: session.Running,
			Default: session.Default,
		})
	}
	return sessions, nil
}

func (h *HerdrRunner) ensureSession(session string) error {
	if h.sessionRunning(session) {
		return nil
	}
	if h.startServer == nil {
		return fmt.Errorf("cannot start herdr session %s", session)
	}
	if err := h.startServer(session); err != nil {
		return err
	}

	const attempts = 100
	for attempt := 0; attempt < attempts; attempt++ {
		if h.sessionRunning(session) {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("herdr session %s did not become ready", session)
}

func (h *HerdrRunner) sessionRunning(session string) bool {
	out, err := h.exec("--session", session, "status", "server", "--json")
	if err != nil {
		return false
	}
	var status struct {
		Running bool `json:"running"`
	}
	return json.Unmarshal(out, &status) == nil && status.Running
}

func (h *HerdrRunner) existingWorkspace(session string, ws *workspace.Workspace) (string, error) {
	out, err := h.exec("--session", session, "workspace", "list")
	if err != nil {
		return "", fmt.Errorf("failed to list herdr workspaces: %w", err)
	}
	var response struct {
		Result struct {
			Workspaces []struct {
				ID    string `json:"workspace_id"`
				Label string `json:"label"`
			} `json:"workspaces"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return "", fmt.Errorf("failed to parse herdr workspace list: %w", err)
	}

	foundLabel := false
	for _, candidate := range response.Result.Workspaces {
		if candidate.Label == ws.Name {
			foundLabel = true
		}
		paneOut, err := h.exec("--session", session, "pane", "list", "--workspace", candidate.ID)
		if err != nil {
			return "", fmt.Errorf("failed to list panes for herdr workspace %s: %w", candidate.ID, err)
		}
		var paneResponse struct {
			Result struct {
				Panes []struct {
					Cwd string `json:"cwd"`
				} `json:"panes"`
			} `json:"result"`
		}
		if err := json.Unmarshal(paneOut, &paneResponse); err != nil {
			return "", fmt.Errorf("failed to parse panes for herdr workspace %s: %w", candidate.ID, err)
		}
		for _, pane := range paneResponse.Result.Panes {
			if filepath.Clean(pane.Cwd) == filepath.Clean(ws.Path) {
				return candidate.ID, nil
			}
		}
	}

	if foundLabel {
		return "", fmt.Errorf("herdr session %s already contains workspace %q at a different working directory", session, ws.Name)
	}
	return "", nil
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
