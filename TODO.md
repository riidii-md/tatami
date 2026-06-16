# Tatami TODO

## Agent session tracking via `tatami run`

**Status:** proposed

**Goal:** make Tatami able to launch and track AI agent CLI sessions such as Claude Code, Codex, Gemini, OpenCode, etc. while keeping the normal interactive terminal UX.

### Problem

Tatami currently supports opening workspaces/layouts in Zellij and tmux, and templates can start AI CLIs by putting commands like `claude`, `codex`, or `gemini` into panes. This launches agents, but Tatami does not own or track the lifecycle of those agent processes.

If a user opens a pane and manually runs:

```sh
claude
```

Tatami has no reliable record of:

- which workspace the agent belongs to
- which multiplexer is hosting it
- which Zellij/tmux pane contains it
- whether it is still running
- whether it exited
- how to list all running agent sessions later

### Proposed UX

Introduce a wrapper command:

```sh
tatami run claude
tatami run codex
tatami run gemini
tatami run opencode
```

The command should behave like running the original agent directly. For example:

```sh
tatami run claude
```

should feel like:

```sh
claude
```

The user should still get the full interactive Claude terminal session, stdin/stdout/stderr, colors, keyboard handling, and TTY behavior.

### Core behavior

When `tatami run <agent> [args...]` starts, Tatami should:

1. detect current multiplexer context
2. record the current Zellij/tmux pane information if available
3. record cwd, tty, command, start time, and process id
4. launch the real agent CLI as a child process
5. stream stdin/stdout/stderr directly between the terminal and child process
6. wait for the child process to exit
7. update the registry with exit time, exit code, and final status

Do **not** use `syscall.Exec` for the normal wrapper path, because Tatami needs to survive after the child exits in order to update the registry.

Use something like:

```go
cmd := exec.Command(realCommand, args...)
cmd.Stdin = os.Stdin
cmd.Stdout = os.Stdout
cmd.Stderr = os.Stderr
cmd.Env = os.Environ()
cmd.Dir = cwd

recordStarted(...)
err := cmd.Run()
recordExited(...)
```

### Zellij context

Inside a Zellij pane, Zellij exposes the pane id through:

```sh
ZELLIJ_PANE_ID
```

Tatami should record at least:

```json
{
  "mux": "zellij",
  "zellij_pane_id": "terminal_7",
  "cwd": "/path/to/repo",
  "tty": "/dev/pts/12"
}
```

Zellij CLI actions may be used later to enrich this with tab/session information:

```sh
zellij action current-tab-info
zellij action query-tab-names
zellij action list-panes
zellij action list-tabs
```

Do not block MVP on perfect Zellij session metadata. `ZELLIJ_PANE_ID`, cwd, tty, PID, command, and status are enough for the first useful version.

### tmux context

Inside tmux, Tatami can use `$TMUX` plus `tmux display-message` / `tmux list-panes` to record session/window/pane metadata.

Useful tmux commands:

```sh
tmux display-message -p '#S'
tmux display-message -p '#I'
tmux display-message -p '#P'
tmux display-message -p '#{pane_id}'
tmux display-message -p '#{pane_current_path}'
tmux list-panes -a -F '#{session_name} #{window_index} #{pane_index} #{pane_id} #{pane_pid} #{pane_current_command} #{pane_current_path}'
```

### Agent registry

Add a persistent local registry, probably under XDG state rather than config:

```txt
~/.local/state/tatami/agents.json
```

or, if concurrent writes become painful:

```txt
~/.local/state/tatami/agents.sqlite
```

Suggested initial struct:

```go
type AgentRun struct {
    ID          string     `json:"id"`
    Agent       string     `json:"agent"`        // claude, codex, gemini, opencode
    Command     []string   `json:"command"`
    Workspace   string     `json:"workspace,omitempty"`
    Path        string     `json:"path"`
    TTY         string     `json:"tty,omitempty"`

    Mux         string     `json:"mux,omitempty"` // zellij, tmux, none
    Session     string     `json:"session,omitempty"`
    Tab         string     `json:"tab,omitempty"`
    Pane        string     `json:"pane,omitempty"`

    PID         int        `json:"pid,omitempty"`
    Status      string     `json:"status"` // running, exited, stale, unknown
    StartedAt   time.Time  `json:"started_at"`
    EndedAt     *time.Time `json:"ended_at,omitempty"`
    ExitCode    *int       `json:"exit_code,omitempty"`

    Tracking    string     `json:"tracking"` // wrapped, discovered, manual
    LastSeenAt  time.Time  `json:"last_seen_at"`
}
```

