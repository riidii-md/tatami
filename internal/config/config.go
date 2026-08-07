package config

import (
	"os"
	"path/filepath"
)

const (
	configDirName  = "tatami"
	workspacesFile = "workspaces.json"
	agentsDirName  = "agents"
)

// Paths holds all configuration paths
type Paths struct {
	ConfigDir      string
	WorkspacesFile string
	StateDir       string
	AgentsDir      string
}

// GetPaths returns the configuration paths, creating directories if needed
func GetPaths() (*Paths, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		configHome = filepath.Join(home, ".config")
	}

	configDir := filepath.Join(configHome, configDirName)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, err
	}

	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	stateDir := filepath.Join(stateHome, configDirName)
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(stateDir, 0700); err != nil {
		return nil, err
	}

	return &Paths{
		ConfigDir:      configDir,
		WorkspacesFile: filepath.Join(configDir, workspacesFile),
		StateDir:       stateDir,
		AgentsDir:      filepath.Join(stateDir, agentsDirName),
	}, nil
}
