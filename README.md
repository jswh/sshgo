# sshgo

一个简洁的 SSH 客户端，支持读取系统 SSH 配置、密钥认证和密码加密存储。

A simple SSH client that reads system SSH config, supports key authentication, and encrypted password storage.

## 功能特性 / Features

- **读取 SSH 配置** - 自动读取 `~/.ssh/config` 中的所有主机配置
  **Read SSH Config** - Automatically reads all host entries from `~/.ssh/config`

- **SSH Key 认证** - 支持 SSH Agent、配置的 IdentityFile 和默认密钥
  **SSH Key Auth** - Supports SSH Agent, configured IdentityFile, and default keys

- **密码加密存储** - 使用 AES-GCM 加密保存密码，主密码保护
  **Encrypted Password Storage** - AES-GCM encryption with master password protection

- **直接登录** - 支持 `user@host:port` 格式直接连接
  **Direct Login** - Supports `user@host:port` format for direct connections

- **交互式选择** - 无参数时列出所有主机供选择
  **Interactive Selection** - Lists all hosts for selection when run without arguments

## 安装 / Installation

```bash
go install github.com/jswh/sshgo@latest
```

或从源码编译 / Or build from source:

```bash
git clone https://github.com/jswh/sshgo.git
cd sshgo
go build -o sshgo
```

## 使用方法 / Usage

```bash
# 直接登录 SSH 配置中的主机
# Login to host defined in SSH config
sshgo zg

# 直接登录指定地址
# Direct login to specified address
sshgo root@1.2.3.4

# 带端口登录
# Login with port
sshgo root@1.2.3.4:2222

# 交互式选择主机
# Interactive host selection
sshgo

# 查看帮助
# Show help
sshgo --help
```

## 认证方式 / Authentication

程序按以下顺序尝试认证 / The program tries authentication in this order:

1. **SSH Agent** - 检查是否有可用的 SSH 密钥代理 / Checks for available SSH key agent
2. **配置的 IdentityFile** - 使用 SSH 配置中指定的密钥文件 / Uses key file specified in SSH config
3. **默认密钥** - 尝试 `~/.ssh/id_rsa`、`id_ed25519`、`id_ecdsa` / Tries default key files
4. **已保存密码** - 使用加密存储的密码 / Uses encrypted stored password
5. **密码提示** - 手动输入密码（可选保存） / Prompts for password (optional save)

## 密码存储 / Password Storage

首次使用密码存储时，会要求设置主密码。所有保存的密码都使用 AES-GCM 加密，存储在 `~/.sshgo_passwords` 文件中。

On first use, you'll be asked to set a master password. All saved passwords are encrypted with AES-GCM and stored in `~/.sshgo_passwords`.

## 许可证 / License

[MIT License](LICENSE)
