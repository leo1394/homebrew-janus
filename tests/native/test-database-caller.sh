#!/bin/bash
# Same-team code with an unrelated identifier must not reach database approval.
set -euo pipefail
IDENTITY=${1:?Usage: test-database-caller.sh CODE_SIGN_IDENTITY}
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/janus-database-caller.XXXXXX")
trap 'rm -rf "$WORK"' EXIT
cat > "$WORK/main.swift" <<'SWIFT'
import Foundation
let process = Process()
process.executableURL = URL(fileURLWithPath: CommandLine.arguments[1])
process.arguments = ["--database-credential"]
let input = Pipe()
let output = Pipe()
process.standardInput = input
process.standardOutput = output
try process.run()
input.fileHandleForWriting.write(Data("{}".utf8))
try input.fileHandleForWriting.close()
let data = output.fileHandleForReading.readDataToEndOfFile()
process.waitUntilExit()
if !data.isEmpty { exit(99) }
exit(process.terminationStatus)
SWIFT
swiftc "$WORK/main.swift" -module-cache-path "$ROOT/build/module-cache" -o "$WORK/foreign"
codesign --force --sign "$IDENTITY" --options runtime --identifier com.janus.test.database.foreign "$WORK/foreign"
if JANUS_TRACE=1 "$WORK/foreign" "$ROOT/build/libexec/janus-keychain" > "$WORK/stdout" 2> "$WORK/stderr"; then
    echo 'FAIL: unrelated database caller was accepted' >&2; exit 1
fi
[ ! -s "$WORK/stdout" ]
grep -q 'database credential operation failed' "$WORK/stderr"
if grep -Eq 'keychain_open_start|authentication_submitted|credential_delivery' "$WORK/stderr"; then
    echo 'FAIL: unrelated caller reached credentials or UI' >&2; exit 1
fi
echo 'PASS: same-team unrelated database caller denied; no credential output'
