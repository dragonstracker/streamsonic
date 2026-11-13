package packager

import (
	"sync"
	"time"
)

// Chunk is one packaged media segment.
type Chunk struct {
	Seq      int
	Start    int64
	From     time.Time
	Duration time.Duration
	Data     []byte
	Disc     bool
}

// Ring keeps the live window for one rendition.
type Ring struct {
	mu      sync.RWMutex
	chunks  []*Chunk
	max     int
	init    []byte
	target  time.Duration
	started bool
	discSeq int
}

func NewRing(max int, init []byte) *Ring {
	if max < 1 {
		max = 1
	}
	return &Ring{max: max, init: init}
}

func (r *Ring) SetInit(b []byte) {
	r.mu.Lock()
	r.init = b
	r.mu.Unlock()
}

func (r *Ring) Init() []byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.init
}

func (r *Ring) Add(c *Chunk) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chunks = append(r.chunks, c)
	if c.Disc {
		r.discSeq++
	}
	if d := c.Duration; d > r.target {
		r.target = d
	}
	for len(r.chunks) > r.max {
		r.chunks = r.chunks[1:]
	}
	r.started = true
}

func (r *Ring) Snapshot() []*Chunk {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Chunk, len(r.chunks))
	copy(out, r.chunks)
	return out
}

func (r *Ring) Target() time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.target
}

func (r *Ring) DiscSeq() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.discSeq
}

func (r *Ring) Get(seq int) *Chunk {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.chunks {
		if c.Seq == seq {
			return c
		}
	}
	return nil
}

func (r *Ring) Last() *Chunk {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.chunks) == 0 {
		return nil
	}
	return r.chunks[len(r.chunks)-1]
}

func (r *Ring) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.chunks)
}

func (r *Ring) Reset() {
	r.mu.Lock()
	r.chunks = nil
	r.started = false
	r.mu.Unlock()
}
