# NetTraffic

面向 CentOS 7 x86_64 的轻量网络流量监控。服务读取 Linux `/proc` 网络计数器与 conntrack 连接表，将数据保存到 SQLite，并提供 MRTG 风格的实时网页。

## 功能

- 实时入站/出站速度（SSE 推送，默认每 2 秒采样）
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

将整个目录或至少 `dist/`、`deploy/`、`scripts/` 上传到服务器，然后执行：

```bash
sudo bash scripts/install-centos7.sh
sudo firewall-cmd --permanent --add-port=8080/tcp
sudo firewall-cmd --reload
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

## 目标网站排行的工作方式

安装脚本会启用 `net.netfilter.nf_conntrack_acct=1`。服务读取 `/proc/net/nf_conntrack` 中本机发起的 80、443、8080、8443 端口连接，按字节增量统计目标 IP，并通过反向 DNS 尽可能显示域名。

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
| `NETTRAFFIC_INTERVAL` | `2s` | 采样间隔，最小 1 秒 |
| `NETTRAFFIC_RETENTION_DAYS` | `90` | 原始样本保留天数 |
| `NETTRAFFIC_USERNAME` | 空 | Basic Auth 用户名 |
| `NETTRAFFIC_PASSWORD` | 空 | Basic Auth 密码 |
| `NETTRAFFIC_RESOLVE_HOSTNAMES` | `true` | 是否反向解析目标 IP |

## API

- `GET /api/overview`：实时状态与累计流量
- `GET /api/series?range=day|week|month`：趋势数据
- `GET /api/daily?days=30`：最近若干天的每日流量
- `GET /api/daily?start=2026-06-01&end=2026-06-30`：自定义日期范围（最多 366 天）
- `GET /api/destinations?range=hour|day|week|month`：网站排行
- `GET /api/live`：实时 SSE 数据流
- `GET /healthz`：健康检查
# net_traffic
