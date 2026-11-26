[English](cli-integration.md) | [简体中文](cli-integration-ZH.md)

# Agent CLI 接入 Janus

本文对应 Janus 0.1.8。Janus 负责原生授权、钥匙串凭据读取、服务端登录和 token 持有；CLI 通过受限会话请求操作，不获取密码或 token。当前内置 provider 为 `clobotics`，其他 CLI 需要实现并注册对应 provider，不能仅通过配置 URL 自动接入。

## 1. 登记账户

```sh
janus credential --add clobotics --environment test --username USER --json
```

用户只在 Janus 原生窗口输入密码。成功响应的 `resourceID` 是后续会话入口；也可以从 `janus credential --json` 读取账户 `id`。ID 是不透明标识，当前格式为 `janusacc-xxxx-xxxx-xxxx-xxxxxxxxxxxx`，不要自行生成或从用户名推导。已有 CLI 应重新发现当前 ID，不要缓存旧 ID 格式。

已接入 Clobotics 的用户入口：

```sh
clobotics auth login --confidential --resource ACCOUNT_ID --idle-timeout 15m
```

需要使用包含 Janus 桥接功能的 Clobotics 版本。密码不得出现在聊天、命令参数、环境变量、日志或 CLI 配置中。

## 2. CLI 启动授权进程

使用进程 API 启动可信安装路径下的 Janus，不拼接 shell 命令：

```text
janus session serve --idle-timeout 15m
```

保持该子进程及 stdin/stdout 管道存活。stdin/stdout 使用 UTF-8、每行一个 JSON 对象；stdout 仅用于协议，stderr 与业务结果分开处理。按顺序发送请求并等待同一 `requestID` 的响应。输入单帧上限约 64 KiB，不发送密码、token 或额外参数。

这不是 A2X，也不是 JSON-RPC 2.0；字段大小写必须与下列示例一致。`janus bridge` 面向远端 Agent 桌面会话，不是 CLI 账户授权入口。

## 3. 申请、使用与关闭

申请授权：

```json
{"version":1,"requestID":"1","method":"session.request","parameters":{"resourceID":"ACCOUNT_ID"}}
```

Janus 弹出原生系统验证，成功后返回：

```json
{"version":1,"requestID":"1","response":{"sessionID":"SESSION_ID","state":"active"}}
```

在同一管道请求身份信息：

```json
{"version":1,"requestID":"2","method":"session.execute","parameters":{"sessionID":"SESSION_ID","operation":"inspect","subset":"identity"}}
```

结果位于 `response.result`，包含 `authenticated`、`environment`、`system` 和经过筛选的 `principal`（`id`、`tenantID`、`username`、`displayName`），不包含 token。

列出当前管道的会话：

```json
{"version":1,"requestID":"3","method":"session.list","parameters":{}}
```

显式退出：

```json
{"version":1,"requestID":"4","method":"session.revoke","parameters":{"sessionID":"SESSION_ID"}}
```

成功返回 `response.revoked=true`，随后进程退出。CLI 应等待响应再关闭管道；不要用一次性管道批量写入后立即 EOF，EOF 会取消授权。每个进程只承载一次账户授权，不能用另一个进程或复制 session ID 复用它。

用户也可执行 `janus session list`、`janus session close SESSION_ID` 或 `janus session revoke SESSION_ID` 管理会话。

## 4. 生命周期与错误处理

- 闲置期限默认 15 分钟，允许 `5m`～`1h`；成功操作重置计时，列表或状态轮询不重置。
- EOF、显式撤销、账户删除/替换、闲置到期会结束授权。远端 token 到期需要重新授权，不自动刷新。
- 处理 `response.error`：`approval_denied`（原生授权未完成）、`resource_unavailable`（资源不可用）、`unsupported_operation`（操作或参数不支持）。登录或运行失败也可能直接导致 EOF/进程退出，不能只检查 JSON 错误。
- 用户取消时停止当前流程；到期后，在用户再次需要操作时重新启动授权，不能循环弹窗或静默绕过 Janus。
- 本地关闭确认不等于远端 logout 成功；远端 logout 为尽力执行。
- 会话 ID 不是 bearer token。同 UID 的完全权限进程不属于强隔离保证，不能把此机制描述为可阻止所有本机恶意进程。

## 5. 接入其他 CLI/provider

当前入口只允许 Clobotics 身份检查，不能透传任意 URL、HTTP header 或业务请求。新增 provider 时需要：

1. 在 `internal/store/accounts.go` 登记 provider 元数据及约束；业务系统名称留在 provider 内，不新增通用 Janus 命令关键词。
2. 在 `internal/cliprovider/` 实现登录、身份校验、token 生命周期和退出；token 留在 Janus，响应仅返回允许字段。
3. 在 `pkg/cli/credentials.go` 和原生 helper 中接入账户登记、恢复及对应名称/图标；遵循现有签名、父进程校验和钥匙串策略。
4. 为 `session.execute` 明确逐项操作白名单、参数与结果结构；业务 API 必须显式实现，不能退化为“返回 token 给 CLI”。
5. 覆盖首次登记/取消、错误密码回滚、连接复用、撤销、EOF、闲置到期、成功操作重置计时及凭据删除后的失效测试。真实资源验收由用户在原生 UI 完成授权。

核心实现：`pkg/cli/mysql.go` 注册 `session serve`，`internal/cliprovider/session.go` 定义 stdio 协议，`internal/cliprovider/client.go` 实现当前 provider。

