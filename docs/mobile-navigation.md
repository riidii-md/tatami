# Mobile navigation with Termius

Tatami's mobile mode is intended for phone-sized SSH terminals, with Termius as
the primary tested interaction model.

Start it explicitly:

```bash
tatami --mobile
# Short form for a phone keyboard:
tatami -m
```

For one-tap access, set `tatami --mobile` as the Termius host Startup Command or
create a shell alias for it.

## Navigation model

- Every selectable collection starts with its search field focused. Type to
  filter immediately, or press `↓` without typing to browse the full list.
- After filtering, press `↓` to enter browse focus on the first result. Press
  `↑` from that first row or `/` to return to the query without losing it.
- Tap a visible home row to select and open it. Numbered submenu choices are
  also tappable. Taps target the rendered result itself, so a filtered or
  asynchronously reordered list cannot turn a tap into query text.
- Use the mouse wheel, long-press and drag with Termius's arrow gesture, or use
  `↑` / `↓`, to move through rows.
- In search focus, `1` through `9` and `b` are normal query text. In browse
  focus, the first nine visible choices use `1` through `9`; `Enter` opens the
  selected result, and `b` goes back from menus.
- `Esc` clears a non-empty query first. With an empty query it performs the
  screen's usual back action. At the home root, mobile `b` in browse focus does
  nothing instead of unexpectedly closing Tatami.
- Existing letter commands such as delete, refresh, and edit work only in
  browse focus. Destructive operations retain their separate confirmation.
- Destructive operations keep their existing confirmation step.

Mobile mode removes decorative menu borders, hides project paths on the home
screen, uses shorter help text, and uses the full available terminal height.
Narrow terminals automatically use compact home rows even without `--mobile`,
but touch/mouse reporting, numbered shortcuts, and `b` navigation require
mobile mode.

Search covers the currently known local and cached federated hierarchy,
including worktrees and safe metadata such as repository identity. It does not
contact remote hosts or run Git/Herdr commands while you type, so stale,
offline, or undiscovered remote content may remain incomplete until refreshed.

## Recommended Termius shortcut bar

Add these keys to the customizable Termius shortcut bar:

- `Esc`
- `Tab`
- `Shift+Tab`
- `Enter`
- `Ctrl`

Termius supports customizable special keys, taps, and touch gestures that emit
arrow keys. Tatami mobile mode enables terminal mouse reporting for taps while
keeping those keyboard gestures available. See Termius's
[mobile agent tips](https://termius.com/blog/8-tips-for-using-ai-agents-on-mobile-in-termius)
and [Touch Terminal overview](https://termius.com/blog/new-touch-terminal-on-ios).
