#!/bin/bash
set -euo pipefail
VERSION=${1:?release version required}
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Package only stable X.Y.Z releases.' >&2; exit 1; }
mkdir -p dist
ASSET="janus_${VERSION}_darwin_$(uname -m).tar.gz"
tar -czf "dist/$ASSET" -C build bin/janus libexec/janus-keychain share/janus/install.sh SHA256SUMS
(cd dist && shasum -a 256 "$ASSET" > "$ASSET.sha256")
(cd dist && shasum -a 256 janus_"${VERSION}"_darwin_*.tar.gz > SHA256SUMS)
echo "Created dist/$ASSET and dist/SHA256SUMS for version $VERSION."
