#!/usr/bin/env bash
set -Eeuo pipefail

PS4='+ [$(date "+%Y-%m-%d %H:%M:%S")] ${BASH_SOURCE##*/}:${LINENO}: '
if [[ "${TRACE:-1}" != "0" ]]; then
  set -x
fi

trace_on() {
  if [[ "${TRACE:-1}" != "0" ]]; then
    set -x
  fi
}

trace_off() {
  set +x
}

on_error() {
  local rc=$?
  set +x
  echo
  echo "NetTraffic 一键安装失败，退出码：$rc" >&2
  echo "请从上方最后一条带 + 的命令开始排查。" >&2
  exit "$rc"
}
trap on_error ERR

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "请使用 root 运行：sudo bash scripts/onekey-install-centos7.sh" >&2
  exit 1
fi

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

if [[ "$(uname -m)" != "x86_64" ]]; then
  echo "当前脚本只支持 x86_64/amd64 服务器。" >&2
  exit 1
fi

if [[ -r /etc/centos-release ]]; then
  cat /etc/centos-release
fi

if [[ -r /etc/os-release ]]; then
  # shellcheck disable=SC1091
  . /etc/os-release
  if [[ "${ID:-}" != "centos" || "${VERSION_ID%%.*}" != "7" ]]; then
    echo "警告：当前系统不是 CentOS 7，脚本会继续执行，但请自行确认兼容性。" >&2
  fi
fi

