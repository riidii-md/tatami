# Kitty Backend Proposal

## Goal

Add Kitty as a first-class Tatami backend so a workspace can be opened,
tracked, focused, and reused in Kitty tabs without requiring Zellij or Tmux.

This should not be implemented as a terminal proxy. Kitty already owns the PTYs,
tabs, windows, rendering, keyboard handling, and process lifecycle. Tatami should
act as a workspace/session coordinator on top of Kitty's remote control API.

## User Story

1. User opens Kitty.
2. User runs `tatami` in a Kitty tab.
3. User selects a workspace.
4. Tatami detects that it is running inside Kitty.
5. If that workspace already has a live Kitty tab, Tatami focuses it.
6. If it does not, Tatami opens a new Kitty tab, sets the title, launches the
   workspace shell or layout command, records the Kitty tab ID, and focuses it.

The same workspace can optionally have multiple tracked Kitty tabs, for example:

- default workspace tab
- worktree tab
- debugging tab
- remote SSH tab
- ad hoc tab created by the user and later attached to the workspace

## Terminology

- `Kitty OS window`: a top-level Kitty application window.
- `Kitty tab`: a tab inside one Kitty OS window.
- `Kitty window`: a pane inside a Kitty tab. Kitty remote control calls panes
  "windows".
- `Tatami workspace`: the configured project entry from `workspaces.json`.
- `Tatami session`: a runtime record that maps a workspace to one or more
  Kitty tab IDs.

## Required Kitty Configuration

Users must enable remote control in `kitty.conf`:

```conf
allow_remote_control yes
listen_on unix:/tmp/kitty
```

Tatami should also support the common case where it is launched from inside
Kitty and `KITTY_LISTEN_ON` is available. In that case Tatami can call:

```sh
kitty @ ls
```

without passing `--to`.

If Tatami is launched outside Kitty, users can set:

```sh
export TATAMI_KITTY_LISTEN_ON=unix:/tmp/kitty
```

Tatami should pass that value as:

```sh
kitty @ --to "$TATAMI_KITTY_LISTEN_ON" ...
```

## Kitty Commands Needed

List current Kitty windows/tabs:

```sh
kitty @ ls
```

Open a new Kitty tab:

```sh
kitty @ launch --type=tab --cwd "$path" --tab-title "$name" "$SHELL"
```

Focus a tracked Kitty tab:

```sh
kitty @ focus-tab --match "id:$tab_id"
```

Set or refresh a tab title:

```sh
kitty @ set-tab-title --match "id:$tab_id" "$name"
```

Close stale tabs only when explicitly requested by the user:

```sh
kitty @ close-tab --match "id:$tab_id"
```

## Session Tracking

Add a runtime state file separate from `workspaces.json`.

Recommended path:

```text
~/.local/share/tatami/kitty_sessions.json
```

Example:

```json
{
  "version": 1,
  "tabs": [
    {
      "workspace_name": "tatami",
      "workspace_path": "/Users/oleksandrbesan/data/oleslab/projects/tatami",
      "kitty_os_window_id": 1,
      "kitty_tab_id": 7,
      "kitty_tab_title": "tatami",
      "kind": "default",
      "created_at": "2026-07-07T12:00:00Z",
      "last_seen_at": "2026-07-07T12:30:00Z"
    },
    {
      "workspace_name": "tatami",
      "workspace_path": "/Users/oleksandrbesan/data/oleslab/projects/tatami/.worktrees/feature-x",
      "kitty_os_window_id": 1,
      "kitty_tab_id": 9,
      "kitty_tab_title": "tatami:feature-x",
      "kind": "worktree",
      "created_at": "2026-07-07T12:05:00Z",
      "last_seen_at": "2026-07-07T12:20:00Z"
    }
  ]
}
```

Do not store Kitty tab IDs in `workspaces.json`. Tab IDs are runtime state and
become stale after Kitty restarts.

## Stale Tab Handling

Before focusing a stored tab ID, Tatami should run `kitty @ ls` and verify:

- the tab ID still exists
- the tab belongs to the same Kitty OS window when that matters
- at least one window in the tab has the expected cwd, command, or Tatami marker

If the tab ID is missing, remove it from `kitty_sessions.json` and create a new
tab.

## Detecting Kitty

Add `KittyRunner` under `internal/shell`.

Initial methods:

```go
type KittyRunner struct{}

func NewKittyRunner() *KittyRunner
func (k *KittyRunner) IsAvailable() bool
func (k *KittyRunner) IsInsideSession() bool
func (k *KittyRunner) List() (*KittyState, error)
func (k *KittyRunner) NewTab(path, name string) (*KittyTab, error)
func (k *KittyRunner) FocusTab(tabID int) error
func (k *KittyRunner) SetTabTitle(tabID int, title string) error
```

