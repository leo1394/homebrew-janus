[English](README.md) | [简体中文](README-ZH.md)


<img src="assets/logo.png" alt="Janus Confidential 标志" width="64">

# Janus

管理 SSH 设备，将密码存入 macOS 钥匙串，通过一个已验证主连接打开多个独立终端会话。

- **主连接验证一次，后续 session 复用。** 已有主连接时不重复读取密码。
- **停止复用，保留工作。** `disconnect` 不打断已建立的 session。
- **配置不含密码。** 设备 JSON 只保存元数据。
- **聚焦基础 CLI。** 从 Janus Confidential 选择性拆分，为后续设备和授权扩展预留接口。

## 环境与发布状态

要求 macOS 14+、Apple Silicon 或 Intel、系统 OpenSSH，以及可显示原生密码和身份验证
窗口的图形登录会话。源码构建需要 Go 1.22+（或当前 macOS 要求的更新版本）与带 Swift
的 Xcode Command Line Tools。目前只在 Apple Silicon 验证；Intel 和较早 macOS 版本
尚未实测。安装器不代表所有旧系统都兼容。

构建版本从 `VERSION.txt` 读取，发布日期使用编译当天日期。设置正式版本号不代表已发布。
正式原生产物须完成 Developer ID 签名和 Apple 公证后发布。预编译分发覆盖 Apple Silicon 和 Intel，两者安装均不需要本地编译。
Intel 运行验收仍未执行，不增加平台验收范围。

## 当前构建与安装

在项目目录运行：

```sh
make build
./build/bin/janus version
./install.sh --local build --prefix "$HOME/.local"
export PATH="$HOME/.local/bin:$PATH"
export MANPATH="$HOME/.local/share/man:${MANPATH:-}"
```

仅构建本项目的 Go CLI 和原生钥匙串助手，不生成任何 Janus Confidential 产物。
安装器独立于 Homebrew，先准备并校验文件，再替换可执行文件；失败时恢复已替换文件。
整个多文件安装不是原子事务。

通过 Homebrew 安装稳定版，无需本地编译：

```sh
brew tap leo1394/janus
brew install leo1394/janus/janus
```

独立安装器支持 `./install.sh --version X.Y.Z --prefix DIR`，使用匹配的 GitHub Release
安装包和校验和。实际可用性以指定版本、架构的已发布产物为准。

## 第一次连接

准备可达的 SSH 主机及支持密码登录的账户：

```sh
janus spawn builder@example.com -p 3233
# 在原生窗口确认主机密钥、输入密码。
# 退出远端 Shell 后，主连接仍可复用。
janus devices
janus rename builder@example.com 'Build Server'
janus connect 'Build Server'
```

列表字段为 `ID NAME PLATFORM TYPE CONNECTED READY`。SSH 行显示
`PLATFORM=terminal`、`TYPE=ssh`，`terminal` 不是命令或别名。
可复用主连接存在时，两列状态均为绿色 `✔︎`；确认失败显示红色 `✘`，未知显示 `-`。
JSON 使用布尔值或 null，不含颜色。

## 命令

| 命令 | 用途 |
| --- | --- |
| `devices [--list\|--json]` | 列出设备；默认表格，执行有时限的网络探测 |
| `spawn USER@HOST [-p PORT]` | 登记新设备，或连接已有设备 |
| `connect [ID\|NAME]` | 打开终端 session；缺省进入选择器 |
| `disconnect [ID\|NAME ...]` | 停止主连接复用，保留活动 session；缺省进入选择器 |
| `remove [ID\|NAME ...]` | 缺省时选择设备；删除设备记录及对应钥匙串凭据，保留活动连接 |
| `rename ID\|NAME NEW_NAME` | 修改名称；名称忽略大小写判重，必须唯一 |
| `help [command]`、`--help` | 无需初始化设备即可查看帮助 |
| `version`、`--version` | 精确输出两行版本信息 |
| `upgrade [--yes]` | 检查稳定版本，更新归属明确的安装 |

`upgrade` 不会在非交互环境静默批准更新，也不会隐式降级；Homebrew 安装继续由对应
Formula 管理，源码构建需要重新构建。开发版本不会被误认为稳定版。

## 补全和手册

两种安装方式都部署 Bash、Zsh、Fish 补全以及 `man janus`。独立安装后，启用所用 Shell：

```sh
# Bash 3.2+（不依赖 bash-completion）
source "$HOME/.local/share/bash-completion/completions/janus"
# Zsh：放在现有 compinit 调用之前
fpath=("$HOME/.local/share/zsh/site-functions" $fpath)
autoload -Uz compinit && compinit
# Fish（Fish 语法）
source ~/.local/share/fish/vendor_completions.d/janus.fish
```

Homebrew 用户如未加载补全目录，按其 Shell 补全说明配置。动态设备补全只读本地设备
元数据；Tab 不启动服务、不读取密码、不下载依赖。

## 能力边界

密码只在原生进程内存中短暂存在，持久化位置是登录钥匙串。主连接复用是 SSH 身份验证
优化，**不是 Agent 授权**。基础运行时使用用户私有的 OpenSSH socket，不实现
Confidential 的原生进程证明和 Agent 准入控制。

现有 Confidential/Workbench 客户端及记录不会自动迁移，后续适配必须使用单一运行时
所有者。旧凭据的助手授权不兼容时，原生窗口允许重新输入 SSH 密码，保存独立的
基础 Janus 凭据；恢复过程保留旧条目，也不会覆盖已有的基础 Janus 凭据。
无缝升级仍需要稳定的发布签名。密码被服务器拒绝时，需要在钥匙串访问中修改对应条目。
本阶段不支持仅密钥登录、任意 SSH 参数、Android/iOS/MySQL 或 Confidential 扩展命令。
首次登录失败会保留设备记录，便于重试。

验证窗口小于 3 秒、复用连接小于 1 秒是**验收目标，不是已测量的性能承诺**。
详见[架构与扩展边界](docs/architecture.md)、[拆分来源](docs/provenance.md)、
[验证记录](docs/validation.md)和[发布清单](docs/releases.md)。

## 开发

运行 `go test ./...`、`make build`，不要求格式化。修改生命周期行为时补充行为测试，
同步维护命令、帮助、补全及中英文说明。许可证见 [LICENSE](LICENSE) 和保留的来源声明。

`disconnect` 和 `remove` 支持多个 ID/名称，缺省时打开多选器（空格勾选，Enter 确认）。
同一设备只执行一次；未匹配或操作失败不会中止其他设备的处理。
完成后汇总并高亮统计数字（遵循 `NO_COLOR`）；存在失败或未匹配时返回非零退出码。
活动终端会话继续保留。

## 协议

项目采用 Apache License 2.0，见 [LICENSE](LICENSE)。第三方代码保留原有许可声明。
