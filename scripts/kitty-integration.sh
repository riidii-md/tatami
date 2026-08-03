#!/usr/bin/env bash
# Tatami x Kitty integration
#
# Configures the Kitty terminal so that opening a new tab launches Tatami
# directly (instead of a plain shell). Kitty launches the command without a
# login shell, so an absolute path to the `tatami` binary is required.
#
# Usage:
#   scripts/kitty-integration.sh            # install / update the config block
#   scripts/kitty-integration.sh --remove   # remove the config block
#   scripts/kitty-integration.sh --print     # print the block without writing
#
# Environment overrides:
#   KITTY_CONFIG   path to kitty.conf (default: $XDG_CONFIG_HOME/kitty/kitty.conf)
#   TATAMI_BIN     path to the tatami binary (default: resolved from PATH)

set -euo pipefail

MARKER_BEGIN="# >>> tatami kitty integration >>>"
MARKER_END="# <<< tatami kitty integration <<<"

mode="install"
case "${1:-}" in
    --remove) mode="remove" ;;
    --print)  mode="print" ;;
    "") ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
esac

# --- resolve kitty.conf --------------------------------------------------------
kitty_conf="${KITTY_CONFIG:-${XDG_CONFIG_HOME:-$HOME/.config}/kitty/kitty.conf}"

# --- resolve tatami binary (absolute, symlinks followed) -----------------------
resolve_bin() {
    local bin="${TATAMI_BIN:-$(command -v tatami || true)}"
    if [[ -z "$bin" ]]; then
        echo "error: 'tatami' not found on PATH. Install it or set TATAMI_BIN." >&2
        exit 1
    fi
    # normalise to an absolute path
    if [[ "$bin" != /* ]]; then
        bin="$(command -v "$bin")"
    fi
    printf '%s\n' "$(cd "$(dirname "$bin")" && pwd)/$(basename "$bin")"
}

# --- build the config block ----------------------------------------------------
build_block() {
    local bin="$1"
    cat <<EOF
$MARKER_BEGIN
# Open a new tab straight into Tatami. Kitty runs the command without a login
# shell, so the absolute path is required (regenerate with the setup script if
# tatami moves). Use the *-shift-t bindings for a plain shell tab.
map kitty_mod+t launch --type=tab --cwd=current $bin
map kitty_mod+shift+t new_tab_with_cwd
# macOS: cmd+t / cmd+shift+t are Kitty's built-in tab shortcuts; override them too.
map cmd+t launch --type=tab --cwd=current $bin
map cmd+shift+t new_tab_with_cwd
$MARKER_END
EOF
}

# --- strip any existing block --------------------------------------------------
strip_block() {
    local file="$1"
    [[ -f "$file" ]] || return 0
    # delete the inclusive range between the markers
    sed "/^${MARKER_BEGIN}$/,/^${MARKER_END}$/d" "$file"
}

reload_kitty() {
    # Kitty reloads its config on SIGUSR1.
    if pgrep -x kitty >/dev/null 2>&1; then
        pkill -USR1 -x kitty && echo "Reloaded running Kitty instance(s)."
    else
        echo "No running Kitty found; changes apply on next launch."
    fi
}

case "$mode" in
    print)
        build_block "$(resolve_bin)"
        exit 0
        ;;
    remove)
        if [[ ! -f "$kitty_conf" ]]; then
            echo "Nothing to do: $kitty_conf does not exist."
            exit 0
        fi
        tmp="$(mktemp)"
        strip_block "$kitty_conf" > "$tmp"
        mv "$tmp" "$kitty_conf"
        echo "Removed Tatami integration block from $kitty_conf"
        reload_kitty
        ;;
    install)
        bin="$(resolve_bin)"
        mkdir -p "$(dirname "$kitty_conf")"
        touch "$kitty_conf"
        tmp="$(mktemp)"
        # keep everything except a previous block (trailing blank lines trimmed),
        # then append a fresh block separated by a single blank line
        strip_block "$kitty_conf" \
            | awk 'NF{last=NR} {line[NR]=$0} END{for(i=1;i<=last;i++) print line[i]}' > "$tmp"
        [[ -s "$tmp" ]] && printf '\n' >> "$tmp"
        build_block "$bin" >> "$tmp"
        mv "$tmp" "$kitty_conf"
        echo "Configured Kitty at: $kitty_conf"
        echo "Tatami binary:       $bin"
        echo
        echo "New tab  -> Tatami       (kitty_mod+t, and cmd+t on macOS)"
        echo "New tab  -> plain shell  (kitty_mod+shift+t, and cmd+shift+t on macOS)"
        echo
        reload_kitty
        ;;
esac
