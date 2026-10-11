#!/usr/bin/env bash
# Installs Filament's cmgen, which xml_export.py runs to bake the environment maps,
# and diffimg, which compares two -screenshot captures
# Usage: ./install_cmgen.sh [install dir, default ~/.local/bin]
set -euo pipefail

# Keep in step with the version notes/SHOWCASE_PBR.md documents the cmgen flags against
VERSION=1.77.3
DEST=${1:-$HOME/.local/bin}
URL=https://github.com/google/filament/releases/download/v$VERSION/filament-v$VERSION-linux.tgz

TOOLS=(cmgen diffimg)

mkdir -p "$DEST"
# Only these binaries are extracted; they need nothing else from the archive
curl -fL "$URL" | tar -xz -C "$DEST" --strip-components=2 "${TOOLS[@]/#/filament/bin/}"
for tool in "${TOOLS[@]}"; do
    echo "installed $DEST/$tool"
done

case ":$PATH:" in
    *":$DEST:"*) ;;
    *) echo "$DEST is not on PATH: add  export PATH=\"$DEST:\$PATH\"  to your shell profile" ;;
esac
