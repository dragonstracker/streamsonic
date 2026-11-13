package media

import (
	"bufio"
	"context"
	"errors"
	"io"
)

const PacketSize = 188

const (
	pidPAT  = 0x0000
	pidNull = 0x1fff
)

type trackState struct {
	track    *Track
	buf      []byte
	pesPTS   int64
	pesDTS   int64
	pesMark  int
	havePTS  bool
	remain   int
	auPTS    int64
	auDTS    int64
	auOn     bool
	step     int64
	audioPTS float64
	audioOn  bool
}

type Demuxer struct {
	br      *bufio.Reader
	pmtPIDs map[uint16]bool
	tracks  map[uint16]*trackState
	order   []*trackState
	info    MediaInfo
	cc      map[uint16]byte
	packets int64
	OnFrame func(Frame) error
	OnInfo  func(*MediaInfo)
	started bool
	sawPCR  bool
}

func NewDemuxer(r io.Reader) *Demuxer {
	return &Demuxer{
		br:      bufio.NewReaderSize(r, 1<<20),
		pmtPIDs: map[uint16]bool{},
		tracks:  map[uint16]*trackState{},
		cc:      map[uint16]byte{},
	}
}

func (d *Demuxer) Info() *MediaInfo { return &d.info }

func (d *Demuxer) Packets() int64 { return d.packets }

func (d *Demuxer) Run(ctx context.Context) error {
	pkt := make([]byte, PacketSize)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.readPacket(pkt); err != nil {
			return err
		}
		d.packets++
		if pkt[0] != 0x47 {
			continue
		}
		if err := d.handle(pkt); err != nil {
			return err
		}
	}
}

func (d *Demuxer) readPacket(pkt []byte) error {
	for attempts := 0; attempts < 8192; attempts++ {
		b, err := d.br.Peek(PacketSize)
		if err != nil {
			return err
		}
		if b[0] == 0x47 {
			copy(pkt, b)
			_, err = d.br.Discard(PacketSize)
			return err
		}
		if _, err := d.br.Discard(1); err != nil {
			return err
		}
	}
	return ErrNoSync
}

func (d *Demuxer) handle(p []byte) error {
	pid := uint16(p[1]&0x1f)<<8 | uint16(p[2])
	pusi := p[1]&0x40 != 0
	afc := (p[3] >> 4) & 3
	cc := p[3] & 0x0f
	payload := p[4:]
	if afc&2 != 0 {
		aLen := int(p[4])
		if aLen > 183 {
			return nil
		}
		payload = payload[1+aLen:]
	}
	if afc&1 == 0 || pid == pidNull {
		return nil
	}
	if pid == pidPAT {
		return d.parsePAT(payload, pusi)
	}
	if d.pmtPIDs[pid] {
		return d.parsePMT(payload, pusi, pid)
	}
	ts, ok := d.tracks[pid]
	if !ok {
		return nil
	}
	if last, seen := d.cc[pid]; seen && !pusi && last == cc {
		return nil
	}
	d.cc[pid] = cc
	if pusi {
		return d.startPES(ts, payload)
	}
	if ts.remain > 0 {
		if len(payload) > ts.remain {
			payload = payload[:ts.remain]
		}
		ts.remain -= len(payload)
	}
	ts.buf = append(ts.buf, payload...)
	return d.consume(ts)
}

func (d *Demuxer) parsePAT(payload []byte, pusi bool) error {
	sec := sectionPayload(payload, pusi)
	if len(sec) < 12 || sec[0] != 0x00 {
		return nil
	}
	secLen := int(sec[1]&0x0f)<<8 | int(sec[2])
	end := 3 + secLen - 4
	if end > len(sec) || end < 12 {
		return nil
	}
	for i := 8; i+4 <= end; i += 4 {
		program := uint16(sec[i])<<8 | uint16(sec[i+1])
		pid := uint16(sec[i+2]&0x1f)<<8 | uint16(sec[i+3])
		if program == 0 {
			continue
		}
		d.pmtPIDs[pid] = true
	}
	return nil
}

func (d *Demuxer) parsePMT(payload []byte, pusi bool, pmtPID uint16) error {
	sec := sectionPayload(payload, pusi)
	if len(sec) < 16 || sec[0] != 0x02 {
		return nil
	}
	secLen := int(sec[1]&0x0f)<<8 | int(sec[2])
	end := 3 + secLen - 4
	if end > len(sec) || end < 16 {
		return nil
	}
	programNum := int(sec[3])<<8 | int(sec[4])
	pcrPID := uint16(sec[8]&0x1f)<<8 | uint16(sec[9])
	progInfoLen := int(sec[10]&0x0f)<<8 | int(sec[11])
	i := 12 + progInfoLen
	changed := false
	for i+5 <= end {
		streamType := sec[i]
		pid := uint16(sec[i+1]&0x1f)<<8 | uint16(sec[i+2])
		esLen := int(sec[i+3]&0x0f)<<8 | int(sec[i+4])
		desc := sec[i+5 : min(i+5+esLen, end)]
		i += 5 + esLen
		if _, exists := d.tracks[pid]; exists {
			continue
		}
		tr := classify(streamType, pid, programNum, desc)
		if tr == nil {
			continue
		}
		ts := &trackState{track: tr}
		d.tracks[pid] = ts
		d.order = append(d.order, ts)
		d.info.Tracks = append(d.info.Tracks, tr)
		changed = true
	}
	_ = pcrPID
	if changed {
		d.info.Programs = []Program{{ID: programNum, PMTPID: pmtPID, Tracks: d.info.Tracks}}
		d.started = true
		if d.OnInfo != nil {
			d.OnInfo(&d.info)
		}
	}
	return nil
}

