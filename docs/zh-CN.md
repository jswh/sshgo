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
| 📖 **读取 SSH 配置** | 自动读取 `~/.ssh/config` 中的所有主机配置 |
| 🔑 **SSH Key 认证** | 支持 SSH Agent、配置的 IdentityFile 和默认密钥 |
| 🔒 **密码加密存储** | 使用 AES-GCM 加密保存密码，主密码保护 |
| 🚀 **直接登录** | 支持 `user@host:port` 格式直接连接 |
| 📋 **交互式选择** | 无参数时列出所有主机供选择 |

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

### 查看帮助

```bash
sshgo --help
```

## 🔐 认证方式

程序按以下顺序尝试认证：

```
┌─────────────────────────────────────────────────────────────┐
│                      认证流程                               │
├─────────────────────────────────────────────────────────────┤
│  1. SSH Agent        → 检查是否有可用的 SSH 密钥代理        │
│  2. IdentityFile     → 使用 SSH 配置中指定的密钥文件        │
│  3. 默认密钥         → 尝试 id_rsa, id_ed25519, id_ecdsa   │
│  4. 已保存密码       → 使用加密存储的密码                   │
│  5. 密码提示         → 手动输入密码（可选保存）              │
└─────────────────────────────────────────────────────────────┘
```

## 🗂️ 项目结构

```
sshgo/
├── main.go          # 主程序入口
├── password.go      # 密码加密存储模块
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
