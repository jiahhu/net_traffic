package collector

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"nettraffic/internal/store"
)

type flowCounter struct {
	ip      string
	bytes   int64
	packets int64
}

type conntrackSampler struct {
	path       string
	local      map[string]bool
	previous   map[string]flowCounter
	resolve    bool
	resolver   *net.Resolver
	cacheMu    sync.Mutex
	hostCache  map[string]cachedHost
	lastWarnAt time.Time
}

type cachedHost struct {
	name    string
	expires time.Time
}

func newConntrackSampler(path string, resolve bool) *conntrackSampler {
	return &conntrackSampler{
		path: path, local: localAddresses(), previous: map[string]flowCounter{}, resolve: resolve,
		resolver: net.DefaultResolver, hostCache: map[string]cachedHost{},
	}
}

func (c *conntrackSampler) sample(ctx context.Context) ([]store.DestinationDelta, error) {
	f, err := os.Open(c.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	current, err := parseConntrack(f, c.local)
	if err != nil {
		return nil, err
	}
	agg := map[string]*store.DestinationDelta{}
	resolved := 0
	for key, now := range current {
		prev, ok := c.previous[key]
		if !ok || now.bytes < prev.bytes {
			continue
		}
		delta := now.bytes - prev.bytes
		packetDelta := now.packets - prev.packets
		if delta <= 0 {
			continue
		}
		host := now.ip
		if c.resolve && resolved < 8 {
			host = c.hostname(ctx, now.ip)
			resolved++
		} else if cached := c.cached(now.ip); cached != "" {
			host = cached
		}
		id := host + "\x00" + now.ip
		if agg[id] == nil {
			agg[id] = &store.DestinationDelta{Host: host, IP: now.ip}
		}
		agg[id].Bytes += delta
		agg[id].Packets += max64(packetDelta, 0)
	}
	c.previous = current
	out := make([]store.DestinationDelta, 0, len(agg))
	for _, row := range agg {
		out = append(out, *row)
	}
	return out, nil
}

func parseConntrack(r io.Reader, local map[string]bool) (map[string]flowCounter, error) {
	out := map[string]flowCounter{}
	s := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	s.Buffer(buf, 2*1024*1024)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 8 {
			continue
		}
		proto := fields[2]
		vals := map[string]string{}
		for _, field := range fields[3:] {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) != 2 {
				continue
			}
			if parts[0] == "src" && vals["src"] != "" {
				break
			}
			vals[parts[0]] = parts[1]
		}
		if !local[vals["src"]] || vals["dst"] == "" || !isPublicIP(vals["dst"]) || !isWebPort(vals["dport"]) {
			continue
		}
		bytes, _ := strconv.ParseInt(vals["bytes"], 10, 64)
		packets, _ := strconv.ParseInt(vals["packets"], 10, 64)
		key := strings.Join([]string{proto, vals["src"], vals["dst"], vals["sport"], vals["dport"]}, "|")
		out[key] = flowCounter{ip: vals["dst"], bytes: bytes, packets: packets}
	}
	return out, s.Err()
}

func isWebPort(port string) bool {
	switch port {
	case "80", "443", "8080", "8443":
		return true
	default:
		return false
	}
}

func isPublicIP(value string) bool {
	ip := net.ParseIP(value)
	if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		return !(v4[0] == 10 || v4[0] == 127 || (v4[0] == 169 && v4[1] == 254) || (v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31) || (v4[0] == 192 && v4[1] == 168))
	}
	return !(ip[0]&0xfe == 0xfc)
}

func (c *conntrackSampler) cached(ip string) string {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	v, ok := c.hostCache[ip]
	if ok && time.Now().Before(v.expires) {
		return v.name
	}
	return ""
}

func (c *conntrackSampler) hostname(parent context.Context, ip string) string {
	if name := c.cached(ip); name != "" {
		return name
	}
	ctx, cancel := context.WithTimeout(parent, 350*time.Millisecond)
	defer cancel()
	names, err := c.resolver.LookupAddr(ctx, ip)
	name := ip
	if err == nil && len(names) > 0 {
		name = strings.TrimSuffix(names[0], ".")
	}
	c.cacheMu.Lock()
	c.hostCache[ip] = cachedHost{name: name, expires: time.Now().Add(6 * time.Hour)}
	c.cacheMu.Unlock()
	return name
}

func conntrackHelp(path string, err error) error {
	return fmt.Errorf("read %s: %w (enable with: sysctl -w net.netfilter.nf_conntrack_acct=1)", path, err)
}
