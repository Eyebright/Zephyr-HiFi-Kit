#!/bin/sh
# Zephyr HiFi Kit installer — x86_64 / aarch64 Linux with systemd.
# Installs the single-file binary, a hardened systemd unit, an editable
# conffile and the GPG trust-root home used by enforce-mode verification.
set -eu

CONF_DIR="/etc/hifihub"
DATA_DIR="/var/lib/hifihub-server"
SVC_USER="hifihub-server"
UNIT="hifihub-server.service"
BIN_DST="/usr/bin/hifihub-server"

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

log() { printf '\033[1;36m[zephyr-server]\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "请以 root 运行：sudo ./install.sh"

# --- pick the binary for this architecture ---
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64)  BIN="$SCRIPT_DIR/hifihub-server-linux-amd64" ;;
  aarch64|arm64) BIN="$SCRIPT_DIR/hifihub-server-linux-arm64" ;;
  *) die "不支持的架构：$ARCH（服务端支持 x86_64 / aarch64）" ;;
esac
[ -f "$BIN" ] || die "找不到服务端二进制：$BIN（请在解压后的 server/ 目录内运行）"

command -v systemctl >/dev/null 2>&1 || die "需要 systemd：未找到 systemctl"

if ! command -v gpg >/dev/null 2>&1; then
  log "警告：未找到 gpg。enforce 模式依赖 gpg 校验制品签名。"
  log "      安装：apt-get install -y gnupg   （或 dnf install -y gnupg2）"
fi

if ! command -v mount.cifs >/dev/null 2>&1; then
  log "警告：未找到 mount.cifs。SMB/CIFS 媒体库挂载依赖 cifs-utils。"
  log "      安装：apt-get install -y cifs-utils   （或 dnf install -y cifs-utils）"
fi

# --- dedicated unprivileged service user ---
if ! id "$SVC_USER" >/dev/null 2>&1; then
  NOLOGIN=$(command -v nologin || echo /usr/sbin/nologin)
  log "创建系统用户 $SVC_USER"
  useradd --system --home-dir "$DATA_DIR" --no-create-home --shell "$NOLOGIN" "$SVC_USER"
fi

# --- binary ---
log "安装二进制 -> $BIN_DST"
install -m 0755 "$BIN" "$BIN_DST"

# --- audio runtime library (only the CGO server links libFLAC for FLAC decode) ---
if command -v ldd >/dev/null 2>&1 && ldd "$BIN_DST" 2>/dev/null | grep -qi 'libFLAC.*not found'; then
  log "警告：音频服务端需要 libFLAC 运行库，当前缺失，FLAC 解码将无法启动。"
  log "      安装：apt-get install -y libflac8   （Ubuntu 24.04+ 为 libflac12）"
fi

# --- conffile (preserve any existing edits) ---
mkdir -p "$CONF_DIR"
if [ ! -f "$CONF_DIR/server.env" ]; then
  install -m 0644 "$SCRIPT_DIR/server.env" "$CONF_DIR/server.env"
  log "写入默认配置 $CONF_DIR/server.env"
else
  log "保留已有配置 $CONF_DIR/server.env（未覆盖）"
fi

# --- state dirs + GPG trust-root home + distribution dir ---
mkdir -p "$DATA_DIR/artifacts" "$DATA_DIR/gnupg" "$DATA_DIR/dist"
chmod 700 "$DATA_DIR/gnupg"
chown -R "$SVC_USER:$SVC_USER" "$DATA_DIR"

# Install the agent distribution set (debs + agent-install.sh + SHA256SUMS)
# served by the server at /dl/. Ships beside this script in the release bundle.
if [ -d "$SCRIPT_DIR/dist" ]; then
  log "安装终端分发文件 -> $DATA_DIR/dist"
  cp -a "$SCRIPT_DIR/dist/." "$DATA_DIR/dist/"
  chown -R "$SVC_USER:$SVC_USER" "$DATA_DIR/dist"
else
  log "未发现 dist/：请将终端安装包放入 $DATA_DIR/dist（通过 /dl/ 下发）。"
fi

# Import the signing trust root if it ships beside this script.
if [ -f "$SCRIPT_DIR/trust-root.asc" ]; then
  log "导入签名信任根公钥 trust-root.asc"
  GNUPGHOME="$DATA_DIR/gnupg" gpg --batch --import "$SCRIPT_DIR/trust-root.asc" \
    || die "公钥导入失败，请检查 trust-root.asc"
  chown -R "$SVC_USER:$SVC_USER" "$DATA_DIR/gnupg"
else
  log "未发现 trust-root.asc：enforce 模式下需先导入签名公钥（见 INSTALL.md 第 3 节）。"
fi

# --- systemd unit ---
log "安装 systemd 单元 $UNIT"
install -m 0644 "$SCRIPT_DIR/$UNIT" "/etc/systemd/system/$UNIT"
systemctl daemon-reload
systemctl enable "$UNIT"
systemctl restart "$UNIT"

sleep 1
systemctl --no-pager --full status "$UNIT" 2>/dev/null || true

log "完成。服务端监听 :8080，签名模式 enforce。"
log "健康检查：curl -s http://127.0.0.1:8080/api/v1/health"
log "下一步：导入签名公钥 -> 上传已签名制品 -> 签发终端注册令牌（见 INSTALL.md）。"