version_ge() {
  local left=$1
  local right=$2
  local IFS=.
  local -a left_parts right_parts
  local i left_num right_num

  read -r -a left_parts <<<"$left"
  read -r -a right_parts <<<"$right"

  for i in 0 1 2; do
    left_num=${left_parts[$i]:-0}
    right_num=${right_parts[$i]:-0}
    left_num=${left_num%%[^0-9]*}
    right_num=${right_num%%[^0-9]*}
    [[ -z "$left_num" ]] && left_num=0
    [[ -z "$right_num" ]] && right_num=0

    if ((10#$left_num > 10#$right_num)); then
      return 0
    fi
    if ((10#$left_num < 10#$right_num)); then
      return 1
    fi
  done

  return 0
}

current_go_version() {
  if command -v go >/dev/null 2>&1; then
    go version | awk '{print $3}' | sed 's/^go//'
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

read_installed_env() {
  local key=$1
  local file=/etc/nettraffic/nettraffic.env

  [[ -r "$file" ]] || return 0
  awk -F= -v key="$key" '$1 == key {sub(/^[^=]*=/, ""); value=$0; sub(/^[[:space:]]+/, "", value); sub(/[[:space:]]+$/, "", value); first=substr(value, 1, 1); last=substr(value, length(value), 1); quote=sprintf("%c", 39); if ((first == "\"" && last == "\"") || (first == quote && last == quote)) value=substr(value, 2, length(value)-2)} END {print value}' "$file"
}

choose_destination_tracking() {
  local answer
  local installed
  local normalized

  if [[ -n "${NETTRAFFIC_DESTINATIONS_ENABLED:-}" ]]; then
    normalized=$(normalize_bool "$NETTRAFFIC_DESTINATIONS_ENABLED") || {
      echo "NETTRAFFIC_DESTINATIONS_ENABLED 只能设置为 true 或 false。" >&2
      return 1
    }
    export NETTRAFFIC_DESTINATIONS_ENABLED="$normalized"
    echo "目标网站排行：${NETTRAFFIC_DESTINATIONS_ENABLED}"
    return 0
  fi

  if [[ -t 0 ]]; then
    trace_off
    echo
    echo "是否开启目标网站排行？"
    echo "  开启：页面显示目标网站/IP排行，会读取 conntrack；连接数很高时有额外开销。"
    echo "  关闭：实时流量、总流量、每日图表仍然可用；Trojan/高连接数服务器建议关闭。"
    read -r -p "开启目标网站排行？[y/N]: " answer
    trace_on
    if [[ "$answer" =~ ^([yY]|yes|YES)$ ]]; then
      export NETTRAFFIC_DESTINATIONS_ENABLED=true
    else
      export NETTRAFFIC_DESTINATIONS_ENABLED=false
    fi
  else
    installed=$(read_installed_env NETTRAFFIC_DESTINATIONS_ENABLED)
    if normalized=$(normalize_bool "$installed"); then
      export NETTRAFFIC_DESTINATIONS_ENABLED="$normalized"
      echo "非交互更新，保留已安装的目标网站排行配置。"
    else
      export NETTRAFFIC_DESTINATIONS_ENABLED=false
      echo "非交互新安装，默认关闭目标网站排行。可设置 NETTRAFFIC_DESTINATIONS_ENABLED=true 开启。"
    fi
  fi

  echo "目标网站排行：${NETTRAFFIC_DESTINATIONS_ENABLED}"
}

configure_runtime_defaults() {
  local existing_interval
  local interval_default=5s
  local installed_resolve
  local normalized

  if [[ "$NETTRAFFIC_DESTINATIONS_ENABLED" == "true" ]]; then
    interval_default=10s
  fi
  if [[ -z "${NETTRAFFIC_INTERVAL:-}" ]]; then
    existing_interval=$(read_installed_env NETTRAFFIC_INTERVAL)
    if valid_interval "$existing_interval"; then
      if [[ "$existing_interval" == "2s" ]]; then
        export NETTRAFFIC_INTERVAL="$interval_default"
      else
        export NETTRAFFIC_INTERVAL="$existing_interval"
      fi
    else
      export NETTRAFFIC_INTERVAL="$interval_default"
    fi
  fi
  if ! valid_interval "$NETTRAFFIC_INTERVAL"; then
    echo "NETTRAFFIC_INTERVAL 不是有效的 Go duration，例如 5s、10s 或 1m。" >&2
    return 1
  fi
  if [[ -z "${NETTRAFFIC_RESOLVE_HOSTNAMES:-}" ]]; then
    installed_resolve=$(read_installed_env NETTRAFFIC_RESOLVE_HOSTNAMES)
    if normalized=$(normalize_bool "$installed_resolve"); then
      export NETTRAFFIC_RESOLVE_HOSTNAMES="$normalized"
    fi
  fi
  normalized=$(normalize_bool "${NETTRAFFIC_RESOLVE_HOSTNAMES:-false}") || {
    echo "NETTRAFFIC_RESOLVE_HOSTNAMES 只能设置为 true 或 false。" >&2
    return 1
  }
  export NETTRAFFIC_RESOLVE_HOSTNAMES="$normalized"

  echo "采样间隔：${NETTRAFFIC_INTERVAL}"
  echo "目标地址反向 DNS：${NETTRAFFIC_RESOLVE_HOSTNAMES}"
}

install_go_if_needed() {
  local min_version=${GO_MIN_VERSION:-1.25.0}
  local install_version=${GO_INSTALL_VERSION:-1.25.4}
  local current_version
  local tarball
  local url
  local tmp_file

  current_version=$(current_go_version || true)
  if [[ -n "$current_version" ]] && version_ge "$current_version" "$min_version"; then
    go version
    return 0
  fi

  tarball="go${install_version}.linux-amd64.tar.gz"
  url="https://go.dev/dl/${tarball}"
  tmp_file="/tmp/${tarball}"

  curl -fL --retry 3 --connect-timeout 20 -o "$tmp_file" "$url"
  if [[ -n "${GO_SHA256:-}" ]]; then
    echo "${GO_SHA256}  ${tmp_file}" | sha256sum -c -
  fi
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "$tmp_file"
  export PATH="/usr/local/go/bin:$PATH"
  go version
}

yum install -y git curl tar make ca-certificates
if command -v update-ca-trust >/dev/null 2>&1; then
  update-ca-trust
fi

choose_destination_tracking
configure_runtime_defaults
install_go_if_needed

export PATH="/usr/local/go/bin:$PATH"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

go mod download
go test ./...
bash "$ROOT/scripts/build-linux.sh"
bash -x "$ROOT/scripts/install-centos7.sh"

normalize_firewall_port() {
  local port=$1
  if [[ "$port" == */* ]]; then
    echo "$port"
  else
    echo "${port}/tcp"
  fi
}

detect_firewall_zone() {
  local scope=$1
  local iface
  local zone

  if [[ -n "${NETTRAFFIC_FIREWALL_ZONE:-}" ]]; then
    echo "$NETTRAFFIC_FIREWALL_ZONE"
    return 0
  fi
  iface=$(ip -4 route show default 2>/dev/null | awk 'NR == 1 {for (i = 1; i <= NF; i++) if ($i == "dev") {print $(i + 1); exit}}' || true)
  if [[ -n "$iface" ]]; then
    if [[ "$scope" == "permanent" ]]; then
      zone=$(firewall-cmd --permanent --get-zone-of-interface="$iface" 2>/dev/null || true)
    else
      zone=$(firewall-cmd --get-zone-of-interface="$iface" 2>/dev/null || true)
    fi
    if [[ -n "$zone" && "$zone" != "no zone" ]]; then
      echo "$zone"
      return 0
    fi
  fi
  firewall-cmd --get-default-zone
}

add_firewall_port() {
  local runtime_zone=$1
  local permanent_zone=$2
  local port
  port=$(normalize_firewall_port "$3")
  if ! firewall-cmd --zone="$runtime_zone" --query-port="$port" >/dev/null; then
    firewall-cmd --zone="$runtime_zone" --add-port="$port"
  fi
  if ! firewall-cmd --permanent --zone="$permanent_zone" --query-port="$port" >/dev/null; then
    firewall-cmd --permanent --zone="$permanent_zone" --add-port="$port"
  fi
  firewall-cmd --zone="$runtime_zone" --query-port="$port" >/dev/null
  firewall-cmd --permanent --zone="$permanent_zone" --query-port="$port" >/dev/null
}

configure_firewall() {
  local mode=${NETTRAFFIC_FIREWALL_MODE:-${NETTRAFFIC_OPEN_FIREWALL:-auto}}
  local port=${NETTRAFFIC_FIREWALL_PORT:-8080}
  local extra_ports=${NETTRAFFIC_EXTRA_FIREWALL_PORTS:-}
  local extra_port
  local permanent_zone
  local runtime_zone

  case "$mode" in
    0|false|FALSE|off|OFF|none|NONE|skip|SKIP)
      echo "跳过 firewalld 配置。"
      return 0
      ;;
    auto|AUTO|""|1|true|TRUE|on|ON|enable|ENABLE|force|FORCE)
      if ! systemctl is-active --quiet firewalld; then
        echo "firewalld 当前未运行；为避免影响 Trojan、SSH 和现有规则，脚本不会主动启动它。"
        echo "请通过云安全组或现有防火墙手动放行 ${port}/tcp。"
        return 0
      fi
      ;;
    *)
      echo "未知 NETTRAFFIC_FIREWALL_MODE=${mode}，跳过 firewalld 配置。"
      return 0
      ;;
  esac

  command -v firewall-cmd >/dev/null 2>&1 || {
    echo "firewalld 正在运行，但未找到 firewall-cmd。" >&2
    return 1
  }

  firewall-cmd --state
  runtime_zone=$(detect_firewall_zone runtime)
  permanent_zone=$(detect_firewall_zone permanent)
  [[ -n "$runtime_zone" && -n "$permanent_zone" ]] || {
    echo "无法确定 firewalld zone，请设置 NETTRAFFIC_FIREWALL_ZONE。" >&2
    return 1
  }
  echo "firewalld 运行时 zone：${runtime_zone}"
  echo "firewalld 永久 zone：${permanent_zone}"
  add_firewall_port "$runtime_zone" "$permanent_zone" "$port"
  for extra_port in $extra_ports; do
    add_firewall_port "$runtime_zone" "$permanent_zone" "$extra_port"
  done
  firewall-cmd --zone="$runtime_zone" --list-all
  firewall-cmd --permanent --zone="$permanent_zone" --list-all
  echo "firewalld 已验证运行时与永久规则，未执行 reload，因此不会清除现有的仅运行时规则。"
}

FIREWALL_PORT=${NETTRAFFIC_FIREWALL_PORT:-8080}
configure_firewall

systemctl --no-pager -l status nettraffic || true
curl -fsS "http://127.0.0.1:${FIREWALL_PORT}/healthz" || true

SERVER_IP=$(hostname -I | awk '{print $1}' || true)

set +x
echo
echo "NetTraffic 一键安装完成。"
echo "访问地址：http://${SERVER_IP:-服务器IP}:${FIREWALL_PORT}"
echo "配置文件：/etc/nettraffic/nettraffic.env"
echo "目标网站排行：${NETTRAFFIC_DESTINATIONS_ENABLED}"
echo "目标地址反向 DNS：${NETTRAFFIC_RESOLVE_HOSTNAMES}"
echo "采样间隔：${NETTRAFFIC_INTERVAL}"
echo "查看日志：sudo journalctl -u nettraffic -f"
echo "重启服务：sudo systemctl restart nettraffic"
