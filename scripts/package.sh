#!/bin/bash
set -euo pipefail
VERSION=${1:?release version required}
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Package only stable X.Y.Z releases.' >&2; exit 1; }
mkdir -p dist
ARCHS=$(lipo -archs build/bin/janus)
[ "$ARCHS" = "$(lipo -archs build/libexec/janus-keychain)" ] || { echo 'CLI/helper architectures differ.' >&2; exit 1; }
for ARCH in $ARCHS; do
    case "$ARCH" in arm64|x86_64) ;; *) echo "Unsupported architecture: $ARCH" >&2; exit 1;; esac
    ASSET="janus_${VERSION}_darwin_${ARCH}.tar.gz"
    tar -czf "dist/$ASSET" -C build bin/janus libexec/janus-keychain share/janus/install.sh SHA256SUMS
    (cd dist && shasum -a 256 "$ASSET" > "$ASSET.sha256")
done
(cd dist && shasum -a 256 janus_"${VERSION}"_darwin_*.tar.gz > SHA256SUMS)
echo "Created precompiled archives for $ARCHS and dist/SHA256SUMS for version $VERSION."
