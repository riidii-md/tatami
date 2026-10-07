package shell

import (
	"github.com/OleksandrBesan/tatami/internal/workspace"
	"os"
	"path/filepath"
	"testing"
)

func TestInvalidRemoteFailsBeforeMuxMutation(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "invoked")
	for _, name := range []string{"tmux", "zellij"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nprintf invoked >> '"+marker+"'\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	remote := &workspace.Remote{Host: "invalid host", Path: "/srv/project"}
	tmux, zellij := NewTmuxRunner(), NewZellijRunner()
	for name, run := range map[string]func() error{
		"tmux window":    func() error { return tmux.NewWindowSSHRemote(remote, "") },
		"tmux pane":      func() error { return tmux.NewPaneSSHRemote(remote, "") },
		"tmux command":   func() error { return tmux.RunPaneSSHRemote(remote, "", "true") },
		"zellij tab":     func() error { return zellij.NewTabSSHRemote(remote, "") },
		"zellij pane":    func() error { return zellij.NewPaneSSHRemote(remote, "") },
		"zellij command": func() error { return zellij.RunPaneSSHRemote(remote, "", "true") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := run(); err == nil {
				t.Fatal("invalid remote reported success")
			}
		})
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("mux was invoked for invalid remote: %v", err)
	}
}
