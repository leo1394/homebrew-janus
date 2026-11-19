#!/bin/bash
# Uses only in-memory ACLs; no Keychain items or credentials are accessed.
set -euo pipefail
IDENTITY=${1:?Usage: test-acl-relocation.sh CODE_SIGN_IDENTITY}
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/janus-acl-relocation.XXXXXX")
trap 'rm -rf "$WORK"' EXIT
ulimit -c 0
cp "$ROOT/tests/native/acl-relocation.swift" "$WORK/main.swift"
swiftc "$ROOT/native/KeychainPolicy.swift" "$WORK/main.swift" -module-cache-path "$WORK/cache" -o "$WORK/original" -framework Security
codesign --force --sign "$IDENTITY" --identifier com.janus.test.acl-relocation "$WORK/original"
cp "$WORK/original" "$WORK/relocated"
cp "$WORK/original" "$WORK/foreign"
codesign --force --sign "$IDENTITY" --identifier com.janus.test.foreign "$WORK/foreign"
cp "$WORK/original" "$WORK/broad"
codesign --force --sign "$IDENTITY" --identifier com.janus.test.acl-relocation -r '=designated => anchor apple generic' "$WORK/broad"
"$WORK/original" "$WORK/relocated" "$WORK/foreign" "$WORK/broad"
