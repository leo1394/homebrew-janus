[English](README.md) | [简体中文](README-ZH.md)

<img src="assets/logo.png" alt="Janus logo" width="64">

# Janus

Manage SSH devices, keep passwords in macOS Keychain, and open independent terminal sessions through one authenticated connection.

- **Janus never stores passwords itself; passwords are stored securely only in macOS Keychain.** Device configuration contains connection details only.
- **Authenticate once per master.** Later sessions reuse the live SSH master.
- **Stop reuse without breaking work.** `disconnect` preserves established sessions.

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

## Commands

<table>
<tr><th width="340">Command</th><th>Purpose</th></tr>
<tr><td><code>devices&nbsp;[--list|--json]</code></td><td>List saved devices; default table, bounded network probes</td></tr>
<tr><td><code>spawn&nbsp;USER@HOST&nbsp;[-p&nbsp;PORT]</code></td><td>Register a new device or connect to an existing one</td></tr>
<tr><td><code>connect&nbsp;[ID|NAME]</code></td><td>Open a session; no argument opens the selector</td></tr>
<tr><td><code>disconnect&nbsp;[ID|NAME&nbsp;...]</code></td><td>Stop master reuse; keep existing sessions; no argument opens the selector</td></tr>
<tr><td><code>remove&nbsp;[ID|NAME&nbsp;...]</code></td><td>Select a device when omitted; delete metadata and the exact Keychain credential; leave connections running</td></tr>
<tr><td><code>rename&nbsp;ID|NAME&nbsp;NEW_NAME</code></td><td>Change the unique, case-insensitive display name</td></tr>
<tr><td><code>help&nbsp;[command]</code>,&nbsp;<code>--help</code></td><td>Help without application initialization</td></tr>
<tr><td><code>version</code>,&nbsp;<code>--version</code></td><td>Exact two-line build metadata</td></tr>
<tr><td><code>upgrade&nbsp;[--yes]</code></td><td>Check stable releases and update an owned installation</td></tr>
</table>

`upgrade` never silently approves non-interactive updates or downgrades. It
respects Homebrew ownership; source builds need a rebuild. Development builds
are explicitly reported rather than treated as stable releases.

## Completion and manuals

Both installation paths deploy Bash, Zsh and Fish completion and `man janus`.
For standalone installation, activate the shell you use:

```sh
# Bash 3.2+ (self-contained)
source "$HOME/.local/share/bash-completion/completions/janus"
# Zsh: before your existing compinit invocation
fpath=("$HOME/.local/share/zsh/site-functions" $fpath)
autoload -Uz compinit && compinit
# Fish (fish syntax)
source ~/.local/share/fish/vendor_completions.d/janus.fish
```

With Homebrew, follow its shell-completion setup if your shell does not already
load its completion directories. Dynamic device completion reads saved metadata
only; pressing Tab never starts a service, reads a password or makes a download.

## Usage notes

Passwords exist briefly in native process memory during authentication and are never written to device configuration or logs.
If the server rejects a password, update the corresponding item in Keychain Access.
A failed first login leaves the device saved for retry. SSH password authentication is supported; key-only authentication and arbitrary SSH options are not.

`disconnect` and `remove` accept multiple IDs/names. With no arguments, use
the multi-select picker (Space toggles, Enter confirms). Matching devices are
processed once; unmatched arguments and failures do not stop other operations.
The final summary highlights counts on terminals (respects `NO_COLOR`). Partial
failure returns a nonzero exit status. Active sessions survive.

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
