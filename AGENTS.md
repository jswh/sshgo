# AGENTS.md

> 面向 AI 编码代理与协作者的仓库工作说明。人类开发者请看 [README.md](README.md)（简体中文：[docs/zh-CN.md](docs/zh-CN.md)）。

## 项目一句话

单二进制 SSH 客户端：把 `~/.ssh/config` 一次性导入本地主机元数据（`~/.sshgo_config`，明文 JSON），口令单独加密存放（`~/.sshgo_passwords`，AES-GCM + 主口令），支持直连地址 `user@host:port`、结构化远程执行（`exec` 输出 JSON）、连接详情查看（`info`）与自更新（`update`）。

## 结构

```
├── main.go            # 子命令分发 + 无参交互选择 + legacy 模式 `sshgo <host> [cmd]`
├── exec.go            # exec：结构化 JSON 输出 / --raw 原始流 / --sudo
├── info.go            # info：非敏感的连接详情（JSON + 人类可读文本）
├── config.go          # config：Host 元数据 CRUD、`config import`、set-sudo-password
├── password.go        # ~/.sshgo_passwords 的 AES-GCM 加解密与主口令
├── update.go          # update：用 HTTP 重定向发现最新 release tag（避开 API 限流）后替换自身
├── types.go           # HostMeta / LocalConfig 等落盘结构
├── version.go         # main.version，构建期由 -ldflags -X 注入，开发构建默认 "dev"
├── internal/ui/       # 纯 bufio.Scanner + x/term 的交互层（替换 promptui 而来，见「硬性约束」）
├── CONTEXT.md         # 领域术语表（角色 / 概念 / _Avoid_ 反例）
├── docs/adr/          # 架构决策记录（目前仅 0001）
├── docs/zh-CN.md      # 面向用户的简体中文说明
├── install.sh / install.ps1   # 一键安装到 ~/.local/bin（Windows 为 PowerShell 5.1 兼容写法）
├── test/              # e2e：Dockerfile（sshd + sudoers）+ setup.sh
└── .github/workflows/release.yml  # tag v* 触发 6 平台交叉编译 + Release
```

## 领域要点（细则见 `CONTEXT.md`）

- **Host** 有三个来源（**Source**）：`imported`（从 `~/.ssh/config` 导入）、`direct`（命令行 `user@host:port`）、`manual`（仅本地元数据）。
- 元数据与口令**分文件存放**（ADR-0001）：元数据非敏感、读得频繁、不需要主口令；口令敏感、只在认证时读、加密存放。
- **子命令优先于主机名**：首个参数命中 `exec`/`info`/`config`/`update` 即按子命令处理 ⇒ 同名 SSH config host 无法用位置参数访问（改用 Alias）。这是有意的设计，不是 bug。
- `exec --sudo` 的 **Sudo Password Priority** 四级有序：已存 sudo 口令 → 复用已存 SSH 口令 → 交互提示 → 依赖目标机 NOPASSWD。
- `info` 的 **Auth Methods** 只描述粒度（如 `SSH Agent (3 keys), Password (saved)`），**永不**输出 key 路径、口令或 passphrase。

## 开发约定

### 构建 / 测试

```bash
go build -o sshgo .                 # 本地开发
go build -ldflags="-s -w -X main.version=vX.Y.Z" -o sshgo .   # 版本注入（与 CI 一致）
go test ./...                       # 单元测试（update_test.go）
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

## 已知问题（本次初始化时记录，待 owner 决定）

- **`main` 在干净 clone 上构建不过**：`go.mod` 里有 `replace golang.org/x/term v0.44.0 => ./.deps/golang.org/x/term`，而 `.deps/` 被 `.gitignore` 排除 ⇒ `go build ./...` 与 `go mod tidy` 都报 `replacement directory ./.deps/golang.org/x/term does not exist`。该 replace 由最新提交 `0eaefab` 引入。删掉这行改用上游 `x/term v0.44.0` 后，构建与 `go test ./...` 均通过（已在临时副本验证，未改动仓库）。
- 上述问题的暴露面：`release.yml` 的「Tidy modules」步骤会失败，但该 workflow 只在打 tag 时跑，而 `0eaefab`/`4302be6` 尚未进任何 release（最新 tag 停在 `v0.5.0`）⇒ **下次发版前需要先处理它**。
- **倾向结论（已核，未动仓库）**：上游 `golang.org/x/term@v0.44.0` 里**没有任何 `func init()`，也不引用 `kernel32`/`AllocConsole`**（在 module cache 里 grep 证实）⇒ 这条 replace 对「Windows 控制台窗口」问题并非必需（真正的元凶 `chzyer/readline` 已在 `0eaefab` 随 promptui 一起移除）。**最小修法＝删掉 `go.mod` 那行 `replace`**，本地已验证 build 与测试都过。