### CLI commands

Add commands such as:

```sh
tatami run <agent> [args...]
tatami agents
tatami agents list
tatami agents status <id>
tatami agents prune
tatami agents attach <id>      # future
tatami agents focus <id>       # future
tatami agents notify --watch   # future
```

MVP should implement:

```sh
tatami run <agent> [args...]
tatami agents list
```

### TUI integration

Add an Agents view in the Tatami TUI, probably bound to `a` from the main list view.

Example:

```txt
AI Agents

● claude   tatami        running   12m   zellij terminal_7
● codex    hermes-agent  running   48m   tmux %3
○ gemini   old-project   exited    2h    zellij terminal_2

[enter]focus  [r]refresh  [p]prune  [esc]back
```

The first TUI version can be read-only plus refresh. Focus/attach can come later.

### Answered design questions

#### 1. Can we enter / return to the specific Claude session?

**Yes, but the implementation quality depends on the multiplexer.**

For Zellij:

- Tatami can record `ZELLIJ_PANE_ID` when `tatami run claude` starts.
- Later, Tatami can use Zellij actions to focus a pane by id if available in the installed Zellij version.
- If exact focus is not reliable, Tatami can at least show the pane id, cwd, and command, and open/focus the containing workspace/tab later once metadata support is improved.

For tmux:

- This is easier because tmux exposes stable session/window/pane targets.
- Tatami can store `#{pane_id}` and later focus it with something like:

```sh
tmux switch-client -t <session>
tmux select-window -t <session>:<window>
tmux select-pane -t <pane_id>
```

MVP should store enough metadata to make future attach/focus possible even if the first version only lists sessions.

#### 2. Can we see updates / notifications for running Claude sessions?

**Yes, in stages.**

MVP:

- show `running`, `exited`, `stale`, and runtime duration
- update status when the wrapped command exits
- `tatami agents list` and TUI Agents view show current state

Next stage:

- a lightweight watcher can periodically check if recorded PIDs still exist
- if a process exits unexpectedly, mark it stale/exited
- emit terminal notifications, desktop notifications, or TUI badges when an agent exits

Richer Herdr-like states such as `working`, `blocked`, `done`, and `idle` require one of:

- parsing the agent's visible screen/output
- agent-specific lifecycle hooks
- integration with the agent's own status/session files if available
- optional log capture or explicit status reporting

Do not overpromise rich status in MVP. Start with lifecycle state, then add agent-specific status later.

#### 3. Can we see all running sessions?

**Yes. This is the main purpose of the registry.**

Because all agents launched through `tatami run` are recorded, Tatami can show them in:

```sh
tatami agents list
```

and in the TUI Agents view.

Example output:

```txt
ID        Agent   Status    Age   Project       Mux      Pane
abc123    claude  running   12m   tatami        zellij   terminal_7
def456    codex   running   3m    hermes-agent  tmux     %4
ghi789    gemini  exited    1h    old-project   zellij   terminal_2
```

### Important limitation

This approach only perfectly tracks agents launched through Tatami:

```sh
tatami run claude
```

If the user bypasses Tatami and runs the raw binary directly:

```sh
claude
```

Tatami will not know unless a later passive discovery layer is added.

Possible later discovery layers:

- shell hook: zsh `preexec` / bash trap reports agent commands to Tatami
- process scanner: find `claude`, `codex`, etc. by process list and TTY
- Zellij plugin: report pane/process/screen events directly to Tatami

### Additional ideas

#### Shell aliases for normal agent UX

Offer optional shell aliases/functions so the user can keep typing familiar commands while still going through Tatami:

```sh
alias claude='tatami run claude'
alias codex='tatami run codex'
alias gemini='tatami run gemini'
alias opencode='tatami run opencode'
```

This should be opt-in and documented clearly, because users may sometimes want to bypass Tatami and run the raw binary.

#### Agent templates should use Tatami tracking automatically

