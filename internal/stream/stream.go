package stream

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"streamsonic/internal/config"
	"streamsonic/internal/dvr"
	"streamsonic/internal/events"
	"streamsonic/internal/media"
	"streamsonic/internal/packager"
	"streamsonic/internal/session"
)

type Stream struct {
	name string
	mgr  *Manager

	mu      sync.RWMutex
	cfg     *config.Stream
	status  string
	lastErr string
	info    media.MediaInfo
	started time.Time

	hls     *packager.Ring
	dashV   *packager.Ring
	dashA   *packager.Ring
	preview *packager.Ring
	muxV    *media.TrackMuxer
	muxA    *media.TrackMuxer
	muxM    *media.MultiMuxer
	seq     int
	seqA    int
	seqP    int
	arch    *dvr.Archive

	ctx    context.Context
	cancel context.CancelFunc

	bytesIn     atomic.Int64
	bytesOut    atomic.Int64
	segments    atomic.Int64
	clients     atomic.Int64
	inputErrors atomic.Int64
	switches    atomic.Int64
	lastPublish atomic.Int64
	bitrate     atomic.Int64
	preferred   atomic.Int32
	lastSegTime time.Time
}

func newStream(mgr *Manager, cfg *config.Stream) *Stream {
	n := cfg.SegmentCountOrDefault()
	s := &Stream{
		name:    cfg.Name,
		mgr:     mgr,
		cfg:     cfg,
		status:  "offline",
		hls:     packager.NewRing(n, nil),
		dashV:   packager.NewRing(n, nil),
		dashA:   packager.NewRing(n, nil),
		preview: packager.NewRing(n, nil),
	}
	s.preferred.Store(-1)
	return s
}

func (s *Stream) Name() string { return s.name }

func (s *Stream) HLS() *packager.Ring       { return s.hls }
func (s *Stream) DASHVideo() *packager.Ring { return s.dashV }
func (s *Stream) DASHAudio() *packager.Ring { return s.dashA }
func (s *Stream) Preview() *packager.Ring   { return s.preview }

func (s *Stream) Codecs() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var parts []string
	for _, t := range s.info.Tracks {
		if t.Codec == media.CodecH264 && len(t.SPS) >= 4 {
			parts = append(parts, fmt.Sprintf("avc1.%02x%02x%02x", t.SPS[1], t.SPS[2], t.SPS[3]))
			break
		}
	}
	for _, t := range s.info.Tracks {
		if t.Codec == media.CodecAAC {
			parts = append(parts, "mp4a.40.2")
			break
		}
	}
	return strings.Join(parts, ",")
}
func (s *Stream) Archive() *dvr.Archive { return s.arch }

func (s *Stream) Config() *config.Stream {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Stream) SetConfig(cfg *config.Stream) {
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
}

func (s *Stream) Info() media.MediaInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info
}

func (s *Stream) AddClient() { s.clients.Add(1) }
func (s *Stream) RemoveClient() {
	if s.clients.Load() > 0 {
		s.clients.Add(-1)
	}
}

func (s *Stream) Clients() int { return int(s.clients.Load()) }

func (s *Stream) AddOutputBytes(n int64) { s.bytesOut.Add(n) }

func (s *Stream) Start(parent context.Context) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.ctx = ctx
	s.cancel = cancel
	s.started = time.Now()
	s.mu.Unlock()
	go s.run(ctx)
}

func (s *Stream) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.ctx = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.mu.Lock()
	s.status = "offline"
	s.mu.Unlock()
}

func (s *Stream) Restart(parent context.Context) {
	s.Stop()
	time.Sleep(100 * time.Millisecond)
	s.Start(parent)
}

