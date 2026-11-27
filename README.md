[English](README.md) | [简体中文](README-ZH.md)

<img src="assets/logo.png" alt="Janus Confidential CLI logo" width="64">

# Janus Confidential CLI

Manage SSH hosts, MySQL databases, remote AI Agents and integrated Agent CLI accounts with native authentication.

- **Janus Confidential CLI never stores passwords itself; passwords are stored securely only in macOS Keychain.** Device configuration contains connection details only.
- **Safer Agent CLI accounts.** Enter passwords in Janus native dialogs instead of AI conversations. Keychain storage and authorized sessions significantly reduce the risk of credential leakage.
- **Authenticate once per master.** Later sessions reuse the live SSH master.
- **Stop reuse without breaking work.** `disconnect` preserves established sessions.
- **A2X-compatible Agents.** Connect to any Agent that follows the [A2X protocol specification](https://github.com/leo1394/A2X).

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

New agent devices default to `subsystem`; use `--agent-transport stdio` when needed. Run `janus spawn agent TERMINAL_ID`, or omit the target to select a saved SSH terminal. Other device types are rejected. Its account, host and port are reused; an existing matching agent follows the normal connect flow. Saved devices retain their transport.

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
<tr><td><code>spawn&nbsp;mysql&nbsp;USER@HOST</code></td><td>Register a MySQL target and account; verify TLS and open a SQL session</td></tr>
<tr><td><code>spawn&nbsp;agent&nbsp;USER@HOST</code></td><td>Register and connect to a remote Agent</td></tr>
<tr><td><code>connect&nbsp;[ID|NAME]&nbsp;[--empty]</code></td><td>Connect to SSH, MySQL or an Agent; omit the target to select a device</td></tr>
<tr><td><code>disconnect&nbsp;[ID|NAME&nbsp;...]</code></td><td>Stop master reuse; keep existing sessions; no argument opens the selector</td></tr>
<tr><td><code>bridge&nbsp;[ID|NAME]</code></td><td>Bridge desktop clients to authenticated A2X/Pi sessions using newline-delimited JSON; omit the target to list agent devices</td></tr>
<tr><td><code>remove&nbsp;[ID|NAME&nbsp;...]</code></td><td>Select devices or accounts when omitted; reject ambiguous names and referenced accounts; MySQL device removal retains its account</td></tr>
<tr><td><code>credential&nbsp;--recover&nbsp;ID|NAME</code></td><td>Recover credentials by account or device ID/name via native UI; preserve the old item</td></tr>
<tr><td><code>credential&nbsp;[--json]</code></td><td>List accounts; --edit verifies a replacement password; --remove deletes an unreferenced account</td></tr>
<tr><td><code>session&nbsp;list|close|revoke</code></td><td>List or terminate managed MySQL and CLI account sessions</td></tr>
<tr><td><code>rename&nbsp;ID|NAME&nbsp;NEW_NAME</code></td><td>Change the unique, case-insensitive display name</td></tr>
<tr><td><code>help&nbsp;[command]</code>,&nbsp;<code>--help</code></td><td>Help without application initialization</td></tr>
<tr><td><code>version</code>,&nbsp;<code>--version</code></td><td>Show version and build date</td></tr>
<tr><td><code>upgrade&nbsp;[--yes]</code></td><td>Check stable releases and update an owned installation</td></tr>
</table>

`upgrade` never silently approves non-interactive updates or downgrades. It
respects Homebrew ownership. Standalone upgrades verify release checksums and update the CLI, helpers, manuals and completions together; manually installed `PREFIX/bin/janus` binaries can initialize installer metadata. Source builds need a rebuild.

## Usage notes

- SSH passwords stay in macOS Keychain, never in configuration or logs. Password authentication is currently supported.
- Use `janus credential --recover ID|NAME` to enroll a replacement credential; it is selected only after verification succeeds, and the old item is retained.
- `disconnect` and `remove` support multiple devices and a multi-select picker. Individual failures do not stop other operations; active terminal sessions remain open.
- Agent devices and recovered credential records require 0.1.6 or later.
- Installation includes shell completion and `man janus`. See `janus help` for command details.


## MySQL

```sh
janus spawn mysql reader@db.example.com -p 3306 --database analytics --ca /path/to/ca.pem
janus connect DEVICE
janus credential --json
janus credential --recover ACCOUNT
janus session list
janus session close SESSION_ID
```

Enter passwords only in native UI; they are stored in Keychain. TLS server verification is the default;
without `--ca`, system trust is used. Each SQL session owns a separate connection.
Terminate SQL with `;`, exit with `\q`; results are JSON.
`disconnect` preserves existing transactions but stops new sessions. `session close/revoke` terminates a SQL session.
Removing a MySQL device retains its account; use `credential --remove ACCOUNT` after removing its references.
A verified password replacement revokes the account's existing SQL sessions. Website login and Agent SQL delegation are not yet supported.

## Agent CLI accounts

```sh
janus credential --add clobotics --environment test --username USER --json
clobotics auth login --confidential --resource ACCOUNT_ID
```

Integrated Agent CLIs request native authorization instead of asking for passwords in chat.
Janus keeps passwords in Keychain and tokens within its process; the CLI uses an authorized session.
The Clobotics integration supports identity checks, authorization reuse and session revocation; business API calls require a separate integration.

For integration details, see the [Agent CLI integration guide](docs/cli-integration.md).

## Device profiles

`janus profile [ID|NAME]` shows cached device information without connecting.
Use `--refresh` to collect through an authorized connection, or `--json` for structured output.
Connections collect SSH system/resources, A2X-declared agent capabilities and MySQL database names, table/view directories and comments.
MySQL column and index details are collected on demand with `--database NAME [--table NAME] --refresh`;
business rows and credentials are never included. Models not reported by an agent remain unknown.

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

For MySQL endpoints without TLS, explicitly use `spawn mysql USER@HOST --tls disabled`. Native approval identifies the unencrypted connection; no automatic downgrade occurs.

Account IDs use `janusacc-xxxx-xxxx-xxxx-xxxxxxxxxxxx` (36 characters, with a random hexadecimal suffix). Device and account IDs are checked for uniqueness together. Legacy account IDs remain accepted; existing Keychain mappings are preserved. Use `janus remove ACCOUNT_ID` or `janus credential --remove ACCOUNT_ID` to delete an unreferenced account.
