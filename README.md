# NetTraffic

面向 CentOS 7 x86_64 的轻量网络流量监控。服务读取 Linux `/proc` 网络计数器与 conntrack 连接表，将数据保存到 SQLite，并提供 MRTG 风格的实时网页。

## 功能

- 实时入站/出站速度（SSE 推送，安装后默认每 5 秒采样）
- 24 小时、7 天、30 天流量趋势及最大/平均/当前值
- 今日、近 30 日累计流量和可自定义日期范围的每日流量柱状图
- 折线图与柱状图支持鼠标悬停查看精确字节数
- HTTP/HTTPS 目标网站流量排行，支持 1 小时、今日、7 天范围
- 自动选择默认路由网卡，也可指定网卡
- SQLite 持久化、90 天原始数据保留、可选 HTTP Basic Auth
- 单个静态 Linux x86_64 二进制，无 Node.js/Python 运行时依赖

## 本地预览

需要 Go 1.25 或更高版本：

```bash
go mod download
make run
```

打开 <http://127.0.0.1:8080>。`--mock` 只用于预览，会生成演示数据。

## 构建 CentOS 7 二进制

```bash
go mod download
make test
make build-linux
```

输出为 `dist/nettraffic-linux-amd64`。构建时使用 `CGO_ENABLED=0`，因此不依赖 CentOS 7 的 glibc 或系统 SQLite。

## 安装到 CentOS 7

从源码下载后，推荐直接执行一键安装脚本。脚本会详细打印正在执行的命令，自动安装依赖、安装 Go、编译二进制并安装 systemd 服务：

```bash
git clone git@github.com:jiahhu/net_traffic.git
cd net_traffic
sudo bash scripts/onekey-install-centos7.sh
```

安装过程中会询问是否开启“目标网站排行”，直接回车默认 `N`。Trojan、代理网关、高连接数服务器建议选择 `N`；实时流量、总流量和每日图表不受影响。选择 `N` 时脚本不会启用 conntrack accounting。非交互新安装默认关闭排行；非交互升级会保留已有合法配置，也可用环境变量显式覆盖。

为减少对代理服务的资源竞争，一键安装关闭排行时默认使用 `NETTRAFFIC_INTERVAL=5s`；主动开启排行时默认使用 `10s`。反向 DNS 默认关闭，systemd 服务使用较低的 CPU 调度优先级。需要更高刷新频率或域名显示时可显式覆盖。

默认会安装 Go `1.25.4`。如果服务器已有 Go `1.25+`，脚本会直接复用现有 Go。可通过环境变量覆盖默认行为：

```bash
# 指定 Go 版本
sudo env GO_INSTALL_VERSION=1.25.4 bash scripts/onekey-install-centos7.sh

# 非交互方式指定是否开启目标网站排行
sudo env NETTRAFFIC_DESTINATIONS_ENABLED=false bash scripts/onekey-install-centos7.sh

# 必须保留排行时，建议使用较长采样间隔并关闭反向 DNS
sudo env NETTRAFFIC_DESTINATIONS_ENABLED=true NETTRAFFIC_INTERVAL=10s NETTRAFFIC_RESOLVE_HOSTNAMES=false bash scripts/onekey-install-centos7.sh

# 防火墙行为默认为 auto：如果 firewalld 已运行，向实际网卡所属 zone 的运行时和永久规则追加放行 8080；
# 如果 firewalld 未运行，不会主动启动，避免影响 Trojan/Nginx 等已有 80/443 服务。
# 脚本不会执行 firewalld reload，因此不会清除已有的仅运行时规则。

# firewalld 已运行时，指定 zone 并额外永久放行 Trojan 常用的 443/tcp
sudo env NETTRAFFIC_FIREWALL_ZONE=public NETTRAFFIC_EXTRA_FIREWALL_PORTS=443/tcp bash scripts/onekey-install-centos7.sh

# 完全跳过 firewalld 配置
sudo env NETTRAFFIC_FIREWALL_MODE=skip bash scripts/onekey-install-centos7.sh

# 关闭命令追踪输出
sudo env TRACE=0 bash scripts/onekey-install-centos7.sh
```

也可以手动编译后再安装：

