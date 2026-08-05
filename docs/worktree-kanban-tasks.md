# Worktree Kanban Tasks

Tatami can grow from a workspace launcher into a small project cockpit: each Git
project gets a lightweight Kanban board where cards are tied to real branches,
worktrees, panes, and agent sessions.

## Goals

- Track what is left for a project without leaving the terminal.
- Start a task by creating or focusing the matching Git worktree.
- Keep task status connected to real development state, not just manual card
  movement.
- Make deletion safe: removing or archiving a task should clean up the worktree
  only when it is safe to do so.
- Stay local-first. Task metadata should live in Tatami config/state, not in a
  synced notes repo and not in Git unless the user exports it intentionally.

## Default board

Keep the first version simple:

```text
TODO -> DOING -> REVIEW -> DONE
```

Optional future states:

```text
BACKLOG -> READY -> DOING -> BLOCKED -> REVIEW -> DONE -> ARCHIVED
```

## User workflow

```bash
# Create a task for the current project.
tatami task new "Fix login redirect"

# Start a task: create/focus its worktree and move the card to DOING.
tatami task start t_abc123

# Open the task's pane/worktree again later.
tatami task focus t_abc123

# Mark ready for human review after tests/diff are prepared.
tatami task review t_abc123

# Finish and clean up the worktree when safe.
tatami task done t_abc123 --delete-worktree
```

The TUI can expose the same lifecycle as a board view:

```text
Project: tatami

TODO                  DOING                    REVIEW                  DONE
Fix login redirect    Add retry logic          Refactor config loader  Old auth cleanup
                      branch: task/retry      needs human review
                      agent: codex running
```

## Data model

Each card should carry enough information to reconnect the task to the actual
work area:

```yaml
id: t_abc123
title: Fix login redirect
project_path: /home/user/src/app
status: doing
base_branch: main
branch: task/fix-login-redirect
worktree_path: /home/user/src/app/.worktrees/task-fix-login-redirect
pane:
  backend: zellij
  session: app
  tab: task-fix-login-redirect
agent:
  cli: codex
  status: running
  tracking: tracked
created_at: 2026-07-13T12:00:00Z
updated_at: 2026-07-13T12:15:00Z
```

A per-project task store can live under Tatami state, keyed by canonical Git repo
root. For example:

```text
~/.local/state/tatami/projects/<repo-id>/tasks.json
```

## Worktree integration

`tatami task start` should:

1. Resolve the current workspace's Git repo root.
2. Create a branch name from the card, for example `task/fix-login-redirect`.
3. Create a Git worktree using Tatami's existing worktree helpers.
4. Open the worktree with the saved layout or a chosen template.
5. Move the card to `DOING`.
6. Record pane/session metadata when available.

If the worktree already exists, Tatami should focus/open it instead of creating a
duplicate.

## Safe cleanup

`tatami task done --delete-worktree` should be conservative:

- Refuse if the worktree has uncommitted changes unless `--force` is passed.
- Warn if the task branch is not merged and no PR/commit reference is recorded.
- Delete the worktree only after the safety checks pass.
- Move the card to `DONE` after successful cleanup.
- Preserve task metadata for history even when the worktree is gone.

`tatami task archive` should hide the card from the default board without
pretending the work was completed.

## Agent session awareness

Tatami should distinguish confidence levels when showing agents attached to a
card:

- `tracked`: launched by Tatami, full lifecycle known.
- `discovered`: found by process/TTY/CWD scanning, partial lifecycle known.
- `attached`: manually linked by the user.
- `stale`: previously known process or pane disappeared.

The board should avoid claiming `done` or `blocked` from process liveness alone.
Use `running` or `unknown` unless a lifecycle hook or explicit user action sets a
stronger state.

## First implementation slice

1. Add local task storage keyed by repo root.
2. Add CLI commands:
   - `tatami task new <title>`
   - `tatami task list`
   - `tatami task start <id>`
   - `tatami task review <id>`
   - `tatami task done <id> [--delete-worktree]`
   - `tatami task archive <id>`
3. Wire `task start` to existing Git worktree creation/opening.
4. Add a board/list TUI view with Todo/Doing/Review/Done columns.
5. Add safe worktree deletion checks before `done --delete-worktree`.

## Non-goals for the first version

- Remote synchronization between machines.
- GitHub Projects/Jira/Linear sync.
- Mandatory transcript logging from agent panes.
- Automatic status inference from screen scraping.

Those can come later, but the initial feature should make the local
project/task/worktree loop feel instant and reliable.
