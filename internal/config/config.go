package config

import (
	"os"
	"path/filepath"
)

const (
	configDirName  = "tatami"
	workspacesFile = "workspaces.json"
	agentsFile     = "agents.json"
)

// Paths holds all configuration paths
type Paths struct {
	ConfigDir      string
	WorkspacesFile string
	AgentsFile     string
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

	return &Paths{
		ConfigDir:      configDir,
		WorkspacesFile: filepath.Join(configDir, workspacesFile),
		AgentsFile:     filepath.Join(configDir, agentsFile),
	}, nil
}
