#!/bin/bash
# Reject an unrelated, same-team caller before any Keychain lookup or UI.
set -euo pipefail
IDENTITY=${1:?Usage: test-migration-caller.sh CODE_SIGN_IDENTITY}
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/janus-migration-caller.XXXXXX")
trap 'rm -rf "$WORK"' EXIT
cat > "$WORK/main.swift" <<'SWIFT'
import Foundation
let process = Process()
process.executableURL = URL(fileURLWithPath: CommandLine.arguments[1])
process.arguments = ["--migrate-identity", "example.invalid", "builder", "22"]
try process.run()
process.waitUntilExit()
exit(process.terminationStatus)
SWIFT
swiftc "$WORK/main.swift" -module-cache-path "$ROOT/build/module-cache" -o "$WORK/foreign"
codesign --force --sign "$IDENTITY" --options runtime --identifier com.janus.test.migration.foreign "$WORK/foreign"
if JANUS_TRACE=1 "$WORK/foreign" "$ROOT/build/libexec/janus-keychain-migrate" > "$WORK/stdout" 2> "$WORK/stderr"; then
    echo 'FAIL: unrelated caller was accepted' >&2; exit 1
fi
[ ! -s "$WORK/stdout" ]
grep -q 'migration stopped' "$WORK/stderr"
if grep -Eq 'keychain_open_start|authentication_submitted|identity_migration_read_start' "$WORK/stderr"; then
    echo 'FAIL: unrelated caller reached Keychain or authentication' >&2; exit 1
fi
echo 'PASS: same-team unrelated caller denied before Keychain or UI; no stdout'
