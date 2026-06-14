package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/OleksandrBesan/tatami/internal/config"
	"github.com/OleksandrBesan/tatami/internal/shell"
	"github.com/OleksandrBesan/tatami/internal/tui"
	"github.com/OleksandrBesan/tatami/internal/workspace"
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