func (s *Stream) run(ctx context.Context) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		inputs := s.orderedInputs()
		if len(inputs) == 0 {
			s.setStatus("offline", "no inputs configured")
			if !sleep(ctx, 2*time.Second) {
				return
			}
			continue
		}
		if p := int(s.preferred.Swap(-1)); p >= 0 && p < len(inputs) {
			inputs = append(append([]config.Input{}, inputs[p:]...), inputs[:p]...)
		}
		for i, in := range inputs {
			if ctx.Err() != nil {
				return
			}
			if i > 0 {
				s.switches.Add(1)
			}
			err := s.play(ctx, in)
			if ctx.Err() != nil {
				return
			}
			if in.Type == "file" {
				if !sleep(ctx, 200*time.Millisecond) {
					return
				}
				break
			}
			s.inputErrors.Add(1)
			reason := "source closed"
			if err != nil {
				reason = err.Error()
			}
			s.setStatus("error", reason)
			s.mgr.events.Emit(events.Event{Name: "source_lost", Stream: s.name,
				Data: map[string]any{"input": in.URL, "reason": reason}})
			if !sleep(ctx, backoff) {
				return
			}
			if backoff < 15*time.Second {
				backoff *= 2
			}
		}
		backoff = time.Second
		if !sleep(ctx, time.Second) {
			return
		}
	}
}

func (s *Stream) orderedInputs() []config.Input {
	cfg := s.Config()
	inputs := append([]config.Input(nil), cfg.Inputs...)
	sort.SliceStable(inputs, func(i, j int) bool {
		return inputs[i].Priority > inputs[j].Priority
	})
	return inputs
}

func (s *Stream) play(ctx context.Context, in config.Input) error {
	src, err := OpenSource(ctx, in)
	if err != nil {
		return err
	}
	defer src.Close()

	timeout := time.Duration(in.SourceTimeout) * time.Second
	var watchdog *time.Timer
	var demux *media.Demuxer
	demux = media.NewDemuxer(src)
	seg := media.NewSegmenter(demux.Info(), time.Duration(s.Config().Duration()*float64(time.Second)))
	seg.SetBase(time.Now())

	if timeout > 0 {
		watchdog = time.AfterFunc(timeout, func() {
			if demux.Packets() == 0 {
				src.Close()
			}
		})
		defer watchdog.Stop()
	}

	demux.OnInfo = func(info *media.MediaInfo) {
		s.setInfo(info)
		s.ensureDash()
	}

	paced := in.Type == "file" || in.Type == "hls" || in.Type == "hlss"
	var wallBase time.Time
	var ptsBase int64
	var anchored bool
	pace := func(f media.Frame) {
		if !paced {
			return
		}
		if hasVideoTrack(demux.Info()) != (f.Kind == media.KindVideo) {
			return
		}
		if !anchored {
			wallBase = time.Now()
			ptsBase = f.PTS
			anchored = true
			return
		}
		delta := time.Duration(f.PTS-ptsBase) * time.Second / 90000
		if delta < -2*time.Second {
			wallBase = time.Now()
			ptsBase = f.PTS
			return
		}
		if wait := delta - time.Since(wallBase); wait > 0 {
			sleep(ctx, wait)
		}
	}

	frames := 0
	demux.OnFrame = func(f media.Frame) error {
		if frames == 0 {
			s.setStatus("online", "")
			s.mgr.events.Emit(events.Event{Name: "stream_online", Stream: s.name})
		}
		frames++
		pace(f)
		if frames%250 == 0 {
			s.bytesIn.Store(demux.Packets() * media.PacketSize)
		}
		for _, closed := range seg.Push(f) {
			s.publish(&closed)
		}
		return nil
	}

	err = demux.Run(ctx)
	for _, closed := range seg.Flush() {
		s.publish(&closed)
	}
	s.bytesIn.Store(demux.Packets() * media.PacketSize)
	return err
}

func (s *Stream) setInfo(info *media.MediaInfo) {
	s.mu.Lock()
	s.info = *info
	s.mu.Unlock()
}

