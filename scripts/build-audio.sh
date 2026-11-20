#!/bin/sh
# Zephyr HiFi Kit — native Linux audio build (CGO + libFLAC + ALSA).
#
# build-release.sh cross-compiles the pure-Go server/agent without CGO. This
# script builds the audio-enabled binaries on a Linux host with libflac-dev and
# libasound2-dev installed, and can run the CGO audio tests. The ALSA "null"
# loopback test needs no sound hardware; the FLAC round-trip test needs the
# `flac` CLI (it skips when absent).
#
#   sh scripts/build-audio.sh            # build server + agent
#   TEST=1 sh scripts/build-audio.sh     # also run CGO audio tests
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

VERSION="${VERSION:-0.51}"
case "$VERSION" in
  [0-9]*) ;;
  *) echo "version must start with a digit" >&2; exit 1 ;;
esac
case "$VERSION" in
  *[!A-Za-z0-9._-]*|*..*) echo "unsafe version: $VERSION" >&2; exit 1 ;;
esac

# --- toolchain: prefer the bundled SDK, else fall back to PATH ---
if [ -x "$HOME/sdk/go/bin/go" ]; then
  export GOROOT="$HOME/sdk/go"
  export PATH="$GOROOT/bin:$PATH"
fi
command -v go >/dev/null 2>&1 || { echo "go toolchain not found on PATH" >&2; exit 1; }

# --- platform guard: the CGO audio backend is Linux-only ---
case "$(uname -s)" in
  Linux) ;;
  *)
    echo "build-audio.sh must run on Linux (CGO + libFLAC + ALSA)." >&2
    echo "On Windows/macOS use build-release.sh for the pure-Go build." >&2
    exit 1
    ;;
esac

# --- pkg-config probe for the audio libraries ---
command -v pkg-config >/dev/null 2>&1 || {
  echo "pkg-config not found; install pkg-config libflac-dev libasound2-dev" >&2
  exit 1
}
for lib in flac alsa; do
  if ! pkg-config --exists "$lib"; then
    echo "missing development package for '$lib' (Debian/Ubuntu: libflac-dev libasound2-dev)" >&2
    exit 1
  fi
  echo "== $lib $(pkg-config --modversion "$lib") =="
done

export CGO_ENABLED=1
export GO111MODULE=on
export GOFLAGS=-mod=mod

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  armv7l) ARCH=armhf ;;
  *) echo "unsupported native audio architecture: $(uname -m)" >&2; exit 1 ;;
esac
OUT="$ROOT/bin/audio/$VERSION"
NAME="Zephyr-HiFi-Kit-terminal-$VERSION-audio-$ARCH"
TERMINAL="$ROOT/release/$NAME"
TARBALL="$ROOT/release/$NAME.tar.gz"
for dir in "$OUT" "$TERMINAL"; do
  [ ! -L "$dir" ] || { echo "refusing symlink output: $dir" >&2; exit 1; }
  if [ -e "$dir" ]; then
    [ -d "$dir" ] && [ -z "$(find "$dir" -mindepth 1 -print -quit)" ] || {
      echo "refusing nonempty output: $dir" >&2; exit 1;
    }
  fi
done
[ ! -e "$TARBALL" ] && [ ! -L "$TARBALL" ] || { echo "refusing existing output: $TARBALL" >&2; exit 1; }
mkdir -p "$OUT" "$TERMINAL/agent"

echo "== building audio-enabled server =="
( cd "$ROOT" && go build -trimpath -ldflags "-X hifihub/server/internal/metrics.Version=$VERSION" -o "$OUT/hifihub-server" ./cmd/hifihub-server )

echo "== building audio-enabled agent =="
( cd "$ROOT/agent" && go build -trimpath -ldflags "-X hifihub/agent/internal/config.BuildVersion=$VERSION" -o "$OUT/hifihub-agent" ./cmd/hifihub-agent )

if [ "${TEST:-0}" = "1" ]; then
  echo "== CGO audio tests =="
  command -v flac >/dev/null 2>&1 || echo "note: 'flac' CLI absent; FLAC round-trip test will skip"
  ( cd "$ROOT" && go test ./internal/decode/... )
  ( cd "$ROOT/agent" && go test ./internal/player/... )
fi

# --- audio terminal bundle (internal deb name remains the /dl/ contract) ---
echo "== packaging audio agent deb ($ARCH) =="
( cd "$ROOT/agent" && go run ./cmd/debpack -audio \
    -bin "$OUT/hifihub-agent" -arch "$ARCH" -version "$VERSION" \
    -pkgdir packaging -out "$OUT/hifihub-agent_${VERSION}_${ARCH}.deb" )
cp "$OUT/hifihub-agent_${VERSION}_${ARCH}.deb" "$TERMINAL/agent/"
cp "$OUT/install.sh" "$TERMINAL/agent/install.sh"
cp "$OUT/install.sh" "$TERMINAL/agent/agent-install.sh"
( cd "$TERMINAL/agent" && sha256sum hifihub-agent_"$VERSION"_"$ARCH".deb agent-install.sh > SHA256SUMS )
cp "$ROOT/README.md" "$ROOT/README.cn.md" "$ROOT/README.en.md" "$ROOT/INSTALL.md" "$ROOT/LICENSE" "$TERMINAL/"
( cd "$TERMINAL" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS )
( cd "$ROOT/release" && tar -czf "$TARBALL" "$NAME" )
echo "terminal: $TARBALL"

echo "== done =="
ls -la "$OUT"