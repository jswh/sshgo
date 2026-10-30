# sshgo

A lightweight SSH client that owns its host list: it imports system SSH config once (via `config import`), then runs on its own metadata. Supports key authentication, encrypted password storage, structured remote command execution, connection inspection, and local host metadata management.

## Language

**Host**:
A remote machine that can be connected to via SSH. Defined by local metadata in `~/.sshgo_config`: created by `config import` (from `~/.ssh/config`, the one-time seed), by a direct address (`user@hostname:port`), or by hand via `config set`.
_Avoid_: Server, node, machine

**Runtime Independence**:
sshgo does not read `~/.ssh/config` while running: no parsing, no completion, no guessing. The file is read only during `config import`. A bare name that is not in local metadata is an error, never a fallback connection.
_Avoid_: sync, live config, config passthrough

**Import Snapshot**:
`config import` is a migration, not a synchronization. It copies the effective (post-`Host *`) values of concrete hosts once; re-running refreshes connection details and keeps Alias/Notes/Tags/Connection Priority, but never deletes a Host that disappeared from `~/.ssh/config`. Wildcard and negated patterns are skipped.
_Avoid_: sync, refresh config, mirror

**exec**:
A subcommand that runs a remote command on a Host and returns the result as structured JSON (`stdout`, `stderr`, `exit_code`) or raw stream (with `--raw` flag). Supports optional `--sudo` flag.
_Avoid_: run, execute (in user-facing help)

**info**:
A subcommand that displays connection details for a Host — Hostname, Port, User, Auth Methods (detailed, non-sensitive), and Source. Output is both JSON (machine) and formatted text (human).
_Avoid_: status, inspect, details

**config** (subcommand):
A subcommand to manage local Host metadata stored in `~/.sshgo_config`. Supports CRUD on per-host fields: Alias, Notes, Tags, Connection Priority, Hostname, Port, User, IdentityFile, ProxyJump. Also provides `config import` to take an Import Snapshot of `~/.ssh/config`, and `config remove <host>` to delete an entire entry (same as `config unset <host>`, no confirmation).
_Avoid_: manage, settings, metadata

**Source**:
The provenance of a Host entry, persisted in `~/.sshgo_config`. One of: `imported` (came from `config import`; means "was once imported", not "currently matches `~/.ssh/config`"), `direct` (from a CLI direct address `user@host:port`), or `manual` (created by `config set`). Provenance is where the entry came from, not who edited it last: editing an `imported` Host leaves it `imported`.
_Avoid_: type, origin (use Source), last-modified-by

**Local Metadata**:
Per-Host information stored in `~/.sshgo_config`, independent of `~/.ssh/config` and the encrypted password store. Fields: Alias (quick-reference name), Notes (free text), Tags (categorical labels for filtering/grouping), Connection Priority (ordering of authentication methods), Hostname, Port, User, IdentityFile, ProxyJump (connection details imported from `~/.ssh/config` or set manually). ProxyJump is stored only and does not affect connections yet.

**Alias**:
A user-defined short name for a Host that can be used as a positional argument instead of the Host name or direct address.
_Avoid_: nickname, shortcut

**Direct Address**:
Any CLI input containing `@` or `:`. This is sshgo's own syntax, unrelated to `~/.ssh/config`: it is always dialed as given and never reported as a missing Host. Port defaults to `22` and user to `root`. IPv6 is rejected.
_Avoid_: direct connection string, ad-hoc host

**Resolution Error**:
One of three typed failures produced by the single resolution entry point, each on stderr with exit code `1`: host not found (a bare name absent from local metadata), no connection details (an entry with no hostname), and IPv6 unsupported (more than one colon).
_Avoid_: connection error, lookup failure

**Auth Methods** (in info output):
The authentication mechanisms available for a Host, described at a granular level but without exposing secrets. Example: `SSH Agent (3 keys), Password (saved)`. Never includes key paths, passwords, or passphrases.

**Credential Key**:
The name a saved credential is filed under: the name the user addresses, so an Alias and the Host name it points at can each hold their own credential. Lookup tries the addressed name, then the canonical Host name, then the resolved Hostname — the last only so entries saved by address still match.
_Avoid_: lookup key, store key, hostname key

**Sudo Password Priority**:
The strategy for `exec --sudo` to determine the sudo password, in order: (1) saved sudo password (via `config set-sudo-password`, looked up by Credential Key), (2) saved SSH password (reuse, same key order), (3) interactive prompt (for key-auth connections without NOPASSWD; a password entered here is saved under the addressed Credential Key), (4) NOPASSWD (rely on target host configuration).

**Subcommand Priority**:
When the first argument matches `exec`, `info`, `config`, or `update`, it is treated as a subcommand rather than a host name. This is a deliberate design choice; SSH config hosts named `exec`/`info`/`config`/`update` cannot be reached via positional argument (use `config set` with an Alias instead).

**update**:
A subcommand that checks for the latest release on GitHub and replaces the current binary. It uses a HTTP redirect to discover the latest release tag (avoiding API rate limits), compares it with the embedded version string (set via `-ldflags -X main.version=...`), downloads the matching OS/arch artifact, and replaces the running binary. Falls back to `sudo mv` when write permission is denied.
_Avoid_: upgrade, self-update

**Install Script**:
A standalone POSIX shell script (`install.sh`) that detects OS/arch via `uname`, fetches the latest release tag from GitHub, downloads the matching binary, and installs it to `~/.local/bin/`. Can be piped directly from GitHub: `curl -fsSL https://github.com/jswh/sshgo/raw/main/install.sh | sh`.

**Version**:
The current binary version, set at build time via `-ldflags -X main.version=<tag>`. Defaults to `"dev"` in development builds. Release builds embed the Git tag (e.g., `v0.0.1`) and are compared using semantic versioning.
