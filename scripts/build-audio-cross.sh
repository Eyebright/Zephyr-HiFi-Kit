#!/bin/sh
# Zephyr HiFi Kit — cross-build the CGO audio agent for ARM on an x86_64
# Debian/Ubuntu host using multiarch. Produces audio-enabled .deb packages for
# arm64 (Raspberry Pi 64-bit) and armhf (32-bit), plus the arm64 audio server
# (FLAC decode) when libflac-dev:arm64 is present.
#
# The native runner path (scripts/build-audio.sh on an arm64 host) needs none of
# this; use cross-build only when no ARM runner is available.
#
# One-time host setup (the CI audio job does this automatically):
#   sudo dpkg --add-architecture arm64
#   sudo dpkg --add-architecture armhf
#   sudo apt-get update
#   sudo apt-get install -y gcc-aarch64-linux-gnu gcc-arm-linux-gnueabihf \
#       libasound2-dev:arm64 libasound2-dev:armhf libflac-dev:arm64
#
#   sh scripts/build-audio-cross.sh                 # arm64 + armhf
#   TARGETS=arm64 sh scripts/build-audio-cross.sh   # arm64 only
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

VERSION="${VERSION:-0.82}"
case "$VERSION" in
  [0-9]*) ;;
  *) echo "version must start with a digit" >&2; exit 1 ;;
esac
case "$VERSION" in
  *[!A-Za-z0-9._-]*|*..*) echo "unsafe version: $VERSION" >&2; exit 1 ;;
esac
TARGETS="${TARGETS:-arm64 armhf}"

# --- toolchain: prefer the bundled SDK, else fall back to PATH ---
if [ -x "$HOME/sdk/go/bin/go" ]; then
  export GOROOT="$HOME/sdk/go"
  export PATH="$GOROOT/bin:$PATH"
fi
command -v go >/dev/null 2>&1 || { echo "go toolchain not found on PATH" >&2; exit 1; }

case "$(uname -s)" in
  Linux) ;;
  *) echo "build-audio-cross.sh must run on Linux (multiarch cross CGO)." >&2; exit 1 ;;
esac
command -v pkg-config >/dev/null 2>&1 || { echo "pkg-config not found" >&2; exit 1; }

OUT="$ROOT/bin/audio-cross/$VERSION"
require_empty_dir() {
  [ ! -L "$1" ] || { echo "refusing symlink output: $1" >&2; exit 1; }
  if [ -e "$1" ]; then
    [ -d "$1" ] && [ -z "$(find "$1" -mindepth 1 -print -quit)" ] || {
      echo "refusing nonempty output: $1" >&2; exit 1;
    }
  fi
}
require_empty_dir "$OUT"
for target in $TARGETS; do
  case "$target" in arm64|armhf) ;; *) echo "unknown target: $target" >&2; exit 2 ;; esac
  terminal="$ROOT/release/Zephyr-HiFi-Kit-terminal-$VERSION-audio-$target"
  require_empty_dir "$terminal"
  [ ! -e "$terminal.tar.gz" ] && [ ! -L "$terminal.tar.gz" ] || {
    echo "refusing existing output: $terminal.tar.gz" >&2; exit 1;
  }
done
mkdir -p "$OUT"

# Build debpack once for the HOST so packaging never inherits the cross GOARCH/CC.
HOSTARCH=$(go env GOARCH)
DEBPACK="$OUT/debpack-host"
echo "== building host debpack ($HOSTARCH) =="
( cd "$ROOT/agent" && CGO_ENABLED=0 GOOS=linux GOARCH="$HOSTARCH" GOARM= \
    go build -o "$DEBPACK" ./cmd/debpack )

export CGO_ENABLED=1 GO111MODULE=on GOFLAGS=-mod=mod GOOS=linux

build_target() {
  arch="$1"; triplet="$2"; goarch="$3"; goarm="$4"
  cc="${triplet}-gcc"
  if ! command -v "$cc" >/dev/null 2>&1; then
    echo "!! $arch: missing cross compiler '$cc' (apt-get install gcc-${triplet})" >&2
    return 1
  fi
  export CC="$cc" GOARCH="$goarch" GOARM="$goarm"
  export PKG_CONFIG_LIBDIR="/usr/lib/${triplet}/pkgconfig:/usr/share/pkgconfig"
  export PKG_CONFIG_PATH=""
  if ! pkg-config --exists alsa; then
    echo "!! $arch: cross alsa.pc not found (apt-get install libasound2-dev:${arch})" >&2
    return 1
  fi

  echo "== $arch: audio agent (CC=$cc, alsa $(pkg-config --modversion alsa)) =="
  bin="$OUT/hifihub-agent-linux-$arch"
  ( cd "$ROOT/agent" && go build -trimpath -ldflags "-s -w -X hifihub/agent/internal/config.BuildVersion=$VERSION -X hifihub/agent/internal/config.BuildFlavor=audio" -o "$bin" ./cmd/hifihub-agent )
  "$DEBPACK" -audio -bin "$bin" -arch "$arch" -version "$VERSION" \
    -pkgdir "$ROOT/agent/packaging" -out "$OUT/hifihub-agent_${VERSION}_${arch}.deb"

  name="Zephyr-HiFi-Kit-terminal-$VERSION-audio-$arch"
  terminal="$ROOT/release/$name"
  mkdir -p "$terminal/agent"
  cp "$OUT/hifihub-agent_${VERSION}_${arch}.deb" "$terminal/agent/"
  cp "$OUT/install.sh" "$terminal/agent/install.sh"
  cp "$OUT/install.sh" "$terminal/agent/agent-install.sh"
  ( cd "$terminal/agent" && sha256sum hifihub-agent_"$VERSION"_"$arch".deb agent-install.sh > SHA256SUMS )
  cp "$ROOT/README.md" "$ROOT/README.cn.md" "$ROOT/README.en.md" "$ROOT/INSTALL.md" "$ROOT/LICENSE" "$terminal/"
  ( cd "$terminal" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS )
  ( cd "$ROOT/release" && tar -czf "$terminal.tar.gz" "$name" )

  # arm64 also gets the audio server (libFLAC decode); armhf is agent-only.
  if [ "$arch" = arm64 ]; then
    if pkg-config --exists flac; then
      echo "== arm64: audio server (flac $(pkg-config --modversion flac)) =="
      ( cd "$ROOT" && go build -trimpath -ldflags "-s -w -X hifihub/server/internal/metrics.Version=$VERSION" \
          -o "$OUT/hifihub-server-linux-arm64" ./cmd/hifihub-server )
    else
      echo "note: libflac-dev:arm64 absent; skipping arm64 audio server" >&2
    fi
  fi
}

for t in $TARGETS; do
  case "$t" in
    arm64) build_target arm64 aarch64-linux-gnu arm64 "" ;;
    armhf) build_target armhf arm-linux-gnueabihf arm 7 ;;
    *) echo "unknown target '$t' (use: arm64 armhf)" >&2; exit 2 ;;
  esac
done

echo "== done =="
ls -la "$OUT"