#!/bin/bash
# Bash 3.2+. Installs without Homebrew. No sudo or shell startup edits.
set -euo pipefail
REPO=https://github.com/leo1394/homebrew-janus
VERSION=
PREFIX=${HOME}/.local
LOCAL=
usage() { echo 'Usage: install.sh [--version X.Y.Z] [--prefix DIR] [--local BUILD_DIR]'; }
while [ $# -gt 0 ]; do
    case "$1" in
        --version|--prefix|--local)
            [ $# -ge 2 ] || { usage >&2; exit 1; }
            case "$1" in --version) VERSION=${2#v};; --prefix) PREFIX=$2;; --local) LOCAL=$2;; esac
            shift 2;;
        --help|-h) usage; exit 0;;
        *) echo "Unknown option: $1" >&2; usage >&2; exit 1;;
    esac
done
[ "$(uname -s)" = Darwin ] || { echo 'Janus Confidential CLI requires macOS.' >&2; exit 1; }
case "$(uname -m)" in arm64|x86_64) ARCH=$(uname -m);; *) echo 'Unsupported architecture.' >&2; exit 1;; esac
for tool in shasum tar mktemp; do command -v "$tool" >/dev/null || { echo "Missing dependency: $tool" >&2; exit 1; }; done
WORK=$(mktemp -d "${TMPDIR:-/tmp}/janus-install.XXXXXX")
STAGE=
COMMITTED=
SUCCESS=0
RETIRED=
cleanup() {
    status=$?
    if [ "$SUCCESS" -ne 1 ] && [ -n "$STAGE" ]; then
        while IFS= read -r relative; do
            [ -n "$relative" ] || continue
            if [ -f "$STAGE/retired/$relative" ]; then
                mkdir -p "$(dirname "$PREFIX/$relative")"
                mv -f "$STAGE/retired/$relative" "$PREFIX/$relative" || echo "Retired file restore failed: $relative" >&2
            fi
        done <<< "$RETIRED"
        for relative in $COMMITTED; do
            if [ -f "$STAGE/backup/$relative" ]; then
                mv -f "$STAGE/backup/$relative" "$PREFIX/$relative" || echo "Rollback failed: $PREFIX/$relative" >&2
            else
                rm -f "$PREFIX/$relative"
            fi
        done
    fi
    [ -z "$STAGE" ] || rm -rf "$STAGE"
    rm -rf "$WORK"
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
if [ -n "$LOCAL" ]; then
    SOURCE=$(cd "$LOCAL" && pwd -P)
else
    command -v curl >/dev/null || { echo 'Missing dependency: curl' >&2; exit 1; }
    if [ -z "$VERSION" ]; then
        # GitHub's latest endpoint redirects only to a published stable release.
        URL=$(curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --connect-timeout 10 --max-time 30 --output /dev/null --write-out '%{url_effective}' "$REPO/releases/latest")
        VERSION=${URL##*/}; VERSION=${VERSION#v}
    fi
    [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'A stable X.Y.Z release is required.' >&2; exit 1; }
    ASSET="janus_${VERSION}_darwin_${ARCH}.tar.gz"
    BASE="$REPO/releases/download/v$VERSION"
    curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --connect-timeout 10 --max-time 120 "$BASE/$ASSET" -o "$WORK/$ASSET"
    curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --connect-timeout 10 --max-time 30 "$BASE/SHA256SUMS" -o "$WORK/releases.sha256"
    EXPECTED=$(awk -v asset="$ASSET" '$2 == asset {print $1}' "$WORK/releases.sha256")
    [[ "$EXPECTED" =~ ^[0-9a-f]{64}$ ]] || { echo 'Missing or invalid release checksum.' >&2; exit 1; }
    ACTUAL=$(shasum -a 256 "$WORK/$ASSET"); ACTUAL=${ACTUAL%% *}
    [ "$EXPECTED" = "$ACTUAL" ] || { echo 'Release checksum mismatch; installation unchanged.' >&2; exit 1; }
    mkdir "$WORK/source"
    tar -xzf "$WORK/$ASSET" -C "$WORK/source" bin/janus libexec/janus-keychain share/janus/install.sh SHA256SUMS
    if tar -tzf "$WORK/$ASSET" | grep -qx libexec/janus-keychain-migrate; then
        tar -xzf "$WORK/$ASSET" -C "$WORK/source" libexec/janus-keychain-migrate
    fi
    SOURCE=$WORK/source
fi
# Copy only the expected payload; reject symlinks and arbitrary checksum paths.
mkdir -p "$WORK/payload/bin" "$WORK/payload/libexec" "$WORK/payload/share/janus"
MIGRATOR=
if [ -e "$SOURCE/libexec/janus-keychain-migrate" ] || [ -L "$SOURCE/libexec/janus-keychain-migrate" ]; then MIGRATOR=libexec/janus-keychain-migrate; fi
for relative in bin/janus libexec/janus-keychain share/janus/install.sh SHA256SUMS $MIGRATOR; do
    [ -f "$SOURCE/$relative" ] && [ ! -L "$SOURCE/$relative" ] || { echo "Invalid payload file: $relative" >&2; exit 1; }
    cp "$SOURCE/$relative" "$WORK/payload/$relative"
done
awk -v migrator="$MIGRATOR" 'seen[$2]++ || NF != 2 || $1 !~ /^[0-9a-f]+$/ || length($1) != 64 || ($2 != "bin/janus" && $2 != "libexec/janus-keychain" && $2 != "share/janus/install.sh" && (migrator == "" || $2 != migrator)) {exit 1} END {if (NR != (migrator == "" ? 3 : 4)) exit 1}' "$WORK/payload/SHA256SUMS" || { echo 'Invalid payload checksum manifest.' >&2; exit 1; }
(cd "$WORK/payload" && shasum -a 256 -c SHA256SUMS)
CANDIDATE=$WORK/payload/bin/janus
chmod 755 "$CANDIDATE" "$WORK/payload/libexec/janus-keychain"
OUTPUT=$("$CANDIDATE" version)
FIRST=${OUTPUT%%$'\n'*}
case "$FIRST" in
    "Janus Confidential CLI version "*) ACTUAL_VERSION=${FIRST#Janus Confidential CLI version };;
    "janus version "*) ACTUAL_VERSION=${FIRST#janus version };;
    *) echo 'Invalid candidate version output.' >&2; exit 1;;
esac
ACTUAL_VERSION=${ACTUAL_VERSION%% *}
[ -n "$VERSION" ] || VERSION=$ACTUAL_VERSION
[ "$ACTUAL_VERSION" = "$VERSION" ] || { echo 'Candidate version mismatch; installation unchanged.' >&2; exit 1; }
[ "$OUTPUT" = "$(printf '%s\n%s' "$FIRST" "$REPO")" ] || { echo 'Invalid candidate metadata.' >&2; exit 1; }
# Avoid overwriting a package-manager installation.
case "$PREFIX" in *$'\n'*|*$'\r'*) echo 'Invalid prefix.' >&2; exit 1;; esac
mkdir -p "$PREFIX"
PREFIX=$(cd "$PREFIX" && pwd -P)
case "$PREFIX" in */Cellar/*|*/Cellar) echo 'Use Homebrew to update Cellar installations.' >&2; exit 1;; esac
if [ -L "$PREFIX/bin/janus" ]; then echo 'Existing janus is a symlink; use its owning package manager or choose another prefix.' >&2; exit 1; fi
STAGE=$(mktemp -d "$PREFIX/.janus-stage.XXXXXX")
mkdir -p "$STAGE/new/bin" "$STAGE/new/libexec" "$STAGE/new/share/man/man1" "$STAGE/new/share/bash-completion/completions" "$STAGE/new/share/zsh/site-functions" "$STAGE/new/share/fish/vendor_completions.d" "$STAGE/new/share/janus"
cp "$CANDIDATE" "$STAGE/new/bin/janus"
cp "$WORK/payload/libexec/janus-keychain" "$STAGE/new/libexec/janus-keychain"
if [ -n "$MIGRATOR" ]; then cp "$WORK/payload/$MIGRATOR" "$STAGE/new/$MIGRATOR"; chmod 755 "$STAGE/new/$MIGRATOR"; fi
cp "$WORK/payload/share/janus/install.sh" "$STAGE/new/share/janus/install.sh"
"$CANDIDATE" completion bash > "$STAGE/new/share/bash-completion/completions/janus"
"$CANDIDATE" completion zsh > "$STAGE/new/share/zsh/site-functions/_janus"
"$CANDIDATE" completion fish > "$STAGE/new/share/fish/vendor_completions.d/janus.fish"
"$CANDIDATE" __manpage > "$STAGE/new/share/man/man1/janus.1"
ESCAPED=$(printf '%s' "$PREFIX" | sed 's/\\/\\\\/g;s/"/\\"/g')
printf '{"method":"standalone","prefix":"%s","version":"%s"}\n' "$ESCAPED" "$VERSION" > "$STAGE/new/share/janus/install.json"
FILES='libexec/janus-keychain share/janus/install.sh share/janus/install.json share/man/man1/janus.1 share/bash-completion/completions/janus share/zsh/site-functions/_janus share/fish/vendor_completions.d/janus.fish bin/janus'
FILES="$FILES $MIGRATOR"
# Inventory only files explicitly owned by a previous Janus Confidential CLI installation.
# This CLI has no .app, LaunchAgent, XPC or PlugInKit registrations.
if [ -f "$PREFIX/share/janus/files.sha256" ]; then
    [ ! -L "$PREFIX/share/janus/files.sha256" ] || { echo 'Unsafe previous manifest.' >&2; exit 1; }
    while read -r expected relative extra; do
        [ -n "$relative" ] || continue
        [[ "$expected" =~ ^[0-9a-f]{64}$ ]] && [ -z "${extra:-}" ] || { echo 'Invalid previous manifest.' >&2; exit 1; }
        case "$relative" in *..*|/*|*\\*) echo 'Unsafe previous manifest path.' >&2; exit 1;; esac
        case "$relative" in bin/janus|libexec/janus-*|share/janus/*|share/man/man1/janus.1|share/bash-completion/completions/janus|share/zsh/site-functions/_janus|share/fish/vendor_completions.d/janus.fish) ;; *) echo 'Unknown manifest owner; refusing cleanup.' >&2; exit 1;; esac
        case " $FILES " in *" $relative "*) continue;; esac
        [ -e "$PREFIX/$relative" ] || continue
        [ -f "$PREFIX/$relative" ] && [ ! -L "$PREFIX/$relative" ] || { echo "Unsafe retired artifact: $relative" >&2; exit 1; }
        parent=$(dirname "$PREFIX/$relative")
        [ "$(cd "$parent" && pwd -P)" = "$parent" ] || { echo "Redirected retired artifact directory: $relative" >&2; exit 1; }
        actual=$(shasum -a 256 "$PREFIX/$relative"); actual=${actual%% *}
        [ "$actual" = "$expected" ] || { echo "Retired artifact modified; preserve and inspect: $relative" >&2; exit 1; }
        RETIRED="${RETIRED}${relative}
"
    done < "$PREFIX/share/janus/files.sha256"
fi
(cd "$STAGE/new" && shasum -a 256 $FILES > share/janus/files.sha256)
FILES="$FILES share/janus/files.sha256"
for relative in $FILES; do
    mkdir -p "$(dirname "$PREFIX/$relative")" "$(dirname "$STAGE/backup/$relative")"
    [ ! -L "$PREFIX/$relative" ] || { echo "Refusing existing symlink: $relative" >&2; exit 1; }
    if [ -e "$PREFIX/$relative" ]; then
        [ -f "$PREFIX/$relative" ] || { echo "Destination is not a file: $relative" >&2; exit 1; }
        cp -p "$PREFIX/$relative" "$STAGE/backup/$relative"
    fi
done
for relative in $FILES; do
    COMMITTED="$relative $COMMITTED"
    mv -f "$STAGE/new/$relative" "$PREFIX/$relative"
done
[ "$("$PREFIX/bin/janus" version)" = "$OUTPUT" ] || { echo 'Installed version verification failed; restoring previous files.' >&2; exit 1; }
while IFS= read -r relative; do
    [ -n "$relative" ] || continue
    mkdir -p "$(dirname "$STAGE/retired/$relative")"
    mv "$PREFIX/$relative" "$STAGE/retired/$relative"
done <<< "$RETIRED"
SUCCESS=1
printf 'Installed Janus Confidential CLI %s in %s\n' "$VERSION" "$PREFIX"
printf 'PATH: export PATH="%s/bin:$PATH"\nMANPATH (preserves system defaults): export MANPATH="%s/share/man:${MANPATH:-}"\n' "$PREFIX" "$PREFIX"
printf 'Bash: source "%s/share/bash-completion/completions/janus"\nZsh: add "%s/share/zsh/site-functions" to fpath before compinit.\nFish: source "%s/share/fish/vendor_completions.d/janus.fish"\n' "$PREFIX" "$PREFIX" "$PREFIX"
