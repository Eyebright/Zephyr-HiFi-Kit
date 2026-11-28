#!/bin/sh
# Zephyr HiFi Kit — package the hifihub-player MCP plugin for every supported
# platform.
#
# The plugin sources (manifest, skill, README) live in git; the MCP executable is
# built here so each release zip is install-and-run. Every zip carries its own
# .mcp.json naming the executable shipped inside it, so no host has to guess a
# platform-specific command. The plugin keeps its own 0.1.x version; the product
# version it was built against is stamped into the packaged README.
#
#   sh scripts/build-plugin.sh                                  # 3 platforms
#   VERSION=0.83 TARGETS=linux-arm64 sh scripts/build-plugin.sh
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
TARGETS="${TARGETS:-windows-amd64 linux-amd64 linux-arm64}"

if [ -x "$HOME/sdk/go/bin/go" ]; then
  export GOROOT="$HOME/sdk/go"
  export PATH="$GOROOT/bin:$PATH"
fi
command -v go >/dev/null 2>&1 || { echo "go toolchain not found on PATH" >&2; exit 1; }

PLUGIN_SRC="$ROOT/plugins/hifihub-player"
[ -d "$PLUGIN_SRC/.qoder-plugin" ] && [ -d "$PLUGIN_SRC/skills" ] \
  || { echo "plugin sources not found under $PLUGIN_SRC" >&2; exit 1; }
[ -f "$ROOT/cmd/hifihub-mcp/main.go" ] || { echo "cmd/hifihub-mcp not found" >&2; exit 1; }

RELEASE="$ROOT/release"
mkdir -p "$RELEASE"
STAGE_ROOT="$RELEASE/.plugin-stage/$VERSION"
require_new_file() {
  [ ! -e "$1" ] && [ ! -L "$1" ] || { echo "refusing existing output: $1" >&2; exit 1; }
}
for target in $TARGETS; do
  case "$target" in
    windows-amd64|linux-amd64|linux-arm64) ;;
    *) echo "unknown target '$target' (use: windows-amd64 linux-amd64 linux-arm64)" >&2; exit 2 ;;
  esac
  # The stage tree is pure intermediate output under release/, so a rerun may
  # replace it; the archives themselves are never overwritten.
  case "$STAGE_ROOT/$target" in
    "$RELEASE/.plugin-stage/"*) rm -rf "$STAGE_ROOT/$target" ;;
    *) echo "refusing to clean unexpected stage path: $STAGE_ROOT/$target" >&2; exit 1 ;;
  esac
  require_new_file "$RELEASE/Zephyr-HiFi-Kit-mcp-plugin-$VERSION-$target.zip"
done

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    command -v powershell.exe >/dev/null 2>&1
    command -v cygpath >/dev/null 2>&1
    ZIP_TOOL=powershell ;;
  *) command -v zip >/dev/null 2>&1 || { echo "install zip to build the plugin archives" >&2; exit 1; }
     ZIP_TOOL=zip ;;
esac

export CGO_ENABLED=0
export GO111MODULE=on
export GOFLAGS=-mod=mod

# The binary reports the manifest version, not a hand-maintained literal.
PLUGIN_VER=$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$PLUGIN_SRC/.qoder-plugin/plugin.json" | head -1)
[ -n "$PLUGIN_VER" ] || { echo "!! could not read version from $PLUGIN_SRC/.qoder-plugin/plugin.json" >&2; exit 1; }
echo "== plugin hifihub-player $PLUGIN_VER for server $VERSION =="

build_target() {
  target="$1"
  os="${target%%-*}"
  arch="${target##*-}"
  ext=""
  [ "$os" = windows ] && ext=".exe"
  name="Zephyr-HiFi-Kit-mcp-plugin-$VERSION-$target"
  stage="$STAGE_ROOT/$target/hifihub-player"
  mkdir -p "$stage/bin"

  echo "== $target: hifihub-mcp$ext =="
  ( cd "$ROOT" && GOOS="$os" GOARCH="$arch" GOARM= \
      go build -trimpath -ldflags "-s -w -X main.pluginVersion=$PLUGIN_VER" -o "$stage/bin/hifihub-mcp$ext" ./cmd/hifihub-mcp )

  cp "$PLUGIN_SRC/README.md" "$PLUGIN_SRC/.mcp.json" "$stage/"
  cp -R "$PLUGIN_SRC/.qoder-plugin" "$PLUGIN_SRC/skills" "$stage/"
  [ -f "$ROOT/LICENSE" ] && cp "$ROOT/LICENSE" "$stage/"

  # The packaged manifest points at the exact file inside this archive, and the
  # README records which product release the bundle was cut from.
  sed -i "s#/bin/hifihub-mcp\"#/bin/hifihub-mcp$ext\"#" "$stage/.mcp.json"
  sed -i "s#@ZEPHYR_PRODUCT_VERSION@#$VERSION#g" "$stage/README.md"
  grep -q "bin/hifihub-mcp$ext\"" "$stage/.mcp.json" \
    || { echo "!! packaged .mcp.json command not rewritten" >&2; exit 1; }
  if grep -q 'ZEPHYR_PRODUCT_VERSION' "$stage/README.md"; then
    echo "!! README product-version token was not replaced" >&2; exit 1
  fi

  ( cd "$stage" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > SHA256SUMS )
  ( cd "$stage" && sha256sum --check --strict SHA256SUMS >/dev/null )

  if [ "$ZIP_TOOL" = zip ]; then
    ( cd "$STAGE_ROOT/$target" && zip -q -r "$RELEASE/$name.zip" hifihub-player )
  else
    ZEPHYR_ZIP_SOURCE="$(cygpath -w "$stage")" \
    ZEPHYR_ZIP_DEST="$(cygpath -w "$RELEASE/$name.zip")" \
      powershell.exe -NoProfile -NonInteractive -Command '
        $ErrorActionPreference = "Stop"
        if (Test-Path -LiteralPath $env:ZEPHYR_ZIP_DEST) { throw "Archive already exists" }
        Compress-Archive -LiteralPath $env:ZEPHYR_ZIP_SOURCE -DestinationPath $env:ZEPHYR_ZIP_DEST
      '
  fi
}

for t in $TARGETS; do build_target "$t"; done

echo "== done =="
ls -la "$RELEASE"/Zephyr-HiFi-Kit-mcp-plugin-"$VERSION"-*.zip
