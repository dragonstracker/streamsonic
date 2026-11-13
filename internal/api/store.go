package api

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"streamsonic/internal/config"
)

// Store holds the live configuration and persists every accepted change.
type Store struct {
	mu   sync.RWMutex
	cfg  *config.Config
	path string
	rev  atomic.Int64
}

func NewStore(cfg *config.Config, path string) *Store {
	s := &Store{cfg: cfg, path: path}
	s.rev.Store(1)
	return s
}

func (s *Store) Current() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Store) Rev() int64 { return s.rev.Load() }

func (s *Store) Update(fn func(c *config.Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg.Clone()
	if err := fn(next); err != nil {
		return err
	}
	if err := s.save(next); err != nil {
		return err
	}
	next.Rev = s.rev.Add(1)
	s.cfg = next
	return nil
}

func (s *Store) Replace(next *config.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.save(next); err != nil {
		return err
	}
	next.Rev = s.rev.Add(1)
	s.cfg = next
	return nil
}

func (s *Store) save(c *config.Config) error {
	if s.path == "" {
		return nil
	}
	if err := config.Validate(c); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(c.RenderText()), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return os.Rename(tmp, s.path)
}
