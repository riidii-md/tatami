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

const (
	herdrAgentStartAttempts   = 100
	herdrAgentStartRetryDelay = 25 * time.Millisecond
)

// HerdrSession is a named Herdr session known to the local Herdr installation.
type HerdrSession struct {
	Name    string
	Running bool
	Default bool
}

// HerdrAgent describes an AI agent currently occupying a Herdr pane.
type HerdrAgent struct {
	Kind           string
	Status         string
	CWD            string
	PaneID         string
	TerminalID     string
	WorkspaceID    string
	AgentSessionID string
}

// HerdrPaneProcessInfo identifies the local process group occupying a Herdr pane.
type HerdrPaneProcessInfo struct {
	PaneID                   string
	ShellPID                 int32
	ForegroundProcessGroupID int32
	ForegroundPIDs           []int32
}

// NewHerdrRunner creates a new Herdr runner.
func NewHerdrRunner() *HerdrRunner {
	return NewHerdrRunnerWithRuntime(func(args ...string) ([]byte, error) {
		cmd := exec.Command("herdr", args...)
		cmd.Stdin = os.Stdin
		if len(args) == 2 && args[0] == "--session" {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return nil, cmd.Run()
		}
		return herdrCommandOutput(cmd)
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

func herdrCommandOutput(cmd *exec.Cmd) ([]byte, error) {
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
			return out, fmt.Errorf("%w: %s", err, stderr)
		}
	}
	return out, err
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

// AttachRemoteSession delegates an interactive remote attachment to Herdr. The
// target is intentionally an argv value, never a shell command fragment.
func (h *HerdrRunner) AttachRemoteSession(target, session string) error {
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("remote Herdr target is required")
	}
	if strings.TrimSpace(session) == "" {
		return fmt.Errorf("herdr session name is required")
	}
	_, err := h.exec("--remote", target, "--session", session)
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

// DeleteSession removes the persisted state for a stopped named Herdr session.
func (h *HerdrRunner) DeleteSession(session string) error {
	if strings.TrimSpace(session) == "" {
		return fmt.Errorf("herdr session name is required")
	}
	if _, err := h.exec("session", "delete", session); err != nil {
		return fmt.Errorf("failed to delete herdr session %s: %w", session, err)
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

// ListHerdrAgents lists the AI agents registered in a running Herdr session.
func ListHerdrAgents(session string) ([]HerdrAgent, error) {
	if strings.TrimSpace(session) == "" {
		return nil, fmt.Errorf("herdr session name is required")
	}
	out, err := exec.Command("herdr", "--session", session, "agent", "list").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list agents in herdr session %s: %w", session, err)
	}
	return parseHerdrAgents(out)
}

func parseHerdrAgents(data []byte) ([]HerdrAgent, error) {
	var response struct {
		Result struct {
			Agents []struct {
				Kind        string `json:"agent"`
				Status      string `json:"agent_status"`
				CWD         string `json:"cwd"`
				PaneID      string `json:"pane_id"`
				TerminalID  string `json:"terminal_id"`
				WorkspaceID string `json:"workspace_id"`
				Session     struct {
					Value string `json:"value"`
				} `json:"agent_session"`
			} `json:"agents"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("failed to parse herdr agents: %w", err)
	}

	agents := make([]HerdrAgent, 0, len(response.Result.Agents))
	for _, candidate := range response.Result.Agents {
		if candidate.PaneID == "" {
			continue
		}
		agents = append(agents, HerdrAgent{
			Kind:           candidate.Kind,
			Status:         candidate.Status,
			CWD:            candidate.CWD,
			PaneID:         candidate.PaneID,
			TerminalID:     candidate.TerminalID,
			WorkspaceID:    candidate.WorkspaceID,
			AgentSessionID: candidate.Session.Value,
		})
	}
	return agents, nil
}

// GetHerdrPaneProcessInfo returns the process group currently occupying a Herdr pane.
func GetHerdrPaneProcessInfo(session, paneID string) (HerdrPaneProcessInfo, error) {
	if strings.TrimSpace(session) == "" {
		return HerdrPaneProcessInfo{}, fmt.Errorf("herdr session name is required")
	}
	if strings.TrimSpace(paneID) == "" {
		return HerdrPaneProcessInfo{}, fmt.Errorf("herdr pane ID is required")
	}
	out, err := exec.Command("herdr", "--session", session, "pane", "process-info", "--pane", paneID).Output()
	if err != nil {
		return HerdrPaneProcessInfo{}, fmt.Errorf("failed to inspect herdr pane %s in session %s: %w", paneID, session, err)
	}
	return parseHerdrPaneProcessInfo(out)
}

func parseHerdrPaneProcessInfo(data []byte) (HerdrPaneProcessInfo, error) {
	var response struct {
		Result struct {
			ProcessInfo struct {
				ForegroundProcessGroupID int32 `json:"foreground_process_group_id"`
				ForegroundProcesses      []struct {
					PID int32 `json:"pid"`
				} `json:"foreground_processes"`
				PaneID   string `json:"pane_id"`
				ShellPID int32  `json:"shell_pid"`
			} `json:"process_info"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return HerdrPaneProcessInfo{}, fmt.Errorf("failed to parse herdr pane process info: %w", err)
	}
	processInfo := response.Result.ProcessInfo
	if processInfo.ForegroundProcessGroupID <= 0 || len(processInfo.ForegroundProcesses) == 0 {
		return HerdrPaneProcessInfo{}, fmt.Errorf("herdr pane %s has no foreground process", processInfo.PaneID)
	}

	info := HerdrPaneProcessInfo{
		PaneID:                   processInfo.PaneID,
		ShellPID:                 processInfo.ShellPID,
		ForegroundProcessGroupID: processInfo.ForegroundProcessGroupID,
		ForegroundPIDs:           make([]int32, 0, len(processInfo.ForegroundProcesses)),
	}
	for _, process := range processInfo.ForegroundProcesses {
		if process.PID > 0 {
			info.ForegroundPIDs = append(info.ForegroundPIDs, process.PID)
		}
	}
	if len(info.ForegroundPIDs) == 0 {
		return HerdrPaneProcessInfo{}, fmt.Errorf("herdr pane %s has no foreground process", processInfo.PaneID)
	}
	return info, nil
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
		for attempt := 0; attempt < herdrAgentStartAttempts; attempt++ {
			_, err := h.exec(args...)
			if err == nil {
				return nil
			}
			if !isHerdrAgentPaneBusy(err) || attempt == herdrAgentStartAttempts-1 {
				return fmt.Errorf("failed to start herdr agent %s: %w", kind, err)
			}
			time.Sleep(herdrAgentStartRetryDelay)
		}
	}

	_, err := h.exec("--session", session, "pane", "run", paneID, command)
	if err != nil {
		return fmt.Errorf("failed to run herdr pane command %q: %w", command, err)
	}
	return nil
}

func isHerdrAgentPaneBusy(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "agent_pane_busy") || strings.Contains(message, "not an available shell")
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
