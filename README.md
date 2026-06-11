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

### Show Help

```bash
sshgo --help
```

## 🔐 Authentication

The program tries authentication in this order:

```
┌─────────────────────────────────────────────────────────────┐
│                    Authentication Flow                      │
├─────────────────────────────────────────────────────────────┤
│  1. SSH Agent        → Checks for available SSH key agent   │
│  2. IdentityFile     → Uses key file specified in config    │
│  3. Default Keys     → Tries id_rsa, id_ed2559, id_ecdsa   │
│  4. Saved Password   → Uses encrypted stored password       │
│  5. Password Prompt  → Manual input (optional save)         │
└─────────────────────────────────────────────────────────────┘
```

## 🗂️ Project Structure

```
sshgo/
├── main.go          # Main entry point
├── password.go      # Password encryption module
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
