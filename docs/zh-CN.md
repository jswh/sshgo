<div align="center">

# sshgo

**一份自有的主机清单，远程命令返回结构化数据。**

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.20+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/jswh/sshgo?logo=github)](https://github.com/jswh/sshgo/releases/latest)

**[English](README.md)** | 简体中文

</div>

---

sshgo 是一个用 Go 写的 SSH 客户端。它通过 `golang.org/x/crypto/ssh` 直接说 SSH 协议而不调系统 ssh，把你常用的主机放进自己的一个清单文件，远程命令以 JSON 返回 `stdout`、`stderr` 与远端退出码。

它不是 `ssh` 全部语法的替代品：`ssh://` URI 与逗号分隔的多跳写法会被拒绝，IPv6 地址暂不支持。主机密钥校验也尚未实现——这一点写在下面的"现在还不做什么"，而不是藏在脚注里。

## 快速上手

```bash
curl -fsSL https://github.com/jswh/sshgo/raw/main/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"

sshgo config import          # 从 ~/.ssh/config 导入一次
sshgo exec web-01 "uptime"   # 跑一条命令，拿回 JSON
```

第二条命令的输出长这样：

```json
{
  "command": "uptime",
  "stdout": " 10:12:03 up 41 days,  2:11,  1 user,  load average: 0.00, 0.01, 0.05\n",
  "stderr": "",
  "exit_code": 0
}
```

导入是可选的。你完全可以只用直接地址（`sshgo root@1.2.3.4:2222`），什么都不导入。

## 最常用的三条命令

### `exec`：远程命令返回结构化数据

```bash
sshgo exec db-staging "SELECT count(*) FROM users"
sshgo exec --raw web-01 "tail -n 100 /var/log/nginx/access.log"   # 原样透传，不出 JSON
sshgo exec --sudo db-staging "systemctl restart postgresql"
```

JSON 携带 `command`、`stdout`、`stderr` 和 `exit_code`。`duration` 是人类可读字符串，只在 `--sudo` 或开启计时时出现。`exit_code` 是远端命令自己的退出码，脚本可以直接分支。本地失败（未知名、缺凭据、某跳不可达）一律退出码 `1`——远端命令跑起来之前出的一切错共用一个码，你的脚本读到的数字不会有两个含义。

`exec` 全程不向你提问。它依次尝试 SSH agent、主机的 `IdentityFile`、默认密钥文件和已存口令；都不行就失败，点名缺凭据的那台主机，并指向 `sshgo config set-password <host>`。这条规则对跳板链的每一跳都成立。CI 任务回答不了提示，所以 sshgo 不问它。

sudo 口令只发给目标机，作为其 `sudo -S` 的 stdin，绝不参与任何一跳的认证。

### `info`：sshgo 对这个名字会做什么

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

`--json` 返回 `name`、`hostname`、`port`、`user`、`source`、`auth_methods`，经跳板访问时还有 `jump`。认证以方法名和数量呈现；密钥路径、口令和 passphrase 绝不出现在任何输出里——这是契约，不是排版选择。`info` 会解析跳板链并在不拨号的情况下报告断链，与真实连接得出同样的结论。

### 直接 `sshgo`：从清单里选

不带参数运行 sshgo 会列出主机供你选择。裸名只在 sshgo 自己的清单里查：

```bash
sshgo zg
```

名字不在清单里时，sshgo 会停下并给出三条出路，而不是像 `ssh` 那样悄悄把 `zg` 当主机名去拨号：

```
Host "zg" not found.
  Import your SSH config:  sshgo config import
  Add it manually:         sshgo config set zg hostname <address>
  Or connect directly:     sshgo user@host or sshgo host:port
```

这条消息就是契约：由 `internal/host/resolve.go` 生成，`internal/cli/cli_test.go:TestResolutionErrorText` 钉住全文。

凡含 `@` 或 `:` 的输入都是直接地址——sshgo 自己的语法，按原样拨号。端口默认 `22`，用户默认 `root`。

## 主机清单从哪来、之后归谁

`sshgo config import` 读取 `~/.ssh/config` 一次，把它描述的主机写进 `~/.sshgo_config`。从此 sshgo 只读自己的文件。改系统配置不影响 sshgo 的行为——这是设计要点，也是代价。

导入是快照，不是同步：

- 导入的是生效值（`Host *` 默认值合并后）。
- 通配与取反 pattern（`Host *`、`Host *.example.com`）被跳过，sshgo 只连具体主机。
- 重复导入刷新连接细节，保留别名、备注、标签与连接优先级。
- 你在 `~/.ssh/config` 里删掉的主机不会在这里消失，要用 `sshgo config remove <host>`。

值得直说：两份清单会各自漂移，把它们合回来是手动动作。

元数据也可以手工编辑：

```bash
sshgo config list                       # sshgo 知道的一切
sshgo config find --tag prod            # 按标签过滤
sshgo config set db1 notes "primary pg" # 九个键之一
sshgo config get db1 hostname
sshgo config unset db1 tags
sshgo config remove db1
```

`config set` 接受 `alias`、`notes`、`tags`、`connection-priority`、`hostname`、`port`、`user`、`identity-file`、`proxy-jump`，其他键一律拒绝并列出这份清单。

## 经跳板抵达目标机

跳板主机就是 sshgo 清单里的一台普通主机。把目标指向它：

```bash
sshgo config set bastion hostname 203.0.113.9
sshgo config set bastion user ops
sshgo config set db1 hostname 10.0.0.5
sshgo config set db1 proxy-jump bastion

sshgo exec db1 "pg_isready"    # 经 bastion 拨号
sshgo info db1                 # Jump: bastion
```

链可以嵌套任意深度。若 `inner` 只有 `bastion` 能到：

```bash
sshgo config set inner proxy-jump bastion
sshgo config set db1 proxy-jump inner
sshgo info db1                 # Jump: bastion -> inner
```

凭据不跨跳。每一跳用它自己的用户、端口、`IdentityFile` 和已存口令；不从目标继承任何东西，也不向目标回传任何东西。SSH agent 是链上唯一共享物，因为它属于你而不是某台主机。

整条链在任何拨号发生前解析。缺一跳、某跳没有地址、成环，都会立即失败并写出路径——环会显示为 `db1 -> bastion -> db1`，而不是超时。前向引用合法：先建目标、后补跳板。

`proxy-jump` 接受 sshgo 主机名、别名、直接地址，或表示直连的 `none`。ssh 的写法不被接受，因为 sshgo 只有一套寻址语法。

## 两个文件，一个主口令

`~/.sshgo_config` 以明文 JSON 存主机元数据。

`~/.sshgo_passwords` 存凭据，AES-GCM 加密。密钥由你的主口令经 argon2id 派生（time 1、内存 64 MiB、threads 4、16 字节随机盐），文件权限 `0600`。主口令不存任何地方、无法恢复；忘了它，已存凭据随之消失。

这两半都要读：口令文件是受保护的，主机清单不是。能读你家目录的人能看到你连哪些机器、用什么账号、哪些端口、哪些只能经跳板抵达。如果这对你重要，等 `known_hosts` 校验与元数据加密落地，或把清单放在你信任的地方。

口令库每次运行最多解锁一次，一条链上多跳需要凭据也不会反复要主口令。

## 安装与更新

```bash
curl -fsSL https://github.com/jswh/sshgo/raw/main/install.sh | sh    # Linux / macOS
irm https://github.com/jswh/sshgo/raw/main/install.ps1 | iex          # Windows PowerShell
go install github.com/jswh/sshgo@latest                               # Go 工具链
```

安装脚本把最新 release 的二进制放进 `~/.local/bin`。源码构建就是在克隆的仓库里 `go build -o sshgo`。

```bash
sshgo update
```

`update` 从 GitHub 取最新 release，与当前二进制内嵌的版本比较，有新版就替换。若二进制在系统目录，回退用 `sudo mv`。

<!-- 待定（owner）：跳板链在 main，最新 release 是 v0.5.0。要不要写"想要跳板链需装 main 或源码"，以及何时出新 release，发布承诺归 owner。 -->

## 现在还不做什么

这些是当前的缺口，不是路线图措辞。

- **主机密钥不校验。** 每一跳都用 `InsecureIgnoreHostKey()`。跳板链不改变这一点：被攻陷的堡垒机可以冒充目标。`known_hosts` 校验是独立特性。
- **ssh 的跳板语法不支持。** `ssh://` URI 与逗号分隔的多跳值（`bastion,inner`）会被拒绝。请用每主机 `proxy-jump` 写链。
- **跳板必须是 sshgo 管理的主机或直接地址。** sshgo 不管理的名字不能当跳板，因为 sshgo 运行时绝不读 `~/.ssh/config`。
- **不支持 IPv6**，主机地址和跳板值都不支持。

## 命令一览

| 命令 | 作用 |
|---|---|
| `sshgo` | 列出主机并选择 |
| `sshgo <name>` | 连接清单里的主机 |
| `sshgo user@host[:port]` | 连接命令行给的地址 |
| `sshgo exec [--raw] [--sudo] <host> <cmd>` | 执行命令，返回 JSON 或原始流 |
| `sshgo info [--json] <host>` | 显示解析后的连接细节与跳板链 |
| `sshgo config import` | 从 `~/.ssh/config` 快照一次 |
| `sshgo config set\|get\|unset\|remove` | 编辑一台主机的元数据 |
| `sshgo config list` / `find --tag` | 查看与过滤清单 |
| `sshgo config set-password` / `set-sudo-password` | 存一台主机的凭据 |
| `sshgo update` | 用最新 release 替换二进制 |

完整参数见 `sshgo <subcommand> --help`。

## 为什么这样设计

三个决定解释了大部分表面：

- [ADR-0001](docs/adr/0001-separate-metadata-from-passwords.md)：主机元数据与凭据分两个文件，列主机不需要口令。
- [ADR-0002](docs/adr/0002-runtime-independence-from-system-ssh.md)：sshgo 运行时绝不读 `~/.ssh/config`；导入是唯一触点。
- [ADR-0003](docs/adr/0003-proxyjump-as-a-chain-of-sshgo-hosts.md)：跳板是 sshgo 主机，链因此可嵌套、凭据因此可按跳隔离。

[CONTEXT.md](CONTEXT.md) 是这些文档用的术语表：Host、Alias、Direct Address、Jump Host、Jump Chain、hop、Credential Key。CLI 里的名字就是这份文件里的名字。

欢迎贡献。[LICENSE](LICENSE) 为 MIT。
