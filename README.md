<div align="center">

# sshgo

**Simple, Secure, and Efficient SSH Client**

A lightweight SSH client that owns its host list: import your system SSH config once, then sshgo reads only its own metadata. Supports key authentication, encrypted password storage, structured remote execution, connection inspection, and jump hosts.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.20+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/jswh/sshgo?logo=github)](https://github.com/jswh/sshgo/releases/latest)
[![GitHub Stars](https://img.shields.io/github/stars/jswh/sshgo?style=social)](https://github.com/jswh/sshgo/stargazers)

**English** | [简体中文](docs/zh-CN.md)

</div>

---

## ✨ Features

| Feature | Description |
|---------|-------------|
| 📖 **Import SSH Config** | One-time import from `~/.ssh/config` via `config import` |
| 🛡️ **Runtime Independence** | Never reads `~/.ssh/config` at runtime; the import is the only touch point |
| 🔑 **SSH Key Auth** | Supports SSH Agent, configured IdentityFile, and default keys |
| 🔒 **Encrypted Storage** | AES-GCM encryption with master password protection |
| 🚀 **Direct Login** | Supports `user@host:port` format for direct connections |
| 📋 **Interactive Selection** | Lists all hosts for selection when run without arguments |
| 📦 **Exec (Structured)** | Run remote commands with structured JSON output (`exec` subcommand) |
| 🔀 **Jump Hosts** | Reach a Host through one or more bastions (`proxy-jump`), each with its own credentials |
| 🔍 **Info** | Inspect connection details for any host (`info` subcommand) |
| 🏷️ **Host Metadata** | Manage aliases, notes, tags, and connection priority per host (`config` subcommand) |
| 🔄 **Auto-Update** | Update to the latest version with a single command (`update` subcommand) |
| 📥 **One-Click Install** | Install script downloads and places the binary in `~/.local/bin` |

## 🚀 Quick Install

### One-Click Install (Recommended)

**Linux / macOS:**
```bash
curl -fsSL https://github.com/jswh/sshgo/raw/main/install.sh | sh
```

**Windows (PowerShell):**
```powershell
irm https://github.com/jswh/sshgo/raw/main/install.ps1 | iex
```

This downloads the latest release binary for your OS/arch and installs it to `~/.local/bin/`.

### Build from Source

```bash
git clone https://github.com/jswh/sshgo.git
cd sshgo
go build -o sshgo
```

### Using go install

```bash
go install github.com/jswh/sshgo@latest
```

## 📖 Usage

### Getting Started

Before using sshgo for the first time, import your SSH config:

```bash
sshgo config import
```

This reads `~/.ssh/config` once and stores the hosts it describes in `~/.sshgo_config`.
From then on sshgo owns that list: it does not read `~/.ssh/config` again at runtime,
so changing your system config does not silently change sshgo's behavior.

The import is a snapshot, not a sync. Values are the effective ones after `Host *`
defaults are applied, and re-running the import refreshes connection details while
keeping your aliases, notes, tags, and priorities. Hosts you delete from
`~/.ssh/config` are **not** removed from sshgo; use `sshgo config remove <host>`.
Wildcard patterns (`Host *`, `Host *.example.com`) are skipped, since sshgo connects
to concrete hosts only.

### Login to a Host from Your List

```bash
sshgo zg
```

A bare name is looked up in sshgo's own host list only. If it is not there, sshgo
exits with an error naming the three ways out — it never falls back to dialing the
name as a hostname the way `ssh` would.

### Direct Login

Input containing `@` or `:` is a direct address, sshgo's own syntax, independent of
`~/.ssh/config`. Direct addresses are always dialed as given:

```bash
sshgo root@1.2.3.4
```

### Login with Port

```bash
sshgo root@1.2.3.4:2222
```

Ports default to `22` and users to `root`. IPv6 addresses are not supported; create a
host entry with the address instead.

### Interactive Host Selection

```bash
sshgo
```

### Exec: Structured Remote Command

Run a command and get structured JSON output (exit code, stdout, stderr):

```bash
sshgo exec db-staging "SELECT count(*) FROM users"
```

Raw output (passthrough):

```bash
sshgo exec --raw web-01 "tail -n 100 /var/log/nginx/access.log"
```

With sudo (reuses SSH password):

```bash
sshgo exec --sudo db-staging "systemctl restart postgresql"
```

### Info: Inspect Connection Details

```bash
sshgo info db-staging
sshgo info --json db-staging    # Machine-readable JSON
```

### Config: Manage Hosts & Metadata

Import hosts from `~/.ssh/config` (a one-time snapshot; see Getting Started):

```bash
sshgo config import
```

Set metadata for a host (stored in `~/.sshgo_config`):

```bash
sshgo config set db-staging alias db-prod
sshgo config set db-staging tags prod,db,postgres
sshgo config set db-staging notes "Production PostgreSQL"
sshgo config set db-staging priority agent,key,password
```

Set or override connection details:

```bash
sshgo config set my-vm hostname 192.168.1.100
sshgo config set my-vm user admin
sshgo config set my-vm port 2222
sshgo config set my-vm identity-file ~/.ssh/my_key
sshgo config set my-vm proxy-jump bastion               # bastion is an existing Host
```

`proxy-jump` names a **Jump Host** and connections go through it. The value is resolved
by the same rules as any other Host argument, so a Host name, an Alias, or a Direct
Address all work; `none` or an empty value means a direct connection. Chains can be any
depth: if `bastion` itself has a `proxy-jump`, that hop is dialled first. A chain that
loops is refused before anything is dialled, and the cycle is printed.

Remove a host entry entirely (no confirmation, safe in scripts):

```bash
sshgo config remove my-vm
```

This is the same as `sshgo config unset my-vm`. You need it because hosts deleted from
`~/.ssh/config` are not removed from sshgo by re-importing.

View all hosts and their metadata:

```bash
sshgo config list
```

Find by tag:

```bash
sshgo config find --tag prod
```

Configure a sudo password (stored encrypted in `~/.sshgo_passwords`):

```bash
sshgo config set-sudo-password db-staging
```

Configure an SSH password (same interaction, same store). Use this for a Host that only
accepts a password — typically a bastion you reach from a script:

```bash
sshgo config set-password bastion
```

### Update: Auto-Update sshgo

Check for the latest release and update the current binary:

```bash
sshgo update
```

The update subcommand fetches the latest release from GitHub, compares the version
tag with the embedded version (set at build time), and replaces the binary if a
newer version is found. If the binary is in a system directory, it falls back to
`sudo mv` automatically.

### Jump Hosts: Reaching a Host Through a Bastion

Give the target a `proxy-jump` pointing at an existing Host:

```bash
sshgo config set bastion hostname 203.0.113.9     # the Host you pass through
sshgo config set bastion user ops
sshgo config set db1 hostname 10.0.0.5            # the Host behind it
sshgo config set db1 proxy-jump bastion

sshgo exec db1 "pg_isready"                       # connects through bastion
sshgo info db1                                    # shows Jump: bastion
```

Every hop is an ordinary Host with its own user, port, IdentityFile, and saved password.
Credentials never cross hops: `db1`'s password and sudo password are never offered to
`bastion`, and `bastion`'s password is never reused for `db1`. Only the SSH agent is
shared, because it belongs to you rather than to a Host.

Chains nest. If `inner` is reachable only from `bastion`:

```bash
sshgo config set inner proxy-jump bastion
sshgo config set db1 proxy-jump inner
sshgo info db1                                    # Jump: bastion -> inner
```

The chain is resolved before anything is dialled, so a missing Hop, a Hop with no
address, or a loop fails immediately with the path spelled out. `exec` never prompts for
SSH authentication on any hop: if a hop has no key and no saved password, it fails and
names that hop. `sshgo config set-password <hop>` is how you fix it.

### Show Help

```bash
sshgo --help
sshgo exec --help
sshgo info --help
sshgo config --help
sshgo update --help
```

## 🔐 Authentication

The program tries authentication in this order:

```
┌─────────────────────────────────────────────────────────────┐
│                    Authentication Flow                      │
├─────────────────────────────────────────────────────────────┤
│  1. SSH Agent        → Captures key signers from agent      │
│  2. IdentityFile     → Uses key file specified in config    │
│  3. Default Keys     → Tries id_rsa, id_ed25519, id_ecdsa  │
│  4. Saved Password   → Uses encrypted stored password       │
│  5. Password Prompt  → Manual input (optional save)         │
└─────────────────────────────────────────────────────────────┘
```

### exec Authentication (Non-Interactive)

The `exec` subcommand automatically tries Agent → IdentityFile → Default Keys → Saved Passwords, without interactive password prompts. This holds for every hop of a jump chain too. A hop with no key and no saved password fails immediately and is named, with `sshgo config set-password <hop>` as the way out.

### Sudo Password

`exec --sudo` uses the following priority to determine the sudo password:

1. **Saved sudo password** (set via `sshgo config set-sudo-password <host>`)
2. **Saved SSH password** (reuses the SSH login password)
3. **Interactive prompt** (for key-authenticated connections without NOPASSWD)
4. **NOPASSWD** (relies on target host's sudo configuration)

To pre-configure a sudo password:

```bash
sshgo config set-sudo-password db-staging
```

When using key authentication and no NOPASSWD is configured, `exec --sudo` will
prompt for a sudo password with an option to save it.

Saved passwords are filed under the name you address, so use the same name for
`set-sudo-password` and `exec`. An Alias and the Host name it points at are
distinct keys: a password saved for `db-staging` is not used when you run
`exec --sudo db-staging-alias`. If no entry matches the addressed name, `exec`
falls back to the resolved Hostname so entries saved by address keep working.

## ⚠️ Limitations

- **Host keys are not verified.** sshgo uses `InsecureIgnoreHostKey()` on every hop. A
  Jump Chain therefore does **not** make sshgo safer: a compromised bastion can
  impersonate the target. `known_hosts` verification is a separate feature.
- **ssh's jump syntax is not supported.** `ssh://` URIs and comma-separated multi-hop
  values (`bastion,inner`) are rejected. Write the chain with `proxy-jump` per Host
  instead.
- **A Jump Host must be an sshgo Host or a Direct Address.** A name that sshgo does not
  manage cannot be a hop, because sshgo never reads `~/.ssh/config` at runtime.
- **IPv6 is not supported**, in a Host address or a jump value.

## 🗂️ Project Structure

```
sshgo/
├── main.go              # os.Exit(cli.Run(...)) plus the version injected by -ldflags
├── internal/cli/        # subcommand dispatch, help text, exit codes, auth strategies
├── internal/host/       # Host metadata, CRUD, provenance, resolution
├── internal/credential/ # encrypted password store (AES-GCM + argon2id)
├── internal/sshconfig/  # config import: pure snapshot builder + ssh config adapter
├── internal/connect/    # the only dial point: Dial / Chain / sessions
├── internal/update/     # self-update, version passed in by the caller
├── internal/ui/         # terminal input: selection, prompts, terminal predicates
├── install.sh           # One-click install script (Unix)
├── install.ps1          # One-click install script (Windows)
├── test/                # e2e: Dockerfile (sshd + sudoers) + setup.sh
├── CONTEXT.md           # Domain glossary
├── docs/
│   ├── adr/
│   │   ├── 0001-separate-metadata-from-passwords.md
│   │   ├── 0002-runtime-independence-from-system-ssh.md
│   │   └── 0003-proxyjump-as-a-chain-of-sshgo-hosts.md
│   └── zh-CN.md         # Simplified Chinese documentation
├── go.mod               # Go module definition
├── go.sum               # Dependency checksums
├── LICENSE              # MIT License
└── README.md            # Documentation
```

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🔗 Links

- [GitHub Repository](https://github.com/jswh/sshgo)
- [Issue Tracker](https://github.com/jswh/sshgo/issues)
- [Go Documentation](https://go.dev/doc/)

---

<div align="center">

**If you find this project helpful, please give it a ⭐ Star!**

</div>
