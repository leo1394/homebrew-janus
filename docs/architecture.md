# Runtime and extension contract

The base project owns SSH profiles and the OpenSSH master. `janus devices` is the
only device-list command; `terminal` is a platform value, never a command alias.

- Profile identity: `terminal:UUID` internally, bare UUID in the table/JSON.
- Transport identity: case-insensitive host, exact user, numeric port.
- Names: case-insensitive uniqueness; initial `user@host`, then `:port` and a
  deterministic numeric suffix if required.
- Storage: private `~/.janus/base/devices.json`, atomic rename, registry flock.
  Per-device flock serializes ensure/disconnect/remove. Passwords never enter Go.
- Credential key: existing `app.janus.ssh.password.v1:host:port` service and user.
- Master: system OpenSSH, independent of CLI process lifetime; persistence 7200s.
- New session: same socket, `ControlMaster=no`, `BatchMode=yes`, failing proxy.
  Missing masters fail closed and do not start a password fallback.
- Disconnect: `ssh -O stop`, stops admission and unlinks the listening socket;
  established SSH channels finish normally. A later connect starts a new master.
- Remove: delete the exact credential, then the record; never signal the master.
  Credential deletion failure retains the record. A subsequent file-write failure
  reports that credential deletion already succeeded and invites retry.
- Status: CONNECTED means a reusable listener; draining sessions alone do not
  imply CONNECTED. READY is true for a reusable master, otherwise a bounded TCP
  probe (not proof of authentication). Unknown is JSON null / `-`.

## Future Confidential integration

`pkg/janus` supplies runtime and device-provider contracts. The CLI composition
root accepts extra device providers and explicit command registrations. No plugin
executables are discovered from PATH and no untrusted extension is auto-loaded.
The public entry point is `pkg/cli.New` for a future CLI composition.

Before Workbench/Confidential adoption, implement an adapter which forwards their
profile/runtime/session requests to this one owner, or transfers ownership once.
Do not run both master pools for the same logical device. Existing Workbench
currently calls Confidential Core and is **not automatically compatible** with
this standalone backend. Integration and live migration are out of this phase.
A provider must map to stable device IDs and preserve the base status semantics.

Agent permission is independent of SSH authentication: a future Confidential
adapter validates a native lease before using any runtime operation. Connected
status is never evidence of a lease. The existing `--controlled` independent
lease path is not implemented or silently mapped to an ordinary mux session.

## Native helper and trust limits

Only `/usr/bin/ssh` children with a pipe stdout may read a password. Every saved
password read requires fresh LocalAuthentication and an exact helper-only ACL;
only an already-authenticated master bypasses another read. The helper never
prints credentials to a terminal. Deletion never reads a secret. Passwords are
transiently held in native memory and written to the SSH askpass pipe only.
New passwords are stored before SSH confirms acceptance; a rejected saved
password requires editing/removing that exact item in Keychain Access.

This standalone same-user mux backend does not offer Confidential's native
process proof / Agent admission controls. Control sockets are private to the
local user, so processes under that user can use them; Agent isolation requires
the future Confidential authority adapter. macOS legacy Keychain APIs are retained
for the existing service/ACL model and emit deprecation warnings on current SDKs.
Ad-hoc helper identities change on updates; an incompatible saved ACL fails closed.
Production release signing and a native authenticated ACL-migration workflow must
be settled before seamless upgrade of existing credentials can be claimed.

Base Janus stores new passwords under `app.janus.cli.ssh.password.v1:<host>:<port>`
with the SSH username as account. Reads prefer that namespace, then consult legacy
`app.janus.ssh.password.v1:<host>:<port>` entries. An incompatible legacy partition
is not mutated: a native confirmation offers separate enrollment with a newly
entered SSH password. Selected legacy ACL/read failures offer the same recovery;
owner cancellation and an existing base credential never trigger it. Recovery
preserves the legacy entry. Explicit `remove` deletes both exact namespace/account
entries without reading their passwords.

For normal CLI sessions, Janus replaces its process with `/usr/bin/ssh` using
exec and retains the original stdin/stdout/stderr descriptors. This applies to
both connect and spawn. Session input, terminal modes, signals and exit status
belong to OpenSSH; Janus does not monitor or log the session after handoff.
JANUS_TRACE covers setup through handoff, not mux acceptance or session exit.
Embedded runtimes with custom streams retain the subprocess path without SSH
session trace collection or the post-session exit milestone.
