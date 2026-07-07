package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Point struct {
	Time   int64   `json:"time"`
	RXRate float64 `json:"rxRate"`
	TXRate float64 `json:"txRate"`
}

type Day struct {
	Date    string `json:"date"`
	RXBytes int64  `json:"rxBytes"`
	TXBytes int64  `json:"txBytes"`
}

type DestinationDelta struct {
	Host    string
	IP      string
	Bytes   int64
	Packets int64
}

type Destination struct {
	Host    string `json:"host"`
	IP      string `json:"ip"`
	Bytes   int64  `json:"bytes"`
	Packets int64  `json:"packets"`
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	const schema = `
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS samples (
  ts INTEGER NOT NULL,
  interface TEXT NOT NULL,
  rx_bytes INTEGER NOT NULL,
  tx_bytes INTEGER NOT NULL,
  rx_rate REAL NOT NULL,
  tx_rate REAL NOT NULL,
  PRIMARY KEY (ts, interface)
);
CREATE INDEX IF NOT EXISTS idx_samples_interface_ts ON samples(interface, ts);
CREATE TABLE IF NOT EXISTS daily (
  day TEXT NOT NULL,
  interface TEXT NOT NULL,
  rx_bytes INTEGER NOT NULL DEFAULT 0,
  tx_bytes INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (day, interface)
);
CREATE TABLE IF NOT EXISTS destinations (
  bucket INTEGER NOT NULL,
  host TEXT NOT NULL,
  ip TEXT NOT NULL,
  bytes INTEGER NOT NULL DEFAULT 0,
  packets INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (bucket, host, ip)
);
CREATE INDEX IF NOT EXISTS idx_destinations_bucket ON destinations(bucket);
`
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) SaveSample(ctx context.Context, at time.Time, iface string, rxTotal, txTotal int64, rxRate, txRate float64, rxDelta, txDelta int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO samples(ts, interface, rx_bytes, tx_bytes, rx_rate, tx_rate)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(ts, interface) DO UPDATE SET rx_bytes=excluded.rx_bytes, tx_bytes=excluded.tx_bytes, rx_rate=excluded.rx_rate, tx_rate=excluded.tx_rate`,
		at.Unix(), iface, rxTotal, txTotal, rxRate, txRate)
	if err != nil {
		return err
	}
	if rxDelta > 0 || txDelta > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO daily(day, interface, rx_bytes, tx_bytes) VALUES (?, ?, ?, ?)
ON CONFLICT(day, interface) DO UPDATE SET rx_bytes=rx_bytes+excluded.rx_bytes, tx_bytes=tx_bytes+excluded.tx_bytes`,
			at.Format("2006-01-02"), iface, max64(rxDelta, 0), max64(txDelta, 0))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SaveDestinations(ctx context.Context, at time.Time, rows []DestinationDelta) error {
	if len(rows) == 0 {
		return nil
	}
	bucket := at.Unix() / 60 * 60
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO destinations(bucket, host, ip, bytes, packets) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(bucket, host, ip) DO UPDATE SET bytes=bytes+excluded.bytes, packets=packets+excluded.packets`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if row.Bytes <= 0 {
			continue
		}
		if _, err := stmt.ExecContext(ctx, bucket, row.Host, row.IP, row.Bytes, max64(row.Packets, 0)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Series(ctx context.Context, iface string, since time.Time, bucketSeconds int64) ([]Point, error) {
	if bucketSeconds < 1 {
		bucketSeconds = 1
	}
	rows, err := s.db.QueryContext(ctx, `SELECT (ts / ?) * ? AS bucket, AVG(rx_rate), AVG(tx_rate)
FROM samples WHERE interface=? AND ts>=? GROUP BY bucket ORDER BY bucket`, bucketSeconds, bucketSeconds, iface, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.Time, &p.RXRate, &p.TXRate); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Daily(ctx context.Context, iface string, days int) ([]Day, error) {
	if days < 1 {
		days = 30
	}
	end := time.Now()
	start := end.AddDate(0, 0, -(days - 1))
	return s.DailyRange(ctx, iface, start, end)
}

func (s *Store) DailyRange(ctx context.Context, iface string, start, end time.Time) ([]Day, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT day, rx_bytes, tx_bytes FROM daily
	WHERE interface=? AND day>=? AND day<=? ORDER BY day`, iface, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Day
	for rows.Next() {
		var d Day
		if err := rows.Scan(&d.Date, &d.RXBytes, &d.TXBytes); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Totals(ctx context.Context, iface string, since time.Time) (int64, int64, error) {
	var rx, tx sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT SUM(rx_bytes), SUM(tx_bytes) FROM daily WHERE interface=? AND day>=?`, iface, since.Format("2006-01-02")).Scan(&rx, &tx)
	return rx.Int64, tx.Int64, err
}

func (s *Store) Destinations(ctx context.Context, since time.Time, limit int) ([]Destination, error) {
	if limit < 1 || limit > 100 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT host, ip, SUM(bytes) AS total, SUM(packets) FROM destinations
WHERE bucket>=? GROUP BY host, ip ORDER BY total DESC LIMIT ?`, since.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Destination
	for rows.Next() {
		var d Destination
		if err := rows.Scan(&d.Host, &d.IP, &d.Bytes, &d.Packets); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Cleanup(ctx context.Context, retentionDays int) error {
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM samples WHERE ts<?`, cutoff); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM destinations WHERE bucket<?`, cutoff)
	return err
}

// SeedDemo inserts deterministic history for local UI development. It is never
// called unless --mock is explicitly enabled.
func (s *Store) SeedDemo(ctx context.Context, at time.Time, iface string) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM samples WHERE interface=?`, iface).Scan(&count); err != nil || count > 0 {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i := 0; i < 30; i++ {
		day := at.AddDate(0, 0, -29+i)
		rx := int64((5.8 + math.Sin(float64(i)*.71)*2.1 + float64(i%5)*.42) * 1e9)
		txBytes := int64((1.7 + math.Cos(float64(i)*.53)*.65 + float64(i%3)*.22) * 1e9)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO daily(day, interface, rx_bytes, tx_bytes) VALUES (?, ?, ?, ?)`, day.Format("2006-01-02"), iface, rx, txBytes); err != nil {
			return err
		}
	}
	baseRX, baseTX := int64(18_300_000_000), int64(7_900_000_000)
	for i := 0; i <= 576; i++ {
		stamp := at.Add(-48*time.Hour + time.Duration(i)*5*time.Minute)
		rxRate := (2.4 + math.Sin(float64(i)/18)*1.15 + math.Sin(float64(i)/5)*.38) * 1e6
		txRate := (.72 + math.Cos(float64(i)/24)*.37 + math.Sin(float64(i)/9)*.16) * 1e6
		if i%97 < 6 {
			rxRate *= 2.8
		}
		if i%131 < 4 {
			txRate *= 3.4
		}
		baseRX += int64(rxRate * 300)
		baseTX += int64(txRate * 300)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO samples(ts, interface, rx_bytes, tx_bytes, rx_rate, tx_rate) VALUES (?, ?, ?, ?, ?, ?)`, stamp.Unix(), iface, baseRX, baseTX, rxRate, txRate); err != nil {
			return err
		}
	}
	sites := []struct {
		host, ip string
		bytes    int64
	}{{"cdn.cloudflare.com", "104.16.132.229", 8_200_000_000}, {"www.youtube.com", "142.250.72.206", 6_150_000_000}, {"github.com", "140.82.114.4", 3_420_000_000}, {"api.openai.com", "104.18.33.45", 2_260_000_000}, {"www.apple.com", "17.253.144.10", 1_090_000_000}}
	for _, site := range sites {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO destinations(bucket,host,ip,bytes,packets) VALUES (?,?,?,?,?)`, at.Unix()/60*60, site.host, site.ip, site.bytes, site.bytes/1200); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
