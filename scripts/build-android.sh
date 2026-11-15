#!/bin/sh
# Zephyr Music Player — build the Android terminal APK on a Linux host.
#
# Cross-builds the Go c-shared core (libzatagent.so) with the NDK clang toolchain
# and the AAudio backend, places it in the app's jniLibs, then assembles the
# Kotlin shell with Gradle. Requires: Go, Android NDK (r26+), Android SDK,
# JDK 17 and Gradle 8.7+ on PATH (the CI android job provisions these).
#
#   ANDROID_NDK_HOME=/opt/android-ndk ANDROID_HOME=/opt/android-sdk \
#     sh scripts/build-android.sh
#
# Signing: if KEYSTORE_FILE/KEYSTORE_PASSWORD/KEY_ALIAS/KEY_PASSWORD are set, a
# release keystore config is written and the APK is release-signed; otherwise the
# release build falls back to debug signing (still installable).
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

VERSION="${VERSION:-0.3.0}"
ABIS="${ABIS:-arm64-v8a armeabi-v7a}"

if [ -x "$HOME/sdk/go/bin/go" ]; then
  export GOROOT="$HOME/sdk"
  export PATH="$GOROOT/bin:$PATH"
fi
command -v go >/dev/null 2>&1 || { echo "go toolchain not found" >&2; exit 1; }

case "$(uname -s)" in
  Linux) ;;
  *) echo "build-android.sh must run on Linux (NDK cross CGO)." >&2; exit 1 ;;
esac

NDK="${ANDROID_NDK_HOME:-${ANDROID_NDK:-${NDK_HOME:-}}}"
[ -n "$NDK" ] || { echo "set ANDROID_NDK_HOME to the NDK root" >&2; exit 1; }
HOST_TAG="linux-x86_64"
LLVM="$NDK/toolchains/llvm/prebuilt/$HOST_TAG/bin"
[ -d "$LLVM" ] || { echo "NDK LLVM toolchain not found at $LLVM" >&2; exit 1; }

JNILIBS="$ROOT/android/app/src/main/jniLibs"
export CGO_ENABLED=1 GO111MODULE=on GOFLAGS=-mod=mod GOOS=android

build_abi() {
  abi="$1"; goarch="$2"; cc="$3"
  [ -x "$LLVM/$cc" ] || { echo "!! missing NDK compiler $cc" >&2; return 1; }
  echo "== $abi: Go c-shared core (CC=$cc) =="
  out="$JNILIBS/$abi"
  mkdir -p "$out"
  ( cd "$ROOT/agent" && CGO_ENABLED=1 GOOS=android GOARCH="$goarch" CC="$LLVM/$cc" \
      go build -buildmode=c-shared -trimpath \
      -ldflags "-s -w -extldflags=-Wl,-soname,libzatagent.so" \
      -o "$out/libzatagent.so" ./cmd/hifihub-agent-android )
  # The generated header is not needed by the JNI shim (it declares the exports).
  rm -f "$out/libzatagent.h"
}

for abi in $ABIS; do
  case "$abi" in
    arm64-v8a)   build_abi arm64-v8a   arm64 aarch64-linux-android26-clang ;;
    armeabi-v7a) build_abi armeabi-v7a arm   armv7a-linux-androideabi26-clang ;;
    *) echo "unknown ABI '$abi'" >&2; exit 2 ;;
  esac
done

# Optional release signing.
if [ -n "${KEYSTORE_FILE:-}" ]; then
  echo "== writing release keystore config =="
  cat > "$ROOT/android/keystore.properties" <<EOF
storeFile=$KEYSTORE_FILE
storePassword=$KEYSTORE_PASSWORD
keyAlias=$KEY_ALIAS
keyPassword=$KEY_PASSWORD
EOF
fi

echo "== assembling APK (Gradle) =="
command -v gradle >/dev/null 2>&1 || { echo "gradle not found on PATH" >&2; exit 1; }
( cd "$ROOT/android" && gradle --no-daemon assembleRelease )

APK="$ROOT/android/app/build/outputs/apk/release/app-release.apk"
[ -f "$APK" ] || { echo "!! APK not produced at $APK" >&2; exit 1; }
mkdir -p "$ROOT/release"
DEST="$ROOT/release/zephyr-terminal-$VERSION.apk"
cp "$APK" "$DEST"
echo "== done: $DEST =="
ls -la "$DEST"