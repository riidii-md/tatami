# Mobile navigation with Termius

Tatami's mobile mode is intended for phone-sized SSH terminals, with Termius as
the primary tested interaction model.

Start it explicitly:

```bash
tatami --mobile
```

For one-tap access, set `tatami --mobile` as the Termius host Startup Command or
create a shell alias for it.

## Navigation model

- Swipe with Termius's arrow gesture, or use `↑` / `↓`, to move through rows.
- Visible choices are numbered from `1` through `9`. A number selects its row;
  `Enter` is still required to open or confirm it.
- Press `b` to go back from menus. At the home root it does nothing instead of
  unexpectedly closing Tatami.
- While a text field is focused, letters and numbers remain normal input. Use
  `Esc` to cancel an input form.
- Destructive operations keep their existing confirmation step.

Mobile mode removes decorative menu borders, hides project paths on the home
screen, uses shorter help text, and limits each numbered home page to nine
visible rows. Narrow terminals automatically use compact home rows even without
`--mobile`, but numbered shortcuts and `b` navigation require mobile mode.

## Recommended Termius shortcut bar

Add these keys to the customizable Termius shortcut bar:

- `Esc`
- `Tab`
- `Shift+Tab`
- `Enter`
- `Ctrl`

Termius supports customizable special keys and touch gestures that emit arrow
keys, so Tatami does not depend on terminal mouse reporting. See Termius's
[mobile agent tips](https://termius.com/blog/8-tips-for-using-ai-agents-on-mobile-in-termius)
and [Touch Terminal overview](https://termius.com/blog/new-touch-terminal-on-ios).
