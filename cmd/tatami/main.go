package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/OleksandrBesan/tatami/internal/agent"
	"github.com/OleksandrBesan/tatami/internal/config"
	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/tui"
	"github.com/OleksandrBesan/tatami/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("tatami %s\n", version)
		return
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load config paths
	paths, err := config.GetPaths()
	if err != nil {
		return fmt.Errorf("failed to get config paths: %w", err)
	}

	if handled, err := handleTopLevelCommand(os.Args[1:], paths, os.Stdout, os.Stderr); handled || err != nil {
		return err
	}

	// Initialize workspace store
	store, err := workspace.NewStore(paths)
	if err != nil {
		return fmt.Errorf("failed to initialize store: %w", err)
	}

	// Prevent lipgloss from blocking on OSC 11 terminal background color query.
	// Some terminals don't respond to OSC queries, causing a 5s hang on first render.
	lipgloss.SetHasDarkBackground(true)

	// Create and run the TUI app
	app := tui.NewApp(store)

	// When running inside the shell wrapper (TATAMI_WRAPPER=1), stdout is redirected
	// to a temp file to capture the result path. We must attach the TUI to /dev/tty
	// directly so the UI renders in the terminal regardless of stdout redirection.
	var opts []tea.ProgramOption
	opts = append(opts, tea.WithAltScreen())
	if os.Getenv("TATAMI_WRAPPER") == "1" {
		tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err == nil {
			opts = append(opts, tea.WithInput(tty), tea.WithOutput(tty))
			defer tty.Close()
		}
	}
	p := tea.NewProgram(app, opts...)

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}

	// Handle result
	finalApp, ok := finalModel.(*tui.App)
	if !ok {
		return nil
	}

	result := finalApp.Result()
	if result == nil {
		return nil
	}

	return handleResult(result)
}

func handleTopLevelCommand(args []string, paths *config.Paths, out, errOut io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "run":
		if len(args) < 2 {
			return true, errors.New("usage: tatami run <agent> [args...]")
		}
		return true, runTrackedAgent(paths, args[1], args[2:])
	case "agents":
		return true, handleAgentsCommand(paths, args[1:], out)
	case "dashboard", "dash":
		return true, renderDashboard(paths, out)
	default:
		return false, nil
	}
}

func handleAgentsCommand(paths *config.Paths, args []string, out io.Writer) error {
	cmd := "list"
	if len(args) > 0 {
		cmd = args[0]
	}
	store := agent.NewStore(paths.AgentsFile)
	switch cmd {
	case "list", "ls":
		return printAgents(store, out)
	case "status":
		if len(args) < 2 {
			return errors.New("usage: tatami agents status <id>")
		}
		session, err := store.Get(args[1])
		if err != nil {
			return err
		}
		return printAgentDetail(session, out)
	case "prune":
		changed, err := store.PruneStale()
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "marked %d stale agent session(s)\n", changed)
		return nil
	case "focus", "open":
		if len(args) < 2 {
			return errors.New("usage: tatami agents focus <id>")
		}
		session, err := store.Get(args[1])
		if err != nil {
			return err
		}
		return focusAgent(session, out)
	default:
		return fmt.Errorf("unknown agents command %q", cmd)
	}
}

