package audio

import "sync/atomic"

// Ring is a single-producer / single-consumer byte ring. Audio callbacks don't allocate.
type Ring struct {
	data        []byte
	read, write atomic.Uint64
}

func NewRing(capacity int) *Ring { return &Ring{data: make([]byte, capacity)} }
func (r *Ring) Write(b []byte) int {
	w, rd := r.write.Load(), r.read.Load()
	space := uint64(len(r.data)) - (w - rd)
	n := len(b)
	if uint64(n) > space {
		n = int(space)
	}
	n -= n % 4
	for i := 0; i < n; i++ {
		r.data[(w+uint64(i))%uint64(len(r.data))] = b[i]
	}
	r.write.Store(w + uint64(n))
	return n
}
func (r *Ring) Read(out []byte) int {
	rd, w := r.read.Load(), r.write.Load()
	n := len(out)
	if uint64(n) > w-rd {
		n = int(w - rd)
	}
	n -= n % 4
	for i := 0; i < n; i++ {
		out[i] = r.data[(rd+uint64(i))%uint64(len(r.data))]
	}
	r.read.Store(rd + uint64(n))
	return n
}
