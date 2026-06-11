# sshgo

一个简洁的 SSH 客户端，支持读取系统 SSH 配置、密钥认证和密码加密存储。

## 功能特性

- **读取 SSH 配置** - 自动读取 `~/.ssh/config` 中的所有主机配置
- **SSH Key 认证** - 支持 SSH Agent、配置的 IdentityFile 和默认密钥
- **密码加密存储** - 使用 AES-GCM 加密保存密码，主密码保护
- **直接登录** - 支持 `user@host:port` 格式直接连接
- **交互式选择** - 无参数时列出所有主机供选择

## 安装

```bash
go install github.com/jswh/sshgo@latest
```

或从源码编译：

```bash
git clone https://github.com/jswh/sshgo.git
cd sshgo
go build -o sshgo
```

## 使用方法

```bash
# 直接登录 SSH 配置中的主机
sshgo zg

# 直接登录指定地址
sshgo root@1.2.3.4

# 带端口登录
sshgo root@1.2.3.4:2222

# 交互式选择主机
sshgo

# 查看帮助
sshgo --help
```

## 认证方式

程序按以下顺序尝试认证：

1. **SSH Agent** - 检查是否有可用的 SSH 密钥代理
2. **配置的 IdentityFile** - 使用 SSH 配置中指定的密钥文件
3. **默认密钥** - 尝试 `~/.ssh/id_rsa`、`id_ed25519`、`id_ecdsa`
4. **已保存密码** - 使用加密存储的密码
5. **密码提示** - 手动输入密码（可选保存）

## 密码存储

首次使用密码存储时，会要求设置主密码。所有保存的密码都使用 AES-GCM 加密，存储在 `~/.sshgo_passwords` 文件中。

## 许可证

[MIT License](LICENSE)
