#!/bin/sh
# Zephyr Music Server uninstaller. Removes the binary and systemd unit but
# PRESERVES configuration and data (artifacts/db) so reinstall is non-destructive.
set -eu

[ "$(id -u)" -eq 0 ] || { echo "请以 root 运行：sudo ./uninstall.sh" >&2; exit 1; }

systemctl stop hifihub-server.service 2>/dev/null || true
systemctl disable hifihub-server.service 2>/dev/null || true
rm -f /etc/systemd/system/hifihub-server.service
systemctl daemon-reload
rm -f /usr/bin/hifihub-server

echo "已移除服务端二进制与 systemd 单元。"
echo "配置 /etc/hifihub/server.env 与数据 /var/lib/hifihub-server 已保留。"
echo "如需彻底清除（不可恢复）："
echo "  rm -rf /etc/hifihub/server.env /var/lib/hifihub-server && userdel hifihub-server"
