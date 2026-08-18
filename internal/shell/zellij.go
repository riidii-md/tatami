package shell

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/OleksandrBesan/tatami/internal/workspace"
)

// ZellijRunner executes Zellij commands
type ZellijRunner struct{}

// NewZellijRunner creates a new Zellij runner
func NewZellijRunner() *ZellijRunner {
	return &ZellijRunner{}
}

// IsAvailable checks if Zellij is installed
func (z *ZellijRunner) IsAvailable() bool {
	_, err := exec.LookPath("zellij")
	return err == nil
}

// IsInsideSession checks if we're inside a Zellij session
func (z *ZellijRunner) IsInsideSession() bool {
	return os.Getenv("ZELLIJ") != ""
}

// WriteChars writes text to the current pane
func (z *ZellijRunner) WriteChars(text string) error {
	cmd := exec.Command("zellij", "action", "write-chars", text)
	return cmd.Run()
}

// NewTab opens a new tab in the current Zellij session
func (z *ZellijRunner) NewTab(path, name string) error {
	args := []string{"action", "new-tab", "--cwd", path}
	if name != "" {
		args = append(args, "--name", name)
	}
	cmd := exec.Command("zellij", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// NewPane opens a new pane in the current Zellij session
func (z *ZellijRunner) NewPane(path string, direction string) error {
	// Use "zellij run" which properly supports --cwd
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	args := []string{"run", "--cwd", path}
	if direction != "" {
		args = append(args, "--direction", direction)
	}
	args = append(args, "--", shell)

	cmd := exec.Command("zellij", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunWithLayout opens a workspace with its configured layout
func (z *ZellijRunner) RunWithLayout(ws *workspace.Workspace) error {
	// First, create a new tab
	if err := z.NewTab(ws.Path, ws.Name); err != nil {
		return fmt.Errorf("failed to create tab: %w", err)
	}

	// Then create panes for the layout
	for _, pane := range ws.Layout.Panes {
		if err := z.RunPane(ws.Path, pane.Direction, pane.Command); err != nil {
			return fmt.Errorf("failed to create pane: %w", err)
		}
	}

	// If there's a main command, focus first pane and run it
	if ws.Layout.MainCmd != "" {
		// Focus the first pane
		if err := z.FocusFirstPane(); err != nil {
			return fmt.Errorf("failed to focus first pane: %w", err)
		}
		// Write the command
		if err := z.WriteChars(ws.Layout.MainCmd + "\n"); err != nil {
			return fmt.Errorf("failed to run main command: %w", err)
		}
	}

	return nil
}

// FocusFirstPane focuses the first pane in the current tab
func (z *ZellijRunner) FocusFirstPane() error {
	// Move to first pane by going left/up multiple times
	for i := 0; i < 10; i++ {
		exec.Command("zellij", "action", "move-focus", "left").Run()
	}
	for i := 0; i < 10; i++ {
		exec.Command("zellij", "action", "move-focus", "up").Run()
	}
	return nil
}

// RunPane opens a new pane with an optional command
func (z *ZellijRunner) RunPane(path, direction, command string) error {
	args := []string{"run", "--cwd", path}

	// Handle stacked panes vs directional splits
	if direction == "stack" {
		// Use in-place to stack in current pane area
		args = append(args, "--in-place")
	} else if direction != "" {
		args = append(args, "--direction", direction)
	}
	args = append(args, "--")

	if command != "" {
		args = append(args, "sh", "-c", command)
	} else {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		args = append(args, shell)
	}

	cmd := exec.Command("zellij", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// NewTabSSH opens a new tab with an SSH session to a remote host
func (z *ZellijRunner) NewTabSSH(host, key, remotePath, name string) error {
	return z.NewTabSSHRemote(&workspace.Remote{Host: host, Key: key, Path: remotePath}, name)
}

// NewTabSSHRemote opens a new tab using the complete remote route.
func (z *ZellijRunner) NewTabSSHRemote(remote *workspace.Remote, name string) error {
	sshCmd := BuildRemoteSSHCommand(remote, "")

	args := []string{"action", "new-tab"}
	if name != "" {
		args = append(args, "--name", name)
	}
	cmd := exec.Command("zellij", args...)
	if err := cmd.Run(); err != nil {
		return err
	}

	// Write SSH command to the new tab
	return z.WriteChars(sshCmd + "\n")
}

// NewPaneSSH opens a new pane with an SSH session to a remote host
func (z *ZellijRunner) NewPaneSSH(host, key, remotePath, direction string) error {
	return z.NewPaneSSHRemote(&workspace.Remote{Host: host, Key: key, Path: remotePath}, direction)
}

// NewPaneSSHRemote opens a new pane using the complete remote route.
func (z *ZellijRunner) NewPaneSSHRemote(remote *workspace.Remote, direction string) error {
	sshCmd := BuildRemoteSSHCommand(remote, "")

	args := []string{"run"}
	if direction != "" {
		args = append(args, "--direction", direction)
	}
	args = append(args, "--", "sh", "-c", sshCmd)

	cmd := exec.Command("zellij", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunPaneSSH opens a new pane with SSH and runs a command
func (z *ZellijRunner) RunPaneSSH(host, key, remotePath, direction, command string) error {
	return z.RunPaneSSHRemote(&workspace.Remote{Host: host, Key: key, Path: remotePath}, direction, command)
}

// RunPaneSSHRemote opens a new pane through the complete route and runs a command.
func (z *ZellijRunner) RunPaneSSHRemote(remote *workspace.Remote, direction, command string) error {
	sshCmd := BuildRemoteSSHCommand(remote, command)

	args := []string{"run"}
	if direction != "" {
		args = append(args, "--direction", direction)
	}
	args = append(args, "--", "sh", "-c", sshCmd)

	cmd := exec.Command("zellij", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunWithLayoutSSH opens a remote workspace with its configured layout via SSH
func (z *ZellijRunner) RunWithLayoutSSH(ws *workspace.Workspace) error {
	// First, create a new tab with SSH
	if err := z.NewTabSSHRemote(ws.Remote, ws.Name); err != nil {
		return fmt.Errorf("failed to create tab: %w", err)
	}

	// Then create panes for the layout
	for _, pane := range ws.Layout.Panes {
		if err := z.RunPaneSSHRemote(ws.Remote, pane.Direction, pane.Command); err != nil {
			return fmt.Errorf("failed to create pane: %w", err)
		}
	}

	// If there's a main command, focus first pane and run it
	if ws.Layout.MainCmd != "" {
		if err := z.FocusFirstPane(); err != nil {
			return fmt.Errorf("failed to focus first pane: %w", err)
		}
		if err := z.WriteChars(ws.Layout.MainCmd + "\n"); err != nil {
			return fmt.Errorf("failed to run main command: %w", err)
		}
	}

	return nil
}
