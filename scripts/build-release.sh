#!/bin/sh
# Zephyr HiFi Kit — pure-Go Linux management + Windows release builder.
# Internal hifihub names are retained for the Linux installer and /dl/ clients.
#   sh scripts/build-release.sh              # default version 0.54
#   VERSION=0.54 sh scripts/build-release.sh
#   AUDIO=1 sh scripts/build-release.sh      # native Linux audio bundles
set -eu

VERSION="${VERSION:-0.54}"
case "$VERSION" in
  [0-9]*) ;;
  *) echo "version must start with a digit" >&2; exit 1 ;;
esac
case "$VERSION" in
  *[!A-Za-z0-9._-]*|*..*) echo "unsafe version: $VERSION" >&2; exit 1 ;;
esac
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
RELEASE="$ROOT/release"

# Never remove or overwrite a prior (including partially built) release.
require_empty_dir() {
  [ ! -L "$1" ] || { echo "refusing symlink output: $1" >&2; exit 1; }
  if [ -e "$1" ]; then
    [ -d "$1" ] && [ -z "$(find "$1" -mindepth 1 -print -quit)" ] || {
      echo "refusing nonempty output: $1" >&2; exit 1;
    }
  fi
}
require_new_file() {
  [ ! -e "$1" ] && [ ! -L "$1" ] || { echo "refusing existing output: $1" >&2; exit 1; }
}
copy_docs() {
  cp "$ROOT/README.md" "$ROOT/README.cn.md" "$ROOT/README.en.md" "$ROOT/INSTALL.md" "$ROOT/LICENSE" "$1/"
}
checksums() {
  ( cd "$1" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS )
}
server_files() {
  cp "$ROOT/packaging/server/install.sh" "$ROOT/packaging/server/uninstall.sh" \
    "$ROOT/packaging/server/server.env" "$ROOT/packaging/server/hifihub-server.service" "$1/"
  chmod +x "$1/install.sh" "$1/uninstall.sh"
}

if [ -x "$HOME/sdk/go/bin/go" ]; then
  export GOROOT="$HOME/sdk/go"
  export PATH="$GOROOT/bin:$PATH"
fi
export CGO_ENABLED=0 GO111MODULE=on GOFLAGS=-mod=mod
command -v go >/dev/null 2>&1 || { echo "go toolchain not found on PATH" >&2; exit 1; }
echo "== toolchain =="; go version

# build-audio.sh also emits the separate terminal audio tarball. No audio deb
# is published under the management deb's external name.
if [ "${AUDIO:-0}" = "1" ]; then
  case "$(uname -s)" in
    Linux) ;;
    *) echo "AUDIO=1 requires Linux (CGO + libFLAC + ALSA)." >&2; exit 1 ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) NATIVE=amd64 ;;
    aarch64|arm64) NATIVE=arm64 ;;
    *) echo "AUDIO=1 server bundles target amd64/arm64; use build-audio-cross.sh for armhf terminals." >&2; exit 1 ;;
  esac
  NAME="Zephyr-HiFi-Kit-server-$VERSION-audio-$NATIVE"
  OUT="$RELEASE/$NAME"
  TARBALL="$RELEASE/$NAME.tar.gz"
  require_empty_dir "$OUT"
  require_new_file "$TARBALL"
  VERSION="$VERSION" TEST="${TEST:-0}" sh "$ROOT/scripts/build-audio.sh"
  mkdir -p "$OUT/server/dist"
  cp "$ROOT/bin/audio/$VERSION/hifihub-server" "$OUT/server/hifihub-server-linux-$NATIVE"
  server_files "$OUT/server"
  cp -R "$RELEASE/Zephyr-HiFi-Kit-terminal-$VERSION-audio-$NATIVE/agent" "$OUT/agent"
  cp "$OUT/agent/hifihub-agent_${VERSION}_${NATIVE}.deb" "$OUT/agent/agent-install.sh" \
    "$OUT/agent/SHA256SUMS" "$OUT/server/dist/"
  copy_docs "$OUT"
  checksums "$OUT"
  ( cd "$RELEASE" && tar -czf "$TARBALL" "$NAME" )
  echo "== done: $TARBALL =="
  exit 0
fi

NAME="Zephyr-HiFi-Kit-server-$VERSION"
TERMINAL_NAME="Zephyr-HiFi-Kit-terminal-$VERSION-linux"
WINDOWS_NAME="Zephyr-HiFi-Kit-server-$VERSION-windows-amd64"
OUT="$RELEASE/$NAME"
TERMINAL="$RELEASE/$TERMINAL_NAME"
WINDOWS="$RELEASE/$WINDOWS_NAME"
TARBALL="$RELEASE/$NAME.tar.gz"
TERMINAL_TAR="$RELEASE/$TERMINAL_NAME.tar.gz"
WINDOWS_ZIP="$RELEASE/$WINDOWS_NAME.zip"
for dir in "$OUT" "$TERMINAL" "$WINDOWS"; do require_empty_dir "$dir"; done
for file in "$TARBALL" "$TERMINAL_TAR" "$WINDOWS_ZIP"; do require_new_file "$file"; done
for arch in amd64 arm64 armhf; do
  require_new_file "$RELEASE/$TERMINAL_NAME-$arch.deb"
