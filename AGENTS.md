# AGENTS.md

> 面向 AI 编码代理与协作者的仓库工作说明。人类开发者请看 [README.md](README.md)（简体中文：[docs/zh-CN.md](docs/zh-CN.md)）。

## 项目一句话

单二进制 SSH 客户端：把 `~/.ssh/config` 一次性导入本地主机元数据（`~/.sshgo_config`，明文 JSON），口令单独加密存放（`~/.sshgo_passwords`，AES-GCM + 主口令），支持直连地址 `user@host:port`、结构化远程执行（`exec` 输出 JSON）、连接详情查看（`info`）与自更新（`update`）。

## 结构

```
├── main.go            # 仅 os.Exit(cli.Run(...)) + version 变量（-ldflags -X main.version 注入）
├── internal/cli/      # 子命令分发、帮助文案、退出码、交互选择、两套认证策略（interactiveAuth / execAuth）
├── internal/host/     # Host 元数据与本地配置、CRUD、Source 推导、解析入口 Resolved（含 CredKeys）
├── internal/credential/ # ~/.sshgo_passwords：AES-GCM + argon2id，Save/Load，按有序键序列查找
├── internal/sshconfig/  # ~/.ssh/config 导入快照的纯函数 + 配置适配器
├── internal/connect/    # 唯一拨号点 Dial + 会话（PTY / 捕获执行）；不依赖 ui/os/term
├── internal/update/     # 自更新，版本号由调用方传入
├── internal/ui/       # 纯 bufio.Scanner + x/term 的交互层（替换 promptui 而来，见「硬性约束」）
├── CONTEXT.md         # 领域术语表（角色 / 概念 / _Avoid_ 反例）
├── docs/adr/          # 架构决策记录（目前仅 0001）
├── docs/zh-CN.md      # 面向用户的简体中文说明
├── install.sh / install.ps1   # 一键安装到 ~/.local/bin（Windows 为 PowerShell 5.1 兼容写法）
├── test/              # e2e：Dockerfile（sshd + sudoers）+ setup.sh
└── .github/workflows/release.yml  # tag v* 触发 6 平台交叉编译 + Release
```

### 分层约定

- **`internal/cli` 是唯一知道用户可见文案的包**：帮助文本、错误文案、退出码、进度行都在这里；其他包只返回类型化错误（`host.ErrHostNotFound` 等）或原文案由 cli 拼装（`connect.Error.Op`）。
- **`internal/connect` 是唯一拨号点**：认证*策略*由 cli 注入 `Authenticator`，`connect` 只负责拨号与会话。因此 `connect` 不 import `ui`/`os`/`term`，可用假策略纯单测整条路径（`internal/connect/connect_test.go` 内含进程内 sshd）。
- **`internal/host` 是单机事实的唯一权威**：它不认识「链」；Credential Key 序列在 `host` 构造，`credential` 只消费 `[]string`。
- **`internal/update` 不读全局版本变量**：版本号是入参，根包 `version` 只用于 `-ldflags -X main.version` 注入与传给 `cli.Run`。

## 领域要点（细则见 `CONTEXT.md`）

- **Host** 有三个来源（**Source**）：`imported`（从 `~/.ssh/config` 导入）、`direct`（命令行 `user@host:port`）、`manual`（仅本地元数据）。
- 元数据与口令**分文件存放**（ADR-0001）：元数据非敏感、读得频繁、不需要主口令；口令敏感、只在认证时读、加密存放。
- **子命令优先于主机名**：首个参数命中 `exec`/`info`/`config`/`update` 即按子命令处理 ⇒ 同名 SSH config host 无法用位置参数访问（改用 Alias）。这是有意的设计，不是 bug。
- `exec --sudo` 的 **Sudo Password Priority** 四级有序：已存 sudo 口令 → 复用已存 SSH 口令 → 交互提示 → 依赖目标机 NOPASSWD。
- 上述两处口令查找与交互提示的保存，都按 **Credential Key** 定位：用户寻址的名字优先，其次 Host 名，最后回落解析后的 Hostname（仅为兼容按地址存的旧条目）。存与取必须用同一套键序，否则存了也查不到（ticket 01 的原始缺陷）。
- `info` 的 **Auth Methods** 只描述粒度（如 `SSH Agent (3 keys), Password (saved)`），**永不**输出 key 路径、口令或 passphrase。

## 开发约定

### 构建 / 测试

```bash
go build -o sshgo .                 # 本地开发
go build -ldflags="-s -w -X main.version=vX.Y.Z" -o sshgo .   # 版本注入（与 CI 一致）
go test ./...                       # 单元测试（各 internal 包自带；cli 内有文案/JSON 金测试）
./test/setup.sh                     # e2e，依赖 Docker（test/Dockerfile 起 sshd + sudoers）
```

- Go 1.25（`go.mod`）；CI 只在 `v*` tag 上跑，矩阵为 linux/darwin/windows × amd64/arm64。
- 注释用英文；面向用户的 help 文案全英文（`8730c18` 统一过）。

### 硬性约束（历史踩坑，勿回退）

- **不要引入会在 `init()` 里 hook Windows 控制台的库**（`promptui` / 其传递依赖 `chzyer/readline` 就是因此被移除的，`0eaefab`）。需要终端交互一律走 `internal/ui`。
- **不要提交运行时产物与本地依赖**：`.gitignore` 已排除二进制 `sshgo`、`test/results.txt`、`.deps/`。
- **ADR-0001 不可静默推翻**：把元数据与口令合并成一个文件属于安全模型变更，需在提案里显式指出冲突并新增 ADR。
- **术语用 `CONTEXT.md` 的说法**：不要用它明确 `_Avoid_` 掉的「run / execute / status / settings / nickname」等说法。
- 输出、日志、错误信息里**不得出现**口令、passphrase 私钥路径等敏感值。
