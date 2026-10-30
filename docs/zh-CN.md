<div align="center">

# sshgo

**简单、安全、高效的 SSH 客户端**

一个简洁的 SSH 客户端：导入一次系统 SSH 配置，之后 sshgo 拥有自己的主机清单，运行时只读自己的元数据。支持密钥认证、密码加密存储、结构化远程执行、连接详情查看与跳板机。

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.20+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/jswh/sshgo?logo=github)](https://github.com/jswh/sshgo/releases/latest)
[![GitHub Stars](https://img.shields.io/github/stars/jswh/sshgo?style=social)](https://github.com/jswh/sshgo/stargazers)

[English](../README.md) | **简体中文**

</div>

---

## ✨ 功能特性

| 功能 | 描述 |
|------|------|
| 📖 **导入 SSH 配置** | 通过 `config import` 一次性导入 `~/.ssh/config` 的主机 |
| 🛡️ **运行时独立** | 运行时永不读取 `~/.ssh/config`，导入是唯一的接触点 |
| 🔑 **SSH Key 认证** | 支持 SSH Agent、配置的 IdentityFile 和默认密钥 |
| 🔒 **密码加密存储** | 使用 AES-GCM 加密保存密码，主密码保护 |
| 🚀 **直接登录** | 支持 `user@host:port` 格式直接连接 |
| 📋 **交互式选择** | 无参数时列出所有主机供选择 |
| 📦 **Exec（结构化执行）** | 远程命令执行，返回结构化 JSON 输出 |
| 🔀 **跳板机** | 经一台或多台堡垒机到达主机（`proxy-jump`），每跳各自持有凭据 |
| 🔍 **Info** | 查看主机的连接详情 |
| 🏷️ **主机元数据** | 管理别名、备注、标签、连接优先级 |
| 🔄 **自动更新** | 一条命令更新到最新版本（`update` 子命令） |
| 📥 **一键安装** | 安装脚本自动下载二进制到 `~/.local/bin` |

## 🚀 快速安装

### 一键安装（推荐）

**Linux / macOS**
```bash
curl -fsSL https://github.com/jswh/sshgo/raw/main/install.sh | sh
```

**Windows (PowerShell)**
```powershell
irm https://github.com/jswh/sshgo/raw/main/install.ps1 | iex
```

自动下载最新 release 二进制到 `~/.local/bin/`。

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

这会一次性读取 `~/.ssh/config`，把它描述的主机存入 `~/.sshgo_config`。
从此这份清单归 sshgo 所有：运行时不再读 `~/.ssh/config`，所以你改系统配置
不会悄悄改变 sshgo 的行为。

导入是**快照**，不是同步。存下的是应用 `Host *` 默认值之后的**有效值**；
重复导入会刷新连接详情，同时保留你的 alias、notes、tags、priority。
在 `~/.ssh/config` 里删掉的主机**不会**从 sshgo 中消失，请用
`sshgo config remove <host>` 手动删除。
通配符 pattern（`Host *`、`Host *.example.com`）会被跳过，因为 sshgo 只连具体主机。

### 登录清单中的主机

```bash
sshgo zg
```

裸名只在 sshgo 自己的主机清单里查找。查不到就报错退出，并给出三条出路，
**不会**像 `ssh` 那样把名字当主机名去连。

### 直接登录指定地址

含 `@` 或 `:` 的输入是直连地址，属于 sshgo 自己的语法，与 `~/.ssh/config`
无关。直连地址永远按原样连接：

```bash
sshgo root@1.2.3.4
```

### 带端口登录

```bash
sshgo root@1.2.3.4:2222
```

端口缺省 `22`，用户缺省 `root`。不支持 IPv6 地址；请改为建一条 Host 条目。

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

从 `~/.ssh/config` 导入主机（一次性快照，详见「首次使用」）：

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
sshgo config set my-vm proxy-jump bastion               # bastion 是已存在的 Host
```

`proxy-jump` 指定一台**跳板机（Jump Host）**，连接会真的经过它。取值按与其他
Host 参数完全相同的规则解析，所以 Host 名、Alias、直连地址都可以；`none` 或空值
表示直连。链可以任意深度：如果 `bastion` 自己也有 `proxy-jump`，那一跳会先被拨通。
成环的链在拨号之前就会被拒绝，并打印出环的路径。

删除整条主机条目（无确认交互，可在脚本中使用）：

```bash
sshgo config remove my-vm
```

等同于 `sshgo config unset my-vm`。之所以需要它：在 `~/.ssh/config` 里删掉的
主机不会因为重新导入而从 sshgo 中消失。

查看所有主机及其元数据：

```bash
sshgo config list
```

按标签搜索：

```bash
sshgo config find --tag prod
```

配置 sudo 密码（加密存储在 `~/.sshgo_passwords`）：

```bash
sshgo config set-sudo-password db-staging
```

配置 SSH 密码（同样的交互，同样的存储）。用于只认口令的主机——通常是脚本里要
经过的堡垒机：

```bash
sshgo config set-password bastion
```

### 自动更新

检查最新 release 并更新当前二进制：

```bash
sshgo update
```

update 子命令从 GitHub 获取最新版本，与构建时嵌入的版本号比较，
发现新版本后自动下载并替换二进制。如果二进制在系统目录中，
会自动 fallback 到 `sudo mv`。

### 跳板机：经堡垒机到达内网主机

给目标机设一个指向已存在 Host 的 `proxy-jump`：

```bash
sshgo config set bastion hostname 203.0.113.9     # 你要经过的那台
sshgo config set bastion user ops
sshgo config set db1 hostname 10.0.0.5            # 它背后的目标
sshgo config set db1 proxy-jump bastion

sshgo exec db1 "pg_isready"                       # 经 bastion 连接
sshgo info db1                                    # 显示 Jump: bastion
```

每一跳都是一台普通 Host，有自己的用户、端口、IdentityFile 与存库口令。凭据**不跨跳
共享**：`db1` 的口令与 sudo 口令绝不会发给 `bastion`，`bastion` 的口令也不会被复用到
`db1`。唯一共享的是 SSH agent，因为它属于你，而不属于某台主机。

链可以嵌套。如果 `inner` 只能从 `bastion` 到达：

```bash
sshgo config set inner proxy-jump bastion
sshgo config set db1 proxy-jump inner
sshgo info db1                                    # Jump: bastion -> inner
```

链在拨号之前就解析完毕，所以跳板不存在、跳板没有地址、或链成环都会立刻失败，
并打印出路径。`exec` 在任何一跳都不会提示输入 SSH 口令：某一跳既无 key 又无存库
口令时立刻失败并点名那一跳，出路是 `sshgo config set-password <跳板>`。

### 查看帮助

```bash
sshgo --help
sshgo exec --help
sshgo info --help
sshgo config --help
sshgo update --help
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
跳板链上的每一跳同样如此：某一跳既无 key 又无存库口令时立刻失败并点名那一跳，
出路是 `sshgo config set-password <跳板>`。

### sudo 密码

`exec --sudo` 按以下优先级确定 sudo 密码：

1. **已保存的 sudo 密码**（通过 `sshgo config set-sudo-password <host>` 设置）
2. **已保存的 SSH 密码**（复用 SSH 登录密码）
3. **交互式提示**（密钥登录且没有 NOPASSWD 时）
4. **NOPASSWD**（依赖远程主机的 sudo 配置）

预先配置 sudo 密码：

```bash
sshgo config set-sudo-password db-staging
```

使用密钥登录且远程没有 NOPASSWD 时，`exec --sudo` 会提示输入 sudo 密码并可选保存。

已保存的口令按**你寻址的名字**存放，因此 `set-sudo-password` 与 `exec` 要用同一个名字。
Alias 与它指向的 Host 名是两个不同的键：为 `db-staging` 存的口令，在运行
`exec --sudo db-staging-alias` 时不会命中。如果寻址的名字没有对应条目，`exec`
会回落到解析后的 Hostname，以兼容按地址存的旧条目。

## ⚠️ 已知限制

- **不校验主机密钥。** sshgo 在每一跳都用 `InsecureIgnoreHostKey()`。因此跳板链**不会**
  让 sshgo 更安全：被攻陷的堡垒机可以冒充目标机。`known_hosts` 校验是另一个独立特性。
- **不支持 ssh 的跳板语法。** `ssh://` URI 与逗号多跳写法（`bastion,inner`）会被拒绝。
  请改为逐台 Host 设置 `proxy-jump`。
- **跳板必须是 sshgo 管理的 Host 或直连地址。** sshgo 不认识的裸名不能当跳板，因为
  运行时它从不读 `~/.ssh/config`。
- **不支持 IPv6**，无论是主机地址还是跳板取值。

## 🗂️ 项目结构

```
sshgo/
├── main.go              # os.Exit(cli.Run(...)) 与 -ldflags 注入的版本号
├── internal/cli/        # 子命令分发、帮助文案、退出码、两套认证策略
├── internal/host/       # Host 元数据、CRUD、出处、解析
├── internal/credential/ # 加密口令库（AES-GCM + argon2id）
├── internal/sshconfig/  # config import：纯函数快照构建 + ssh 配置适配器
├── internal/connect/    # 唯一拨号点：Dial / Chain / 会话
├── internal/update/     # 自更新，版本号由调用方传入
├── internal/ui/         # 终端输入：选择、提示、终端判定
├── install.sh           # 一键安装脚本 (Unix)
├── install.ps1          # 一键安装脚本 (Windows)
├── test/                # e2e：Dockerfile（sshd + sudoers）+ setup.sh
├── CONTEXT.md           # 领域术语表
├── docs/
│   ├── adr/
│   │   ├── 0001-separate-metadata-from-passwords.md
│   │   ├── 0002-runtime-independence-from-system-ssh.md
│   │   └── 0003-proxyjump-as-a-chain-of-sshgo-hosts.md
│   └── zh-CN.md         # 简体中文文档
├── go.mod               # Go 模块定义
├── go.sum               # 依赖校验
├── LICENSE              # MIT 许可证
└── README.md            # 项目文档
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
