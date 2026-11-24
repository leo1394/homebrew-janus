# a2x devices and isolated delegate IPC

The service project is **Agenux-cli**, and its executable is always `agenux`.
Janus remains `janus`. Existing fixed remote launchers continue to execute
`agenux stdio` or `agenux-instance NAME stdio`; no `agenux-cli` command is added.

## Device compatibility

New device records use `platform: a2x`. Types include `pi`, `codex`, `opencode`,
`claude-code`, and deployment-named ACP implementations matching
`[a-z][a-z0-9-]{0,31}`. Type names are metadata, never executable paths or shell
commands. The connection adapter dispatches all validated a2x implementations
through the same SSH/A2X/authentication flow.

Reading old `ai/agent` maps to a2x/pi; old `Agenux/TYPE` maps to a2x/TYPE. Reading
alone does not rewrite the registry. Registration reuse or authenticated default
service binding canonicalizes the record without changing its ID, saved session,
SSH target, credential mapping, master socket identity or pinned instance ID.
An instance change or duplicate target/type conflict still fails closed.
Transient named instances never overwrite the default device binding.

## Independent OS identities

Use an owner UID and one or more distinct **non-root** delegate UIDs. The owner
service keeps its original private 0700 directory, owner.sock, SSH master and
owner credentials. A separate, non-nested 0755 directory contains only the
exported `delegated.sock`. Delegates cannot write that directory or replace its
socket. The socket's 0666 mode permits connection attempts, but is **not** its
authorization boundary: the service checks the kernel peer UID before accepting
requests, then applies the grant/method/instance checks.

Linux uses `SO_PEERCRED`; macOS uses `LOCAL_PEERCRED`. The caller also checks the
connected service's kernel UID against `localOwnerUid` in the private grant file
before sending the capability. New grant exports record this UID automatically.
Older files without it remain same-UID only; they do not silently gain cross-UID
access. Transfer only the selected delegation, not the owner's keychain, SSH
agent, master socket, data directory or other private files.

### Owner commands

Run these as the existing owner, with the isolated development registry and
preauthenticated SSH socket already configured as described in the M1 guide:

```sh
janus agent service DEVICE --instance acceptance \
  --directory /absolute/private/owner-service \
  --delegate-directory /absolute/traversable/janus-delegate-ipc \
  --delegate-uid 1002

janus agent grant create --directory /absolute/private/owner-service \
  --actor-id report-delegate --profile pi-report \
  --ttl-seconds 3600 --max-concurrent 1 \
  --credential-out /absolute/private/report-grant.json
```

All ancestors of the shared directory must permit delegate traversal and be
non-writable by untrusted users (root-owned sticky temporary directories are
allowed). Paths must be clean, absolute and without symlinks. Repeat the UID flag
or supply comma-separated UIDs to allow multiple delegates. Do not use a UID that
is root or equal to the owner.

An administrator provisions the delegate's private 0700 directory and installs
**only** the selected grant with that delegate as file owner and mode 0600. For
example, using a pre-existing delegated account on the same kernel:

```sh
sudo install -d -m 700 -o delegate -g delegate /var/lib/janus-delegate
sudo install -m 600 -o delegate -g delegate \
  /absolute/private/report-grant.json /var/lib/janus-delegate/report.json
```

These administrative commands are explicit provisioning, not a way to bypass
Janus approval. The credential value never appears in argv or logs. Existing
credential exports are not overwritten by Janus; use new paths or explicit owner
rotation. Revoke grants through the original owner endpoint.

### Delegate commands

Run as the delegate UID:

```sh
printf '%s\n' '{}' | janus agent delegated \
  --directory /absolute/traversable/janus-delegate-ipc \
  --credential-file /var/lib/janus-delegate/report.json \
  --method agent.status

printf '%s\n' '{"idempotencyKey":"report-001","profile":"pi-report","text":"Prepare a report"}' |
  janus agent delegated --directory /absolute/traversable/janus-delegate-ipc \
    --credential-file /var/lib/janus-delegate/report.json --method tasks.submit
```