Update built-in AI templates so panes created by Tatami are tracked by default:

```go
MainCmd: "tatami run claude"
Pane{Command: "tatami run codex", Direction: "right"}
```

This means the existing Tatami workspace/layout UX becomes the recommended way to start tracked AI sessions.

#### Focus / enter running sessions

Add `tatami agents focus <id>` as the command for jumping back into a specific running agent.

For tmux, store enough to target the pane directly:

```txt
session name
window index/name
pane id
```

For Zellij, store:

```txt
ZELLIJ_PANE_ID
tab id/name if discoverable
session name if discoverable
```

Then attempt to focus with Zellij actions. If exact pane focus is unavailable or unreliable in a Zellij version, degrade gracefully:

1. show the pane id and cwd
2. focus/open the containing workspace/tab if known
3. print a manual command/hint

The important UX is that the Agents view should have a clear `[enter]focus` path even if some muxes initially provide a less perfect jump.

#### Notifications / updates

Add lifecycle notifications first:

- agent started
- agent exited successfully
- agent exited with non-zero code
- agent became stale because the PID disappeared

Possible notification surfaces:

- TUI badge in the Agents view
- terminal bell / OSC notification if available
- desktop notification through `notify-send` on Linux or `osascript` on macOS
- optional webhook/event stream later

Suggested config:

```json
{
  "agents": {
    "notifications": true,
    "notify_on_exit": true,
    "notify_on_failure": true,
    "notify_on_blocked": false
  }
}
```

Do not make rich `blocked` notifications part of MVP. They require deeper agent-specific state detection.

#### Stale process reconciliation

Add `tatami agents reconcile` or make `tatami agents list` opportunistically reconcile stale records:

1. load registry
2. for every `running` agent, check if PID still exists
3. if PID is gone, mark as `stale` or `exited_unknown`
4. if PID exists but command no longer matches, mark as `unknown`

This helps if Tatami is killed before it can write the final exit status.

#### Agent status levels

Keep lifecycle status separate from semantic status:

```txt
Lifecycle: running | exited | stale | unknown
Semantic: idle | working | blocked | done | unknown
Tracking:  wrapped | discovered | manual
```

MVP should only implement lifecycle + tracking. Semantic state can be added later via screen parsing, hooks, or integrations.

#### Optional passive discovery later

After `tatami run` exists, add passive discovery as a second layer:

- process scanner for known agent command names
- shell preexec hook for commands typed manually
- Zellij plugin if deeper pane/screen events are needed

Discovered agents should be shown with lower confidence:

```txt
claude   running   discovered   cwd=/repo   tty=/dev/pts/12
```

This prevents confusion between fully tracked sessions and guessed sessions.

#### Privacy / logs

Do not capture transcripts by default. Agent terminals can contain credentials, prompts, proprietary code, or private messages.

If log capture is added later:

- make it opt-in
- store under `~/.local/state/tatami/agents/`
- never store in the repo or synced notes by default
- show clearly when logging is active

#### Tatami dashboard for all sessions

Add a dedicated dashboard entry point:

```sh
tatami dashboard
# or shorter alias later:
tatami dash
```

The dashboard should open a TUI focused on **all active Tatami/Zellij/tmux context**, not only the current workspace list.

Goal: one place to see and enter:

- AI agent sessions started with `tatami run ...`
- normal Tatami workspaces
- Zellij sessions/tabs/panes where discoverable
- tmux sessions/windows/panes where discoverable
- exited/stale agent runs
- remote workspaces if they are represented in Tatami's workspace registry

Possible layout:

```txt
Tatami Dashboard

AI Agents
  ● claude   running   tatami        zellij terminal_7   12m
  ● codex    running   hermes-agent  tmux %4             3m
  ○ gemini   exited    old-project   zellij terminal_2   1h

Workspaces
  ◆ tatami        /home/oles/work/tatami        zellij
  ◆ hermes-agent  /home/oles/.hermes/hermes-agent tmux

Zellij Sessions
  ● main          current    3 tabs
  ○ old-session   exited

Tmux Sessions
  ● agent         4 windows
  ● dev           2 windows

[enter]open/focus  [a]agents  [w]workspaces  [z]zellij  [t]tmux  [r]refresh  [esc]quit
```

Preferred two-pane dashboard layout:

