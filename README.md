<div align="center">

# sshgo

**An SSH client that keeps its own host list and returns remote output as data.**

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.20+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/jswh/sshgo?logo=github)](https://github.com/jswh/sshgo/releases/latest)

**English** | [简体中文](docs/zh-CN.md)

</div>

---

sshgo is an SSH client written in Go. It talks the protocol itself through
`golang.org/x/crypto/ssh` rather than shelling out, keeps the hosts you work with in one
file of its own, and gives remote commands back as `stdout`, `stderr` and the remote exit
code in JSON.

It is not a replacement for the whole `ssh` option grammar: `ssh://` URIs and
comma-separated hop lists are rejected, and IPv6 addresses are not supported. It does not
verify host keys yet, which is stated in the limitations section below rather than buried
in a footnote.

## Get started

```bash
curl -fsSL https://github.com/jswh/sshgo/raw/main/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"

sshgo config import          # take the hosts from ~/.ssh/config, once
sshgo exec web-01 "uptime"   # run something, get JSON back
```

The output of that second command is shaped like this:

```json
{
  "command": "uptime",
  "stdout": " 10:12:03 up 41 days,  2:11,  1 user,  load average: 0.00, 0.01, 0.05\n",
  "stderr": "",
  "exit_code": 0
}
```

The import step is optional. You can work entirely with direct addresses
(`sshgo root@1.2.3.4:2222`) and never import anything.

## The three commands you will use most

### `exec`: remote commands as structured data

```bash
sshgo exec db-staging "SELECT count(*) FROM users"
sshgo exec --raw web-01 "tail -n 100 /var/log/nginx/access.log"   # passthrough, no JSON
sshgo exec --sudo db-staging "systemctl restart postgresql"
```

The JSON carries `command`, `stdout`, `stderr` and `exit_code`. `duration` is a
human-readable string and appears only when `--sudo` is used or timing is enabled. `exit_code` is the remote
command's own code, so a script can branch on it. Local failures (unknown host, no
credentials, a hop that cannot be reached) always exit `1`, deliberately: one exit code for
everything that went wrong before the remote command ran, so the number your script reads
never means two different things.

`exec` never stops to ask you for anything. It tries the SSH agent, the host's
`IdentityFile`, the default key files, and a saved password, in that order. If none of them
work it fails and names the host that had no credential, pointing at
`sshgo config set-password <host>` as the way out. This holds for every hop of a jump
chain. A CI job cannot answer a prompt, so sshgo does not ask it one.

The sudo password is sent to the target only, as stdin of its `sudo -S`. It is never part of
any hop's authentication.

### `info`: what sshgo will do for a name

```bash
sshgo info db1
sshgo info --json db1
```

```
Host: db1
  Hostname: 10.0.0.5
  Port:     22
  User:     ops
  Source:   imported
  Jump:     bastion
  Auth:
    - SSH Agent (3 keys)
    - Password store (encrypted)
```

`--json` returns `name`, `hostname`, `port`, `user`, `source`, `auth_methods`, and `jump`
when the host is reached through a chain. Authentication is reported as method names and
counts. Key paths, passwords and passphrases never appear in any output; that exclusion is
the contract, not a formatting choice. `info` resolves the chain and reports a broken one
without dialling, so it reaches the same verdict a connection would.

### Plain `sshgo`: pick from the list

Running sshgo with no arguments lists your hosts and lets you choose one. With a bare name
it looks that name up in sshgo's own list only:

```bash
sshgo zg
```

If the name is not in the list, sshgo stops and names the three ways out, instead of quietly
dialling `zg` as a hostname the way `ssh` would:

```
Host "zg" not found.
  Import your SSH config:  sshgo config import
  Add it manually:         sshgo config set zg hostname <address>
  Or connect directly:     sshgo user@host or sshgo host:port
```

That message is the contract: `internal/host/resolve.go` builds it, and
`internal/cli/cli_test.go:TestResolutionErrorText` pins the text.

Anything containing `@` or `:` is a direct address, sshgo's own syntax, and is always dialled
as given. Port defaults to `22`, user to `root`.

## Where the host list comes from

`sshgo config import` reads `~/.ssh/config` once and writes the hosts it describes into
`~/.sshgo_config`. From then on sshgo reads only its own file. Editing the system config
does not change what sshgo does; that is the point, and it is also the cost.

The import is a snapshot, not a sync:

- Values are the effective ones, after `Host *` defaults are folded in.
- Wildcard and negated patterns (`Host *`, `Host *.example.com`) are skipped. sshgo connects
  to concrete hosts.
- Re-running the import refreshes connection details and keeps your aliases, notes, tags and
  connection priorities.
- A host you delete from `~/.ssh/config` is not deleted here. Use `sshgo config remove <host>`.

The consequence is worth stating plainly: the two lists drift apart, and syncing them back is
a manual act.

Metadata is edited by hand too:

```bash
sshgo config list                       # everything sshgo knows
sshgo config find --tag prod            # filter by tag
sshgo config set db1 notes "primary pg" # one of nine keys (see config --help)
sshgo config get db1 hostname
sshgo config unset db1 tags
sshgo config remove db1
```

`config set` accepts `alias`, `notes`, `tags`, `connection-priority`, `hostname`, `port`,
`user`, `identity-file` and `proxy-jump`. Any other key is rejected with that list.

