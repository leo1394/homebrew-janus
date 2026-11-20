# Signing identity transition

The CLI is signed as `com.janus.ai.confidential.cli`; its credential helper is
`com.janus.ai.confidential.cli.keychain`. Both use the configured Developer ID
Application identity and Hardened Runtime. An ad-hoc build cannot migrate a
Developer ID credential.

The transition-only `janus-keychain-migrate` retains the previous
`com.janus.ai.cli.keychain` identity. It accepts only a running new helper signed
by the same Developer ID team. The new helper validates the bridge's signature
before launching it; the bridge validates its parent before authorization and
again before reading and saving. An unrelated same-team executable is rejected.

When no new item exists, the bridge requests explicit native migration approval,
reads the old CLI item through the Keychain API, and creates a separate item under
`app.janus.confidential.cli.ssh.password.v1:HOST:PORT`. Its ACL trusts the new
helper. It never sends a password through stdout, arguments, logs or temporary
files. The existing `app.janus.cli.ssh.password.v1:HOST:PORT` item and ACL remain
unchanged. An existing target is never replaced. Cancellation before saving
leaves no new item; cancellation of the subsequent connection authorization
leaves the completed migration intact, without delivering a password to SSH.

The first successful migration has two explicit owner authorizations: migration,
then normal credential use. Future fresh connections need one authorization;
master reuse needs none. There should be no Keychain "change access permissions"
prompts during this transition. A locked or incompatible source fails closed;
there is no automatic ACL repair or retry of migration after cancellation.
Older app-only entries with incompatible ACLs offer separate credential entry
rather than modifying their access permissions.

The installed previous CLI can still use the preserved old item for rollback.
Explicit device removal deletes current, previous CLI and legacy app items, as
required by the CLI's removal contract. Do not retire the bridge until users
have had a supported transition window. This migration does not register a
LaunchAgent, XPC service or background daemon.

Validation includes signed ACL relocation tests, rejection of an unrelated
same-team caller, installer rollback and manifest checks, and native interactive
acceptance. Automated tests alone do not establish actual OS dialog counts.
