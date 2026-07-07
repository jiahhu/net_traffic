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
install -d -m 0750 /etc/nettraffic /var/lib/nettraffic
if [[ ! -f /etc/nettraffic/nettraffic.env ]]; then
  install -m 0600 "$ROOT/deploy/nettraffic.env.example" /etc/nettraffic/nettraffic.env
fi
set_env_value() {
  local key=$1
  local value=$2
  local file=/etc/nettraffic/nettraffic.env

  if grep -q "^${key}=" "$file"; then
    sed -i "s|^${key}=.*|${key}=${value}|" "$file"
  else
    echo "${key}=${value}" >>"$file"
  fi
}
if [[ -n "${NETTRAFFIC_DESTINATIONS_ENABLED:-}" ]]; then
  set_env_value NETTRAFFIC_DESTINATIONS_ENABLED "$NETTRAFFIC_DESTINATIONS_ENABLED"
fi

env_bool() {
  local value=${1:-}
  case "$value" in
    1|true|TRUE|yes|YES|y|Y|on|ON)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}
destination_enabled=$(awk -F= '/^NETTRAFFIC_DESTINATIONS_ENABLED=/ {print $2}' /etc/nettraffic/nettraffic.env | tail -n 1)
if [[ -z "$destination_enabled" ]]; then
  destination_enabled=true
  set_env_value NETTRAFFIC_DESTINATIONS_ENABLED "$destination_enabled"
fi
if env_bool "$destination_enabled"; then
  install -D -m 0644 "$ROOT/deploy/99-nettraffic.conf" /etc/sysctl.d/99-nettraffic.conf
  modprobe nf_conntrack 2>/dev/null || true
  sysctl -p /etc/sysctl.d/99-nettraffic.conf >/dev/null
else
  rm -f /etc/sysctl.d/99-nettraffic.conf
  sysctl -w net.netfilter.nf_conntrack_acct=0 >/dev/null 2>&1 || true
fi

systemctl daemon-reload
systemctl enable nettraffic
systemctl restart nettraffic

echo "NetTraffic 已启动：http://$(hostname -I | awk '{print $1}'):8080"
echo "配置文件：/etc/nettraffic/nettraffic.env"
