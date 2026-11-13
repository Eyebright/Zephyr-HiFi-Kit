#!/bin/sh
# Zephyr Music Server — one-shot release builder.
# Cross-compiles the pure-Go server (linux amd64/arm64) and terminal agent
# (linux amd64/arm64/armhf), builds the agent .deb packages with the in-tree
# debpack tool (no dpkg-deb needed), and assembles a self-contained release
# bundle + tarball + SHA256SUMS. Runs on Linux or Windows Git Bash.
#
#   sh scripts/build-release.sh            # default version 0.2.0
#   VERSION=0.2.0 sh scripts/build-release.sh
set -eu

VERSION="${VERSION:-0.2.0}"
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

# --- toolchain: prefer the bundled SDK, else fall back to PATH ---
if [ -x "$HOME/sdk/go/bin/go" ]; then
  export GOROOT="$HOME/sdk/go"
  export PATH="$GOROOT/bin:$PATH"
fi
export CGO_ENABLED=0
export GO111MODULE=on
export GOFLAGS=-mod=mod
command -v go >/dev/null 2>&1 || { echo "go toolchain not found on PATH" >&2; exit 1; }

echo "== toolchain =="; go version

OUT="$ROOT/release/zephyr-music-server-$VERSION"
rm -rf "$OUT"
mkdir -p "$OUT/server" "$OUT/agent"

# --- server: linux amd64 + arm64 (single file, embedded console) ---
echo "== building server =="
LDFLAGS="-s -w -X hifihub/server/internal/metrics.Version=$VERSION"
( cd "$ROOT" && GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" \
    -o "$OUT/server/hifihub-server-linux-amd64" ./cmd/hifihub-server )
( cd "$ROOT" && GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" \
    -o "$OUT/server/hifihub-server-linux-arm64" ./cmd/hifihub-server )

# server packaging scripts + unit + conffile
cp "$ROOT/packaging/server/install.sh"            "$OUT/server/install.sh"
cp "$ROOT/packaging/server/uninstall.sh"          "$OUT/server/uninstall.sh"
cp "$ROOT/packaging/server/server.env"            "$OUT/server/server.env"
cp "$ROOT/packaging/server/hifihub-server.service" "$OUT/server/hifihub-server.service"
chmod +x "$OUT/server/install.sh" "$OUT/server/uninstall.sh"

# --- agent: linux amd64/arm64/armhf + .deb packages ---
echo "== building agent =="
build_agent() {
  arch="$1"; goarch="$2"; goarm="$3"
  bin="$ROOT/agent/bin/hifihub-agent-linux-$arch"
  mkdir -p "$ROOT/agent/bin"
  ( cd "$ROOT/agent" && GOOS=linux GOARCH="$goarch" GOARM="$goarm" \
      go build -trimpath -ldflags "-s -w" -o "$bin" ./cmd/hifihub-agent )
  ( cd "$ROOT/agent" && go run ./cmd/debpack \
      -bin "$bin" -arch "$arch" -version "$VERSION" \
      -pkgdir packaging -out "$OUT/agent/hifihub-agent_${VERSION}_${arch}.deb" )
}
build_agent amd64 amd64 ""
build_agent arm64 arm64 ""
build_agent armhf arm 7

# --- agent distribution set (served by the server at /dl/) ---
# Copies the debs + a version-baked agent-install.sh + SHA256SUMS into both
# server/dist/ (what the server serves) and agent/ (standalone bundle copy).
echo "== assembling dist =="
[ -f "$ROOT/agent/packaging/install.sh" ] || { echo "agent/packaging/install.sh missing" >&2; exit 1; }
mkdir -p "$OUT/server/dist"
sed "s/@VERSION@/$VERSION/g" "$ROOT/agent/packaging/install.sh" > "$OUT/server/dist/agent-install.sh"
chmod +x "$OUT/server/dist/agent-install.sh"
cp "$OUT/server/dist/agent-install.sh" "$OUT/agent/agent-install.sh"
cp "$OUT"/agent/hifihub-agent_"$VERSION"_*.deb "$OUT/server/dist/"
( cd "$OUT/server/dist" && sha256sum hifihub-agent_"$VERSION"_*.deb agent-install.sh > SHA256SUMS )
cp "$OUT/server/dist/SHA256SUMS" "$OUT/agent/SHA256SUMS"

# --- docs ---
cp "$ROOT/INSTALL.md" "$OUT/INSTALL.md"

# --- checksums (relative paths inside the bundle) ---
echo "== checksums =="
( cd "$OUT" && find . -type f ! -name SHA256SUMS -print0 \
    | sort -z | xargs -0 sha256sum > SHA256SUMS )

# --- tarball ---
TARBALL="$ROOT/release/zephyr-music-server-$VERSION.tar.gz"
( cd "$ROOT/release" && tar -czf "$TARBALL" "zephyr-music-server-$VERSION" )

echo "== done =="
echo "bundle : $OUT"
echo "tarball: $TARBALL"
ls -la "$OUT/server" "$OUT/agent"
