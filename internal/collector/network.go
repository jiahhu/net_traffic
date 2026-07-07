package collector

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
)

type counters struct {
	rx int64
	tx int64
}

func readNetDev(path, iface string) (counters, error) {
	f, err := os.Open(path)
	if err != nil {
		return counters{}, err
	}
	defer f.Close()
	return parseNetDev(f, iface)
}

func parseNetDev(r io.Reader, iface string) (counters, error) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) != iface {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			return counters{}, fmt.Errorf("invalid /proc/net/dev row for %s", iface)
		}
		rx, err1 := strconv.ParseInt(fields[0], 10, 64)
		tx, err2 := strconv.ParseInt(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			return counters{}, fmt.Errorf("invalid byte counters for %s", iface)
		}
		return counters{rx: rx, tx: tx}, nil
	}
	if err := s.Err(); err != nil {
		return counters{}, err
	}
	return counters{}, fmt.Errorf("interface %s not found in /proc/net/dev", iface)
}

func defaultInterface(requested string) (string, error) {
	if requested != "" {
		return requested, nil
	}
	if data, err := os.ReadFile("/proc/net/route"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) > 3 && f[1] == "00000000" && f[3] != "0000" {
				return f[0], nil
			}
		}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, i := range ifaces {
		if i.Flags&net.FlagUp != 0 && i.Flags&net.FlagLoopback == 0 {
			return i.Name, nil
		}
	}
	return "", fmt.Errorf("no active network interface found")
}

func localAddresses() map[string]bool {
	out := map[string]bool{}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err == nil {
				out[ip.String()] = true
			}
		}
	}
	return out
}
