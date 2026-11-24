#!/bin/sh
# Imports one audio terminal bundle into a server /dl/ directory so audio-build
# terminals upgrade with the ALSA-capable deb. Management filenames stay
# unchanged; the audio copy gets a _audio suffix.
#   scripts/import-audio-dist.sh /var/lib/hifihub-server/dist \
#       Zephyr-HiFi-Kit-terminal-0.68-audio-arm64.tar.gz
set -eu

[ "$#" -eq 2 ] || { echo "usage: $0 <dist-dir> <terminal-audio-bundle.tar.gz>" >&2; exit 2; }
DIST=$1
BUNDLE=$2
[ -d "$DIST" ] || { echo "dist directory not found: $DIST" >&2; exit 1; }
[ -f "$BUNDLE" ] || { echo "bundle not found: $BUNDLE" >&2; exit 1; }

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
tar -xzf "$BUNDLE" -C "$TMP"

AGENT_DIR=$(find "$TMP" -type d -name agent | head -1)
[ -n "$AGENT_DIR" ] || { echo "bundle has no agent directory" >&2; exit 1; }
[ -f "$AGENT_DIR/SHA256SUMS" ] || { echo "bundle has no agent SHA256SUMS" >&2; exit 1; }
( cd "$AGENT_DIR" && sha256sum -c SHA256SUMS )

DEB=$(find "$AGENT_DIR" -maxdepth 1 -type f -name 'hifihub-agent_*_*.deb' | head -1)
[ -n "$DEB" ] || { echo "bundle has no terminal deb" >&2; exit 1; }
BASE=$(basename "$DEB")
NAME="${BASE%.deb}_audio.deb"

cp "$DEB" "$DIST/$NAME"
(
  cd "$DIST"
  for f in hifihub-agent_*.deb agent-install.sh; do
    [ -f "$f" ] && sha256sum "$f"
  done
) | LC_ALL=C sort -k2 > "$DIST/SHA256SUMS"

echo "imported $NAME"
echo "dist SHA256SUMS refreshed"
