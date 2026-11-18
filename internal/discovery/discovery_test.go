package discovery

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"superflow.local/superflow/internal/config"
)

func sample(t *testing.T) (Announcement, *config.Config) {
	t.Helper()
	c, err := config.New("hub", "test-machine")
	if err != nil {
		t.Fatal(err)
	}
	return Announcement{Protocol: "superflow/2", ID: c.ID, Name: c.Name, OS: "darwin", Pin: c.Pin, Port: 42800, Ready: true}, c
}
func TestPublicMetadataAndPinnedResolution(t *testing.T) {
	a, c := sample(t)
	packet, err := Encode(a)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := c.Code()
	for _, private := range []string{c.Secret, c.PrivateKey, c.Certificate, code} {
		if strings.Contains(string(packet), private) {
			t.Fatal("credential exposed in discovery")
		}
	}
	p, err := Decode(packet, net.ParseIP("192.168.1.20"), time.Now())
	if err != nil || p.Address != "192.168.1.20:42800" {
		t.Fatal(p, err)
	}
	b := &Browser{peers: map[string]Peer{"test": p}}
	if b.Resolve(c.Pin, "old:42800") != p.Address || b.Resolve(strings.Repeat("0", 64), "old:42800") != "old:42800" {
		t.Fatal("followed an unrelated announcement")
	}
	p.Seen = time.Now().Add(-expiry - time.Second)
	b.peers["test"] = p
	if len(b.Snapshot()) != 0 {
		t.Fatal("stale discovery retained")
	}
	for _, bad := range [][]byte{[]byte(`{"protocol":"superflow/2","query":true}`), make([]byte, maxPacket+1), []byte(`{"name":"spoof"}`)} {
		if _, err := Decode(bad, net.ParseIP("192.168.1.20"), time.Now()); err == nil {
			t.Fatal("accepted invalid metadata")
		}
	}
}
func TestReceiveOnRealUDP(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &Browser{conn: conn, peers: map[string]Peer{}, self: func() Announcement { return Announcement{} }, wake: make(chan struct{}, 1), cancel: cancel, done: make(chan struct{})}
	go func() { <-ctx.Done(); conn.Close() }()
	go b.receive(ctx)
	defer b.Close()
	sender, err := net.DialUDP("udp4", nil, conn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	a, _ := sample(t)
	packet, _ := Encode(a)
	if _, err = sender.Write(packet); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(b.Snapshot()) == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("real UDP announcement was not received")
}
func TestMulticastDiscovery(t *testing.T) {
	if os.Getenv("SUPERFLOW_DISCOVERY_TEST") != "1" {
		t.Skip("set SUPERFLOW_DISCOVERY_TEST=1 to exercise local multicast")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, _ := sample(t)
	other, _ := sample(t)
	other.Name = "second-test-machine"
	first, err := Start(ctx, func() Announcement { return a })
	if err != nil {
		t.Skip("multicast unavailable", err)
	}
	defer first.Close()
	second, err := Start(ctx, func() Announcement { return other })
	if err != nil {
		t.Skip("multicast unavailable", err)
	}
	defer second.Close()
	for ctx.Err() == nil {
		for _, peer := range second.Snapshot() {
			if peer.ID == a.ID {
				t.Log("received multicast announcement over a real local interface")
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("local multicast discovery did not receive a peer")
}