```txt
┌──────────────────────────────┬──────────────────────────────────────┐
│ Tatami Dashboard             │ Notifications / Agent Detail          │
│                              │                                      │
│ AI Agents                    │ claude · tatami                      │
│  ● claude   tatami     12m   │ Status: running                      │
│  ● codex    hermes      3m   │ Mux: zellij                          │
│                              │ Pane: terminal_7                     │
│ Workspaces                   │ Cwd: /home/oles/work/tatami          │
│  ◆ tatami                    │ Started: 12m ago                     │
│  ◆ hermes-agent              │                                      │
│                              │ Recent events                        │
│ Zellij Sessions              │ 12:04 started                        │
│  ● main                      │ 12:06 still running                  │
│                              │                                      │
│ Tmux Sessions                │ Actions                              │
│  ● agent                     │ [enter] focus agent                  │
│  ● dev                       │ [o] open/focus                       │
│                              │ [l] logs later                       │
│                              │ [k] kill later                       │
└──────────────────────────────┴──────────────────────────────────────┘
```

Dashboard behavior:

- Left pane is the navigation list grouped by `AI Agents`, `Workspaces`, `Zellij Sessions`, and `Tmux Sessions`.
- Right pane shows notifications plus details/actions for the currently selected item.
- When an AI agent is selected, right pane should prioritize agent detail and actions.
- `[enter]` on an AI agent should focus/open that exact agent session if possible.
- `[enter]` on a normal workspace should open the existing workspace action menu.
- `[enter]` on Zellij/tmux sessions should attach/focus the session where possible.
- Later, the right pane can show richer events such as `blocked`, `done`, or `needs input` once semantic agent state exists.

Dashboard should be read-only first, then become navigable.

Suggested dashboard views:

```txt
DashboardHome
DashboardAgents
DashboardWorkspaces
DashboardZellijSessions
DashboardTmuxSessions
DashboardDetails
```

Important behavior:

- `[enter]` on an AI agent should try `tatami agents focus <id>`.
- `[enter]` on a normal workspace should open the existing workspace action menu.
- `[enter]` on a Zellij session should attach/focus if not already nested in Zellij.
- `[enter]` on a tmux session should attach/switch if possible.
- Dashboard should clearly show when exact focus is unavailable and print a manual hint instead of failing silently.

Implementation idea:

- Reuse existing `SessionView` concepts, but generalize from only Zellij sessions to a broader dashboard model.
- Add `internal/dashboard` or keep under `internal/tui` initially.
- Data sources should be separate from rendering:
  - agent registry provider
  - workspace store provider
  - Zellij session provider
  - tmux session provider
- Each item should have a `Kind`, `Label`, `Status`, `Subtitle`, and optional `Action` target.

This dashboard can become Tatami's Herdr-like overview without requiring Tatami to replace Zellij/tmux as the multiplexer.

### Recommended implementation order

1. Add `internal/agent` package with registry load/save/list/update.
2. Add `tatami run <agent> [args...]` command path before launching the Bubble Tea TUI.
3. Detect Zellij/tmux context and record pane metadata.
4. Run the real agent as an interactive child process.
5. Update the registry on process exit.
6. Add `tatami agents list` CLI output.
7. Update built-in AI templates to use `tatami run ...`.
8. Add a read-only TUI Agents view.
9. Add `tatami dashboard` as a read-only overview of agents, workspaces, Zellij sessions, and tmux sessions.
10. Add stale reconciliation for crashed wrappers.
11. Add `tatami agents focus <id>` for tmux first, then Zellij.
12. Make dashboard entries navigable via `[enter]` where focus/attach is supported.
13. Add lifecycle notifications.
14. Later add passive discovery and semantic agent status.

### Acceptance criteria for MVP

- Running `tatami run claude` starts an interactive Claude session normally.
- While Claude is running, `tatami agents list` shows it as `running`.
- After Claude exits, `tatami agents list` shows it as `exited` with exit code and end time.
- Inside Zellij, the run records `ZELLIJ_PANE_ID`.
- Inside tmux, the run records tmux pane/session metadata where possible.
- Existing workspace/layout behavior continues to work.
- AI templates can be updated to run `tatami run claude`, `tatami run codex`, etc. instead of raw commands.