func runTrackedAgent(paths *config.Paths, agentName string, args []string) error {
	binary, err := exec.LookPath(agentName)
	if err != nil {
		return fmt.Errorf("%s not found in PATH: %w", agentName, err)
	}
	cwd, _ := os.Getwd()
	store := agent.NewStore(paths.AgentsFile)
	session := agent.NewSession(agentName, args, cwd, agent.DetectContext())
	cmd := exec.Command(binary, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	cmd.Dir = cwd

	if err := cmd.Start(); err != nil {
		return err
	}
	session.PID = cmd.Process.Pid
	if err := store.Create(session); err != nil {
		_ = cmd.Process.Kill()
		return err
	}

	runErr := cmd.Wait()
	now := time.Now().UTC()
	session.EndedAt = &now
	session.Status = agent.StatusExited
	exitCode := 0
	if runErr != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	session.ExitCode = &exitCode
	if err := store.Update(session); err != nil {
		return err
	}
	if runErr != nil {
		return runErr
	}
	return nil
}

func printAgents(store *agent.Store, out io.Writer) error {
	_, _ = store.PruneStale()
	sessions, err := store.List()
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Fprintln(out, "No tracked AI agent sessions yet. Start one with: tatami run claude")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tAGENT\tSTATUS\tMUX\tPANE\tPID\tAGE\tCWD")
	for _, s := range sessions {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n", shortID(s.ID), s.Agent, s.Status, s.Context.Mux, paneLabel(s), s.PID, age(s.StartedAt), s.Cwd)
	}
	return tw.Flush()
}

func printAgentDetail(s *agent.Session, out io.Writer) error {
	fmt.Fprintf(out, "ID:      %s\n", s.ID)
	fmt.Fprintf(out, "Agent:   %s\n", s.Agent)
	fmt.Fprintf(out, "Status:  %s\n", s.Status)
	fmt.Fprintf(out, "PID:     %d\n", s.PID)
	fmt.Fprintf(out, "Mux:     %s\n", s.Context.Mux)
	fmt.Fprintf(out, "Pane:    %s\n", paneLabel(s))
	fmt.Fprintf(out, "Cwd:     %s\n", s.Cwd)
	fmt.Fprintf(out, "Started: %s\n", s.StartedAt.Format(time.RFC3339))
	if s.EndedAt != nil {
		fmt.Fprintf(out, "Ended:   %s\n", s.EndedAt.Format(time.RFC3339))
	}
	return nil
}

func renderDashboard(paths *config.Paths, out io.Writer) error {
	store := agent.NewStore(paths.AgentsFile)
	_, _ = store.PruneStale()
	sessions, err := store.List()
	if err != nil {
		return err
	}
	wsStore, err := workspace.NewStore(paths)
	if err != nil {
		return err
	}
	workspaces := wsStore.List()

	fmt.Fprintln(out, "Tatami Dashboard")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "┌─ AI Agents ─────────────────────────────┬─ Notifications / Agent Detail ─────────────┐")
	if len(sessions) == 0 {
		fmt.Fprintln(out, "│ no tracked agents                        │ start one: tatami run claude               │")
	} else {
		for i, s := range sessions {
			right := ""
			if i == 0 {
				right = fmt.Sprintf("Status: %s  Mux: %s  Pane: %s", s.Status, s.Context.Mux, paneLabel(s))
			}
			fmt.Fprintf(out, "│ %-39s │ %-43s │\n", fmt.Sprintf("%s %s %s", shortID(s.ID), s.Agent, s.Cwd), right)
		}
	}
	fmt.Fprintln(out, "├─ Workspaces ─────────────────────────────┼─────────────────────────────────────────────┤")
	if len(workspaces) == 0 {
		fmt.Fprintln(out, "│ no workspaces                            │                                             │")
	} else {
		limit := len(workspaces)
		if limit > 8 {
			limit = 8
		}
		for i := 0; i < limit; i++ {
			fmt.Fprintf(out, "│ %-39s │ %-43s │\n", workspaces[i].Name, "")
		}
	}
	fmt.Fprintln(out, "├─ Zellij Sessions ────────────────────────┼─────────────────────────────────────────────┤")
	fmt.Fprintln(out, "│ use existing Tatami zellij session view  │ exact pane focus depends on zellij support  │")
	fmt.Fprintln(out, "├─ Tmux Sessions ──────────────────────────┼─────────────────────────────────────────────┤")
	fmt.Fprintln(out, "│ tatami agents focus <id> can target tmux │ enter/focus action maps here                │")
	fmt.Fprintln(out, "└──────────────────────────────────────────┴─────────────────────────────────────────────┘")
	return nil
}

func focusAgent(s *agent.Session, out io.Writer) error {
	switch s.Context.Mux {
	case "tmux":
		if s.Context.TmuxSession == "" && s.Context.TmuxPane == "" {
			return errors.New("tmux session has no target metadata")
		}
		if s.Context.TmuxSession != "" {
			if err := exec.Command("tmux", "switch-client", "-t", s.Context.TmuxSession).Run(); err != nil {
				return err
			}
		}
		if s.Context.TmuxWindow != "" {
			_ = exec.Command("tmux", "select-window", "-t", s.Context.TmuxWindow).Run()
		}
		if s.Context.TmuxPane != "" {
			_ = exec.Command("tmux", "select-pane", "-t", s.Context.TmuxPane).Run()
		}
		fmt.Fprintf(out, "focused tmux agent %s\n", shortID(s.ID))
		return nil
	case "zellij":
		fmt.Fprintf(out, "zellij agent %s is in session=%q pane=%q; attach/focus through zellij for now\n", shortID(s.ID), s.Context.ZellijSession, s.Context.ZellijPaneID)
		return nil
	default:
		return fmt.Errorf("agent %s was not started inside zellij/tmux", shortID(s.ID))
	}
}

