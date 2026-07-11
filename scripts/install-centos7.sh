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
new_environment=false
if [[ ! -f /etc/nettraffic/nettraffic.env ]]; then
  install -m 0600 "$ROOT/deploy/nettraffic.env.example" /etc/nettraffic/nettraffic.env
  new_environment=true
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
normalize_bool() {
  local value=${1:-}
  case "$value" in
    1|true|TRUE|True|t|T|yes|YES|y|Y|on|ON)
      echo true
      ;;
    0|false|FALSE|False|f|F|no|NO|n|N|off|OFF)
      echo false
      ;;
    *)
      return 1
      ;;
  esac
}

valid_interval() {
  local value=${1:-}
  [[ "$value" =~ ^([0-9]+([.][0-9]+)?(ns|us|ms|s|m|h))+$ && "$value" =~ [1-9] ]]
}

read_env_value() {
  local key=$1
  awk -F= -v key="$key" '$1 == key {sub(/^[^=]*=/, ""); value=$0; sub(/^[[:space:]]+/, "", value); sub(/[[:space:]]+$/, "", value); first=substr(value, 1, 1); last=substr(value, length(value), 1); quote=sprintf("%c", 39); if ((first == "\"" && last == "\"") || (first == quote && last == quote)) value=substr(value, 2, length(value)-2)} END {print value}' /etc/nettraffic/nettraffic.env
}

interval_override=false
if [[ -n "${NETTRAFFIC_DESTINATIONS_ENABLED:-}" ]]; then
  destination_enabled=$(normalize_bool "$NETTRAFFIC_DESTINATIONS_ENABLED") || {
    echo "NETTRAFFIC_DESTINATIONS_ENABLED 只能设置为 true 或 false。" >&2
    exit 1
  }
  set_env_value NETTRAFFIC_DESTINATIONS_ENABLED "$destination_enabled"
fi
if [[ -n "${NETTRAFFIC_RESOLVE_HOSTNAMES:-}" ]]; then
  resolve_hostnames=$(normalize_bool "$NETTRAFFIC_RESOLVE_HOSTNAMES") || {
    echo "NETTRAFFIC_RESOLVE_HOSTNAMES 只能设置为 true 或 false。" >&2
    exit 1
  }
  set_env_value NETTRAFFIC_RESOLVE_HOSTNAMES "$resolve_hostnames"
fi
if [[ -n "${NETTRAFFIC_INTERVAL:-}" ]]; then
  if ! valid_interval "$NETTRAFFIC_INTERVAL"; then
    echo "NETTRAFFIC_INTERVAL 不是有效的 Go duration，例如 5s、10s 或 1m。" >&2
    exit 1
  fi
  set_env_value NETTRAFFIC_INTERVAL "$NETTRAFFIC_INTERVAL"
  interval_override=true
fi

destination_enabled=$(read_env_value NETTRAFFIC_DESTINATIONS_ENABLED)
destination_enabled=$(normalize_bool "${destination_enabled:-false}") || {
  echo "/etc/nettraffic/nettraffic.env 中的 NETTRAFFIC_DESTINATIONS_ENABLED 无效。" >&2
  exit 1
}
set_env_value NETTRAFFIC_DESTINATIONS_ENABLED "$destination_enabled"

resolve_hostnames=$(read_env_value NETTRAFFIC_RESOLVE_HOSTNAMES)
resolve_hostnames=$(normalize_bool "${resolve_hostnames:-false}") || {
  echo "/etc/nettraffic/nettraffic.env 中的 NETTRAFFIC_RESOLVE_HOSTNAMES 无效。" >&2
  exit 1
}
set_env_value NETTRAFFIC_RESOLVE_HOSTNAMES "$resolve_hostnames"

interval=$(read_env_value NETTRAFFIC_INTERVAL)
if [[ -z "$interval" ]]; then
  if [[ "$destination_enabled" == "true" ]]; then
    interval=10s
  else
    interval=5s
  fi
fi
if ! valid_interval "$interval"; then
  echo "/etc/nettraffic/nettraffic.env 中的 NETTRAFFIC_INTERVAL 无效：${interval}" >&2
  exit 1
fi
if [[ "$interval_override" == "false" ]]; then
  if [[ "$interval" == "2s" ]]; then
    if [[ "$destination_enabled" == "true" ]]; then
      interval=10s
    else
      interval=5s
    fi
  elif [[ "$new_environment" == "true" && "$destination_enabled" == "true" && "$interval" == "5s" ]]; then
    interval=10s
  fi
fi
set_env_value NETTRAFFIC_INTERVAL "$interval"

if [[ "$destination_enabled" == "true" ]]; then
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
