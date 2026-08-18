# Herdr Hub

Tatami's Herdr Hub is a personal, federated session inventory. The local
machine is always present. Add remote machines in `~/.config/tatami/herdr-hosts.json`:

```json
{"hosts":[{"id":"workbox","label":"Workbox","target":"workbox"}]}
```

`target` can be an SSH-config alias, hostname, IP address, or a direct
`user@host` destination such as `oles@bmo.local`. Herdr's
`ssh://user@host:port` form is also supported. It is never treated as a host
command. Direct destinations use normal OpenSSH keys and `ssh-agent`. For a
specific key, define an alias in `~/.ssh/config`:

```sshconfig
Host macmini
  HostName bmo.local
  User oles
  IdentityFile ~/.ssh/id_ed25519
  IdentitiesOnly yes
```

Then use `macmini` as the Tatami destination. Tatami performs background
inventory with `ssh -o BatchMode=yes`, so a host requiring a password is shown
as authentication-needed instead of prompting in the TUI. Interactive
attachment is delegated to `herdr --remote <target> --session <name>`.

When a selected host shows `authentication-needed`, Tatami displays the
passwordless-SSH setup command and retry action. For a host that accepts account
passwords, run the displayed command in another terminal:

```sh
ssh-copy-id user@host
```

Complete the password prompt, verify `ssh user@host` no longer needs a password,
then press `r` in Tatami. If the remote already authorizes a different key, use
an SSH-config alias with `IdentityFile` instead. Tatami stores neither account
passwords nor private keys.

The private inventory cache lives at `$XDG_STATE_HOME/tatami/herdr-hub.json`
with mode `0600`. It contains only endpoint state and session inventory, never
credentials, SSH keys, command arguments, pane contents, prompts, or terminal
frames. Cached data may be shown as stale while a host is unreachable.

Remote stop, delete, creation, workspace/layout creation, live previews,
notifications, and metrics probes are deliberately deferred. Remote CPU and
RAM are unavailable until Herdr defines a compatible read-only metrics
capability.

Tatami renders cached inventory first, then refreshes endpoints independently
in the background. Use `r` to refresh the selected endpoint (or all endpoints
when no endpoint is selected), and `R` to refresh all. `Space` collapses an
endpoint; `a`, `e`, and `d` add, edit, and remove a selected remote host. Saving
a host performs the same asynchronous non-interactive inventory test. Remote
rows are attach-only: `Enter` selects the exact endpoint/session pair and does
not expose local stop or delete actions. Entering a stopped named session lets
Herdr use its normal restore path. For safe non-interactive SSH inventory,
remote session names must use letters, numbers, `.`, `_`, or `-`.
