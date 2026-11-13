package media

import (
	"sort"
	"time"
)

// Segment is one closed chunk of media addressed by its wall-clock start time.
type Segment struct {
	Seq      int
	From     time.Time
	Duration time.Duration
	Frames   []Frame
	StartPTS int64
	EndPTS   int64
	Keyframe bool
	Disc     bool
}

func (s *Segment) Video() []Frame {
	var out []Frame
	for _, f := range s.Frames {
		if f.Kind == KindVideo {
			out = append(out, f)
		}
	}
	return out
}

func (s *Segment) Audio() []Frame {
	var out []Frame
	for _, f := range s.Frames {
		if f.Kind == KindAudio {
			out = append(out, f)
		}
	}
	return out
}

// Segmenter cuts a frame stream into GOP aligned segments of roughly the
// requested duration. Timing is driven by video PTS when present.
type Segmenter struct {
	info     *MediaInfo
	duration time.Duration
	base     time.Time
	haveBase bool
	cur      []Frame
	curStart int64
	curEnd   int64
	vStart   int64
	vEnd     int64
	haveVSeg bool
	seq      int
	fpsEMA   float64
	lastVPTS int64
	haveV    bool
}

func NewSegmenter(info *MediaInfo, duration time.Duration) *Segmenter {
	if duration <= 0 {
		duration = 6 * time.Second
	}
	return &Segmenter{info: info, duration: duration}
}

func (s *Segmenter) SetBase(t time.Time) {
	s.base = t
	s.haveBase = true
}

func (s *Segmenter) ticks(d time.Duration) int64 {
	return int64(d.Seconds() * 90000)
}

func (s *Segmenter) reset(pts int64) {
	s.curStart, s.curEnd = pts, pts
	s.vStart, s.vEnd = pts, pts
	s.haveVSeg = false
}

func (s *Segmenter) elapsed() time.Duration {
	if s.haveVSeg {
		return time.Duration(s.vEnd-s.vStart) * time.Second / 90000
	}
	return time.Duration(s.curEnd-s.curStart) * time.Second / 90000
}

func (s *Segmenter) Push(f Frame) []Segment {
	if !s.haveBase {
		s.base = time.Now().Add(-time.Duration(f.PTS) * time.Second / 90000)
		s.haveBase = true
	}
	isVideo := f.Kind == KindVideo
	if isVideo {
		if s.haveV && f.PTS > s.lastVPTS {
			d := float64(f.PTS-s.lastVPTS) / 90000.0
			if d > 0 && d < 1 {
				if s.fpsEMA == 0 {
					s.fpsEMA = 1 / d
				} else {
					s.fpsEMA = s.fpsEMA*0.9 + (1/d)*0.1
				}
			}
		}
		s.lastVPTS = f.PTS
		s.haveV = true
	}

	var out []Segment
	if len(s.cur) == 0 {
		s.reset(f.PTS)
	} else if isVideo && f.PTS+s.ticks(30*time.Second) < s.vStart {
		out = append(out, s.close(true))
		s.reset(f.PTS)
	} else if !s.haveVSeg && f.PTS+s.ticks(30*time.Second) < s.curStart {
		out = append(out, s.close(true))
		s.reset(f.PTS)
	}

	elapsed := s.elapsed()
	switch {
	case isVideo && f.Keyframe && elapsed >= s.duration:
		out = append(out, s.close(false))
		s.reset(f.PTS)
	case !isVideo && !s.haveVSeg && elapsed >= s.duration:
		out = append(out, s.close(false))
		s.reset(f.PTS)
	case len(s.cur) > 8192:
		out = append(out, s.close(false))
		s.reset(f.PTS)
	}

	s.cur = append(s.cur, f)
	if f.PTS > s.curEnd {
		s.curEnd = f.PTS
	}
	if isVideo {
		if !s.haveVSeg {
			s.haveVSeg = true
			s.vStart = f.PTS
		}
		if f.PTS > s.vEnd {
			s.vEnd = f.PTS
		}
	}
	return out
}

func (s *Segmenter) Flush() []Segment {
	if len(s.cur) == 0 {
		return nil
	}
	return []Segment{s.close(false)}
}

func (s *Segmenter) close(disc bool) Segment {
	frames := s.cur
	sort.SliceStable(frames, func(i, j int) bool { return frames[i].PTS < frames[j].PTS })
	start, end := s.curStart, s.curEnd
	if s.haveVSeg {
		start, end = s.vStart, s.vEnd
	}
	seg := Segment{
		Seq:      s.seq,
		From:     s.base.Add(time.Duration(start) * time.Second / 90000),
		StartPTS: start,
		EndPTS:   end,
		Frames:   frames,
		Disc:     disc,
	}
	seg.Duration = time.Duration(end-start) * time.Second / 90000
	for _, f := range frames {
		if f.Kind == KindVideo && f.Keyframe {
			seg.Keyframe = true
			break
		}
	}
	if seg.Duration <= 0 {
		seg.Duration = s.duration
	}
	s.seq++
	s.cur = nil
	if info := s.info; info != nil && s.fpsEMA > 0 {
		for _, t := range info.Tracks {
			if t.Type == KindVideo && t.FPS == 0 {
				t.FPS = roundFPS(s.fpsEMA)
			}
		}
	}
	return seg
}

func roundFPS(v float64) float64 {
	cands := []float64{23.976, 24, 25, 29.97, 30, 50, 59.94, 60}
	best := v
	diff := 0.6
	for _, c := range cands {
		d := v - c
		if d < 0 {
			d = -d
		}
		if d < diff {
			diff = d
			best = c
		}
	}
	return best
}