## Reaching a host through a jump host

A jump host is an ordinary host in sshgo's list. Point the target at it:

```bash
sshgo config set bastion hostname 203.0.113.9
sshgo config set bastion user ops
sshgo config set db1 hostname 10.0.0.5
sshgo config set db1 proxy-jump bastion

sshgo exec db1 "pg_isready"    # dialled through bastion
sshgo info db1                 # Jump: bastion
```

Chains nest to any depth. If `inner` is only reachable from `bastion`:

```bash
sshgo config set inner proxy-jump bastion
sshgo config set db1 proxy-jump inner
sshgo info db1                 # Jump: bastion -> inner
```

Credentials never cross hops. A hop uses its own user, port, `IdentityFile` and saved
password; nothing is inherited from the target and nothing is handed back to it. The SSH
agent is the one thing shared across a chain, because it belongs to you rather than to a
host.

The whole chain is resolved before anything is dialled. A missing hop, a hop with no
address, or a loop fails immediately with the path spelled out, so a cycle reads as
`db1 -> bastion -> db1` rather than as a timeout. Forward references are legal: create the
target first and its bastion later.

`proxy-jump` takes an sshgo host name, an alias, a direct address, or `none` for a direct
connection. ssh's spellings are not accepted, because sshgo has exactly one addressing
grammar.

## Two files, one master password

`~/.sshgo_config` holds host metadata as plain JSON.

`~/.sshgo_passwords` holds credentials, encrypted with AES-GCM. The key is derived from your
master password with argon2id (time 1, memory 64 MiB, threads 4, a 16-byte random salt), and
the file is written with `0600` permissions. The master password is not stored anywhere and
cannot be recovered; forget it and the saved credentials are gone with it.

Read both halves of that. The password file is protected; the host list is not. Whoever can
read your home directory can see which machines you connect to, under which account, on which
ports, and which of them are reachable only through another. If that matters to you, wait for
`known_hosts` verification and encrypted metadata to land, or keep the list somewhere you
trust.

The password store is unlocked at most once per invocation, so a chain that needs credentials
on several hops does not ask you for the master password several times.

## Install and update

```bash
curl -fsSL https://github.com/jswh/sshgo/raw/main/install.sh | sh    # Linux / macOS
irm https://github.com/jswh/sshgo/raw/main/install.ps1 | iex          # Windows PowerShell
go install github.com/jswh/sshgo@latest                               # Go toolchain
```

The install scripts place the latest release binary for your OS and architecture in
`~/.local/bin`. Building from source is `go build -o sshgo` in a clone of this repository.

```bash
sshgo update
```

`update` fetches the latest release from GitHub, compares its tag with the version compiled
into the running binary, and replaces the binary if there is a newer one. If the binary sits
in a system directory it falls back to `sudo mv`.

## What sshgo does not do

These are current gaps, not roadmap hedges.

- **Host keys are not verified.** sshgo uses `InsecureIgnoreHostKey()` on every hop. A jump
  chain does not change that: a compromised bastion can impersonate the target. `known_hosts`
  verification is a separate feature.
- **ssh's jump syntax is not supported.** `ssh://` URIs and comma-separated multi-hop values
  (`bastion,inner`) are rejected. Write the chain with `proxy-jump` per host instead.
- **A jump host must be an sshgo host or a direct address.** A name sshgo does not manage
  cannot be a hop, because sshgo never reads `~/.ssh/config` at runtime.
- **IPv6 is not supported**, neither in a host address nor in a jump value.

## Commands

| Command | What it does |
|---|---|
| `sshgo` | List hosts and pick one |
| `sshgo <name>` | Connect to a host from sshgo's list |
| `sshgo user@host[:port]` | Connect to an address given on the command line |
| `sshgo exec [--raw] [--sudo] <host> <cmd>` | Run a command, get JSON or raw streams back |
| `sshgo info [--json] <host>` | Show resolved connection details and the jump chain |
| `sshgo config import` | Snapshot hosts out of `~/.ssh/config`, once |
| `sshgo config set\|get\|unset\|remove` | Edit one host's metadata |
| `sshgo config list` / `find --tag` | See and filter the list |
| `sshgo config set-password` / `set-sudo-password` | Store credentials for a host |
| `sshgo update` | Replace the binary with the latest release |

Full flags are in `sshgo <subcommand> --help`.

## Why it is built this way

Three decisions explain most of the surface:

- [ADR-0001](docs/adr/0001-separate-metadata-from-passwords.md): host metadata and
  credentials are two files, so listing your hosts does not need a password.
- [ADR-0002](docs/adr/0002-runtime-independence-from-system-ssh.md): sshgo never reads
  `~/.ssh/config` at runtime; the import is the only touch point.
- [ADR-0003](docs/adr/0003-proxyjump-as-a-chain-of-sshgo-hosts.md): a jump host is an sshgo
  host, which is what makes chains nestable and per-hop credentials possible.

[CONTEXT.md](CONTEXT.md) is the glossary those documents use: Host, Alias, Direct Address,
Jump Host, Jump Chain, hop, Credential Key. The names in the CLI are the names in that file.

Contributions are welcome. [LICENSE](LICENSE) is MIT.
