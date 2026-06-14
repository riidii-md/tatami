# Tatami

Terminal workspace manager with Zellij/Tmux integration. Quickly switch between projects with predefined layouts.

## Installation

### Homebrew (macOS/Linux)

```bash
brew tap OleksandrBesan/tap
brew install tatami
```

### Go Install

```bash
go install github.com/OleksandrBesan/tatami/cmd/tatami@latest
```

### Build from Source

```bash
git clone https://github.com/OleksandrBesan/tatami.git
cd tatami
make install
```

### Download Binary

Download from [Releases](https://github.com/OleksandrBesan/tatami/releases).

## Shell Integration

For `cd` to work in the current terminal, add to `~/.zshrc` or `~/.bashrc`:

```bash
tatami() {
  local tmp exit_code output
  tmp=$(mktemp)
  TATAMI_WRAPPER=1 command tatami "$@" > "$tmp"
  exit_code=$?
  output=$(cat "$tmp")
  rm -f "$tmp"
  if [[ $exit_code -eq 0 && -d "$output" ]]; then
    cd "$output"
  elif [[ -n "$output" ]]; then
    echo "$output"
  fi
  return $exit_code
}
```

Without the wrapper, `cd` will type the command in Zellij or copy to clipboard.

## Usage

```bash
tatami
```

## Features

### Folders
Organize workspaces into folders. Navigate into folders with `Enter` or `l`, go back with `h`, `Esc`, or select `../`.

### Quick Access
Star workspaces with `*` or `s` to pin them to the "Quick Access" section at the root level for fast access.

### Remote Workspaces
Connect to remote servers via SSH. Opens an SSH session directly to the remote host.

1. Create workspace with **Remote Host** field (e.g., `user@server.com`)
2. Set **SSH Key** if needed (e.g., `~/.ssh/my_key`)
3. Set **Path** to remote path (e.g., `/home/user/project`)
4. When opening, tatami SSHs to the remote and runs commands there (nvim, shell, etc.)

No extra dependencies required - uses standard SSH.

### Git Worktrees
Open worktrees in new tabs for git-enabled workspaces. When selecting a workspace that is a git repository:

1. Select **"open worktree..."** from the action menu
2. Choose an existing worktree or create a new one
3. Select how to open:
   - **with saved layout** - use workspace's configured layout
   - **with template...** - choose a layout template
   - **plain** - open simple tab

Worktrees are created at `.worktrees/<branch-name>/` inside the repository.

### Layout Templates
Apply predefined pane layouts when opening workspaces.

### Stacked Panes (Zellij)
Use `stack` direction to create stacked/tabbed panes that share the same space. Switch between stacked panes with Zellij shortcuts (`Ctrl+p` then `w`).

## Keyboard Shortcuts

### List View
| Key | Action |
|-----|--------|
| `j` / `↓` | Move down |
| `k` / `↑` | Move up |
| `Enter` / `l` | Open action menu / Enter folder |
| `h` / `Esc` | Go back (in folder) / Quit (at root) |
| `n` | New workspace |
| `e` | Edit workspace |
| `d` | Delete workspace |
| `*` / `s` | Toggle quick access (star) |
| `f` | Create folder |
| `/` | Filter workspaces |
| `q` | Quit |

### Create/Edit View
| Key | Action |
|-----|--------|
| `Tab` | Autocomplete path (on path field) |
| `Ctrl+J` / `Ctrl+N` | Next field |
| `Ctrl+K` | Previous field |
| `F2` | Choose template |
| `←` / `→` | Change layout type (on layout field) |
| `Enter` | Save |
| `Esc` | Cancel |

### Action Menu
| Key | Action |
|-----|--------|
| `j` / `k` | Navigate |
| `1-4` | Quick select |
| `Enter` | Execute |
| `Esc` | Back |

### Worktree View
| Key | Action |
|-----|--------|
| `j` / `k` | Navigate |
| `Enter` | Select worktree / Create new |
| `d` | Delete worktree |
| `Tab` | Cycle branch suggestions (when creating) |
| `Esc` | Back |

## Actions

When opening a workspace (ordered by priority):

| Action | Description |
|--------|-------------|
| **with saved layout** | Open with workspace's saved layout (if configured) |
| **open worktree...** | Open git worktree in new tab (git repos only) |
| **with template** | Open with a layout template |
| **new pane** | Open in new pane |
| **new tab** | Open in new Zellij tab / Tmux window |
| **cd here** | Change to workspace directory |

## Layout Templates

Select a template when creating a workspace (`F2`) or when opening (`with template`).

### Editor Layouts
| Template | Layout |
|----------|--------|
| `nvim-left` | nvim LEFT, term RIGHT |
| `nvim-left-2term` | nvim LEFT, term RIGHT TOP, term RIGHT BOTTOM |
| `nvim-left-lazygit` | nvim LEFT, lazygit RIGHT |
| `nvim-top` | nvim TOP, term BOTTOM |
| `term-left-nvim` | term LEFT, nvim RIGHT |
| `term-left-lazygit` | term LEFT, lazygit RIGHT |
| `term-left-nvim-lazygit` | term LEFT, nvim RIGHT TOP, lazygit RIGHT BOTTOM |

### Terminal Layouts
| Template | Layout |
|----------|--------|
| `2-side` | term LEFT, term RIGHT |
| `2-stack` | term TOP, term BOTTOM |
| `3-right-stack` | term LEFT, term RIGHT TOP, term RIGHT BOTTOM |

### AI Assistant Layouts (Claude, Gemini, Codex)
| Template | Layout |
|----------|--------|
| `claude` | claude fullscreen |
| `claude-left` | claude LEFT, term RIGHT |
| `claude-left-nvim` | claude LEFT, nvim RIGHT |
| `nvim-left-claude` | nvim LEFT, claude RIGHT |
| `nvim-left-claude-term` | nvim LEFT, claude RIGHT TOP, term RIGHT BOTTOM |
| `nvim-left-claude-term-stack` | nvim LEFT, stacked [claude\|term] RIGHT |
| `nvim-left-term-claude-stack` | nvim LEFT, stacked [term\|claude] RIGHT |
| `term-left-claude` | term LEFT, claude RIGHT |
| `gemini` | gemini fullscreen |
| `gemini-left` | gemini LEFT, term RIGHT |
| `nvim-left-gemini` | nvim LEFT, gemini RIGHT |
| `codex` | codex fullscreen |
| `codex-left` | codex LEFT, term RIGHT |
| `nvim-left-codex` | nvim LEFT, codex RIGHT |

## Configuration

Workspaces are stored in `~/.config/tatami/workspaces.json`:

```json
{
  "workspaces": [
    {
      "name": "myproject",
      "path": "/home/user/projects/myproject",
      "folder": "work/clients",
      "quick_access": true,
      "layout": {
        "type": "zellij",
        "main_cmd": "nvim",
        "panes": [
          { "command": "claude", "direction": "right" },
          { "command": "", "direction": "stack" }
        ]
      }
    },
    {
      "name": "server-project",
      "path": "/home/user/project",
      "remote": {
        "host": "user@server.com",
        "key": "~/.ssh/server_key",
        "path": "/home/user/project"
      },
      "layout": { "type": "zellij", "panes": [] }
    }
  ]
}
```

### Workspace Fields

| Field | Description |
|-------|-------------|
| `name` | Workspace name |
| `path` | Directory path (local or remote) |
| `folder` | Organization folder (e.g., `work/clients`) |
| `quick_access` | Show in Quick Access section |
| `remote.host` | Remote host (e.g., `user@server.com`) |
| `remote.key` | SSH key path (e.g., `~/.ssh/my_key`) |
| `remote.path` | Path on remote server |

### Layout Fields

| Field | Description |
|-------|-------------|
| `type` | `none`, `zellij`, or `tmux` |
| `main_cmd` | Command to run in the original (left/top) pane |
| `panes` | Array of additional panes |
| `panes[].command` | Command to run (empty = shell) |
| `panes[].direction` | `right`, `down`, or `stack` (Zellij only) |

## Requirements

- **Zellij** or **Tmux** (for tab/pane features)
- Works without them for basic `cd` functionality

## License

MIT
