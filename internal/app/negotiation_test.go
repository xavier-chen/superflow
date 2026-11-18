package app

import (
	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/transport"
	"superflow.local/superflow/internal/wire"
	"testing"
)

func TestAutomaticCodecFitsEverySelectedTarget(t *testing.T) {
	good := &transport.Session{Peer: transport.Hello{Codecs: []string{"pcm", "opus"}}}
	legacy := &transport.Session{Peer: transport.Hello{}}
	peers := map[string]*transport.Session{"capable": good, "legacy": legacy}
	for _, tc := range []struct {
		name, target string
		available    bool
		want         byte
	}{
		{"mixed group", "", true, wire.PCM},
		{"selected capable target", "capable", true, wire.Opus},
		{"local runtime missing", "capable", false, wire.PCM},
		{"legacy peer", "legacy", true, wire.PCM},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := negotiatedCodec(tc.available, tc.target, peers); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}

func TestDiscoveryHintCannotRemoveOriginalPinnedDestination(t *testing.T) {
	hub, _ := config.New("hub", "hub")
	node, _ := config.New("node", "node")
	code, _ := hub.Code()
	if err := node.Pair(code, "192.168.1.20:42800"); err != nil {
		t.Fatal(err)
	}
	a, err := New(node)
	if err != nil {
		t.Fatal(err)
	}
	a.ResolveServer = func(pin, fallback string) string { return "192.168.1.99:42800" }
	candidate := a.connectionConfig()
	if candidate.Server != "192.168.1.99:42800" || len(candidate.ServerAlternates) != 1 || candidate.ServerAlternates[0] != node.Server || a.config.Server != node.Server {
		t.Fatal("an untrusted discovery hint replaced the saved fallback")
	}
}