The same delegate can run `janus agent a2a` with these directory/file arguments
for its loopback-only standard HTTP/SSE adapter. Delegates cannot invoke grant
management or terminal/session operations by selecting another method.

## Containers and kernel boundaries

Supported topology: an owner service and a non-root delegate container on the
**same Linux kernel**, with compatible UID mappings. Bind-mount only the shared
IPC directory and that delegate's grant file, read-only. Do not mount owner
private directories, SSH sockets, Docker sockets or home directories. Drop all
capabilities, disable privilege escalation and run with the allowlisted numeric
UID. A read-only bind mount still permits connection to an existing Unix socket.

User namespaces that map the owner or delegate UID differently must be accounted
for explicitly; unmapped/mismatched kernel identities fail closed. A privileged
container, root-equivalent process, compromised kernel or full-privilege process
running as the owner is outside this boundary. Same-UID processes remain capable
of accessing owner permissions; a grant file alone is not same-UID isolation.

A macOS Unix socket is **not** forwarded into Docker Desktop/Colima across a VM
boundary by this implementation. The Colima acceptance below runs both owner
and delegate inside Linux. It proves kernel/filesystem isolation with a mocked
already-authenticated remote, not Linux support for the macOS native MFA UI.

## Reproducible OS acceptance

`internal/agentservice/isolation_linux_test.go` starts a real service process as
UID 1001, a delegate as UID 1002 and an unlisted peer as UID 1003. Only the test
coordinator runs as root to assign fixture UIDs. It uses temporary mock secrets
and sockets, never live owner credentials or SSH sessions.

The optional container test builds a scratch image from the static test binary,
runs as UID 1002 with no network/capabilities and mounts only delegated IPC plus
its own grant. It checks:

- UID 1002 succeeds with a valid scoped capability; a wrong grant fails.
- UID 1003 fails even when given the same test capability.
- Owner private credentials, owner.sock and the SSH-master fixture are inaccessible.
- Delegated raw HTTP cannot create grants or enter terminal/session operations.
- Delegates cannot replace the shared socket; a wrong server-UID pin fails.
- Container mounts contain no owner private state; the real Linux `janus agent delegated` command succeeds without exposing the credential.

Build and run inside an explicitly isolated Linux test environment:

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c ./internal/agentservice -o janus-uid-linux.test
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o janus-linux-arm64 ./cmd/janus
sudo env JANUS_UID_OS=1 JANUS_UID_CONTAINER=1 JANUS_UID_CLI="$PWD/janus-linux-arm64" ./janus-uid-linux.test \
  -test.run '^TestUIDIsolationOS$' -test.v
```

The implementation was accepted in the separate Colima profile
`janus-uid-accept`, with host directory mounts, SSH-agent forwarding and default
Docker-context activation disabled. No production service was restarted.

## Authentication before registration

`spawn agent USER@HOST` no longer writes a default Pi record before connecting.
It uses an in-memory pending SSH identity, authenticates A2X/native MFA once,
then validates `agent.info.instanceId` and `type` and atomically saves the real
device. The saved ID uses that same SSH master identity. `--type` is an optional
assertion against authenticated type; omitted type is discovered, not persisted
as a guess. Matching existing targets reuse their saved identity and transport.

Authentication failure, missing identity for a new instance, explicit type
mismatch or changed pinned identity does not create/update a device record.
Failed pending connections stop only their own newly created master reuse;
borrowed masters and existing Keychain credentials are never deleted by cleanup.
Already saved, unbound legacy Pi devices whose server explicitly lacks
`agent.info` can still use the legacy session protocol. New unidentifiable
servers cannot register. Legacy ai/Agenux unbound records can migrate from
verified info while retaining ID/session/SSH target.

An authenticated instance advertising delegated tasks but no native terminal is
successfully registered and shown a machine-task/service command hint. It does
not attempt `session.open`. No second A2X connection or MFA is created inside
the spawn/connect flow.
