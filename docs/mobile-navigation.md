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

- Tap a visible home row to select and open it. Numbered submenu choices are
  also tappable.
- Use the mouse wheel, long-press and drag with Termius's arrow gesture, or use
  `↑` / `↓`, to move through rows.
- The first nine visible choices have `1` through `9` shortcuts. A number
  selects its row; `Enter` opens or confirms it.
- Press `b` to go back from menus. At the home root it does nothing instead of
  unexpectedly closing Tatami.
- While a text field is focused, letters and numbers remain normal input. Use
  `Esc` to cancel an input form.
- Destructive operations keep their existing confirmation step.

Mobile mode removes decorative menu borders, hides project paths on the home
screen, uses shorter help text, and uses the full available terminal height.
Narrow terminals automatically use compact home rows even without `--mobile`,
but touch/mouse reporting, numbered shortcuts, and `b` navigation require
mobile mode.

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
