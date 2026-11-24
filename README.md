[English](README.md) | [简体中文](README-ZH.md)

<img src="assets/logo.png" alt="Janus Confidential CLI logo" width="64">

# Janus Confidential CLI

Manage SSH hosts and remote AI Agents from one CLI. Connect to terminals, resume Agent sessions, and run tasks with native authentication.

- **Janus Confidential CLI never stores passwords itself; passwords are stored securely only in macOS Keychain.** Device configuration contains connection details only.
- **Authenticate once per master.** Later sessions reuse the live SSH master.
- **Stop reuse without breaking work.** `disconnect` preserves established sessions.
- **Agent support in 0.1.6.** Connect to Pi, Codex, OpenCode and other adapters exposed by an Agenux server.

## Requirements

macOS 14+ (Apple Silicon or Intel), system OpenSSH, and a graphical login session for password and authentication dialogs.

## Installation

Install with Homebrew; no local compilation is needed:

```sh
brew tap leo1394/janus
brew install leo1394/janus/janus
```

Alternatively, use the standalone installer without Homebrew. From this checkout:

```sh
./install.sh --prefix "$HOME/.local"
export PATH="$HOME/.local/bin:$PATH"
export MANPATH="$HOME/.local/share/man:${MANPATH:-}"
```

The installer downloads and verifies the matching package. Use `--version X.Y.Z` to select a version.

## First connection

You need an SSH account that accepts password authentication and a reachable host:

```sh
janus spawn builder@example.com -p 3233
# Confirm the host key and enter the password in native UI.
# Exit the remote shell to return here; the master stays reusable.
janus devices
janus rename builder@example.com 'Build Server'
janus connect 'Build Server'
```

The table contains `ID NAME PLATFORM TYPE CONNECTED READY`. SSH rows have
`PLATFORM=terminal`, `TYPE=ssh`; `terminal` is not a command or alias. A live,
reusable master shows green `✔︎` in both status columns. Confirmed failure is red
`✘`, unknown is `-`; JSON uses booleans/null and no colour.

## Agent connections

The remote host must run an Agenux server:

```sh
janus spawn agent user@example.com -p 7263
janus connect agent-device-name
janus connect agent-device-name --empty
# To connect through the system SSH server:
janus spawn agent user@example.com -p 22 --agent-transport stdio
```

`connect` selects the connection adapter from the device type; `--empty` starts a
new Agent session. Interactive instances open the Agent's terminal interface;
task-only instances use the local connection service. Agent devices show
`PLATFORM=a2x`, with their type verified by the remote server.

When two-factor authentication is required, enter the six-digit code in the native
dialog; codes are never saved. Local MCP tools are not shared unless explicitly
allowed with `--share-mcp`. Machine calls use `janus agent info|status|send` and
return JSON. Available capabilities depend on the remote Agent server.

## Commands

<table>
<tr><th width="320">Command</th><th>Purpose</th></tr>
<tr><td><code>devices&nbsp;[--list|--json]</code></td><td>List saved devices; default table, bounded network probes</td></tr>
<tr><td><code>spawn&nbsp;USER@HOST&nbsp;[-p&nbsp;PORT]</code></td><td>Register a new device or connect to an existing one</td></tr>
<tr><td><code>spawn&nbsp;agent&nbsp;USER@HOST</code></td><td>Register and connect to a remote Agent</td></tr>
<tr><td><code>connect&nbsp;[ID|NAME]&nbsp;[--empty]</code></td><td>Connect to an SSH host or Agent; omit the target to select a device</td></tr>
<tr><td><code>disconnect&nbsp;[ID|NAME&nbsp;...]</code></td><td>Stop master reuse; keep existing sessions; no argument opens the selector</td></tr>
<tr><td><code>remove&nbsp;[ID|NAME&nbsp;...]</code></td><td>Select a device when omitted; delete metadata and the exact Keychain credential; leave connections running</td></tr>
<tr><td><code>recover-credential&nbsp;ID|NAME</code></td><td>Enroll a separate SSH credential via native UI; switch only after SSH verification succeeds; preserve the old item</td></tr>
<tr><td><code>rename&nbsp;ID|NAME&nbsp;NEW_NAME</code></td><td>Change the unique, case-insensitive display name</td></tr>
<tr><td><code>help&nbsp;[command]</code>,&nbsp;<code>--help</code></td><td>Help without application initialization</td></tr>
<tr><td><code>version</code>,&nbsp;<code>--version</code></td><td>Show version and build date</td></tr>
<tr><td><code>upgrade&nbsp;[--yes]</code></td><td>Check stable releases and update an owned installation</td></tr>
</table>

`upgrade` never silently approves non-interactive updates or downgrades. It
respects Homebrew ownership; source builds need a rebuild. Development builds
are explicitly reported rather than treated as stable releases.

## Usage notes

- SSH passwords stay in macOS Keychain, never in configuration or logs. Password authentication is currently supported.
- Use `janus recover-credential ID|NAME` to enroll a replacement credential; it is selected only after verification succeeds, and the old item is retained.
- `disconnect` and `remove` support multiple devices and a multi-select picker. Individual failures do not stop other operations; active terminal sessions remain open.
- Agent devices and recovered credential records require 0.1.6 or later.
- Installation includes shell completion and `man janus`. See `janus help` for command details.

For services, delegated tasks and integrations: [connection service](docs/agenux-m1.md) · [A2A interface](docs/agenux-m23.md) · [permission isolation](docs/a2x-delegate-isolation.md).

## Development

Source builds require Go 1.22+ (or a newer version required by your macOS) and Xcode Command Line Tools with Swift.

```sh
go test ./...
make build
./build/bin/janus --help
```

Install a local build with `./install.sh --local build --prefix "$HOME/.local"`.
Runtime validation has been performed on Apple Silicon; Intel and other supported macOS versions have not received hardware acceptance testing.

## License

Apache License 2.0. See [LICENSE](LICENSE). Third-party notices retain their original licenses.
