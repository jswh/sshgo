# sshgo

A lightweight SSH client that imports system SSH config (via `config import`), supports key authentication, encrypted password storage, structured remote command execution, connection inspection, and local host metadata management.

## Language

**Host**:
A remote machine that can be connected to via SSH. Defined either in `~/.ssh/config` (SSH Config Host), as a direct address (`user@hostname:port`), or as a locally managed entry with metadata.
_Avoid_: Server, node, machine

**exec**:
A subcommand that runs a remote command on a Host and returns the result as structured JSON (`stdout`, `stderr`, `exit_code`) or raw stream (with `--raw` flag). Supports optional `--sudo` flag.
_Avoid_: run, execute (in user-facing help)

**info**:
A subcommand that displays connection details for a Host — Hostname, Port, User, Auth Methods (detailed, non-sensitive), and Source. Output is both JSON (machine) and formatted text (human).
_Avoid_: status, inspect, details

**config** (subcommand):
A subcommand to manage local Host metadata stored in `~/.sshgo_config`. Supports CRUD on per-host fields: Alias, Notes, Tags, Connection Priority, Hostname, Port, User, IdentityFile, ProxyJump. Also provides `config import` to import hosts from `~/.ssh/config`.
_Avoid_: manage, settings, metadata

**Source**:
The origin of a Host definition. One of: `imported` (imported from `~/.ssh/config` via `config import`), `direct` (from CLI argument `user@host:port`), or `manual` (created via local metadata without connection details).

**Local Metadata**:
Per-Host information stored in `~/.sshgo_config`, independent of `~/.ssh/config` and the encrypted password store. Fields: Alias (quick-reference name), Notes (free text), Tags (categorical labels for filtering/grouping), Connection Priority (ordering of authentication methods), Hostname, Port, User, IdentityFile, ProxyJump (connection details imported from `~/.ssh/config` or set manually).

**Alias**:
A user-defined short name for a Host that can be used as a positional argument instead of the full SSH config Host name or direct address.
_Avoid_: nickname, shortcut

**Auth Methods** (in info output):
The authentication mechanisms available for a Host, described at a granular level but without exposing secrets. Example: `SSH Agent (3 keys), Password (saved)`. Never includes key paths, passwords, or passphrases.

**Sudo Reuse**:
The strategy where `exec --sudo` reuses the SSH login password as the sudo password, avoiding a separate prompt.

**Subcommand Priority**:
When the first argument matches `exec`, `info`, or `config`, it is treated as a subcommand rather than a host name. This is a deliberate design choice; SSH config hosts named `exec`/`info`/`config` cannot be reached via positional argument (use `config set` with an Alias instead).
