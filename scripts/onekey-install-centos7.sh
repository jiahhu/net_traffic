#!/usr/bin/env bash
set -Eeuo pipefail

PS4='+ [$(date "+%Y-%m-%d %H:%M:%S")] ${BASH_SOURCE##*/}:${LINENO}: '
if [[ "${TRACE:-1}" != "0" ]]; then
  set -x
fi

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

yum install -y git curl tar make ca-certificates firewalld
if command -v update-ca-trust >/dev/null 2>&1; then
  update-ca-trust
fi

install_go_if_needed

export PATH="/usr/local/go/bin:$PATH"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

go mod download
go test ./...
bash "$ROOT/scripts/build-linux.sh"
bash -x "$ROOT/scripts/install-centos7.sh"

FIREWALL_PORT=${NETTRAFFIC_FIREWALL_PORT:-8080}
if [[ "${NETTRAFFIC_OPEN_FIREWALL:-1}" == "1" ]]; then
  if systemctl enable --now firewalld; then
    firewall-cmd --permanent --add-port="${FIREWALL_PORT}/tcp" || echo "firewalld 端口放行失败，请手动检查防火墙。"
    firewall-cmd --reload || echo "firewalld reload 失败，请手动检查防火墙。"
  else
    echo "firewalld 启动失败，跳过自动开放端口。"
  fi
fi

systemctl --no-pager -l status nettraffic || true
curl -fsS "http://127.0.0.1:${FIREWALL_PORT}/healthz" || true

SERVER_IP=$(hostname -I | awk '{print $1}' || true)

set +x
echo
echo "NetTraffic 一键安装完成。"
echo "访问地址：http://${SERVER_IP:-服务器IP}:${FIREWALL_PORT}"
echo "配置文件：/etc/nettraffic/nettraffic.env"
echo "查看日志：sudo journalctl -u nettraffic -f"
echo "重启服务：sudo systemctl restart nettraffic"
