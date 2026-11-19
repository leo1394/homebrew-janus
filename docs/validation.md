# Validation record — 2026-11-19

Validated locally on Apple Silicon macOS with system OpenSSH 10.5p1 and Swift
6.4.2. This record distinguishes isolated integration from real credential/UI
acceptance. No Janus Confidential app, Core, bridge or MCP artifact was built.

## Passed

- `go test -race -count=1 ./...`: CLI contract, existing extracted selector tests,
  version output, help without initialization, command/flag errors, read-only
  completion, registry corruption rejection, unique names and concurrent adds,
  removal failure behavior, mux attachment safety, and upgrade decision tests.
- Isolated loopback sshd integration using freshly generated disposable keys:
  master reuse works without an installed credential helper, `disconnect` stops
  new attachment, and an already-running session completes after disconnect.
  It never uses a real account password, real SSH configuration or saved keys.
- Runtime concurrent ensure: eight callers create only one master.
- Upgrade tests mock stable metadata and execution: equal/newer/ahead versions,
  numeric ordering, development/prerelease rejection, failed metadata, no/empty/
  EOF/invalid responses, non-interactive refusal, yes/--yes, failed install,
  failed post-install verification, and Homebrew lag/changed-target refusal.
- `make build`: standalone Go CLI and native Keychain helper; ad-hoc signature.
  Current SDK emits expected legacy Keychain API deprecation warnings.
- Native `--self-test`: helper-only ACL construction/inspection, without opening
  or reading a saved credential.
- `python3 scripts/test-install.py`: isolated prefix containing spaces, install,
  manual discovery/render, invalid version, corrupted checksum, and restoration
  after a forced post-install verification failure.
- Actual Bash 3.2 completion function returns a saved device name containing a
  space. Zsh completion passes syntax checking. Shared completion protocol checks
  commands, enums, -- separator and non-path arguments without state mutation.
- `ruby -c Formula/janus.rb` and Bash syntax checks.
- Original logo and banner rasterized using AppKit and visually inspected: text
  fits, small logo is recognizable, and dark fill/outline works on light or dark
  page backgrounds. Both READMEs embed the delivered relative SVG assets.

## Still requires target acceptance

- Native enrollment, saved-password LocalAuthentication, user cancellation,
  rejected passwords and exact Keychain deletion against disposable credentials.
- Command-to-visible-window <3 seconds and reused interactive terminal <1 second
  on real target hosts. Loopback mux correctness is not a latency acceptance test.
- Intel, macOS 14 and other supported older macOS versions.
- Fish shell execution and interactive Zsh completion insertion (syntax and the
  shared protocol have been checked, not an interactive shell session).
- Actual Homebrew installation/`brew test`, stable Formula archive/checksum,
  published remote installer/download failures and a real version upgrade.
- Production helper signing and authenticated ACL identity migration.
- Workbench/Confidential adoption of the shared runtime boundary. Existing
  clients remain on their original Core until an explicit future adapter.

Funding: explicitly declined; no funding configuration was created. No tag,
release, push or global installation was performed. Temporary installation tests
used disposable prefixes only. Project output is in `homebrew-janus/build`.

## Follow-up fix

Empty table output now explains how to add a device; JSON remains `[]`. Spawn
accepts SSH arguments without an `ssh` subcommand. Existing credential helper
ACL migration now requires LocalAuthentication before updating the exact item;
the ACL is re-read and checked before the secret is read. Real migration and
Keychain system prompts still need user-assisted acceptance.

## Connection latency investigation

User trace (2026-11-19): device selection 2,979 ms; selected device to Keychain
lookup ~13,006 ms; lookup to authentication submission 15 ms; authentication
completion to master-ready 7,818 ms. The trace did not distinguish network,
helper launch and credential delivery, so no root cause or target attainment
is claimed from those numbers. A credential-free TCP/SSH probe from the agent
host timed out after 10 seconds, unlike the user's successful interactive run.

Expanded `JANUS_TRACE=1` records TCP/banner/key exchange, helper entry, Keychain
open/find, ACL check/migration/commit, LocalAuthentication, credential read/delivery,
master reuse and mux session admission. Raw SSH diagnostics use a private temporary
file deleted after the process; emitted events contain fixed names only. Selector
interaction and owner approval time must be reported separately from startup.

Installer now inventories and removes obsolete files from the previous ownership
manifest, verifies their checksums, and rolls them back on failure. Modified or
redirected artifacts stop cleanup for inspection. Base Janus creates no macOS app,
LaunchAgent or PlugInKit registration; it does not unregister Confidential apps.
The build reuses an unchanged native helper to avoid needless helper identity/ACL
migration; changed native source builds and verifies a replacement before renaming.

