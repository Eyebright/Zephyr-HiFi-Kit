#!/usr/bin/env bash
# HiFiHub terminal agent installer (Debian/Ubuntu, systemd).
#
#   sudo bash install.sh --server https://music.example.com --token <one-time-token>
#
# Options:
#   --server URL   management server base URL (required)
#   --token TOKEN  one-time registration token from the server console (required)
#   --bind ADDR    local console bind address (default 0.0.0.0, port 8080)
#   --deb PATH     install a local .deb instead of downloading from the server
#
# The release build bakes the package version into @VERSION@ below.
# Re-running this script is safe: unrelated keys in /etc/hifihub/agent.env are
# preserved and the service is simply re-enabled and restarted.
set -euo pipefail

VERSION="@VERSION@"
SERVER=""
TOKEN=""
BIND="0.0.0.0"
DEB=""

usage() {
  cat <<'EOF'
Usage: install.sh --server URL --token TOKEN [--bind ADDR] [--deb PATH]
  --server URL   HiFiHub management server base URL (required)
  --token TOKEN  one-time registration token (required)
  --bind ADDR    local console bind address (default 0.0.0.0; port 8080)
  --deb PATH     install this local .deb instead of downloading
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --server|--token|--bind|--deb)
      if [ $# -lt 2 ]; then
        echo "Missing value for $1" >&2
        exit 2
      fi
      case "$1" in
        --server) SERVER="$2" ;;
        --token)  TOKEN="$2" ;;
        --bind)   BIND="$2" ;;
        --deb)    DEB="$2" ;;
      esac
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [ "$(id -u)" -ne 0 ]; then
  echo "Please run as root, e.g. sudo bash install.sh ..." >&2
  exit 1
fi
if [ -z "$SERVER" ] || [ -z "$TOKEN" ]; then
  echo "--server and --token are required." >&2
  usage >&2
  exit 2
fi
SERVER="${SERVER%/}"

case "$(uname -m)" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  armv7l)  ARCH="armhf" ;;
  *)
    echo "Unsupported architecture: $(uname -m) (need x86_64, aarch64 or armv7l)." >&2
    exit 1
    ;;
esac

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if [ -n "$DEB" ]; then
  if [ ! -f "$DEB" ]; then
    echo "--deb file not found: $DEB" >&2
    exit 1
  fi
  PKG="$DEB"
else
  if [ "$VERSION" = "@VERSION@" ]; then
    echo "This copy of install.sh has no baked-in version." >&2
    echo "Use a release copy, or pass --deb PATH to install a local package." >&2
    exit 1
  fi
  PKG="$TMP/hifihub-agent_${VERSION}_${ARCH}.deb"
  echo "Downloading $SERVER/dl/hifihub-agent_${VERSION}_${ARCH}.deb ..."
  curl -fsSL "$SERVER/dl/hifihub-agent_${VERSION}_${ARCH}.deb" -o "$PKG"
fi

echo "Installing hifihub-agent ${VERSION} (${ARCH}) ..."
# dpkg may fail on missing dependencies; apt-get -f completes the install.
# --force-confold keeps the operator's agent.env: this script rewrites the
# managed keys below, and a conffile prompt would hang a noninteractive install.
dpkg -i --force-confold "$PKG" ||
  apt-get -f install -y -o Dpkg::Options::="--force-confold"
if ! dpkg -s hifihub-agent 2>/dev/null | grep -q '^Status: install ok installed'; then
  echo "hifihub-agent installation failed; see the output above." >&2
  exit 1
fi

# Write the managed keys while preserving every unrelated key the operator
# added. LISTEN_ADDR is also managed because it would override LOCAL_BIND.
# The token is written to a 0600 root-owned file and never printed.
ENV_FILE="/etc/hifihub/agent.env"
mkdir -p /etc/hifihub
if [ -f "$ENV_FILE" ]; then
  grep -vE '^[[:space:]]*(HIFIHUB_AGENT_)?(SERVER_URL|TOKEN|LOCAL_BIND|LISTEN_ADDR)[[:space:]]*=' \
    "$ENV_FILE" > "$TMP/agent.env" || true
else
  : > "$TMP/agent.env"
fi
{
  echo "HIFIHUB_AGENT_SERVER_URL=$SERVER"
  echo "HIFIHUB_AGENT_TOKEN=$TOKEN"
  echo "HIFIHUB_AGENT_LOCAL_BIND=$BIND"
} >> "$TMP/agent.env"
install -m 0600 -o root -g root "$TMP/agent.env" "$ENV_FILE"

systemctl daemon-reload
systemctl enable --now hifihub-agent.service
systemctl restart hifihub-agent.service

IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
if [ -z "$IP" ]; then
  IP="<terminal-ip>"
fi
echo
echo "hifihub-agent is installed and running."
echo "  Local console : http://${IP}:8080  (bind ${BIND}:8080)"
echo "  Service state : systemctl status hifihub-agent"
echo "  Live logs     : journalctl -u hifihub-agent -f"
echo "Next: open the server console, confirm this terminal registered and is"
echo "online, then install the audio services you need from its catalog."