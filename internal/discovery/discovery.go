// Package discovery exchanges bounded, public metadata on the local subnet.
// It does not grant trust: a connection still requires the invitation's TLS pin
// and pairing secret. Network announcements never include credentials.
package discovery

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
	"superflow.local/superflow/internal/config"
)

const group = "239.255.42.80:42801"
const maxPacket = 768
const expiry = 12 * time.Second

type Announcement struct {
	Protocol string `json:"protocol"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	OS       string `json:"os"`
	Port     int    `json:"port"`
	Pin      string `json:"pin"`
	Ready    bool   `json:"ready"`
	Query    bool   `json:"query,omitempty"`
}
type Peer struct {
	ID, Name, OS, Pin, Address string
	Ready                      bool
	Seen                       time.Time
}
type Browser struct {
	mu         sync.Mutex
	peers      map[string]Peer
	conn       *net.UDPConn
	packet     *ipv4.PacketConn
	address    *net.UDPAddr
	interfaces []net.Interface
	self       func() Announcement
	wake       chan struct{}
	cancel     context.CancelFunc
	done       chan struct{}
}

func Valid(a Announcement) bool {
	id, e := hex.DecodeString(a.ID)
	pin, p := hex.DecodeString(a.Pin)
	return a.Protocol == "superflow/2" && e == nil && len(id) == 16 && p == nil && len(pin) == 32 && config.ValidName(a.Name) && a.Port > 0 && a.Port <= 65535 && (a.OS == "darwin" || a.OS == "windows" || a.OS == "linux") && !a.Query
}
func Encode(a Announcement) ([]byte, error) {
	if !Valid(a) {
		return nil, errors.New("invalid discovery announcement")
	}
	return json.Marshal(a)
}
func Decode(data []byte, source net.IP, now time.Time) (Peer, error) {
	var a Announcement
	if len(data) == 0 || len(data) > maxPacket || source == nil || source.IsUnspecified() || source.IsMulticast() {
		return Peer{}, errors.New("invalid discovery packet")
	}
	if json.Unmarshal(data, &a) != nil || !Valid(a) {
		return Peer{}, errors.New("invalid discovery metadata")
	}
	return Peer{ID: a.ID, Name: a.Name, OS: a.OS, Pin: a.Pin, Address: net.JoinHostPort(source.String(), strconv.Itoa(a.Port)), Ready: a.Ready, Seen: now}, nil
}
func Start(parent context.Context, self func() Announcement) (*Browser, error) {
	address, _ := net.ResolveUDPAddr("udp4", group)
	conn, err := net.ListenMulticastUDP("udp4", nil, address)
	if err != nil {
		return nil, err
	}
	packet := ipv4.NewPacketConn(conn)
	_ = packet.SetMulticastTTL(1)
	_ = packet.SetMulticastLoopback(true)
	_ = conn.SetReadBuffer(16 << 10)
	ctx, cancel := context.WithCancel(parent)
	b := &Browser{peers: map[string]Peer{}, conn: conn, packet: packet, address: address, self: self, wake: make(chan struct{}, 1), cancel: cancel, done: make(chan struct{})}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := iface.Addrs()
		eligible := false
		for _, address := range addresses {
			ip, _, _ := net.ParseCIDR(address.String())
			if ip.To4() != nil && !ip.IsLoopback() {
				eligible = true
				break
			}
		}
		if eligible {
			_ = packet.JoinGroup(&iface, address)
			b.interfaces = append(b.interfaces, iface)
		}
	}
	go func() { <-ctx.Done(); _ = conn.Close() }()
	go b.receive(ctx)
	go b.send(ctx)
	return b, nil
}
func (b *Browser) Close() { b.cancel(); <-b.done }
func (b *Browser) Refresh() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}
func (b *Browser) Snapshot() []Peer {
	b.mu.Lock()
	defer b.mu.Unlock()
	var peers []Peer
	for key, p := range b.peers {
		if time.Since(p.Seen) > expiry {
			delete(b.peers, key)
			continue
		}
		peers = append(peers, p)
	}
	sort.Slice(peers, func(i, j int) bool {
		if peers[i].Name == peers[j].Name {
			return peers[i].Address < peers[j].Address
		}
		return peers[i].Name < peers[j].Name
	})
	return peers
}

// Resolve only follows announcements matching an already pinned identity.
// TLS verifies possession of that certificate before any credentials are sent.
func (b *Browser) Resolve(pin, fallback string) string {
	for _, peer := range b.Snapshot() {
		if peer.Pin == pin && peer.Ready {
			return peer.Address
		}
	}
	return fallback
}
func (b *Browser) receive(ctx context.Context) {
	defer close(b.done)
	data := make([]byte, maxPacket+1)
	lastQuery := time.Time{}
	for {
		n, source, err := b.conn.ReadFromUDP(data)
		if err != nil {
			return
		}
		var q Announcement
		if n <= maxPacket && json.Unmarshal(data[:n], &q) == nil && q.Protocol == "superflow/2" && q.Query {
			if time.Since(lastQuery) > time.Second {
				lastQuery = time.Now()
				b.Refresh()
			}
			continue
		}
		peer, err := Decode(data[:n], source.IP, time.Now())
		if err != nil || peer.ID == b.self().ID {
			continue
		}
		b.mu.Lock()
		if len(b.peers) < 128 {
			b.peers[peer.ID+peer.Address] = peer
		} else if _, ok := b.peers[peer.ID+peer.Address]; ok {
			b.peers[peer.ID+peer.Address] = peer
		}
		b.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
	}
}
func (b *Browser) send(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	transmit := func(data []byte) {
		if len(b.interfaces) == 0 {
			_, _ = b.conn.WriteToUDP(data, b.address)
			return
		}
		for i := range b.interfaces {
			_ = b.packet.SetMulticastInterface(&b.interfaces[i])
			_, _ = b.conn.WriteToUDP(data, b.address)
		}
	}
	transmit([]byte(`{"protocol":"superflow/2","query":true}`))
	for {
		if data, err := Encode(b.self()); err == nil {
			transmit(data)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-b.wake:
		}
	}
}
