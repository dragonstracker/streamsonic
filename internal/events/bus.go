package events

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"streamsonic/internal/config"
)

type Event struct {
	Name   string         `json:"name"`
	Stream string         `json:"stream,omitempty"`
	Time   int64          `json:"time"`
	Data   map[string]any `json:"data,omitempty"`
}

type Bus struct {
	mu    sync.RWMutex
	sinks []*sink
}

type sink struct {
	cfg    config.EventSink
	client *http.Client
	queue  chan Event
}

func NewBus(cfg []*config.EventSink) *Bus {
	b := &Bus{}
	b.Update(cfg)
	return b
}

func (b *Bus) Update(cfg []*config.EventSink) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.sinks {
		close(s.queue)
	}
	b.sinks = nil
	for _, c := range cfg {
		s := &sink{cfg: *c, client: &http.Client{Timeout: 5 * time.Second}, queue: make(chan Event, 4096)}
		b.sinks = append(b.sinks, s)
		go s.run()
	}
}

func (b *Bus) Emit(e Event) {
	if e.Time == 0 {
		e.Time = time.Now().UnixMilli()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.sinks {
		if !s.match(e) {
			continue
		}
		select {
		case s.queue <- e:
		default:
		}
	}
}

func (s *sink) match(e Event) bool {
	if len(s.cfg.Only) > 0 {
		ok := false
		for _, m := range s.cfg.Only {
			for name, streams := range m {
				if name != e.Name {
					continue
				}
				if len(streams) == 0 || contains(streams, e.Stream) {
					ok = true
				}
			}
		}
		if !ok {
			return false
		}
	}
	for _, m := range s.cfg.Except {
		for name, streams := range m {
			if name == e.Name && (len(streams) == 0 || contains(streams, e.Stream)) {
				return false
			}
		}
	}
	return true
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func (s *sink) run() {
	delay := time.Duration(s.cfg.ThrottleDelay) * time.Millisecond
	if delay < 50*time.Millisecond {
		delay = 200 * time.Millisecond
	}
	var batch []Event
	timer := time.NewTimer(delay)
	for {
		select {
		case e, ok := <-s.queue:
			if !ok {
				return
			}
			batch = append(batch, e)
			if len(batch) >= 100 {
				s.flush(batch)
				batch = nil
				timer.Reset(delay)
			}
		case <-timer.C:
			if len(batch) > 0 {
				s.flush(batch)
				batch = nil
			}
			timer.Reset(delay)
		}
	}
}

func (s *sink) flush(batch []Event) {
	if s.cfg.URL == "" {
		return
	}
	body, err := json.Marshal(map[string]any{"events": batch})
	if err != nil {
		return
	}
	for attempt := 0; attempt <= s.cfg.ResendLimit; attempt++ {
		req, err := http.NewRequest(http.MethodPost, s.cfg.URL, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range s.cfg.Extra {
			req.Header.Set(k, v)
		}
		resp, err := s.client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 300 {
				return
			}
		}
		time.Sleep(time.Duration(s.cfg.ResendTimeout) * time.Millisecond)
	}
}
