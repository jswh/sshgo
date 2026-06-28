<div align="center">

# sshgo

**简单、安全、高效的 SSH 客户端**

一个简洁的 SSH 客户端，支持读取系统 SSH 配置、密钥认证和密码加密存储。

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.20+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![GitHub Stars](https://img.shields.io/github/stars/jswh/sshgo?style=social)](https://github.com/jswh/sshgo/stargazers)

[English](../README.md) | **简体中文**

</div>

---

## ✨ 功能特性

| 功能 | 描述 |
|------|------|
| 📖 **导入 SSH 配置** | 通过 `config import` 一次性导入 `~/.ssh/config` 的主机 |
| 🔑 **SSH Key 认证** | 支持 SSH Agent、配置的 IdentityFile 和默认密钥 |
| 🔒 **密码加密存储** | 使用 AES-GCM 加密保存密码，主密码保护 |
| 🚀 **直接登录** | 支持 `user@host:port` 格式直接连接 |
| 📋 **交互式选择** | 无参数时列出所有主机供选择 |
| 📦 **Exec（结构化执行）** | 远程命令执行，返回结构化 JSON 输出 |
| 🔍 **Info** | 查看主机的连接详情 |
| 🏷️ **主机元数据** | 管理别名、备注、标签、连接优先级 |

## 🚀 快速安装

### 从源码编译

```bash
git clone https://github.com/jswh/sshgo.git
cd sshgo
go build -o sshgo
```

### 使用 go install

```bash
go install github.com/jswh/sshgo@latest
```

## 📖 使用方法

### 首次使用

首次使用前，先导入 SSH 配置：

```bash
sshgo config import
```

这会一次性读取 `~/.ssh/config` 中的所有主机，并存储到 `~/.sshgo_config`。
更新 `~/.ssh/config` 后重新执行即可同步变更。

### 登录 SSH 配置中的主机

```bash
sshgo zg
```

### 直接登录指定地址

```bash
sshgo root@1.2.3.4
```

### 带端口登录

```bash
sshgo root@1.2.3.4:2222
```

### 交互式选择主机

```bash
sshgo
```

### Exec: 远程命令执行（结构化输出）

执行命令并返回结构化 JSON（含 stdout、stderr、exit_code）：

```bash
sshgo exec db-staging "SELECT count(*) FROM users"
```

原始输出透传：

```bash
sshgo exec --raw web-01 "tail -n 100 /var/log/nginx/access.log"
```

使用 sudo 提权：

```bash
sshgo exec --sudo db-staging "systemctl restart postgresql"
```

### Info: 查看连接详情

```bash
sshgo info db-staging
sshgo info --json db-staging    # 机器可读 JSON 格式
```

### Config: 管理主机与元数据

从 `~/.ssh/config` 导入主机：

```bash
sshgo config import
```

设置主机元数据（存储在 `~/.sshgo_config`）：

```bash
sshgo config set db-staging alias db-prod
sshgo config set db-staging tags prod,db,postgres
sshgo config set db-staging notes "生产 PostgreSQL 实例"
sshgo config set db-staging priority agent,key,password
```

设置或覆盖连接详情：

```bash
sshgo config set my-vm hostname 192.168.1.100
sshgo config set my-vm user admin
sshgo config set my-vm port 2222
sshgo config set my-vm identity-file ~/.ssh/my_key
sshgo config set my-vm proxy-jump bastion.example.com
```

查看所有主机及其元数据：

```bash
sshgo config list
```

按标签搜索：

```bash
sshgo config find --tag prod
```

### 查看帮助

```bash
sshgo --help
sshgo exec --help
sshgo info --help
sshgo config --help
```

## 🔐 认证方式

程序按以下顺序尝试认证：

```
┌─────────────────────────────────────────────────────────────┐
│                      认证流程                               │
├─────────────────────────────────────────────────────────────┤
│  1. SSH Agent        → 从 SSH 代理获取密钥签名              │
│  2. IdentityFile     → 使用 SSH 配置中指定的密钥文件        │
│  3. 默认密钥         → 尝试 id_rsa, id_ed25519, id_ecdsa   │
│  4. 已保存密码       → 使用加密存储的密码                   │
│  5. 密码提示         → 手动输入密码（可选保存）              │
└─────────────────────────────────────────────────────────────┘
```

### exec 认证（非交互式）

exec 子命令自动尝试 Agent → IdentityFile → 默认密钥 → 已保存密码，不会交互式提示输入密码。

### sudo 密码复用

`exec --sudo` 会自动复用 SSH 登录密码作为 sudo 密码，无需额外输入。

## 🗂️ 项目结构

```
sshgo/
├── main.go          # CLI 路由、遗留模式、认证、连接
├── exec.go          # 结构化远程命令执行（exec 子命令）
├── info.go          # 连接信息展示（info 子命令）
├── config.go        # 主机元数据管理（config 子命令）
├── types.go         # 共享类型：ExecResult, InfoResult, HostMeta, LocalConfig
├── password.go      # AES-GCM 加密密码存储
├── CONTEXT.md       # 领域术语表
├── docs/
│   └── adr/
│       └── 0001-separate-metadata-from-passwords.md
├── go.mod           # Go 模块定义
├── go.sum           # 依赖校验
├── LICENSE          # MIT 许可证
└── README.md        # 项目文档
```

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 创建 Pull Request

## 📄 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

## 🔗 相关链接

- [GitHub 仓库](https://github.com/jswh/sshgo)
- [问题反馈](https://github.com/jswh/sshgo/issues)
- [Go 官方文档](https://go.dev/doc/)

---

<div align="center">

**如果这个项目对你有帮助，请给个 ⭐ Star 支持一下！**

</div>
