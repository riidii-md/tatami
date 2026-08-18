# Federated Tatami Hub

Tatami's hub is a personal, federated navigator for workspaces and Herdr
sessions. The local machine is always present. Add a remote machine from the
home screen with `a`, or edit `~/.config/tatami/herdr-hosts.json`:

```json
{"hosts":[{"id":"workbox","label":"Workbox","target":"workbox"}]}
```

`target` can be an SSH-config alias, hostname, IP address, a `user@host`
destination, or Herdr's `ssh://user@host:port` form. It is always passed as an
argument and is never treated as a shell command. For a specific key, define an
alias in `~/.ssh/config`:

```sshconfig
Host macmini
  HostName bmo.local
  User oles
  IdentityFile ~/.ssh/id_ed25519
  IdentitiesOnly yes
```

Then use `macmini` as the Tatami destination. Tatami stores no SSH passwords,
key passphrases, or private-key material.

## What a remote machine exposes

A current Tatami installation answers the versioned, read-only command:

```sh
tatami hub inventory --json
```

After discovery, the remote machine remains inside the normal home screen and
shows its:

- Quick Access workspaces
- Tatami projects and folders
- named Herdr sessions
- saved downstream Tatami hosts

The inventory contains only the display and connection metadata required to
navigate. It excludes SSH key paths, layout commands, agent command arguments,
pane contents, prompts, terminal frames, and credentials. An older remote
without the inventory command falls back to a Herdr-session-only view.

## Authentication and refresh

Background discovery uses `ssh -o BatchMode=yes`, so it never takes over the
TUI with a password prompt. A host that needs authentication is marked
`authentication-needed`. Highlight it and press `Enter`; OpenSSH then owns the
terminal and may ask for an account password or encrypted-key passphrase. When
the command completes, Tatami stays on its main screen and expands the newly
discovered content.

Passwords work for interactive discovery and opening. For automatic background
refresh, configure non-interactive SSH. Load an encrypted key into your agent:

```sh
ssh-add ~/.ssh/private-key
```

Or install the public key once:

```sh
ssh-copy-id user@host
ssh -o BatchMode=yes user@host true
```

For a downstream host, Tatami shows the equivalent `ProxyJump` form in the
on-screen authentication help.

## Bastions and downstream hosts

Discovery is lazy. Tatami contacts only hosts you have explicitly opened; it
does not automatically authenticate to every machine found on a remote. If a
laptop knows `bastion`, and the Tatami installation on `bastion` knows
`macmini`, opening `bastion` reveals `macmini` as a child. Opening that child
uses local OpenSSH ProxyJump:

```sh
ssh -J bastion macmini
```

Longer saved chains work the same way, up to four SSH hops. Tatami detects
repeated host IDs or targets as cycles. All hop credentials remain on the
machine running the visible Tatami process: Tatami never enables `ssh -A` or
`ForwardAgent`. Remote Herdr sessions, workspaces, and new panes/tabs all
preserve the selected route. Federated inventory intentionally omits layout
commands, so a discovered workspace opens without executing remote-supplied
layout automation.

## Controls and cache

- `Enter` expands an online host, authenticates/discovers an unavailable host,
  or opens the selected workspace/session.
- `Space` collapses or expands a host without connecting.
- `r` refreshes the selected host, including a discovered downstream host.
- `R` refreshes saved top-level hosts.
- `/` filters local and discovered workspaces, sessions, and host labels.
- `a`, `e`, and `d` manage top-level saved hosts. Downstream hosts are managed
  on the Tatami installation that owns them.

The private inventory cache lives at
`$XDG_STATE_HOME/tatami/herdr-hub.json` with mode `0600`. Cached successful data
may remain visible as stale while a host is unreachable. Remote rows are
navigation-only; destructive session and workspace operations remain local.
Remote CPU and RAM are unavailable until Herdr defines a compatible read-only
metrics capability.
