#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "请使用 root 运行：sudo bash scripts/install-centos7.sh" >&2
  exit 1
fi

ROOT=$(cd "$(dirname "$0")/.." && pwd)
BIN=${NETTRAFFIC_BINARY:-$ROOT/dist/nettraffic-linux-amd64}
if [[ ! -x "$BIN" ]]; then
  echo "未找到 $BIN，请先执行 scripts/build-linux.sh" >&2
  exit 1
fi

install -D -m 0755 "$BIN" /usr/local/bin/nettraffic
install -D -m 0644 "$ROOT/deploy/nettraffic.service" /etc/systemd/system/nettraffic.service
install -D -m 0644 "$ROOT/deploy/99-nettraffic.conf" /etc/sysctl.d/99-nettraffic.conf
install -d -m 0750 /etc/nettraffic /var/lib/nettraffic
if [[ ! -f /etc/nettraffic/nettraffic.env ]]; then
  install -m 0600 "$ROOT/deploy/nettraffic.env.example" /etc/nettraffic/nettraffic.env
fi

modprobe nf_conntrack 2>/dev/null || true
sysctl --system >/dev/null
systemctl daemon-reload
systemctl enable --now nettraffic

echo "NetTraffic 已启动：http://$(hostname -I | awk '{print $1}'):8080"
echo "配置文件：/etc/nettraffic/nettraffic.env"