done
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    command -v powershell.exe >/dev/null 2>&1
    command -v cygpath >/dev/null 2>&1
    ZIP_TOOL=powershell ;;
  *) command -v zip >/dev/null 2>&1 || { echo "install zip to build the Windows archive" >&2; exit 1; }
     ZIP_TOOL=zip ;;
esac
mkdir -p "$OUT/server/dist" "$OUT/agent" "$TERMINAL" "$WINDOWS"

# Embedded console; only the public verifier is part of these binaries.
echo "== building servers =="
LDFLAGS="-s -w -X hifihub/server/internal/metrics.Version=$VERSION"
for arch in amd64 arm64; do
  GOOS=linux GOARCH="$arch" go build -trimpath -ldflags "$LDFLAGS" \
    -o "$OUT/server/hifihub-server-linux-$arch" ./cmd/hifihub-server
done
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" \
  -o "$WINDOWS/Zephyr-HiFi-Kit-server.exe" ./cmd/hifihub-server
server_files "$OUT/server"
cp "$ROOT/packaging/server/start.ps1" "$WINDOWS/start.ps1"

# Stage intermediate binaries in the guarded version directory, not a shared
# agent/bin output. debpack runs on the host, never with a cross GOOS/GOARCH.
echo "== building agents =="
build_agent() {
  arch="$1"; goarch="$2"; goarm="$3"
  bin="$OUT/agent/hifihub-agent-linux-$arch"
  ( cd "$ROOT/agent" && GOOS=linux GOARCH="$goarch" GOARM="$goarm" \
      go build -trimpath -ldflags "-s -w -X hifihub/agent/internal/config.BuildVersion=$VERSION" -o "$bin" ./cmd/hifihub-agent )
  ( cd "$ROOT/agent" && go run ./cmd/debpack \
      -bin "$bin" -arch "$arch" -version "$VERSION" \
      -pkgdir packaging -out "$OUT/agent/hifihub-agent_${VERSION}_${arch}.deb" )
  rm "$bin"
  # External release name only: Package and /dl/ filenames stay compatible.
  cp "$OUT/agent/hifihub-agent_${VERSION}_${arch}.deb" "$RELEASE/$TERMINAL_NAME-$arch.deb"
}
build_agent amd64 amd64 ""
build_agent arm64 arm64 ""
build_agent armhf arm 7

# debpack already bakes the installer version without replacing its sentinel.
cp "$OUT/agent/install.sh" "$OUT/agent/agent-install.sh"
cp "$OUT"/agent/hifihub-agent_"$VERSION"_*.deb "$OUT/agent/agent-install.sh" "$OUT/server/dist/"
( cd "$OUT/server/dist" && sha256sum hifihub-agent_"$VERSION"_*.deb agent-install.sh > SHA256SUMS )
cp "$OUT/server/dist/SHA256SUMS" "$OUT/agent/SHA256SUMS"
cp -R "$OUT/agent" "$TERMINAL/agent"
cp -R "$OUT/server/dist" "$WINDOWS/dist"
for dir in "$OUT" "$TERMINAL" "$WINDOWS"; do
  copy_docs "$dir"
  checksums "$dir"
done

( cd "$RELEASE" && tar -czf "$TARBALL" "$NAME" )
( cd "$RELEASE" && tar -czf "$TERMINAL_TAR" "$TERMINAL_NAME" )
if [ "$ZIP_TOOL" = zip ]; then
  ( cd "$RELEASE" && zip -q -r "$WINDOWS_ZIP" "$WINDOWS_NAME" )
else
  # Pass paths as data, not interpolated PowerShell code; LiteralPath also
  # protects spaces, non-ASCII characters and wildcard characters in paths.
  ZEPHYR_ZIP_SOURCE="$(cygpath -w "$WINDOWS")" \
  ZEPHYR_ZIP_DEST="$(cygpath -w "$WINDOWS_ZIP")" \
    powershell.exe -NoProfile -NonInteractive -Command '
      $ErrorActionPreference = "Stop"
      if (Test-Path -LiteralPath $env:ZEPHYR_ZIP_DEST) { throw "Archive already exists" }
      Compress-Archive -LiteralPath $env:ZEPHYR_ZIP_SOURCE -DestinationPath $env:ZEPHYR_ZIP_DEST
    '
fi
echo "== done =="
ls -la "$TARBALL" "$TERMINAL_TAR" "$WINDOWS_ZIP" "$RELEASE"/"$TERMINAL_NAME"-*.deb