```bash
go mod download
make test
make build-linux
sudo bash scripts/install-centos7.sh
# 仅在 firewalld 已运行时执行；请把 public 替换为实际 active zone
sudo firewall-cmd --zone=public --add-port=8080/tcp
sudo firewall-cmd --permanent --zone=public --add-port=8080/tcp
```

访问 `http://服务器IP:8080`。公网开放前，请编辑 `/etc/nettraffic/nettraffic.env` 设置：

```bash
NETTRAFFIC_USERNAME=admin
NETTRAFFIC_PASSWORD=替换为高强度密码
```

修改配置后运行：

```bash
sudo systemctl restart nettraffic
sudo journalctl -u nettraffic -f
```

若服务器上已有 Trojan、Nginx、Caddy 等服务占用 80/443，NetTraffic 默认不会占用这些端口。若安装后 443 无法访问，优先检查 firewalld 是否被启用且未放行 443：

```bash
sudo systemctl status firewalld
sudo firewall-cmd --get-active-zones
sudo firewall-cmd --zone=实际active-zone --list-all
sudo ss -lntp | egrep ':443|:8080'
```

如果确认是 firewalld 拦截 Trojan 的 443 端口，执行：

```bash
# 请把 public 替换为服务器网卡实际所在的 active zone
sudo firewall-cmd --zone=public --add-port=443/tcp
sudo firewall-cmd --permanent --zone=public --add-port=443/tcp
```

如果服务器连接数很高，并且怀疑 conntrack 扫描或 accounting 带来额外负载，可以临时关闭“目标网站排行”，实时总流量监控不受影响：

```bash
sudo sed -i 's/^NETTRAFFIC_DESTINATIONS_ENABLED=.*/NETTRAFFIC_DESTINATIONS_ENABLED=false/' /etc/nettraffic/nettraffic.env
sudo rm -f /etc/sysctl.d/99-nettraffic.conf
sudo sysctl -w net.netfilter.nf_conntrack_acct=0
sudo systemctl restart nettraffic
```

## 目标网站排行的工作方式

只有开启目标网站排行时，安装脚本才会启用 `net.netfilter.nf_conntrack_acct=1`。服务读取 `/proc/net/nf_conntrack` 中本机发起的 80、443、8080、8443 端口连接，按字节增量统计目标 IP，并可选通过反向 DNS 显示域名。

这是一种无需抓取数据包内容的低开销方案，但存在明确边界：CDN 地址可能显示为 CDN 主机名或 IP；反向 DNS 不一定等于浏览器地址栏中的域名；同一连接复用多个域名时无法区分。页面中的说明会如实标记为“按 HTTP/HTTPS 连接与反向 DNS 统计”。

若排行提示未启用，检查：

```bash
sysctl net.netfilter.nf_conntrack_acct
ls -l /proc/net/nf_conntrack
```

## 配置

配置位于 `/etc/nettraffic/nettraffic.env`，也可使用同名环境变量：

| 变量 | 默认值 | 说明 |
|---|---:|---|
| `NETTRAFFIC_LISTEN` | `:8080` | HTTP 监听地址 |
| `NETTRAFFIC_INTERFACE` | 自动选择 | 监控网卡 |
| `NETTRAFFIC_DB` | `/var/lib/nettraffic/nettraffic.db` | SQLite 路径 |
| `NETTRAFFIC_INTERVAL` | `5s` | 安装后的采样间隔，程序允许最小 1 秒 |
| `NETTRAFFIC_RETENTION_DAYS` | `90` | 原始样本保留天数 |
| `NETTRAFFIC_USERNAME` | 空 | Basic Auth 用户名 |
| `NETTRAFFIC_PASSWORD` | 空 | Basic Auth 密码 |
| `NETTRAFFIC_DESTINATIONS_ENABLED` | `false` | 是否启用目标网站排行 |
| `NETTRAFFIC_RESOLVE_HOSTNAMES` | `false` | 是否反向解析目标 IP |

## API

- `GET /api/overview`：实时状态与累计流量
- `GET /api/series?range=day|week|month`：趋势数据
- `GET /api/daily?days=30`：最近若干天的每日流量
- `GET /api/daily?start=2026-06-01&end=2026-06-30`：自定义日期范围（最多 366 天）
- `GET /api/destinations?range=hour|day|week|month`：网站排行
- `GET /api/live`：实时 SSE 数据流
- `GET /healthz`：健康检查
# net_traffic
# net_traffic
