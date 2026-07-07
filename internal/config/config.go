package config

import (
	"flag"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen           string
	Database         string
	Interface        string
	Interval         time.Duration
	RetentionDays    int
	Username         string
	Password         string
	Mock             bool
	ConntrackPath    string
	ResolveHostnames bool
}

func Parse() Config {
	c := Config{}
	flag.StringVar(&c.Listen, "listen", env("NETTRAFFIC_LISTEN", ":8080"), "HTTP listen address")
	flag.StringVar(&c.Database, "db", env("NETTRAFFIC_DB", "/var/lib/nettraffic/nettraffic.db"), "SQLite database path")
	flag.StringVar(&c.Interface, "interface", env("NETTRAFFIC_INTERFACE", ""), "network interface; empty selects the default route")
	flag.DurationVar(&c.Interval, "interval", envDuration("NETTRAFFIC_INTERVAL", 2*time.Second), "sampling interval")
	flag.IntVar(&c.RetentionDays, "retention-days", envInt("NETTRAFFIC_RETENTION_DAYS", 90), "raw sample retention")
	flag.StringVar(&c.Username, "username", os.Getenv("NETTRAFFIC_USERNAME"), "optional HTTP Basic Auth username")
	flag.StringVar(&c.Password, "password", os.Getenv("NETTRAFFIC_PASSWORD"), "optional HTTP Basic Auth password")
	flag.StringVar(&c.ConntrackPath, "conntrack", env("NETTRAFFIC_CONNTRACK", "/proc/net/nf_conntrack"), "conntrack table path")
	flag.BoolVar(&c.ResolveHostnames, "resolve-hostnames", envBool("NETTRAFFIC_RESOLVE_HOSTNAMES", true), "reverse-resolve destination IPs")
	flag.BoolVar(&c.Mock, "mock", envBool("NETTRAFFIC_MOCK", false), "generate demo data (development only)")
	flag.Parse()
	if c.Interval < time.Second {
		c.Interval = time.Second
	}
	if c.RetentionDays < 7 {
		c.RetentionDays = 7
	}
	return c
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v, err := strconv.Atoi(os.Getenv(key))
	if err == nil && v > 0 {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v, err := time.ParseDuration(os.Getenv(key))
	if err == nil && v > 0 {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
