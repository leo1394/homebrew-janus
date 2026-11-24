# M2/M3: local A2A 1.0 adapter and codex devices

Janus Confidential CLI 0.1.6 exposes an existing M1 grant through **loopback-only**
JSON-RPC HTTP and SSE. It does not expose a remote HTTP server, create another
SSH connection, mint a grant, or add another authentication credential.

Every HTTP request needs `Authorization: Bearer <grant credential>`. The adapter
is bound to a single private grant file and its `grantId`/`instanceId`; each call
still goes through M1 `delegated.sock` and remote grant authorization. Revocation
and expiry therefore also stop HTTP access. The local bearer check is constant
time. Same-UID full-privilege processes remain outside the isolation guarantee.

## Start an isolated instance

First follow [M1 setup](agenux-m1.md): keep the local connection service running
against a named non-production instance and export an explicitly scoped grant.
Do not restart production. Then start a separate adapter process:

```sh
/absolute/path/to/homebrew-janus/build/bin/janus agent a2a \
  --directory /private/tmp/janus-m1-service \
  --credential-file /private/tmp/janus-m1-credentials/report.json \
  --listen 127.0.0.1:8765
```

Port `0` (the default) selects an available port. The readiness JSON contains
`url`, `agentCard`, and `instanceId`, never the credential. Only numeric
`127.0.0.1` or `::1` binds are allowed. Requests with a mismatched Host or foreign
Origin are rejected. No CORS permissions are granted.

Read the credential inside a client process, not through shell interpolation or
an argument containing the token. Example standard JSON-RPC call:

```sh
python3 - <<'PY'
import json, urllib.request
with open('/private/tmp/janus-m1-credentials/report.json') as f:
    grant = json.load(f)
headers = {
    'Authorization': 'Bearer ' + grant['credential'],
    'Content-Type': 'application/json',
    'A2A-Version': '1.0',
}
request = urllib.request.Request(
    'http://127.0.0.1:8765/rpc',
    data=json.dumps({
        'jsonrpc': '2.0', 'id': 'acceptance-1', 'method': 'SendMessage',
        'params': {'message': {
            'messageId': 'acceptance-message-1', 'role': 'ROLE_USER',
            'parts': [{'text': 'Produce a short report'}],
        }},
    }).encode(), headers=headers,
)
with urllib.request.urlopen(request) as response:
    print(response.read().decode())
PY
```

Default blocking `SendMessage` submits the same message once with
`configuration.returnImmediately: true`, then issues short delegated `GetTask`
requests until completed, failed, canceled, rejected, input-required or
auth-required. The wait stays in the local HTTP adapter, outside the M1 owner
connection's per-request deadlines/slots. `tenant`, all submission metadata and
configuration, and the optional `historyLength` (including zero) are preserved.
Explicit `returnImmediately: true` still returns the initial response without
polling. Cancellation stops polling without resubmitting or canceling the remote
task; grant expiry/revocation and remote errors are propagated with safe codes.

For streaming, change the method to `SendStreamingMessage`, add
`Accept: text/event-stream`, and read the response line by line. To resume a task
stream use `SubscribeToTask` with `params: {"id":"TASK_ID"}`. The adapter reads
the task snapshot/cursor, polls `a2x.events`, and emits standard JSON-RPC SSE
`data:` objects containing one `task`, `message`, `statusUpdate` or
`artifactUpdate`. Internal cursor/sequence fields are not exposed as A2A result
fields. It stops on terminal state, revocation, transport errors or client
cancellation; it never automatically resubmits tasks or cancels a task merely
because the HTTP stream disconnected.

`POST /rpc`, `/rpc/`, and `/` share the same JSON-RPC handler.