func classify(streamType byte, pid uint16, program int, desc []byte) *Track {
	t := &Track{PID: pid, Program: program}
	switch streamType {
	case 0x1b:
		t.Type, t.Codec = KindVideo, CodecH264
	case 0x24:
		t.Type, t.Codec = KindVideo, CodecHEVC
	case 0x01:
		t.Type, t.Codec = KindVideo, CodecMPEG2
	case 0x02:
		t.Type, t.Codec = KindVideo, CodecMPEG2
	case 0x0f:
		t.Type, t.Codec = KindAudio, CodecAAC
	case 0x11:
		t.Type, t.Codec = KindAudio, CodecAAC
	case 0x03, 0x04:
		t.Type, t.Codec = KindAudio, CodecMP3
	case 0x81:
		t.Type, t.Codec = KindAudio, CodecAC3
	case 0x87:
		t.Type, t.Codec = KindAudio, CodecEAC3
	case 0x86:
		t.Type, t.Codec = KindData, CodecSCTE
	case 0x06:
		t.Type = KindAudio
		t.Codec = CodecAC3
		for j := 0; j+1 < len(desc); {
			tag := desc[j]
			l := int(desc[j+1])
			if j+2+l > len(desc) {
				break
			}
			body := desc[j+2 : j+2+l]
			switch tag {
			case 0x0a:
				if len(body) >= 3 {
					t.Language = string(body[:3])
				}
			case 0x6a:
				t.Codec = CodecAC3
			case 0x7a:
				t.Codec = CodecEAC3
			case 0x59:
				t.Type, t.Codec = KindData, CodecData
			}
			j += 2 + l
		}
	default:
		return nil
	}
	return t
}

func sectionPayload(payload []byte, pusi bool) []byte {
	if !pusi {
		return payload
	}
	if len(payload) == 0 {
		return nil
	}
	ptr := int(payload[0])
	if 1+ptr >= len(payload) {
		return nil
	}
	return payload[1+ptr:]
}

func (d *Demuxer) startPES(ts *trackState, payload []byte) error {
	if len(payload) < 6 || payload[0] != 0 || payload[1] != 0 || payload[2] != 1 {
		return nil
	}
	streamID := payload[3]
	pesLen := int(payload[4])<<8 | int(payload[5])
	body := payload[6:]
	ts.remain = 0
	if pesHasHeader(streamID) && len(body) >= 3 {
		flags := body[1]
		hdrLen := int(body[2])
		if 3+hdrLen > len(body) {
			return nil
		}
		if flags&0x80 != 0 && hdrLen >= 5 {
			prev := ts.pesPTS
			ts.pesPTS = readTimestamp(body[3:8])
			if ts.havePTS {
				if delta := ts.pesPTS - prev; delta > 0 && delta <= 90000 {
					ts.step = delta
				}
			}
			ts.havePTS = true
			if ts.track.Type == KindAudio {
				ts.audioOn = false
			}
		}
		if flags&0x40 != 0 && hdrLen >= 10 {
			ts.pesDTS = readTimestamp(body[8:13])
		} else {
			ts.pesDTS = ts.pesPTS
		}
		body = body[3+hdrLen:]
		if pesLen > 0 {
			ts.remain = pesLen - 3 - hdrLen
		}
	}
	if ts.remain > 0 && len(body) > ts.remain {
		body = body[:ts.remain]
	}
	if ts.remain > 0 {
		ts.remain -= len(body)
	}
	ts.pesMark = len(ts.buf)
	ts.buf = append(ts.buf, body...)
	if len(ts.buf) > 8<<20 {
		ts.buf = append([]byte(nil), ts.buf[len(ts.buf)-(4<<20):]...)
	}
	return d.consume(ts)
}

func pesHasHeader(streamID byte) bool {
	switch streamID {
	case 0xbc, 0xbe, 0xbf, 0xf0, 0xf1, 0xf2, 0xf8, 0xff:
		return false
	}
	return true
}

func readTimestamp(b []byte) int64 {
	return int64(b[0]&0x0e)<<29 |
		int64(b[1])<<22 |
		int64(b[2]&0xfe)<<14 |
		int64(b[3])<<7 |
		int64(b[4])>>1
}

func (d *Demuxer) consume(ts *trackState) error {
	switch ts.track.Codec {
	case CodecH264, CodecHEVC:
		return d.consumeVideo(ts)
	case CodecAAC:
		return d.consumeADTS(ts)
	case CodecSCTE, CodecData:
		return nil
	}
	return nil
}