func (s *Stream) setStatus(status, reason string) {
	s.mu.Lock()
	prev := s.status
	s.status = status
	s.lastErr = reason
	s.mu.Unlock()
	if prev != status {
		s.mgr.events.Emit(events.Event{Name: "stream_state", Stream: s.name,
			Data: map[string]any{"status": status, "reason": reason}})
	}
}

func (s *Stream) Status() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *Stream) publish(segment *media.Segment) {
	if len(segment.Frames) == 0 {
		return
	}
	info := s.Info()
	chunk := &packager.Chunk{
		Seq:      s.seq,
		From:     segment.From,
		Duration: segment.Duration,
		Start:    segment.StartPTS,
	}
	if last := s.hls.Last(); last != nil {
		gap := segment.From.Sub(last.From.Add(last.Duration))
		if gap > 500*time.Millisecond || gap < -500*time.Millisecond || segment.Disc {
			chunk.Disc = true
		}
	}
	ts, err := s.marshalTS(&info, segment)
	if err != nil {
		s.setStatus("error", err.Error())
		return
	}
	chunk.Data = ts
	s.hls.Add(chunk)
	s.seq++
	s.segments.Add(1)
	s.lastPublish.Store(time.Now().UnixMilli())

	if s.arch != nil {
		if err := s.arch.Append(segment.From, segment.Duration, ts); err != nil {
			s.mgr.logf("dvr append %s: %v", s.name, err)
		}
	}

	s.ensureDash()
	s.mu.RLock()
	muxV, muxA, muxM := s.muxV, s.muxA, s.muxM
	s.mu.RUnlock()
	if muxV != nil {
		if vf := segment.Video(); len(vf) > 0 {
			frag := muxV.Fragment(vf)
			if len(frag) > 0 {
				s.dashV.Add(&packager.Chunk{
					Seq:      s.seqA,
					From:     segment.From,
					Duration: segment.Duration,
					Start:    vf[0].DTS,
					Data:     frag,
				})
			}
		}
	}
	if muxA != nil {
		if af := segment.Audio(); len(af) > 0 {
			frag := muxA.Fragment(af)
			if len(frag) > 0 {
				start := int64(float64(af[0].PTS) * float64(muxA.Timescale()) / 90000)
				s.dashA.Add(&packager.Chunk{
					Seq:      s.seqA,
					From:     segment.From,
					Duration: segment.Duration,
					Start:    start,
					Data:     frag,
				})
			}
		}
	}
	s.seqA++

	if muxM != nil {
		if frag := muxM.Fragment(segment.Video(), segment.Audio()); len(frag) > 0 {
			s.preview.Add(&packager.Chunk{
				Seq:      s.seqP,
				From:     segment.From,
				Duration: segment.Duration,
				Start:    segment.StartPTS,
				Data:     frag,
			})
			s.seqP++
		}
	}

	if segment.Duration > 0 {
		bps := int64(float64(len(ts)*8) / segment.Duration.Seconds())
		prev := s.bitrate.Load()
		if prev == 0 {
			s.bitrate.Store(bps)
		} else {
			s.bitrate.Store(prev*3/4 + bps/4)
		}
	}
	s.mgr.events.Emit(events.Event{Name: "segment_finished", Stream: s.name,
		Data: map[string]any{"from": segment.From.UTC().Format(time.RFC3339),
			"duration": segment.Duration.Seconds(), "size": len(ts)}})
	s.mgr.cleanupClients(s)
}

