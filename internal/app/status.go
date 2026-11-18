package app

import (
	"errors"
	"math"
	"sort"
	"time"

	"superflow.local/superflow/internal/audio"
)

type PeerStatus struct {
	Gain                                  float64 `json:"gain"`
	ID                                    string  `json:"id"`
	Name                                  string  `json:"name"`
	Address                               string  `json:"address"`
	RTTMS                                 float64 `json:"rtt_ms"`
	BytesSent, BytesReceived, PacketsLost uint64
	DroppedAudio, DroppedMotion           uint64
	Audio                                 audio.PeerStats `json:"audio"`
}
type Status struct {
	Name, Mode, State, Target, ListenAddress, LastError string
	Started                                             time.Time
	Input, Clipboard                                    bool
	Peers                                               []PeerStatus
}

// Snapshot contains operational metadata only, never tokens, certificates or clipboard contents.
func (a *App) Snapshot() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := Status{Name: a.config.Name, Mode: a.config.Mode, State: a.status, Target: a.focus.Load().(string), ListenAddress: a.listenAddress, LastError: a.lastError, Started: a.started, Input: a.config.Input, Clipboard: a.config.Clipboard}
	for _, peer := range a.peers {
		n := peer.Statistics()
		p := PeerStatus{ID: peer.Peer.ID, Name: peer.Peer.Name, Address: peer.RemoteAddr(), RTTMS: float64(n.SmoothedRTT) / float64(time.Millisecond), BytesSent: n.BytesSent, BytesReceived: n.BytesReceived, PacketsLost: n.PacketsLost, DroppedAudio: peer.DroppedAudio.Load(), DroppedMotion: peer.DroppedMotion.Load()}
		if a.audio != nil {
			p.Audio = a.audio.Stats(peer.Peer.ID)
			p.Gain = a.audio.Gain(peer.Peer.ID)
		}
		s.Peers = append(s.Peers, p)
	}
	sort.Slice(s.Peers, func(i, j int) bool { return s.Peers[i].Name < s.Peers[j].Name })
	return s
}
func (a *App) Select(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.canControl || a.router == nil || a.config.Mode != "hub" {
		return errors.New("键鼠主控尚未运行")
	}
	return a.router.Select(name)
}
func (a *App) ReturnLocal() error { return a.Select(a.config.Name) }
func (a *App) SetGain(name string, gain float64) error {
	if math.IsNaN(gain) || math.IsInf(gain, 0) || gain < 0 || gain > 2 {
		return errors.New("invalid gain")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.gains[name] = gain
	peer := a.peers[name]
	if peer == nil || a.audio == nil {
		return nil
	}
	a.audio.SetGain(peer.Peer.ID, gain)
	return nil
}
