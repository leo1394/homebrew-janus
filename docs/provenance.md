# Extraction record

Source inspected: `~/TARS/apps/janus-confidential` (local source snapshot, 2026-11-19).
No source files in that tree were modified and no Confidential product was compiled.
The source directory has no Git metadata; no source revision is invented.

| Source | Destination / treatment |
| --- | --- |
| `cli/internal/selector/*` | Copied to `internal/selector`; only the terminal import path changed. Includes existing tests and MIT notice. |
| `cli/internal/terminal/style.go` | Copied unchanged with its MIT notice. |
| `scripts/janus-confidential.swift`, `checked` through `authorizedSecret` | Extracted verbatim into `native/KeychainPolicy.swift` with imports. Helper-only ACL identity and authorization gate preserved. |
| `scripts/janus-confidential-ssh-registry.swift`, `verifiedRawTarget` | Host/user/port identity, service key, UUID namespace, default name and 7200-second persistence adapted into the independent store. Unique names and live removal added. |
| `scripts/janus-confidential-ssh-runtime.swift`, owned-master/session-attachment model | Adapted to the system OpenSSH mux backend. Runtime/XPC/installation authority classes were not copied: they require the Confidential product. |
| `cli/cmd/connect.go`, `spawn.go`, `terminal.go` | Command behavior reduced to this phase's SSH contract; selector reused; no arbitrary SSH argv or Confidential commands. |

This is a selective extraction plus replacement of the Confidential-bound host,
not a byte-for-byte relocation of its Core or native transport. The new Go command,
registry, mux adapter, installer and upgrade ownership checks are independent code.
It does not reuse the Confidential private socket/proof protocol or claim its
Agent security boundary. A running Confidential-owned master is not automatically
adopted, nor are saved records silently imported. Existing credentials with an
incompatible helper ACL fail closed, without overwriting that item.
