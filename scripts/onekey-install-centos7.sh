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
    1|true|TRUE|yes|YES|y|Y|on|ON)
      echo true
      ;;
    0|false|FALSE|no|NO|n|N|off|OFF)
      echo false
      ;;
    *)
      return 1
      ;;
  esac
}

choose_destination_tracking() {
  local answer
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
    export NETTRAFFIC_DESTINATIONS_ENABLED=true
    echo "非交互环境，默认开启目标网站排行。可设置 NETTRAFFIC_DESTINATIONS_ENABLED=false 关闭。"
  fi

  echo "目标网站排行：${NETTRAFFIC_DESTINATIONS_ENABLED}"
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

add_firewall_port() {
  local port
  port=$(normalize_firewall_port "$1")
  firewall-cmd --permanent --add-port="$port" || echo "firewalld 端口 ${port} 放行失败，请手动检查防火墙。"
}

configure_firewall() {
  local mode=${NETTRAFFIC_FIREWALL_MODE:-${NETTRAFFIC_OPEN_FIREWALL:-auto}}
  local port=${NETTRAFFIC_FIREWALL_PORT:-8080}
  local extra_ports=${NETTRAFFIC_EXTRA_FIREWALL_PORTS:-}
  local extra_port

  case "$mode" in
    0|false|FALSE|off|OFF|none|NONE|skip|SKIP)
      echo "跳过 firewalld 配置。"
      return 0
      ;;
    auto|AUTO|"")
      if ! systemctl is-active --quiet firewalld; then
        echo "firewalld 当前未运行，默认不主动启动，避免影响已有 80/443 等服务。"
        echo "如需脚本启动并配置 firewalld，请使用：NETTRAFFIC_FIREWALL_MODE=enable"
        return 0
      fi
      ;;
    1|true|TRUE|on|ON|enable|ENABLE|force|FORCE)
      yum install -y firewalld
      systemctl enable --now firewalld
      ;;
    *)
      echo "未知 NETTRAFFIC_FIREWALL_MODE=${mode}，跳过 firewalld 配置。"
      return 0
      ;;
  esac

  if ! command -v firewall-cmd >/dev/null 2>&1; then
    yum install -y firewalld
  fi

  firewall-cmd --state
  add_firewall_port "$port"
  for extra_port in $extra_ports; do
    add_firewall_port "$extra_port"
  done
  firewall-cmd --reload || echo "firewalld reload 失败，请手动检查防火墙。"
  firewall-cmd --list-all || true
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
echo "查看日志：sudo journalctl -u nettraffic -f"
echo "重启服务：sudo systemctl restart nettraffic"