## Saved-password read optimization

The subsequent successful remote trace isolates 5,990 ms inside
`SecKeychainItemCopyContent`, after LocalAuthentication completed. TCP connect
was 2,571 ms, askpass launch-to-helper-entry 908 ms, and mux session admission
about 2,450 ms. These are independent stages; the entire post-approval delay
must not be attributed to the credential read.

Replaced the legacy content copy with Confidential's exact-item
`SecItemCopyMatching` pattern (`kSecMatchItemList`, return data, UI fail), scoped
inside temporarily disabled legacy Keychain interaction. Owner authentication
and ACL verification/migration still precede the read. Any denied read fails
without fallback to another credential or a password prompt. The prior interaction
setting is restored on both success and error. No password cache was added.

Native fixture tests (`native/tests/main.swift`) verify UI scoping/restoration,
authorization rejection and exact-item queries without accessing saved secrets.
The optimized production helper builds and passes signature validation. A new
real-user trace is still required to establish improvement and acceptance targets.

## Spawn lookup and error diagnosis

Reproduced the non-terminal warning with the metadata-only `ssh -G` query;
`-G -T` removes it without changing the interactive session's TTY policy.
Credential lookup now uses the exact login-keychain service/account reference
query, requests no data, and disables implicit Keychain UI, matching the native
Confidential lookup approach. Stage/domain/OSStatus are reported on failure;
completed ACL migration is no longer incorrectly described as no modification.
The user-reported multi-prompt failure remains subject to native-user validation;
previous generic output cannot identify whether migration, read or save failed.
No real credential was inspected, deleted, overwritten or read during tests.

## Partition ACL correction

A later trace returned errSecAuthFailed (-25293) at the silent credential read
although the trusted-application ACL passed. Source comparison found that the
extracted migration omitted Confidential's independent partition ACL validation
and update. The helper now checks both rules and, only after LocalAuthentication,
updates the existing selected item's exact trusted-application rule and partition
rule, persists, re-reads and verifies both before reading a password. Missing,
mixed or malformed partition rules fail closed rather than inventing permissions.

Unlike the signed Confidential product, this development helper uses its validated
current cdhash partition, not Confidential's Team ID or an all-applications grant.
Changed helper builds require a new authorized migration; unchanged builds retain
the helper. Tests cover strict partition parsing/encoding alongside the native
read authorization/UI gates. Real-item repair and successful SSH authentication
remain pending the user's next native authorization run.

## Signed identity alignment

After an ACL commit failed with -25293 despite user confirmation, metadata-only
inspection established: login keychain unlocked, item present, application ACL
current, but a Team ID partition incompatible with the ad-hoc helper. The host
has the original project's Developer ID identity. The local helper is now signed
with Developer ID Application: Clobotics corporation (9DL29GTL2M).
`helperPartition` derives the team from its strictly validated own signature;
it never takes a desired team from the item or environment. Exact application
ACL checking remains mandatory. Ad-hoc builds still use their own cdhash.

Local signing choice is in gitignored `signing.local.mk`, not a repository-wide
certificate requirement. Before release, configure the intended signing identity.
Post-signing read-only inspection of the user-reported target B showed both
applicationCurrent=true and partitionCurrent=true, with credentialRead=false.
The other target A retains a cdhash partition and still needs authorized
migration. No real credential was read or connection initiated by this diagnostic.
Actual SSH login with the new signed helper remains user-assisted acceptance.

## Confirmed credential recovery and live measurements

The Team ID repair alone was insufficient. Apple's implementation of
SecTrustedApplicationCopyData returns only a path string. The extracted policy
compared paths and therefore accepted an obsolete embedded signing requirement.
The helper now validates that requirement against its running code identity;
legacy trusted references use their verification API. The macOS-exported
SecTrustedApplicationCopyRequirement / ValidateWithPath SPIs are explicitly
bridged; compatibility must be checked on each supported macOS release.
Source: https://github.com/apple-oss-distributions/Security/blob/main/OSX/libsecurity_keychain/lib/SecTrustedApplication.cpp

Metadata inspection changed applicationCurrent from true to false while the
Team ID partition remained current. With native user authorization, the first
commit returned -25293 but a fresh readback established the full policy had been
updated. That failed attempt did not read or deliver a password. A separate,
freshly authorized attempt then read the saved password in 20 ms and successfully
opened SSH. The test shell was exited without business commands.

Observed target B timings (2026-11-19):
- Command start to authentication request submitted: 402 ms. System window
  visibility still has no API callback and is not inferred from this timestamp.