`GET /.well-known/agent-card.json` also requires the same bearer. The returned
card advertises only the local `/rpc` JSONRPC interface and local grant bearer
scheme. Upstream card signatures are removed because rewriting invalidates them.
`GetExtendedAgentCard` receives the same interface/security rewrite.
The GET card uses `Cache-Control: private, max-age=0, must-revalidate` and varies
on `Authorization`. Its ETag hashes the final rewritten representation. Every
conditional request rechecks the remote grant before a matching ETag can return
304; revoked grants and failed checks remain non-cacheable. Last-Modified records when this adapter first generated or changed the rewritten
representation. It remains stable for unchanged content and does not claim to be
the upstream file modification time.

## Methods and errors

Implemented JSON-RPC method routing:

- `SendMessage`, `SendStreamingMessage`
- `GetTask`, `ListTasks`, `CancelTask`, `SubscribeToTask`
- `CreateTaskPushNotificationConfig`, `GetTaskPushNotificationConfig`,
  `ListTaskPushNotificationConfigs`, `DeleteTaskPushNotificationConfig`
- `GetExtendedAgentCard`

Server capabilities and the fixed grant determine which calls are actually
permitted. For example, an unsupported push configuration returns the server's
safe A2A error code; local routing does not imply a push delivery implementation.
Terminal, session management, grant creation and arbitrary method calls cannot
be invoked through this HTTP interface. Request bodies are bounded; active
requests and streaming calls are bounded to eight per adapter.

Malformed JSON, invalid request envelopes, methods and parameters map to their
standard JSON-RPC codes. Numeric A2A codes are retained, including remote errors
whose message begins `A2A <code> ...`; remote exception text/data is never echoed. Recognized A2A codes include a locally
constructed `google.rpc.ErrorInfo` entry in `error.data`, using domain
`a2a-protocol.org` and the corresponding standard reason.
Unsupported A2A versions or extension negotiation fail explicitly. This adapter
implements the JSON-RPC/SSE binding, not a separate REST or gRPC binding, and
requires request IDs rather than accepting notifications or batch requests.

References: [A2A 1.0 specification](https://a2a-protocol.org/v1.0.0/specification/)
and the official tagged protobuf contract used by the server integration.

## M3 device compatibility

`platform: a2x` supports `pi`, `codex`, `opencode`, `claude-code` and safe deployment-named ACP types. Legacy `Agenux/TYPE` keeps its type; `ai/agent`
continues to normalize to Pi. The authenticated `agent.info.type` can update the
default connection-service device record; type is never inferred from hostname.
Device ID, SSH target and master/credential identity stay unchanged, and a
changed `instanceId` still fails closed. A conflicting existing device causes
migration to fail rather than creating duplicate identity records.

`spawn agent --type NAME` can explicitly select an initial implementation.
Subsequent authenticated `agent.info.type` is authoritative when binding the
default service. Transient `service --instance NAME` remains isolated and never
updates the default device's type, instance or session. Grant scopes/profile
requirements are unchanged: codex support does not grant broader local tools or
owner privileges.

## Acceptance coverage

Local tests cover all method routes, safe error codes, authenticated cards,
SSE cursor polling and terminal termination, rejection of replayed/foreign events,
client cancellation, malicious Host/Origin, missing/wrong bearer, instance pins,
codex adapter dispatch and registry migration. The real local end-to-end fixture
runs CLI HTTP → private Unix delegated socket → simulated A2X/MFA, including
grant revocation while the adapter remains running. Live Runner interoperability
must be checked against the isolated named instance; these tests do not claim
production or native MFA UI acceptance.

The opt-in long-task regression crosses the former 30/35/40-second limits with
a 41-second task over real local HTTP and Unix sockets, while an owner query
must still finish within two seconds:

```sh
JANUS_LONG_TASK_TEST=1 go test -race ./pkg/cli -run '^TestAgentServiceLocalEndToEnd$' -count=1
```

For a completed blocking SendMessage task, the trusted service may declare
`task.metadata["a2x.replyMode"] = "message"`. The adapter then returns the
validated full `ROLE_AGENT` reply from `task.status.message` as `{message: ...}`.
The reply must contain messageId, contextId and parts. Explicit immediate mode
and streams that have already emitted a Task keep their Task semantics.