func (s *Stream) marshalTS(info *media.MediaInfo, segment *media.Segment) ([]byte, error) {
	filtered := &media.MediaInfo{}
	seenVideo, seenAudio := false, false
	for _, t := range info.Tracks {
		if t.Type == media.KindVideo && !seenVideo {
			filtered.Tracks = append(filtered.Tracks, t)
			seenVideo = true
		}
		if t.Type == media.KindAudio && !seenAudio {
			filtered.Tracks = append(filtered.Tracks, t)
			seenAudio = true
		}
	}
	if len(filtered.Tracks) == 0 {
		return nil, fmt.Errorf("no supported tracks in segment")
	}
	var buf bytes.Buffer
	mux := media.NewTSMuxer(&buf, filtered)
	if err := mux.WritePSI(); err != nil {
		return nil, err
	}
	for _, f := range segment.Frames {
		if f.Kind != media.KindVideo && f.Kind != media.KindAudio {
			continue
		}
		if err := mux.WriteFrame(f); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func (s *Stream) ensureDash() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.info.Tracks {
		if t.Codec == media.CodecH264 && s.muxV == nil {
			m, err := media.NewTrackMuxer(t, 1)
			if err == nil {
				s.muxV = m
				s.dashV.SetInit(m.Init())
			}
		}
		if t.Codec == media.CodecAAC && s.muxA == nil {
			m, err := media.NewTrackMuxer(t, 2)
			if err == nil {
				s.muxA = m
				s.dashA.SetInit(m.Init())
			}
		}
	}
	if s.muxM == nil {
		var vt, at *media.Track
		for _, t := range s.info.Tracks {
			if t.Codec == media.CodecH264 && vt == nil {
				vt = t
			}
			if t.Codec == media.CodecAAC && at == nil {
				at = t
			}
		}
		if vt != nil || at != nil {
			if mm, err := media.NewMultiMuxer(vt, at); err == nil {
				s.muxM = mm
				s.preview.SetInit(mm.Init())
			}
		}
	}
}

func (s *Stream) Snapshot() map[string]any {
	s.mu.RLock()
	status := s.status
	lastErr := s.lastErr
	info := s.info
	started := s.started
	cfg := s.cfg
	s.mu.RUnlock()

	stats := map[string]any{
		"bytes_in":       s.bytesIn.Load(),
		"bytes_out":      s.bytesOut.Load(),
		"segments":       s.segments.Load(),
		"clients":        s.clients.Load(),
		"input_errors":   s.inputErrors.Load(),
		"input_switches": s.switches.Load(),
		"bitrate":        s.bitrate.Load(),
		"last_publish":   s.lastPublish.Load(),
		"hls_segments":   s.hls.Len(),
		"dash_segments":  s.dashV.Len(),
	}
	out := map[string]any{
		"name":       s.name,
		"status":     status,
		"stats":      stats,
		"media_info": info,
		"started":    started.UTC().Format(time.RFC3339),
	}
	if lastErr != "" {
		out["last_error"] = lastErr
	}
	if cfg != nil {
		out["comment"] = cfg.Comment
		out["title"] = cfg.Title
		out["ondemand"] = cfg.Ondemand
		out["disabled"] = cfg.Disabled
	}
	if s.arch != nil {
		out["dvr"] = map[string]any{
			"from":    s.arch.From().UTC().Format(time.RFC3339),
			"to":      s.arch.To().UTC().Format(time.RFC3339),
			"size":    s.arch.Size(),
			"entries": len(s.arch.Entries()),
		}
	}
	return out
}

func hasVideoTrack(info *media.MediaInfo) bool {
	if info == nil {
		return false
	}
	for _, t := range info.Tracks {
		if t.Type == media.KindVideo {
			return true
		}
	}
	return false
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

type Manager struct {
	mu       sync.RWMutex
	streams  map[string]*Stream
	cfg      *config.Config
	events   *events.Bus
	sessions *session.Registry
	parent   context.Context
	dvrRoot  string
}

func NewManager(cfg *config.Config, bus *events.Bus, reg *session.Registry, dvrRoot string) *Manager {
	return &Manager{
		streams:  map[string]*Stream{},
		cfg:      cfg,
		events:   bus,
		sessions: reg,
		dvrRoot:  dvrRoot,
	}
}

func (m *Manager) Events() *events.Bus         { return m.events }
func (m *Manager) Sessions() *session.Registry { return m.sessions }
func (m *Manager) Config() *config.Config      { return m.cfg }

func (m *Manager) Get(name string) *Stream {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.streams[name]
}

func (m *Manager) List() []*Stream {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Stream, 0, len(m.streams))
	for _, s := range m.streams {
		out = append(out, s)
	}
	return out
}

func (m *Manager) Start(ctx context.Context) {
	m.parent = ctx
	m.Apply(m.cfg)
}

func (m *Manager) Restart(name string) bool {
	m.mu.RLock()
	s := m.streams[name]
	parent := m.parent
	m.mu.RUnlock()
	if s == nil || parent == nil {
		return false
	}
	s.Restart(parent)
	return true
}

func (m *Manager) SelectInput(name string, index int) bool {
	m.mu.RLock()
	s := m.streams[name]
	parent := m.parent
	m.mu.RUnlock()
	if s == nil || parent == nil {
		return false
	}
	cfg := s.Config()
	if cfg == nil || index < 0 || index >= len(cfg.Inputs) {
		return false
	}
	s.preferred.Store(int32(index))
	s.Restart(parent)
	return true
}

func (m *Manager) EnsureStarted(name string) *Stream {
	m.mu.RLock()
	s := m.streams[name]
	parent := m.parent
	m.mu.RUnlock()
	if s == nil {
		return nil
	}
	if cfg := s.Config(); cfg != nil && cfg.Disabled {
		return s
	}
	if parent != nil {
		s.Start(parent)
	}
	return s
}

func (m *Manager) Stop() {
	m.mu.Lock()
	streams := make([]*Stream, 0, len(m.streams))
	for _, s := range m.streams {
		streams = append(streams, s)
	}
	m.mu.Unlock()
	for _, s := range streams {
		s.Stop()
	}
}

func (m *Manager) Apply(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
	for name, sc := range cfg.Streams {
		if existing, ok := m.streams[name]; ok {
			existing.SetConfig(sc)
			m.attachDVR(existing, sc)
			continue
		}
		s := newStream(m, sc)
		m.attachDVR(s, sc)
		m.streams[name] = s
		if m.parent != nil && !sc.Disabled && !sc.Ondemand {
			s.Start(m.parent)
		}
	}
	for name, s := range m.streams {
		if _, ok := cfg.Streams[name]; !ok {
			s.Stop()
			delete(m.streams, name)
		}
	}
}

func (m *Manager) attachDVR(s *Stream, sc *config.Stream) {
	if sc.DVR == nil || m.dvrRoot == "" {
		return
	}
	root := m.dvrRoot
	expiration := time.Duration(0)
	limit := int64(0)
	if sc.DVR.Reference != "" {
		if d, ok := m.cfg.DVRs[sc.DVR.Reference]; ok {
			if d.Root != "" {
				root = d.Root
			}
			expiration = time.Duration(d.Expiration) * time.Second
			limit = d.StorageLimit
		}
	}
	if sc.DVR.Expiration > 0 {
		expiration = time.Duration(sc.DVR.Expiration) * time.Second
	}
	if sc.DVR.StorageLimit > 0 {
		limit = sc.DVR.StorageLimit
	}
	arch, err := dvr.Open(dvr.Options{Root: root, Expiration: expiration, StorageLimit: limit}, sc.Name)
	if err != nil {
		m.logf("dvr %s: %v", sc.Name, err)
		return
	}
	s.arch = arch
}

func (m *Manager) cleanupClients(s *Stream) {
	timeout := 30 * time.Second
	if c := s.Config(); c != nil && c.ClientsTimeout != nil && *c.ClientsTimeout > 0 {
		timeout = time.Duration(*c.ClientsTimeout) * time.Second
	}
	m.sessions.Sweep(timeout)
}

func (m *Manager) logf(format string, args ...any) {
	if m.events == nil {
		return
	}
	m.events.Emit(events.Event{Name: "log", Data: map[string]any{"message": fmt.Sprintf(format, args...)}})
}
