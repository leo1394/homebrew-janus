[English](README.md) | [简体中文](README-ZH.md)

<img src="assets/logo.png" alt="Janus Confidential标志" width="64">

# Janus Confidential CLI

管理 SSH 设备，将密码存入 macOS 钥匙串，通过一个已验证主连接打开多个独立终端会话。

- **Janus 不保存任何密码，密码仅安全存放在 macOS 钥匙串中。** 设备配置只包含连接信息。
- **主连接验证一次，后续会话复用。** 已有主连接时不重复读取密码。
- **停止复用，保留工作。** `disconnect` 不打断已建立的终端会话。

## 环境要求

macOS 14+（Apple Silicon 或 Intel）、系统 OpenSSH，以及可显示密码和身份验证窗口的图形登录会话。

## 安装

通过 Homebrew 安装，无需本地编译：

```sh
brew tap leo1394/janus
brew install leo1394/janus/janus
```

也可以使用独立安装器，无需 Homebrew。在项目目录运行：

```sh
./install.sh --prefix "$HOME/.local"
export PATH="$HOME/.local/bin:$PATH"
export MANPATH="$HOME/.local/share/man:${MANPATH:-}"
```

安装器下载并校验匹配的安装包；可通过 `--version X.Y.Z` 指定版本。

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

<table>
<tr><th width="320">命令</th><th>用途</th></tr>
<tr><td><code>devices&nbsp;[--list|--json]</code></td><td>列出设备；默认表格，执行有时限的网络探测</td></tr>
<tr><td><code>spawn&nbsp;USER@HOST&nbsp;[-p&nbsp;PORT]</code></td><td>登记新设备，或连接已有设备</td></tr>
<tr><td><code>connect&nbsp;[ID|NAME]</code></td><td>打开终端 session；缺省进入选择器</td></tr>
<tr><td><code>disconnect&nbsp;[ID|NAME&nbsp;...]</code></td><td>停止主连接复用，保留活动 session；缺省进入选择器</td></tr>
<tr><td><code>remove&nbsp;[ID|NAME&nbsp;...]</code></td><td>缺省时选择设备；删除设备记录及对应钥匙串凭据，保留活动连接</td></tr>
<tr><td><code>rename&nbsp;ID|NAME&nbsp;NEW_NAME</code></td><td>修改名称；名称忽略大小写判重，必须唯一</td></tr>
<tr><td><code>help&nbsp;[command]</code>、<code>--help</code></td><td>无需初始化设备即可查看帮助</td></tr>
<tr><td><code>version</code>、<code>--version</code></td><td>精确输出两行版本信息</td></tr>
<tr><td><code>upgrade&nbsp;[--yes]</code></td><td>检查稳定版本，更新归属明确的安装</td></tr>
</table>

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

## 使用说明

密码仅在身份验证时短暂存在于原生进程内存中，不写入设备配置或日志。
密码被服务器拒绝时，可在“钥匙串访问”中更新对应条目。首次登录失败会保留设备记录，便于重试。
当前支持 SSH 密码登录，不支持仅密钥登录或任意 SSH 参数。

`disconnect` 和 `remove` 支持多个 ID/名称，缺省时打开多选器（空格勾选，Enter 确认）。
同一设备只执行一次；未匹配或操作失败不会中止其他设备的处理。
完成后汇总并高亮统计数字（遵循 `NO_COLOR`）；存在失败或未匹配时返回非零退出码。
活动终端会话继续保留。

## 开发

源码构建需要 Go 1.22+（或当前 macOS 要求的更新版本）与带 Swift 的 Xcode Command Line Tools。

```sh
go test ./...
make build
./build/bin/janus --help
```

本地构建可通过 `./install.sh --local build --prefix "$HOME/.local"` 安装。
目前已在 Apple Silicon 上验证；Intel 和其他支持的 macOS 版本尚未完成实机验收。

## 协议

项目采用 Apache License 2.0，见 [LICENSE](LICENSE)。第三方代码保留原有许可声明。