- Keychain data read: 20 ms.
- Authentication completed to session accepted: 1,444 ms.
- Subsequent reused-master command start to session accepted: 309 ms; the
  remote test command was only `exit`.
- Three independent TCP opens: 36.1, 35.9, 39.9 ms; SSH banner total: 76.7,
  97.3, 121.0 ms. Tailscale reported direct endpoint connectivity at 31 ms.

This supports restoring correctness and eliminating the local six-second read
stall, but does not guarantee every network condition meets the target. The
prior multi-second TCP timings reflect a different path/time; no speculative
DNS change was made for the numeric-IP target. Resident-helper work awaits the
user's scope choice now that process startup is only 4–21 ms in the live samples.

Native regression uses a trusted-application reference with the current test
path but an intentionally nonmatching signing requirement. The new validator
rejects it; the old path-only comparison would not detect this mismatch.

The user selected keeping the on-demand helper; no resident service or new system
registration is introduced. Final Developer ID signature/self-ACL verification
must run in the normal user context: inside the execution sandbox Security
reported an invalid signature/unprotected ACL, while the same unchanged artifact
outside the sandbox verified `valid on disk`, satisfied its designated requirement,
and passed `ACL policy OK`. Installation fixture tests and Go regressions passed.

## Legacy credential recovery acceptance (2026-11-19)

The target A legacy item still had an incompatible cdhash partition after the
failed ACL commit. This was distinct from the previously verified target B item.
Base Janus now offers explicit native enrollment into a separate CLI namespace
instead of repeatedly mutating this incompatible legacy partition. The original
item remains intact. Cancellation and an existing base credential cannot trigger
replacement enrollment.

User-assisted real target A acceptance with the signed build:
- Recovery selected in native UI; SSH password entered only in the secure field.
- New credential saved in 22 ms; authenticated login and terminal session succeeded.
- Exited the test terminal, stopped master reuse (preserving active sessions),
  and connected again to force fresh authorization and a saved-password read.
- Authentication submitted at 3,070 ms (not a window-visible callback).
- Authorization completed at 8,266 ms; credential read 8,267–8,312 ms (45 ms).
- SSH authenticated at 8,772 ms; mux accepted at 10,624 ms. No ACL commit prompt
  or -25293 on this second connection. The remaining session wait is not eliminated.
- Test terminals were exited; no remote business commands were executed.

Native policy/recovery gate tests, helper self-test and strict signature check,
CLI/store/upgrade tests, and isolated installer/completion checks passed.

## SSH handshake delay investigation (2026-11-19)

Password-only authentication was already enforced; public-key scanning is not
responsible for the observed pause. Two credential-free probes to the target A
endpoint completed initial authentication discovery in 6,263 / 1,162 ms (expected
exit 255 with no supported authentication enabled). Local KEX preparation took
about 3 ms, while waiting for the peer KEX reply took 1,170 / 246 ms. Initial
userauth-none response waits were 53 / 596 ms. These are observations, not a
stable latency baseline or proof of a server configuration fault.

Tailscale reported a direct LAN endpoint with 434 ms, timeout, and 577 ms across
three probes. A subsequent paired TCP/banner-only comparison of overlay and LAN
addresses measured TCP 64/63, 21/20, 62/61 ms; banner waits 88/123, 308/432,
239/232 ms. No credentials were sent or remote configuration changed. The direct
LAN endpoint was used only for transport probes, not as a trusted login identity.

JANUS_TRACE now includes peer KEX proposal/reply, client NEWKEYS, initial auth
probe, advertised auth methods, password method selection and password packet
submission milestones. Debug level 3 is enabled only in trace mode; private raw
logs remain temporary and only fixed phase names are emitted. The first type-50
packet is the initial none probe in this password-only flow; later type-50 packets
must not overwrite that timestamp. Regression coverage checks this deduplication
and that raw diagnostic strings do not leak. Tests passed before/after instrumentation
with the new milestone test initially failing, then passing; CLI tests and the
local SSH integration suite with the race detector also passed.

No speculative cipher, authentication or host-key policy changes were made.
Handshake variability remains unresolved; timing targets are not yet guaranteed.

## Direct terminal handoff (2026-11-19)

Normal CLI attachment now execs SSH with the original standard descriptors.
A subprocess regression verifies identical process IDs across replacement,
unchanged input/arguments, native exit status 7 and no Janus trace output after
handoff even with JANUS_TRACE enabled. The previous ssh_process_exited line
is no longer emitted for terminal attachments. The reported two-second remote
keyboard echo delay has not been reproduced or proven fixed by this change;
the prior implementation also passed terminal file descriptors directly.
