[English](cli-integration.md) | [简体中文](cli-integration-ZH.md)

# Integrating Agent CLIs with Janus

This guide covers Janus 0.1.8. Janus handles native approval, Keychain access, remote sign-in and token ownership. The CLI requests operations through a restricted session without receiving passwords or tokens. The built-in provider is currently `clobotics`; other CLIs require a provider implementation and registration, not just a configured URL.

## 1. Register an account

```sh
janus credential --add clobotics --environment test --username USER --json
```

The user enters the password only in Janus native UI. Use the successful response's `resourceID` for subsequent session requests, or read the account `id` from `janus credential --json`. Treat IDs as opaque identifiers. The current format is `janusacc-xxxx-xxxx-xxxx-xxxxxxxxxxxx`; do not generate IDs or derive them from usernames. Existing clients should rediscover current IDs rather than retain an older ID format.

The integrated Clobotics user entry point is:

```sh
clobotics auth login --confidential --resource ACCOUNT_ID --idle-timeout 15m
```

Use a Clobotics version that includes the Janus integration. Passwords must never appear in chat, command arguments, environment variables, logs or CLI configuration.

## 2. Start the authorization process

Use a process API to launch Janus from a trusted installation path, without composing a shell command:

```text
janus session serve --idle-timeout 15m
```

Keep the child process and its stdin/stdout pipes open. The protocol uses UTF-8 with one JSON object per line. Reserve stdout for protocol messages and handle stderr separately from operation results. Send requests sequentially and wait for a response with the matching `requestID`. Input frames are limited to approximately 64 KiB. Do not send passwords, tokens or extra parameters.

This protocol is neither A2X nor JSON-RPC 2.0. Field names are case-sensitive and must match the examples below. `janus bridge` serves desktop sessions with remote Agents; it is not the CLI account authorization endpoint.

## 3. Request, use and close a session

Request authorization:

```json
{"version":1,"requestID":"1","method":"session.request","parameters":{"resourceID":"ACCOUNT_ID"}}
```

Janus displays native system authentication. On success:

```json
{"version":1,"requestID":"1","response":{"sessionID":"SESSION_ID","state":"active"}}
```

Request identity information over the same pipe:

```json
{"version":1,"requestID":"2","method":"session.execute","parameters":{"sessionID":"SESSION_ID","operation":"inspect","subset":"identity"}}
```

`response.result` contains `authenticated`, `environment`, `system` and an allowlisted `principal` with `id`, `tenantID`, `username` and `displayName`. It contains no tokens.

List sessions belonging to the current pipe:

```json
{"version":1,"requestID":"3","method":"session.list","parameters":{}}
```

Explicitly sign out:

```json
{"version":1,"requestID":"4","method":"session.revoke","parameters":{"sessionID":"SESSION_ID"}}
```

Success returns `response.revoked=true`, then the process exits. Wait for the response before closing the pipe. Do not send a batch through a one-shot pipe and immediately signal EOF: EOF cancels authorization. Each process supports one account authorization; another process cannot reuse it by copying the session ID.

Users can also manage sessions with `janus session list`, `janus session close SESSION_ID` and `janus session revoke SESSION_ID`.

## 4. Lifecycle and error handling

- The idle timeout defaults to 15 minutes and accepts `5m` through `1h`. Successful operations reset the timer; list/status polling does not.
- EOF, explicit revocation, account removal/replacement and idle expiry end authorization. Remote token expiry requires fresh authorization; tokens are not automatically refreshed.
- Handle `response.error`: `approval_denied` means native approval was not completed; `resource_unavailable` means the resource is unavailable; `unsupported_operation` means the operation or parameters are unsupported. Sign-in or runtime failures may instead produce EOF or process exit, so checking JSON errors alone is insufficient.
- Stop the current flow when the user cancels. After expiry, request authorization again when the user needs an operation; do not repeatedly open dialogs or silently bypass Janus.
- Local closure acknowledgment does not confirm remote logout. Remote logout is best-effort.
- A session ID is not a bearer token. Fully privileged processes running under the same UID are outside the isolation guarantee; do not claim protection against every malicious local process.

## 5. Add another CLI/provider

The current endpoint allows Clobotics identity inspection only. It does not forward arbitrary URLs, HTTP headers or business requests. To add a provider:

1. Register provider metadata and constraints in `internal/store/accounts.go`. Keep business-system names inside the provider rather than introducing generic Janus command keywords.
2. Implement sign-in, identity verification, token lifecycle and sign-out in `internal/cliprovider/`. Keep tokens in Janus and return only allowlisted fields.
3. Integrate account enrollment, recovery and the appropriate display name/icon in `pkg/cli/credentials.go` and the native helper. Preserve signing, parent-process verification and Keychain policies.
4. Define an explicit operation allowlist and parameter/result schemas for `session.execute`. Business APIs require deliberate implementation, not returning a token to the CLI.
5. Test initial enrollment/cancellation, failed-password rollback, connection reuse, revocation, EOF, idle expiry, successful-operation timer reset and invalidation after credential deletion. Users complete native authorization for real-resource acceptance.

Implementation entry points: `pkg/cli/mysql.go` registers `session serve`; `internal/cliprovider/session.go` defines the stdio protocol; `internal/cliprovider/client.go` implements the current provider.