func (d *Demuxer) consumeVideo(ts *trackState) error {
	hevc := ts.track.Codec == CodecHEVC
	for {
		nals := SplitNALs(ts.buf, hevc)
		if len(nals) == 0 {
			return nil
		}
		first := -1
		cut := -1
		for _, n := range nals {
			if n.Type == 7 && !hevc {
				d.captureParams(ts, n, false)
			}
			if n.Type == 8 && !hevc && len(ts.track.PPS) == 0 {
				ts.track.PPS = append([]byte(nil), ts.buf[n.Header:n.End]...)
			}
			if n.Type == 33 && hevc {
				d.captureParams(ts, n, true)
			}
			if n.Type == 32 && hevc {
				d.captureParams(ts, n, true)
			}
			if !isVCL(n.Type, hevc) || !IsPictureStart(ts.buf, n, hevc) {
				continue
			}
			if first < 0 {
				first = n.Start
				continue
			}
			cut = n.Start
			break
		}
		if first < 0 {
			return nil
		}
		if !ts.auOn {
			ts.auOn = true
			if first >= ts.pesMark {
				ts.auPTS, ts.auDTS = ts.pesPTS, ts.pesDTS
			} else {
				ts.auPTS, ts.auDTS = d.anchorPTS(ts)
			}
		}
		if cut <= 0 {
			return nil
		}
		nextPTS, nextDTS := ts.pesPTS, ts.pesDTS
		if cut < ts.pesMark {
			step := ts.step
			if step <= 0 {
				step = 3600
			}
			nextPTS, nextDTS = ts.auPTS+step, ts.auDTS+step
		}
		au := make([]byte, cut)
		copy(au, ts.buf[:cut])
		rest := make([]byte, len(ts.buf)-cut)
		copy(rest, ts.buf[cut:])
		ts.buf = rest
		ts.pesMark -= cut
		if ts.pesMark < 0 {
			ts.pesMark = 0
		}
		key := false
		for _, n := range SplitNALs(au, hevc) {
			if IsKeyframeNAL(n.Type, hevc) {
				key = true
				break
			}
		}
		f := Frame{PID: ts.track.PID, Kind: KindVideo, PTS: ts.auPTS, DTS: ts.auDTS,
			Keyframe: key, Data: au, Codec: ts.track.Codec}
		ts.auPTS, ts.auDTS = nextPTS, nextDTS
		if d.OnFrame != nil {
			if err := d.OnFrame(f); err != nil {
				return err
			}
		}
		d.info.Frames++
	}
}

func (d *Demuxer) anchorPTS(ts *trackState) (int64, int64) {
	if ts.havePTS {
		return ts.pesPTS, ts.pesDTS
	}
	return 0, 0
}

func (d *Demuxer) captureParams(ts *trackState, n NAL, hevc bool) {
	body := ts.buf[n.Header:n.End]
	if len(body) < 4 {
		return
	}
	if hevc {
		switch n.Type {
		case 32:
			if len(ts.track.VPS) == 0 {
				ts.track.VPS = append([]byte(nil), body...)
			}
			return
		case 33:
			if len(ts.track.SPS) == 0 {
				ts.track.SPS = append([]byte(nil), body...)
			}
		default:
			return
		}
	} else if len(ts.track.SPS) == 0 {
		ts.track.SPS = append([]byte(nil), body...)
	}
	p, err := ParseSPS(body, hevc)
	if err != nil {
		return
	}
	if ts.track.params == nil {
		ts.track.params = &p
	}
	ts.track.Width = p.Width
	ts.track.Height = p.Height
	ts.track.Profile = p.Profile
	ts.track.Level = p.Level
}

func (d *Demuxer) consumeADTS(ts *trackState) error {
	for {
		f, n, err := ParseADTS(ts.buf)
		if err != nil {
			if errors.Is(err, ErrIncomplete) || len(ts.buf) < 7 {
				return nil
			}
			idx := -1
			for i := 1; i+1 < len(ts.buf); i++ {
				if ts.buf[i] == 0xFF && ts.buf[i+1]&0xF0 == 0xF0 {
					idx = i
					break
				}
			}
			if idx < 0 {
				ts.buf = ts.buf[:0]
				return nil
			}
			ts.buf = ts.buf[idx:]
			continue
		}
		ts.track.SampleRate = f.SampleRate
		ts.track.Channels = f.Channels
		if !ts.audioOn {
			ts.audioOn = true
			ts.audioPTS = float64(ts.pesPTS)
		}
		pts := int64(ts.audioPTS)
		ts.audioPTS += 90000.0 * 1024.0 / float64(f.SampleRate)
		data := make([]byte, n)
		copy(data, ts.buf[:n])
		fr := Frame{PID: ts.track.PID, Kind: KindAudio, PTS: pts, DTS: pts,
			Keyframe: true, Data: data, Rate: f.SampleRate, Channels: f.Channels, Codec: CodecAAC}
		ts.buf = ts.buf[n:]
		if d.OnFrame != nil {
			if err := d.OnFrame(fr); err != nil {
				return err
			}
		}
		d.info.Frames++
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
