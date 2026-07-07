package collector

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"nettraffic/internal/config"
	"nettraffic/internal/store"
)

type Status struct {
	Time                int64   `json:"time"`
	Interface           string  `json:"interface"`
	RXRate              float64 `json:"rxRate"`
	TXRate              float64 `json:"txRate"`
	RXTotal             int64   `json:"rxTotal"`
	TXTotal             int64   `json:"txTotal"`
	Uptime              int64   `json:"uptime"`
	DestinationTracking bool    `json:"destinationTracking"`
}

type Collector struct {
	cfg       config.Config
	store     *store.Store
	iface     string
	conntrack *conntrackSampler
	mu        sync.RWMutex
	latest    Status
	subs      map[chan Status]struct{}
}

func New(cfg config.Config, db *store.Store) (*Collector, error) {
	iface := "demo0"
	var err error
	if !cfg.Mock {
		iface, err = defaultInterface(cfg.Interface)
		if err != nil {
			return nil, err
		}
	}
	if cfg.Mock {
		if err := db.SeedDemo(context.Background(), time.Now(), iface); err != nil {
			return nil, fmt.Errorf("seed demo data: %w", err)
		}
	}
	return &Collector{cfg: cfg, store: db, iface: iface, conntrack: newConntrackSampler(cfg.ConntrackPath, cfg.ResolveHostnames), subs: map[chan Status]struct{}{}}, nil
}

func (c *Collector) Interface() string { return c.iface }

func (c *Collector) Latest() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest
}

func (c *Collector) Subscribe() (<-chan Status, func()) {
	ch := make(chan Status, 4)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	if c.latest.Time > 0 {
		ch <- c.latest
	}
	c.mu.Unlock()
	return ch, func() {
		c.mu.Lock()
		delete(c.subs, ch)
		close(ch)
		c.mu.Unlock()
	}
}

func (c *Collector) Run(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()
	cleanup := time.NewTicker(24 * time.Hour)
	defer cleanup.Stop()
	var previous counters
	var previousAt time.Time
	for {
		select {
		case now := <-ticker.C:
			current, err := c.readCounters(now)
			if err != nil {
				log.Printf("collect network counters: %v", err)
				continue
			}
			rxDelta, txDelta := int64(0), int64(0)
			rxRate, txRate := float64(0), float64(0)
			if !previousAt.IsZero() {
				seconds := now.Sub(previousAt).Seconds()
				if current.rx >= previous.rx {
					rxDelta = current.rx - previous.rx
					rxRate = float64(rxDelta) / seconds
				}
				if current.tx >= previous.tx {
					txDelta = current.tx - previous.tx
					txRate = float64(txDelta) / seconds
				}
			}
			previous, previousAt = current, now
			if err := c.store.SaveSample(ctx, now, c.iface, current.rx, current.tx, rxRate, txRate, rxDelta, txDelta); err != nil {
				log.Printf("save network sample: %v", err)
			}
			destinationTracking := false
			if c.cfg.Mock {
				destinationTracking = true
				_ = c.store.SaveDestinations(ctx, now, c.mockDestinations())
			} else if c.cfg.Destinations {
				rows, err := c.conntrack.sample(ctx)
				if err != nil {
					if now.Sub(c.conntrack.lastWarnAt) > time.Hour {
						log.Printf("destination tracking unavailable: %v", conntrackHelp(c.cfg.ConntrackPath, err))
						c.conntrack.lastWarnAt = now
					}
					status := Status{Time: now.Unix(), Interface: c.iface, RXRate: rxRate, TXRate: txRate, RXTotal: current.rx, TXTotal: current.tx, Uptime: uptimeSeconds(), DestinationTracking: destinationTracking}
					c.publish(status)
					continue
				}
				destinationTracking = true
				if err := c.store.SaveDestinations(ctx, now, rows); err != nil {
					log.Printf("save destination sample: %v", err)
				}
			}
			status := Status{Time: now.Unix(), Interface: c.iface, RXRate: rxRate, TXRate: txRate, RXTotal: current.rx, TXTotal: current.tx, Uptime: uptimeSeconds(), DestinationTracking: destinationTracking}
			c.publish(status)
		case <-cleanup.C:
			if err := c.store.Cleanup(ctx, c.cfg.RetentionDays); err != nil {
				log.Printf("cleanup old samples: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (c *Collector) publish(status Status) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latest = status
	for ch := range c.subs {
		select {
		case ch <- status:
		default:
		}
	}
}

func (c *Collector) readCounters(now time.Time) (counters, error) {
	if !c.cfg.Mock {
		return readNetDev("/proc/net/dev", c.iface)
	}
	c.mu.RLock()
	last := c.latest
	c.mu.RUnlock()
	baseRX, baseTX := last.RXTotal, last.TXTotal
	if baseRX == 0 {
		baseRX, baseTX = 18_300_000_000, 7_900_000_000
	}
	wave := (math.Sin(float64(now.Unix())/19) + 1.4) * 900_000
	return counters{rx: baseRX + int64(wave+rand.Float64()*650_000), tx: baseTX + int64(wave*.34+rand.Float64()*180_000)}, nil
}

func (c *Collector) mockDestinations() []store.DestinationDelta {
	sites := []struct {
		host, ip string
		weight   float64
	}{
		{"cdn.cloudflare.com", "104.16.132.229", 1.0}, {"www.youtube.com", "142.250.72.206", .82},
		{"github.com", "140.82.114.4", .48}, {"api.openai.com", "104.18.33.45", .36}, {"www.apple.com", "17.253.144.10", .22},
	}
	out := make([]store.DestinationDelta, 0, len(sites))
	for _, site := range sites {
		bytes := int64((450_000 + rand.Float64()*1_800_000) * site.weight)
		out = append(out, store.DestinationDelta{Host: site.host, IP: site.ip, Bytes: bytes, Packets: bytes / 1200})
	}
	return out
}

func uptimeSeconds() int64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	value := strings.Fields(string(data))
	if len(value) == 0 {
		return 0
	}
	f, _ := strconv.ParseFloat(value[0], 64)
	return int64(f)
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (c *Collector) String() string { return fmt.Sprintf("collector(%s)", c.iface) }
