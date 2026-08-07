package workspace

// Template represents a predefined layout template
type Template struct {
	Name        string
	Description string
	MainCmd     string // Command to run in the original (left/top) pane
	Panes       []Pane
}

// GetTemplates returns all available layout templates
func GetTemplates() []Template {
	return []Template{
		// nvim LEFT layouts
		{
			Name:        "nvim-left",
			Description: "nvim LEFT, term RIGHT",
			MainCmd:     "nvim",
			Panes: []Pane{
				{Command: "", Direction: "right"},
			},
		},
		{
			Name:        "nvim-left-2term",
			Description: "nvim LEFT, term RIGHT TOP, term RIGHT BOTTOM",
			MainCmd:     "nvim",
			Panes: []Pane{
				{Command: "", Direction: "right"},
				{Command: "", Direction: "down"},
			},
		},
		{
			Name:        "nvim-left-lazygit",
			Description: "nvim LEFT, lazygit RIGHT",
			MainCmd:     "nvim",
			Panes: []Pane{
				{Command: "lazygit", Direction: "right"},
			},
		},
		{
			Name:        "nvim-top",
			Description: "nvim TOP, term BOTTOM",
			MainCmd:     "nvim",
			Panes: []Pane{
				{Command: "", Direction: "down"},
			},
		},
		// term LEFT layouts
		{
			Name:        "term-left-nvim",
			Description: "term LEFT, nvim RIGHT",
			Panes: []Pane{
				{Command: "nvim", Direction: "right"},
			},
		},
		{
			Name:        "term-left-lazygit",
			Description: "term LEFT, lazygit RIGHT",
			Panes: []Pane{
				{Command: "lazygit", Direction: "right"},
			},
		},
		{
			Name:        "term-left-nvim-lazygit",
			Description: "term LEFT, nvim RIGHT TOP, lazygit RIGHT BOTTOM",
			Panes: []Pane{
				{Command: "nvim", Direction: "right"},
				{Command: "lazygit", Direction: "down"},
			},
		},
		// terminal only layouts
		{
			Name:        "2-side",
			Description: "term LEFT, term RIGHT",
			Panes: []Pane{
				{Command: "", Direction: "right"},
			},
		},
		{
			Name:        "2-stack",
			Description: "term TOP, term BOTTOM",
			Panes: []Pane{
				{Command: "", Direction: "down"},
			},
		},
		{
			Name:        "3-right-stack",
			Description: "term LEFT, term RIGHT TOP, term RIGHT BOTTOM",
			Panes: []Pane{
				{Command: "", Direction: "right"},
				{Command: "", Direction: "down"},
			},
		},
		// AI assistant layouts - Claude
		{
			Name:        "claude",
			Description: "claude fullscreen",
			MainCmd:     "claude",
			Panes:       []Pane{},
		},
		{
			Name:        "claude-left",
			Description: "claude LEFT, term RIGHT",
			MainCmd:     "claude",
			Panes: []Pane{
				{Command: "", Direction: "right"},
			},
		},
		{
			Name:        "claude-left-nvim",
			Description: "claude LEFT, nvim RIGHT",
			MainCmd:     "claude",
			Panes: []Pane{
				{Command: "nvim", Direction: "right"},
			},
		},
		{
			Name:        "nvim-left-claude",
			Description: "nvim LEFT, claude RIGHT",
			MainCmd:     "nvim",
			Panes: []Pane{
				{Command: "claude", Direction: "right"},
			},
		},
		{
			Name:        "nvim-left-claude-term",
			Description: "nvim LEFT, claude RIGHT TOP, term RIGHT BOTTOM",
			MainCmd:     "nvim",
			Panes: []Pane{
				{Command: "claude", Direction: "right"},
				{Command: "", Direction: "down"},
			},
		},
		{
			Name:        "nvim-left-claude-term-stack",
			Description: "nvim LEFT, stacked [claude|term] RIGHT",
			MainCmd:     "nvim",
			Panes: []Pane{
				{Command: "claude", Direction: "right"},
				{Command: "", Direction: "stack"},
			},
		},
		{
			Name:        "nvim-left-term-claude-stack",
			Description: "nvim LEFT, stacked [term|claude] RIGHT",
			MainCmd:     "nvim",
			Panes: []Pane{
				{Command: "", Direction: "right"},
				{Command: "claude", Direction: "stack"},
			},
		},
		{
			Name:        "term-left-claude",
			Description: "term LEFT, claude RIGHT",
			Panes: []Pane{
				{Command: "claude", Direction: "right"},
			},
		},
		// AI assistant layouts - Gemini
		{
			Name:        "gemini",
			Description: "gemini fullscreen",
			MainCmd:     "gemini",
			Panes:       []Pane{},
		},
		{
			Name:        "gemini-left",
			Description: "gemini LEFT, term RIGHT",
			MainCmd:     "gemini",
			Panes: []Pane{
				{Command: "", Direction: "right"},
			},
		},
		{
			Name:        "nvim-left-gemini",
			Description: "nvim LEFT, gemini RIGHT",
			MainCmd:     "nvim",
			Panes: []Pane{
				{Command: "gemini", Direction: "right"},
			},
		},
		// AI assistant layouts - Codex
		{
			Name:        "codex",
			Description: "codex fullscreen",
			MainCmd:     "codex",
			Panes:       []Pane{},
		},
		{
			Name:        "codex-left",
			Description: "codex LEFT, term RIGHT",
			MainCmd:     "codex",
			Panes: []Pane{
				{Command: "", Direction: "right"},
			},
		},
		{
			Name:        "nvim-left-codex",
			Description: "nvim LEFT, codex RIGHT",
			MainCmd:     "nvim",
			Panes: []Pane{
				{Command: "codex", Direction: "right"},
			},
		},
	}
}

// GetTemplate returns a template by name
func GetTemplate(name string) *Template {
	for _, t := range GetTemplates() {
		if t.Name == name {
			return &t
		}
	}
	return nil
}
