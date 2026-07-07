package collector

import (
	"strings"
	"testing"
)

func TestParseNetDev(t *testing.T) {
	input := `Inter-| Receive | Transmit
 face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed
  eth0: 12345 1 0 0 0 0 0 0 67890 2 0 0 0 0 0 0`
	got, err := parseNetDev(strings.NewReader(input), "eth0")
	if err != nil {
		t.Fatal(err)
	}
	if got.rx != 12345 || got.tx != 67890 {
		t.Fatalf("unexpected counters: %+v", got)
	}
}

func TestParseConntrack(t *testing.T) {
	line := "ipv4 2 tcp 6 431999 ESTABLISHED src=192.0.2.10 dst=142.250.72.206 sport=50400 dport=443 packets=10 bytes=12000 src=142.250.72.206 dst=192.0.2.10 sport=443 dport=50400 packets=11 bytes=18000 [ASSURED]"
	got, err := parseConntrack(strings.NewReader(line), map[string]bool{"192.0.2.10": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("wanted one flow, got %d", len(got))
	}
	for _, flow := range got {
		if flow.ip != "142.250.72.206" || flow.bytes != 12000 || flow.packets != 10 {
			t.Fatalf("unexpected flow: %+v", flow)
		}
	}
}

func TestParseConntrackExcludesNonWeb(t *testing.T) {
	line := "ipv4 2 tcp 6 10 ESTABLISHED src=192.0.2.10 dst=8.8.8.8 sport=50500 dport=53 packets=2 bytes=200 src=8.8.8.8 dst=192.0.2.10 sport=53 dport=50500 packets=2 bytes=250"
	got, err := parseConntrack(strings.NewReader(line), map[string]bool{"192.0.2.10": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected DNS flow to be excluded")
	}
}
