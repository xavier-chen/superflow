package audio

type StreamStats struct {
	Received, Concealed, Late, Trimmed uint64
	BufferedMS                         int
}
type PeerStats struct{ System, Microphone StreamStats }

func (m *Manager) SetGain(peer string, gain float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gains[peer] = gain
}
func (m *Manager) Gain(peer string) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if gain, ok := m.gains[peer]; ok {
		return gain
	}
	return 1
}

func (b *Buffer) stats() StreamStats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return StreamStats{Received: b.Received, Concealed: b.Concealed, Late: b.Late, Trimmed: b.Trimmed, BufferedMS: len(b.packets)*b.codec.Frames/48 + len(b.current)/4/48}
}
func (m *Manager) Stats(peer string) PeerStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out PeerStats
	for key, buffer := range m.buffers {
		if key.Peer == peer {
			if key.Channel == 1 {
				out.System = buffer.stats()
			} else if key.Channel == 2 {
				out.Microphone = buffer.stats()
			}
		}
	}
	return out
}
