package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetPathsSeparatesConfigAndPrivateRuntimeState(t *testing.T) {
	root := t.TempDir()
	configHome := filepath.Join(root, "config")
	stateHome := filepath.Join(root, "state")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", stateHome)

	paths, err := GetPaths()
	if err != nil {
		t.Fatalf("GetPaths: %v", err)
	}
	if paths.WorkspacesFile != filepath.Join(configHome, "tatami", "workspaces.json") {
		t.Fatalf("WorkspacesFile = %q", paths.WorkspacesFile)
	}
	if paths.AgentsDir != filepath.Join(stateHome, "tatami", "agents") {
		t.Fatalf("AgentsDir = %q", paths.AgentsDir)
	}
	if paths.HerdrHostsFile != filepath.Join(configHome, "tatami", "herdr-hosts.json") || paths.HerdrHubFile != filepath.Join(stateHome, "tatami", "herdr-hub.json") {
		t.Fatalf("unexpected Herdr hub paths: %#v", paths)
	}
	info, err := os.Stat(paths.StateDir)
	if err != nil {
		t.Fatalf("stat state directory: %v", err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("state directory permissions = %v, want 0700", info.Mode().Perm())
	}
}

func TestGetPathsUsesXDGStateFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")

	paths, err := GetPaths()
	if err != nil {
		t.Fatalf("GetPaths: %v", err)
	}
	if paths.StateDir != filepath.Join(home, ".local", "state", "tatami") {
		t.Fatalf("StateDir = %q", paths.StateDir)
	}
}

func TestGetPathsReportsInvalidConfigHome(t *testing.T) {
	root := t.TempDir()
	configFile := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(configFile, []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", configFile)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))

	if _, err := GetPaths(); err == nil {
		t.Fatal("GetPaths succeeded with a file as XDG_CONFIG_HOME")
	}
}