Detection rules:

- `IsAvailable`: `exec.LookPath("kitty")`
- `IsInsideSession`: `KITTY_WINDOW_ID != ""`
- remote address:
  - prefer `TATAMI_KITTY_LISTEN_ON`
  - else use `KITTY_LISTEN_ON`
  - else call `kitty @` without `--to` when inside Kitty

## Action Menu Changes

Today the action menu enables tab/pane actions only when inside Zellij or Tmux.
With Kitty support, Tatami should treat Kitty as a third session backend.

Suggested actions when inside Kitty:

- `switch to existing tab` if a live tracked tab exists
- `new tab`
- `attach current tab`
- `detach tab from workspace`
- `cd here`

For worktrees:

- `open worktree...` should create or focus a separate tracked Kitty tab
- tab title should include the branch/worktree name

## Attach Current Tab

The flow described in the feature request is valid:

1. User manually creates a Kitty tab.
2. User opens `tatami` in that tab.
3. User selects a workspace.
4. Tatami offers `attach current tab`.
5. Tatami stores the current tab ID against that workspace.

To find the current tab ID:

1. Read `KITTY_WINDOW_ID`.
2. Run `kitty @ ls`.
3. Find the Kitty tab containing a window with that window ID.

This allows multiple tabs per workspace because the state file stores an array,
not a single `workspace -> tab_id` mapping.

## Switching Through Tatami

Once Tatami tracks Kitty tabs, it can provide:

- workspace picker: select workspace, focus its live tab if present
- session picker: list all tracked Kitty tabs and focus one
- fuzzy search: search by workspace name, path, tab title, worktree branch
- cleanup: remove stale tracked tab records
- reopen: if tracked tab is gone, create a replacement tab

This gives Tatami a higher-level view than Kitty's native tab switcher. Kitty
knows tab titles and cwd; Tatami knows workspace names, folders, quick access,
remote metadata, worktrees, and layout templates.

## Layout Support

Kitty can create tabs and panes, but its pane model differs from Zellij/Tmux.
Start with simple tab support first:

- local workspace: open tab with cwd
- remote workspace: open tab and run SSH command
- worktree: open tab with worktree cwd

Add Kitty pane layout support later if needed:

```sh
kitty @ launch --type=window --location=vsplit --cwd "$path" "$command"
kitty @ launch --type=window --location=hsplit --cwd "$path" "$command"
```

Map Tatami directions:

- `right` -> `vsplit`
- `down` -> `hsplit`
- `stack` -> not supported initially

## Implementation Phases

### Phase 1: Basic Kitty Backend

- Add `internal/shell/kitty.go`.
- Parse `kitty @ ls` JSON.
- Detect current Kitty tab from `KITTY_WINDOW_ID`.
- Add `NewTab`, `FocusTab`, and `SetTabTitle`.
- Add `~/.local/share/tatami/kitty_sessions.json`.
- Add action menu support for `new tab`, `switch to existing tab`, and
  `attach current tab`.

### Phase 2: Search and Session UI

- Add a Tatami view that lists tracked Kitty sessions.
- Search by workspace name, folder, path, tab title, and worktree branch.
- Mark stale sessions visually and allow cleanup.

### Phase 3: Worktrees and Remote Workspaces

- Track one or more Kitty tabs per workspace.
- Add `kind` values: `default`, `worktree`, `remote`, `custom`.
- Reuse existing worktree UI to open/focus Kitty tabs.
- Add SSH launch support for remote workspaces.

### Phase 4: Kitty Layouts

- Add optional Kitty layout type:

```json
{
  "layout": {
    "type": "kitty",
    "main_cmd": "nvim",
    "panes": [
      { "command": "lazygit", "direction": "right" },
      { "command": "", "direction": "down" }
    ]
  }
}
```

- Support `right` and `down`.
- Leave `stack` unsupported unless there is a clear Kitty equivalent.

## Non-Goals

- Do not proxy terminal input/output.
- Do not replace Kitty's tab bar.
- Do not require Zellij or Tmux for Kitty mode.
- Do not store runtime Kitty tab IDs in `workspaces.json`.
- Do not close user tabs automatically.

## Open Questions

- Should Tatami focus existing tabs by default, or always ask when multiple
  tabs exist for one workspace?
- Should tracked sessions be global or scoped per Kitty OS window?
- Should Tatami create tabs in the current Kitty OS window only?
- Should `attach current tab` update the Kitty tab title automatically?
- Should Kitty support be enabled automatically or behind a config flag?

