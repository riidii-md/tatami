# Federated Tatami Hub

Tatami's hub navigates local and remote workspaces, Herdr sessions, and saved
downstream hosts. The local machine is always present.

## Saved SSH host setup

In home-screen browse focus, press `a` to add or `e` to edit a saved root.

| Section | Settings |
|---|---|
| Host | Alias (display name), group, comma-separated tags |
| Connection | SSH mode, hostname/OpenSSH alias, username, port, ProxyJump |
| Credentials | OpenSSH config/agent, password prompt, identity, certificate + identity, security-key/FIDO2 identity |
| Advanced | Advertised destination, local jump alias, Tatami/Herdr executable overrides |

Explicit mode separates hostname, username and port. Alias mode delegates these
and credentials to an existing OpenSSH config alias. Existing hosts keep their
opaque legacy destination until you explicitly change the mode. Internal IDs
are generated once and survive display-name edits. Groups organize saved
roots; tags participate in cached search and host detail. Mosh is staged.

Tab/Shift-Tab move among visible fields and Save/Test/Cancel. Arrow keys cycle
mode/auth choices. Enter advances on a field or invokes an action; Escape
cancels. Focus follows a scrolling viewport on short terminals.

Save validates and persists without connecting or refreshing. Test performs
interactive discovery on the unsaved draft, keeping success/failure in the
form. Save may retain matching Test data. Missing local identity/certificate
files are reported by Test but may be saved for offline editing. Leading
`~/` expands locally; relative identity references become absolute at Save.

Legacy files remain readable without rewriting:

```json
{"hosts":[{"id":"workbox","label":"Workbox","target":"workbox"}]}
```

Opaque targets support aliases, hosts, IPs, `user@host`, and
`ssh://user@host:port`. They are arguments, never shell commands.

## Authentication and bastions

OpenSSH owns password, passphrase, host-key, and security-key PIN/touch
interaction. Tatami stores no passwords, passphrases, or key contents.
`Password prompt (not stored)` prefers password/keyboard-interactive for the
final destination. OpenSSH may make multiple attempts; no one-prompt guarantee
is made. Status 255 alone does not prove password rejection: it can be an SSH
failure or a remote command's exit status.

Background queries use `BatchMode=yes` and do not take over the terminal.
Highlight an unavailable host and press Enter for interactive discovery;
failure leaves endpoint-local guidance and a usable home list. For unattended
refresh, configure non-interactive SSH:

```sh
ssh-add ~/.ssh/private-key
ssh-copy-id user@host
ssh -o BatchMode=yes user@host true
```

If you first SSH to a bastion and run Tatami there, authentication originates
on the bastion. Your laptop's `-i ~/.ssh/pi_key` does not configure the
bastion's downstream connection. Configure downstream identities/agent/aliases
on the machine running the visible Tatami process.

For per-hop credentials, use local OpenSSH config:

```sshconfig
Host bastion-hop
  HostName bastion.local
  User oles
  IdentityFile ~/.ssh/pi_key
  IdentitiesOnly yes
```

Final-destination key/certificate options do not configure ProxyJump
subprocesses. Credential-bearing explicit roots need their Advanced Jump alias
set to such a configured alias before opening descendants. Alias-mode roots
already supply an alias. Root credentials are never applied to another child.
An advertised destination makes a local alias profile projectable to peers;
an alias without one is local-only. Authentication references are never shared.

## One-connection remote discovery

A fixed POSIX dispatcher searches Tatami first, then Herdr only if Tatami is
absent, inside one SSH process. A selected executable failure never reconnects
or falls back. A Tatami installation lacking the inventory command needs an
upgrade.

Executable overrides take precedence; use an absolute remote POSIX path
containing only letters, numbers and `/ . _ + ~ -`. Use a safe symlink for other
paths. An invalid override is an explicit configuration
error. Otherwise search remote PATH, `$HOME/go/bin`, `$HOME/.local/bin`,
`/usr/local/bin`, then `/opt/homebrew/bin`. No interactive startup files are
sourced. SSH Herdr attach/agent commands use the same resolver.

Unexpected startup output, incompatible JSON, excessive output, missing tools
and selected-tool failures produce safe guidance, not raw saved stderr.

## Federation and opening

Current Tatami peers expose `tatami hub inventory --json`. Discovery expands
the normal home screen with Quick Access, projects/folders, named Herdr
sessions and saved downstream hosts. Herdr-only peers expose a session-only
view. Inventory excludes local auth references, key paths, layout commands,
agent arguments, pane contents, prompts and terminal frames.

Descendants are opened lazily using local ProxyJump, up to four hosts total.
For example, `ssh -J bastion-hop child`. Cyclic IDs or destinations are
rejected. Shared SSH commands disable agent forwarding with `-a`, even when
OpenSSH config enables it. Workspaces, sessions,
new panes and tabs preserve their selected route; discovered workspaces never
execute remote-supplied layout automation.

Structured profiles attach over SSH to preserve username, port and auth
settings. Inside a Herdr pane, selected remote named sessions also attach
over SSH to avoid a forbidden nested local Herdr client. Outside Herdr,
direct legacy opaque hosts retain native Herdr remote-client compatibility.

Before a hub action is handed off, known route changes invalidate it and
captured auth settings are removed. Main re-resolves the saved root before
direct/mux/typed/clipboard execution; changed/deleted roots require reopening
Tatami.

## Controls and private cache

- Type to search known hosts, workspaces, sessions, groups/tags, safe cached
  agent metadata, paths/folders, and sanitized repository identities.
- Down enters browse focus; slash returns to search. Escape clears the query
  before normal Back behavior. Typing never starts network discovery.
- Enter expands an online host, discovers an unavailable host, or opens a
  selected workspace/session. Space only collapses/expands.
- `r` refreshes the selected host; `R` refreshes saved roots. `a/e/d`
  manage top-level hosts. Downstream hosts are managed by their owning peer.
- Remote rows are navigation-only. Destructive operations remain local.
  Remote CPU/RAM remain unavailable without a compatible Herdr capability.

The cache is `$XDG_STATE_HOME/tatami/herdr-hub.json`, mode 0600. Version 1
is disposable and discarded in memory. Malformed current caches are preserved
with writes disabled. Version 2 binds content to saved-root revision and
effective route, restoring only reachable matching data as stale. Root edits,
deletion/re-add and child retargets purge previous-host data and reject delayed
replies. After a failed topology cache write and restart, unchanged-root/
same-route child data may recover as stale until fresh parent discovery.
Durable advertised-child tombstones are not maintained.

Inventory version 1 may include optional sanitized `repository` display
identity such as `github.com/owner/repository`. Owners remove credentials,
transport usernames, query strings, fragments, ports and trailing `.git`
before sharing. Old peers may omit/ignore the field; raw origin URLs are never
shared.

## Migration and rollback

Saved hosts use private schema version 2. Reads do not rewrite v1. The first
successful v2 save publishes a completed no-overwrite mode-0600
`herdr-hosts.json.v1.bak` before replacing configuration. Partial,
non-private, symlinked or different existing backups block migration rather
than being overwritten. Configuration and cache are separate files, not a
cross-file transaction; persisted root revisions prevent old-root restoration
even when cache persistence fails.

To roll back, stop Tatami, preserve the v2 file and restore the backup as
`herdr-hosts.json`. Old readers can read projectable v2 ID/label/target
entries, but an old writer drops new fields. Restoring the original backup also
removes subsequently added hosts.
