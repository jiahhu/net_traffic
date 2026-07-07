package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	if err := db.SaveSample(ctx, now, "eth0", 1000, 2000, 100, 200, 100, 200); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSample(ctx, now.Add(time.Minute), "eth0", 1300, 2500, 300, 500, 300, 500); err != nil {
		t.Fatal(err)
	}
	points, err := db.Series(ctx, "eth0", now.Add(-time.Minute), 1)
	if err != nil || len(points) != 2 {
		t.Fatalf("series: len=%d err=%v", len(points), err)
	}
	days, err := db.Daily(ctx, "eth0", 7)
	if err != nil || len(days) != 1 || days[0].RXBytes != 400 || days[0].TXBytes != 700 {
		t.Fatalf("daily: %+v err=%v", days, err)
	}
	rangeDays, err := db.DailyRange(ctx, "eth0", now.AddDate(0, 0, -1), now)
	if err != nil || len(rangeDays) != 1 {
		t.Fatalf("daily range: %+v err=%v", rangeDays, err)
	}
	if err := db.SaveDestinations(ctx, now, []DestinationDelta{{Host: "example.com", IP: "93.184.216.34", Bytes: 1234, Packets: 2}}); err != nil {
		t.Fatal(err)
	}
	dest, err := db.Destinations(ctx, now.Add(-time.Hour), 10)
	if err != nil || len(dest) != 1 || dest[0].Bytes != 1234 {
		t.Fatalf("destinations: %+v err=%v", dest, err)
	}
}
