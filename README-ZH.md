[English](README.md) | [简体中文](README-ZH.md)

<img src="assets/logo.png" alt="Janus Confidential CLI 标志" width="64">

# Janus Confidential CLI

统一管理 SSH 主机与远程 AI Agent，通过原生身份验证连接终端、恢复 Agent 会话和执行任务。

- **Janus Confidential CLI 不保存任何密码，密码仅安全存放在 macOS 钥匙串中。** 设备配置只包含连接信息。
- **主连接验证一次，后续会话复用。** 已有主连接时不重复读取密码。
- **停止复用，保留工作。** `disconnect` 不打断已建立的终端会话。
- **支持 A2X Agent。** 支持所有遵循 [A2X 协议规范](https://github.com/leo1394/A2X)的 Agent。

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

## AI Agent 连接

远端需运行 Agenux 服务：

```sh
janus spawn agent user@example.com -p 7263
janus connect agent-device-name
janus connect agent-device-name --empty
# 使用系统 SSH 接入时：
janus spawn agent user@example.com -p 22 --agent-transport stdio
```

`connect` 根据设备类型自动选择连接方式；`--empty` 新建 Agent 会话。
支持交互终端的实例可直接进入 Agent 界面，任务型实例通过本机连接服务调用。
Agent 设备显示为 `PLATFORM=a2x`，类型由远端服务确认。

服务端要求双因素验证时，在原生窗口输入 6 位验证码，验证码不保存。
默认不共享本机 MCP 工具，仅通过 `--share-mcp` 显式允许的工具可用。
机器调用可使用 `janus agent info|status|send`，结果以 JSON 输出。
具体能力取决于远端 Agent 服务。

## 命令

<table>
<tr><th width="320">命令</th><th>用途</th></tr>
<tr><td><code>devices&nbsp;[--list|--json]</code></td><td>列出设备；默认表格，执行有时限的网络探测</td></tr>
<tr><td><code>spawn&nbsp;USER@HOST&nbsp;[-p&nbsp;PORT]</code></td><td>登记新设备，或连接已有设备</td></tr>
<tr><td><code>spawn&nbsp;agent&nbsp;USER@HOST</code></td><td>登记并连接远程 Agent</td></tr>
<tr><td><code>connect&nbsp;[ID|NAME]&nbsp;[--empty]</code></td><td>连接 SSH 主机或 Agent；缺省进入设备选择器</td></tr>
<tr><td><code>disconnect&nbsp;[ID|NAME&nbsp;...]</code></td><td>停止主连接复用，保留活动 session；缺省进入选择器</td></tr>
<tr><td><code>remove&nbsp;[ID|NAME&nbsp;...]</code></td><td>缺省时选择设备；删除设备记录及对应钥匙串凭据，保留活动连接</td></tr>
<tr><td><code>recover-credential&nbsp;ID|NAME</code></td><td>通过原生窗口重新登记独立 SSH 凭据，仅在 SSH 验证成功后切换，保留旧条目</td></tr>
<tr><td><code>rename&nbsp;ID|NAME&nbsp;NEW_NAME</code></td><td>修改名称；名称忽略大小写判重，必须唯一</td></tr>
<tr><td><code>help&nbsp;[command]</code>、<code>--help</code></td><td>无需初始化设备即可查看帮助</td></tr>
<tr><td><code>version</code>、<code>--version</code></td><td>显示版本号与编译日期</td></tr>
<tr><td><code>upgrade&nbsp;[--yes]</code></td><td>检查稳定版本，更新归属明确的安装</td></tr>
</table>

`upgrade` 不会在非交互环境静默批准更新，也不会隐式降级；Homebrew 安装继续由对应
Formula 管理，源码构建需要重新构建。开发版本不会被误认为稳定版。

## 使用说明

- SSH 密码仅保存在 macOS 钥匙串，不写入配置或日志；当前支持密码登录。
- 凭据失效时，使用 `janus recover-credential ID|NAME` 重新登记，验证成功后切换并保留旧条目。
- `disconnect` 和 `remove` 支持多个设备及多选器；单项失败不影响其他设备，活动终端会话保留。
- Agent 设备与恢复后的凭据记录需要 0.1.6 或更新版本。
- 安装包含 Shell 补全和 `man janus`；更多命令用法见 `janus help`。

连接服务、任务委派与集成说明：[连接服务](docs/agenux-m1.md) · [A2A 接口](docs/agenux-m23.md) · [权限隔离](docs/a2x-delegate-isolation.md)。

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
