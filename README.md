# Tatami

Terminal workspace manager with Zellij/Tmux integration. Quickly switch between projects with predefined layouts.

## Installation

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

Download the archive for your platform from [Releases](https://github.com/OleksandrBesan/tatami/releases), extract it, and move the `tatami` binary somewhere on your `PATH`.

On macOS, direct downloads from GitHub Releases may be blocked by Gatekeeper because the binary is not currently notarized. If you trust the downloaded release, remove the quarantine attribute after extracting:

```bash
xattr -dr com.apple.quarantine /path/to/tatami
```

### Homebrew

Homebrew installation is not published yet. Until a `homebrew-tap` repository exists, prefer `go install`, `make install`, or downloading a release binary.

## Shell Integration

For `cd` to work in the current terminal, add to `~/.zshrc` or `~/.bashrc`:

```bash
tatami() {
  if [[ "$1" == "run" || "$1" == "agents" ]]; then
    command tatami "$@"
    return $?
  fi
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

## Kitty Integration

If you use the [Kitty](https://sw.kovidgoyal.net/kitty/) terminal, you can make
a new tab open straight into Tatami instead of a plain shell:

```bash
scripts/kitty-integration.sh          # install / update the keybindings
scripts/kitty-integration.sh --remove # remove them
scripts/kitty-integration.sh --print  # preview without writing
```

The script appends a marked, idempotent block to your `kitty.conf` (default
`~/.config/kitty/kitty.conf`) and reloads any running Kitty instance:

```conf
# >>> tatami kitty integration >>>
map kitty_mod+t launch --type=tab --cwd=current /bin/zsh -lic "exec '/abs/path/to/tatami' --new-tab"
map kitty_mod+shift+t new_tab_with_cwd
map cmd+t launch --type=tab --cwd=current /bin/zsh -lic "exec '/abs/path/to/tatami' --new-tab"
map cmd+shift+t new_tab_with_cwd
# <<< tatami kitty integration <<<
```

- New tab → Tatami: `kitty_mod+t` (and `cmd+t` on macOS). Selecting a workspace opens actions for entering the project or choosing a Git worktree; the resulting shell stays in the current Kitty tab.
- New tab → plain shell: `kitty_mod+shift+t` (and `cmd+shift+t` on macOS).

The integration launches Tatami through your interactive login shell so it sees
the same `PATH` as a normal terminal, including user-installed tools such as
Herdr in `~/.local/bin`. Re-run the script if `tatami` moves. Override the paths
with the `KITTY_CONFIG` and `TATAMI_BIN` environment variables.

If you also use Zellij and want to skip its startup tip popup in fresh
sessions, set `show_startup_tips false` in `~/.config/zellij/config.kdl`.

## Usage

```bash
tatami
```

For compact, phone-friendly navigation over SSH:

```bash
tatami --mobile # or: tatami -m
```

Mobile mode shows numbered choices, keeps `Enter` as confirmation, adds `b` as
a safe Back key outside text fields, hides paths on the home screen, and removes
decorative menu borders. See [Mobile navigation with Termius](docs/mobile-navigation.md)
for the recommended Termius shortcut bar and Startup Command.

### Agent session tracking

Use `tatami run` when you want Tatami to track the lifecycle and terminal
location of an AI CLI while preserving its normal interactive terminal:

```bash
tatami run claude
tatami run codex
tatami run gemini

tatami agents list
tatami agents status <id>
tatami agents prune
```

Tatami records the agent name, working directory, PID, lifecycle timestamps,
exit code, and available Zellij, tmux, Herdr, or Kitty context. It deliberately
does not store command arguments or terminal transcripts because they can
contain prompts, source code, and secrets.

Runtime records live as private per-session JSON files under
`$XDG_STATE_HOME/tatami/agents` (or `~/.local/state/tatami/agents`). Separate
files keep simultaneous agent starts from overwriting each other.

Herdr-backed layouts continue to launch known AI commands through Herdr's own
agent control plane. Use `tatami run` for agents launched manually in a shell,
Zellij/tmux pane, or Kitty window.

To see a current resource snapshot for every AI agent managed by running Herdr
sessions:

```bash
tatami resources
```

Tatami groups agents by Herdr session and reports CPU, summed resident memory
(RSS), process count, and runtime. Descendant processes such as MCP servers are
included. Agent CPU uses the usual per-core convention and can exceed 100%; the
summary also normalizes it against the machine's logical CPU count. RSS can
double-count shared memory pages, so it is an operational estimate rather than
an exact billing number. If a pane changes occupants while Tatami samples it,
that agent is shown as unavailable instead of being guessed.

Resource snapshots are read-only and stay local. Tatami does not parse, print,
or persist process arguments, prompts, or terminal contents. Resource reporting
requires Herdr 0.8 or newer.

## Features

### Folders
Organize workspaces into folders. Navigate into folders with `Enter` or `l`, go back with `h`, `Esc`, or select `../`.

### Quick Access
Star workspaces with `*` or `s` to pin them to the "Quick Access" section at the root level for fast access.

The home page groups folders and unfiled saved workspaces together as **Tatami Projects**. A Tatami workspace is the saved project definition: its name, path, optional folder, quick-access flag, and launch layout. **Herdr Sessions** are shown separately because they are live or persisted runtime sessions, not project definitions.

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

### Worktree Kanban Tasks
A proposed next step is a per-project Kanban board where each task can create or
focus a Git worktree, open the matching Tatami pane, and move through
Todo/Doing/Review/Done as real work progresses. See
[`docs/worktree-kanban-tasks.md`](docs/worktree-kanban-tasks.md) for the design.

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
| `1`–`9` | Select a visible row in mobile mode |
| `Enter` / `l` | Open action menu / Enter folder |
| `b` | Go back in mobile mode (outside text inputs) |
| `Enter` on a Herdr session | Start/attach to that session |
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
    },
    {
      "name": "agent-project",
      "path": "/home/user/agent-project",
      "layout": {
        "type": "herdr",
        "main_cmd": "nvim",
        "panes": [
          { "command": "claude", "direction": "right" }
        ]
      }
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
| `type` | `none`, `zellij`, `tmux`, or `herdr` |
| `main_cmd` | Command to run in the original (left/top) pane |
| `panes` | Array of additional panes |
| `panes[].command` | Command to run (empty = shell) |
| `panes[].direction` | `right`, `down`, or `stack` (Zellij only) |

### Herdr Layout Backend

Set `layout.type` to `herdr` to make Herdr the runtime for every way that workspace is opened, including its saved layout, a selected template, or a Git worktree. Before opening any of those targets, Tatami asks where it should go:

- **New / separate Herdr session** opens a name field prefilled with `tatami-<workspace>` or `tatami-<branch>`. Accept the generated name or enter a custom session name.
- **Existing Herdr session** shows every known running or stopped session. Choose any session to add the project, template target, or worktree as a workspace there.

When the selected session already contains the same working directory, Tatami focuses and attaches to it without replaying layout commands. Otherwise, Tatami creates the root workspace with the selected project or worktree path as its working directory, splits panes from the chosen layout, starts known AI commands such as `claude`, `codex`, and `gemini` via `herdr agent start`, runs other commands with `herdr pane run`, then attaches to the Herdr session.

When Tatami itself is launched from a Herdr pane, the current session is marked and placed first in the existing-session picker. Selecting it creates or focuses the target workspace in the same Herdr server and returns to the original pane without launching a nested Herdr client.

The Tatami home page reads Herdr's session inventory and shows every known Herdr session with its running or stopped status. Select a session and press Enter to start or attach to it directly. Press `x` to stop a running session; on a stopped named session, `x` opens a confirmation before deleting its persisted session state. Herdr's built-in `default` session cannot be deleted. Herdr 0.8 does not expose a supported command for renaming an existing session, so session names are chosen when a new session is opened.

This keeps Tatami responsible for selecting the local workspace while Herdr owns the agent panes, status, and control plane.

## Requirements

- **Zellij** or **Tmux** (for tab/pane features)
- **Herdr** (for `layout.type: "herdr"`)
- Works without them for basic `cd` functionality

## License

MIT