func paneLabel(s *agent.Session) string {
	if s.Context.Mux == "zellij" {
		return s.Context.ZellijPaneID
	}
	if s.Context.Mux == "tmux" {
		parts := []string{}
		if s.Context.TmuxSession != "" {
			parts = append(parts, s.Context.TmuxSession)
		}
		if s.Context.TmuxWindow != "" {
			parts = append(parts, s.Context.TmuxWindow)
		}
		if s.Context.TmuxPane != "" {
			parts = append(parts, s.Context.TmuxPane)
		}
		return strings.Join(parts, ":")
	}
	return ""
}

func shortID(id string) string {
	if len(id) <= 18 {
		return id
	}
	return id[:18]
}

func age(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	if d < 0 {
		d = 0
	}
	return d.String()
}

func isKnownAgentCommand(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	if fields[0] == "tatami" && len(fields) >= 3 && fields[1] == "run" {
		return true
	}
	switch fields[0] {
	case "claude", "codex", "gemini", "opencode", "agy":
		return true
	default:
		return false
	}
}

func copyToClipboard(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func handleResult(result *tui.Result) error {
	// Handle session attachment first (doesn't need workspace)
	if result.Action == tui.ActionAttachSession {
		if result.SessionName == "" {
			return fmt.Errorf("no session selected")
		}
		// Use syscall.Exec to replace current process with zellij attach
		// This is necessary because the TUI uses alternate screen and
		// zellij attach needs full terminal control
		zellijPath, err := exec.LookPath("zellij")
		if err != nil {
			return fmt.Errorf("zellij not found: %w", err)
		}
		args := shell.AttachSessionCmd(result.SessionName)
		return syscall.Exec(zellijPath, args, os.Environ())
	}

	ws := result.Workspace
	zellij := shell.NewZellijRunner()
	tmux := shell.NewTmuxRunner()
	isRemote := ws.IsRemote()

	switch result.Action {
	case tui.ActionCD:
		if isRemote {
			// For remote, SSH to the host
			var sshCmd string
			if ws.Remote.Key != "" {
				sshCmd = fmt.Sprintf("ssh -i %s %s -t 'cd %s && exec $SHELL'", ws.Remote.Key, ws.Remote.Host, ws.Remote.Path)
			} else {
				sshCmd = fmt.Sprintf("ssh %s -t 'cd %s && exec $SHELL'", ws.Remote.Host, ws.Remote.Path)
			}
			if zellij.IsInsideSession() {
				return zellij.WriteChars(sshCmd + "\n")
			}
			if tmux.IsInsideSession() {
				return tmux.SendKeys(sshCmd)
			}
			if err := copyToClipboard(sshCmd); err == nil {
				fmt.Printf("%s  (copied to clipboard, paste to run)\n", sshCmd)
			} else {
				fmt.Println(sshCmd)
			}
			return nil
		}

		// Local workspace
		if os.Getenv("TATAMI_WRAPPER") == "1" {
			fmt.Println(ws.Path)
			return nil
		}
		if zellij.IsInsideSession() {
			return zellij.WriteChars(fmt.Sprintf("cd %s\n", ws.Path))
		}
		if tmux.IsInsideSession() {
			return tmux.SendKeys(fmt.Sprintf("cd %s", ws.Path))
		}
		cdCmd := fmt.Sprintf("cd %s", ws.Path)
		if err := copyToClipboard(cdCmd); err == nil {
			fmt.Printf("%s  (copied to clipboard, paste to run)\n", cdCmd)
		} else {
			fmt.Println(cdCmd)
		}
		return nil

	case tui.ActionNewTab:
		if isRemote {
			if zellij.IsInsideSession() {
				return zellij.NewTabSSH(ws.Remote.Host, ws.Remote.Key, ws.Remote.Path, ws.Name)
			}
			if tmux.IsInsideSession() {
				return tmux.NewWindowSSH(ws.Remote.Host, ws.Remote.Key, ws.Remote.Path, ws.Name)
			}
		} else {
			if zellij.IsInsideSession() {
				return zellij.NewTab(ws.Path, ws.Name)
			}
			if tmux.IsInsideSession() {
				return tmux.NewWindow(ws.Path, ws.Name)
			}
		}
		fmt.Fprintf(os.Stderr, "Not inside a Zellij or Tmux session\n")
		return nil

	case tui.ActionNewPane:
		if isRemote {
			if zellij.IsInsideSession() {
				return zellij.NewPaneSSH(ws.Remote.Host, ws.Remote.Key, ws.Remote.Path, "down")
			}
			if tmux.IsInsideSession() {
				return tmux.NewPaneSSH(ws.Remote.Host, ws.Remote.Key, ws.Remote.Path, "down")
			}
		} else {
			if zellij.IsInsideSession() {
				return zellij.NewPane(ws.Path, "down")
			}
			if tmux.IsInsideSession() {
				return tmux.NewPane(ws.Path, "down")
			}
		}
		fmt.Fprintf(os.Stderr, "Not inside a Zellij or Tmux session\n")
		return nil

	case tui.ActionWithLayout:
		if isRemote {
			if zellij.IsInsideSession() && ws.Layout.Type == workspace.LayoutZellij {
				return zellij.RunWithLayoutSSH(ws)
			}
			if tmux.IsInsideSession() && ws.Layout.Type == workspace.LayoutTmux {
				return tmux.RunWithLayoutSSH(ws)
			}
		} else {
			if zellij.IsInsideSession() && ws.Layout.Type == workspace.LayoutZellij {
				return zellij.RunWithLayout(ws)
			}
			if tmux.IsInsideSession() && ws.Layout.Type == workspace.LayoutTmux {
				return tmux.RunWithLayout(ws)
			}
		}
		fmt.Fprintf(os.Stderr, "Layout type mismatch or not inside session\n")
		return nil

	case tui.ActionWithTemplate:
		if result.Template == nil {
			return fmt.Errorf("no template selected")
		}
		tmplWs := &workspace.Workspace{
			Name:   ws.Name,
			Path:   ws.Path,
			Remote: ws.Remote,
			Layout: workspace.Layout{
				MainCmd: result.Template.MainCmd,
				Panes:   result.Template.Panes,
			},
		}
		if isRemote {
			if zellij.IsInsideSession() {
				tmplWs.Layout.Type = workspace.LayoutZellij
				return zellij.RunWithLayoutSSH(tmplWs)
			}
			if tmux.IsInsideSession() {
				tmplWs.Layout.Type = workspace.LayoutTmux
				return tmux.RunWithLayoutSSH(tmplWs)
			}
		} else {
			if zellij.IsInsideSession() {
				tmplWs.Layout.Type = workspace.LayoutZellij
				return zellij.RunWithLayout(tmplWs)
			}
			if tmux.IsInsideSession() {
				tmplWs.Layout.Type = workspace.LayoutTmux
				return tmux.RunWithLayout(tmplWs)
			}
		}
		fmt.Fprintf(os.Stderr, "Not inside a Zellij or Tmux session\n")
		return nil

	case tui.ActionWorktree:
		if result.Worktree == nil {
			return fmt.Errorf("no worktree selected")
		}
		wt := result.Worktree
		tabName := wt.Branch
		if tabName == "" {
			tabName = "worktree"
		}

		// If template selected with panes, run with template layout
		if result.Template != nil && len(result.Template.Panes) > 0 {
			wtWs := &workspace.Workspace{
				Name: tabName,
				Path: wt.Path,
				Layout: workspace.Layout{
					MainCmd: result.Template.MainCmd,
					Panes:   result.Template.Panes,
				},
			}
			if zellij.IsInsideSession() {
				wtWs.Layout.Type = workspace.LayoutZellij
				return zellij.RunWithLayout(wtWs)
			}
			if tmux.IsInsideSession() {
				wtWs.Layout.Type = workspace.LayoutTmux
				return tmux.RunWithLayout(wtWs)
			}
		}

		// If no template but workspace has saved layout, use that
		if result.Template == nil && len(ws.Layout.Panes) > 0 {
			wtWs := &workspace.Workspace{
				Name: tabName,
				Path: wt.Path,
				Layout: workspace.Layout{
					Type:    ws.Layout.Type,
					MainCmd: ws.Layout.MainCmd,
					Panes:   ws.Layout.Panes,
				},
			}
			if zellij.IsInsideSession() && ws.Layout.Type == workspace.LayoutZellij {
				return zellij.RunWithLayout(wtWs)
			}
			if tmux.IsInsideSession() && ws.Layout.Type == workspace.LayoutTmux {
				return tmux.RunWithLayout(wtWs)
			}
			// Fallback: use current session type
			if zellij.IsInsideSession() {
				wtWs.Layout.Type = workspace.LayoutZellij
				return zellij.RunWithLayout(wtWs)
			}
			if tmux.IsInsideSession() {
				wtWs.Layout.Type = workspace.LayoutTmux
				return tmux.RunWithLayout(wtWs)
			}
		}

		// Plain - just open new tab
		if zellij.IsInsideSession() {
			return zellij.NewTab(wt.Path, tabName)
		}
		if tmux.IsInsideSession() {
			return tmux.NewWindow(wt.Path, tabName)
		}
		fmt.Fprintf(os.Stderr, "Not inside a Zellij or Tmux session\n")
		return nil
	}

	return nil
}
