package workspace

// LayoutType represents the multiplexer type
type LayoutType string

const (
	LayoutNone   LayoutType = "none"
	LayoutZellij LayoutType = "zellij"
	LayoutTmux   LayoutType = "tmux"
	LayoutHerdr  LayoutType = "herdr"
)

// Pane represents a single pane in a layout
type Pane struct {
	Command   string `json:"command"`
	Direction string `json:"direction"` // "down", "right"
}

// Layout represents the pane arrangement for a workspace
type Layout struct {
	Type    LayoutType `json:"type"`
	MainCmd string     `json:"main_cmd,omitempty"` // Command for the original (left/top) pane
	Panes   []Pane     `json:"panes"`
}

// Remote represents remote connection settings
type Remote struct {
	Host string `json:"host"`          // user@hostname or hostname
	Path string `json:"path"`          // Remote path
	Key  string `json:"key,omitempty"` // SSH key path (optional)
}

// Workspace represents a terminal workspace
type Workspace struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	Folder      string  `json:"folder,omitempty"`       // Folder path like "work/clients"
	QuickAccess bool    `json:"quick_access,omitempty"` // Show in quick access
	Remote      *Remote `json:"remote,omitempty"`       // Remote connection settings
	Layout      Layout  `json:"layout"`
}

// IsRemote returns true if this is a remote workspace
func (w *Workspace) IsRemote() bool {
	return w.Remote != nil && w.Remote.Host != ""
}

// NewWorkspace creates a new workspace with default values
func NewWorkspace(name, path string) *Workspace {
	return &Workspace{
		Name: name,
		Path: path,
		Layout: Layout{
			Type:  LayoutNone,
			Panes: []Pane{},
		},
	}
}
