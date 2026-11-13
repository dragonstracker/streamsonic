package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"
)

type Session struct {
	ID        string    `json:"id"`
	Stream    string    `json:"stream"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent,omitempty"`
	Protocol  string    `json:"protocol"`
	Referer   string    `json:"referer,omitempty"`
	Started   time.Time `json:"started"`
	lastSeen  atomic.Int64
	sent      atomic.Int64
	token     string
}

func (s *Session) AddBytes(n int64) { s.sent.Add(n) }
func (s *Session) Sent() int64      { return s.sent.Load() }
func (s *Session) Touch()           { s.lastSeen.Store(time.Now().UnixMilli()) }
func (s *Session) Idle() time.Duration {
	return time.Duration(time.Now().UnixMilli()-s.lastSeen.Load()) * time.Millisecond
}

func (s *Session) View() map[string]any {
	return map[string]any{
		"id":         s.ID,
		"stream":     s.Stream,
		"ip":         s.IP,
		"user_agent": s.UserAgent,
		"protocol":   s.Protocol,
		"referer":    s.Referer,
		"started":    s.Started.UTC().Format(time.RFC3339),
		"bytes":      s.Sent(),
		"played":     int64(time.Since(s.Started).Seconds()),
	}
}

type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewRegistry() *Registry {
	return &Registry{sessions: map[string]*Session{}}
}

func NewID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (r *Registry) Add(s *Session) {
	if s.ID == "" {
		s.ID = NewID()
	}
	s.Touch()
	r.mu.Lock()
	r.sessions[s.ID] = s
	r.mu.Unlock()
}

func (r *Registry) Remove(id string) {
	r.mu.Lock()
	delete(r.sessions, id)
	r.mu.Unlock()
}

func (r *Registry) Get(id string) *Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessions[id]
}

func (r *Registry) List(stream string) []*Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		if stream == "" || s.Stream == stream {
			out = append(out, s)
		}
	}
	return out
}

func (r *Registry) Count(stream string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if stream == "" {
		return len(r.sessions)
	}
	n := 0
	for _, s := range r.sessions {
		if s.Stream == stream {
			n++
		}
	}
	return n
}

func (r *Registry) Sweep(idle time.Duration) {
	r.mu.Lock()
	for id, s := range r.sessions {
		if s.Idle() > idle {
			delete(r.sessions, id)
		}
	}
	r.mu.Unlock()
}
