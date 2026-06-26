<div align="center">

# sshgo

**Simple, Secure, and Efficient SSH Client**

A lightweight SSH client that reads system SSH config, supports key authentication, and provides encrypted password storage.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.20+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![GitHub Stars](https://img.shields.io/github/stars/jswh/sshgo?style=social)](https://github.com/jswh/sshgo/stargazers)

**English** | [简体中文](docs/zh-CN.md)

</div>

---

## ✨ Features

| Feature | Description |
|---------|-------------|
| 📖 **Read SSH Config** | Automatically reads all host entries from `~/.ssh/config` |
| 🔑 **SSH Key Auth** | Supports SSH Agent, configured IdentityFile, and default keys |
| 🔒 **Encrypted Storage** | AES-GCM encryption with master password protection |
| 🚀 **Direct Login** | Supports `user@host:port` format for direct connections |
| 📋 **Interactive Selection** | Lists all hosts for selection when run without arguments |
| 📦 **Exec (Structured)** | Run remote commands with structured JSON output (`exec` subcommand) |
| 🔍 **Info** | Inspect connection details for any host (`info` subcommand) |
| 🏷️ **Host Metadata** | Manage aliases, notes, tags, and connection priority per host (`config` subcommand) |

## 🚀 Quick Install

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

### Login to SSH Config Host

```bash
sshgo zg
```

### Direct Login

```bash
sshgo root@1.2.3.4
```

### Login with Port

```bash
sshgo root@1.2.3.4:2222
```

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

### Config: Manage Host Metadata

Set metadata for a host (stored in `~/.sshgo_config`):

```bash
sshgo config set db-staging alias db-prod
sshgo config set db-staging tags prod,db,postgres
sshgo config set db-staging notes "Production PostgreSQL"
sshgo config set db-staging priority agent,key,password
```

List all hosts with metadata:

```bash
sshgo config list
```

Find by tag:

```bash
sshgo config find --tag prod
```

### Show Help

```bash
sshgo --help
sshgo exec --help
sshgo info --help
sshgo config --help
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

The `exec` subcommand automatically tries Agent → IdentityFile → Default Keys → Saved Passwords, without interactive password prompts. If none succeed, it returns a structured error.

### Sudo Password Reuse

`exec --sudo` reuses the SSH login password as the sudo password, avoiding a separate prompt. For key-authenticated connections, it relies on `NOPASSWD` sudo configuration on the target host.

## 🗂️ Project Structure

```
sshgo/
├── main.go          # CLI routing, legacy mode, authentication, connection
├── exec.go          # Structured remote command execution (exec subcommand)
├── info.go          # Connection information display (info subcommand)
├── config.go        # Local host metadata management (config subcommand)
├── types.go         # Shared types: ExecResult, InfoResult, HostMeta, LocalConfig
├── password.go      # AES-GCM encrypted password store
├── CONTEXT.md       # Domain glossary
├── docs/
│   └── adr/
│       └── 0001-separate-metadata-from-passwords.md
├── go.mod           # Go module definition
├── go.sum           # Dependency checksums
├── LICENSE          # MIT License
└── README.md        # Documentation
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
