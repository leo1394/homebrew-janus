# Agenux M1 local connection service

Janus Confidential CLI 0.1.6 keeps one owner-authenticated A2X connection alive over the
existing Janus SSH master. SSH host verification, Keychain authorization and
native Agenux MFA use the interactive connection implementation. The service
runs in the foreground; an owner can supervise it externally. There is no
automatic background installation, remote restart or silent reauthentication.

## Identity and isolation

New agent devices use `platform: a2x`, with the type verified by the authenticated remote service. Legacy `ai/agent` and `Agenux/TYPE` records
remain readable and resolve to the same adapter and device identity. On default
service startup, authenticated `agent.info.instanceId` is bound atomically to
the record, preserving its ID, SSH target, session and credential mapping.
An existing different instance ID is rejected for owner review.

`--instance NAME` selects a transient named deployment, **only with stdio**.
Names must match `[a-z][a-z0-9-]{0,31}`. SSH executes only the fixed launcher
`exec "$HOME/.local/bin/agenux-instance" NAME stdio`. The remote launcher must
already be installed by the deployment owner. No default device metadata or
session is written in this mode. The deployment name is not `instanceId`.

Use `JANUS_DEV_DATA_DIR` with the existing isolated development device registry.
`JANUS_DEV_CONTROL_SOCKET` may reuse an existing authenticated SSH master under
that development scope; no credentials are copied. Do not point development
builds at the stable registry. No migration command below needs the live Runner
service to be restarted.

## Cross-machine acceptance

Set these to the existing isolated registry, authenticated SSH socket and saved
stdio Runner device. The Janus service runs locally; the named Agenux instance
runs on Runner. Paths in the example are placeholders, not production defaults.

```sh
export JANUS_DEV_DATA_DIR=/absolute/path/to/isolated-registry
export JANUS_DEV_CONTROL_SOCKET=/absolute/path/to/authenticated-master.sock
JANUS=/absolute/path/to/homebrew-janus/build/bin/janus

# Terminal 1: authenticate once and keep this process alive.
"$JANUS" agent service DEVICE --instance m1-test --directory /private/tmp/janus-m1-service
```

After the service prints `{"ready":true,...}`, use another local terminal:

```sh
JANUS=/absolute/path/to/homebrew-janus/build/bin/janus
mkdir -m 700 /private/tmp/janus-m1-credentials
"$JANUS" agent grant create --directory /private/tmp/janus-m1-service \
  --actor-id report-agent --ttl-seconds 3600 --max-concurrent 1 \
  --credential-out /private/tmp/janus-m1-credentials/report.json

printf '%s\n' '{"idempotencyKey":"acceptance-001","text":"Produce a short environment report","profile":"pi-report"}' |
  "$JANUS" agent delegated --directory /private/tmp/janus-m1-service \
  --credential-file /private/tmp/janus-m1-credentials/report.json --method tasks.submit

printf '%s\n' '{"taskId":"TASK_ID_FROM_SUBMIT"}' |
  "$JANUS" agent delegated --directory /private/tmp/janus-m1-service \
  --credential-file /private/tmp/janus-m1-credentials/report.json --method tasks.get

printf '%s\n' '{}' |
  "$JANUS" agent delegated --directory /private/tmp/janus-m1-service \
  --credential-file /private/tmp/janus-m1-credentials/report.json --method tasks.list

"$JANUS" agent tasks list --directory /private/tmp/janus-m1-service
"$JANUS" agent tasks get TASK_ID --directory /private/tmp/janus-m1-service
"$JANUS" agent grant revoke GRANT_ID --directory /private/tmp/janus-m1-service
```

Retest with the same idempotency key: the remote service should return the same
task. Retest after revoke or expiry: the request must fail. `agent.status` also
accepts `{}` on the delegated endpoint. `terminal.open`, other terminal methods,
`message/send`, `session.*` and grant management must fail on that endpoint.
The CLI passes through task states (`queued`, `working`, `completed`, `failed`,
`canceled`), artifacts and usage without inventing local task state.

## Permission boundary

The service directory is 0700; `owner.sock` and `delegated.sock` are 0600. The
owner endpoint permits grant create/revoke, tasks get/list and agent status.
The delegated endpoint always wraps calls as `delegated.call` with grant ID and
credential, and permits only tasks submit/get/list and agent status. Submission
defaults to profile `pi-report`. `grant create --profile NAME` and task profile
accept only `[a-z][a-z0-9-]{0,31}` identifiers for administrator-registered profiles;
commands, paths, arguments and environment overrides are not accepted. The remote
service enforces registered profiles, revocation,
expiry, task ownership, idempotency and grant concurrency. Local checks do not
replace remote enforcement.

Credentials are delivered exclusively to a new 0600 file in a private directory,
never printed as JSON results or passed as command arguments/environment. File
paths may appear in arguments; credential values must not. Symlink/public
credential files and overwriting existing exports are rejected. If export fails,
the CLI attempts to revoke the just-created grant. Credentials remain sensitive
capabilities, not SSH passwords, and should be deleted by the owner when retired.

**These permissions do not isolate a malicious full-privilege process running
under the same UID.** Such a process can read owner-accessible files and contact
the owner socket. Use an OS-enforced sandbox or separate OS identities for that
threat model; handing an agent only a grant file is an application-level boundary.

Transport EOF stops the local service. Restarting it requires the owner connection
and MFA flow again. It does not revoke grants implicitly: previously issued grants
remain subject to the remote instance's expiry/revocation state. Local clients
reject credentials bound to a different instance. No task retries or reconnections
occur silently; use explicit idempotency keys when retrying a submission.

## Local regression evidence

`TestAgentServiceLocalEndToEnd` uses an independent private registry, real Unix
sockets and a simulated bidirectional A2X peer, including native MFA through the
injected runtime seam. It covers default migration, transient named instances,
one retained connection/MFA, task submission/query/idempotency, owner queries,
revocation, method rejection, instance mismatch, secret-free JSON and transport
EOF cleanup. It does not claim real Runner/Apple MFA UI acceptance.

```sh
go test -race ./...
```

### Register a named deployment while starting its owner service

```sh
JANUS_DEV_DATA_DIR=/private/tmp/janus-accept build/bin/janus agent service DEVICE \
  --instance opencode-test --register-instance --directory /private/tmp/janus-owner
```

The explicit opt-in registers a separate `a2x` device with the authenticated
`agent.info.type`, `instanceId`, and persistent `instanceName`. The readiness JSON
includes `registeredDeviceId`. Registration and the owner service use the same
A2X connection and MFA approval. The source/default device and its sessions stay
unchanged. Repeating registration reuses the named device only if its pinned
identity still matches. Without `--register-instance`, `--instance` remains transient.

Use the returned device ID with ordinary `connect`, `agent service`, or machine
commands; its saved name selects the fixed `agenux-instance NAME stdio` launcher.
New later connections may require MFA; registration does not cache an MFA bypass.
No remote command or path can be supplied through the instance name.

Native terminal input reporting uses DECRQM to discover the host's original
mouse, focus and bracketed-paste modes, then restores explicit values on exit.
Terminals such as macOS Terminal.app can ignore private mode save/restore and
DECRQM while accepting mode changes. When no baseline is available, Janus blocks
remote changes to those input-report modes instead of guessing the host state.
Normal keyboard input remains available; those optional modes are unavailable
on such terminals. This also applies to abnormal A2X EOF and cancellation.
