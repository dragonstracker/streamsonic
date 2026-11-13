package media

import (
	"encoding/binary"
	"errors"
)

// TrackMuxer produces fragmented MP4 for one track: an init segment plus
// moof+mdat fragments. Timescale is 90000 for video, the sample rate for audio.
type TrackMuxer struct {
	track   *Track
	trackID uint32
	seq     uint32
	baseDTS uint64
	init    []byte
	audio   bool
}

func NewTrackMuxer(t *Track, id uint32) (*TrackMuxer, error) {
	m := &TrackMuxer{track: t, trackID: id, audio: t.Type == KindAudio}
	switch t.Codec {
	case CodecH264:
		if len(t.SPS) < 4 || len(t.PPS) == 0 {
			return nil, errors.New("media: h264 track has no SPS/PPS")
		}
	case CodecAAC:
		if t.SampleRate == 0 || t.Channels == 0 {
			return nil, errors.New("media: aac track has no audio configuration")
		}
	default:
		return nil, errors.New("media: fmp4 does not support " + string(t.Codec))
	}
	m.init = append(m.ftyp(), m.moov()...)
	return m, nil
}

func (m *TrackMuxer) Init() []byte { return m.init }

func (m *TrackMuxer) Timescale() uint32 {
	if m.audio {
		return uint32(m.track.SampleRate)
	}
	return 90000
}

func (m *TrackMuxer) Fragment(frames []Frame) []byte {
	traf, data, dur := m.buildTraf(frames)
	if traf == nil {
		return nil
	}
	m.seq++
	mfhd := newBw()
	mfhd.u8(0)
	mfhd.u24(0)
	mfhd.u32(m.seq)
	moof := box("moof", fullBox("mfhd", mfhd.b), traf)
	mdat := box("mdat", data)
	patchTruns(moof, []uint32{uint32(len(moof) + 8)})
	m.baseDTS += dur
	return append(moof, mdat...)
}

func (m *TrackMuxer) buildTraf(frames []Frame) ([]byte, []byte, uint64) {
	var data []byte
	var durations []uint32
	var sizes []uint32
	var offsets []int32
	var flags []uint32
	var total uint64
	prev := frames[0].PTS
	for i, f := range frames {
		sample := f.Data
		if !m.audio {
			sample = annexBToAVCC(f.Data)
			if len(sample) == 0 {
				continue
			}
		} else {
			sample = adtsPayload(f.Data)
		}
		dur := uint32(1024)
		if !m.audio {
			if i+1 < len(frames) {
				dur = uint32(maxInt64(frames[i+1].PTS-prev, 1))
			} else if len(durations) > 0 {
				dur = durations[len(durations)-1]
			} else {
				dur = 3000
			}
		}
		prev = f.PTS
		sizes = append(sizes, uint32(len(sample)))
		durations = append(durations, dur)
		offsets = append(offsets, int32(f.PTS-f.DTS))
		if f.Keyframe {
			flags = append(flags, 0x02000000)
		} else {
			flags = append(flags, 0x01010000)
		}
		total += uint64(dur)
		data = append(data, sample...)
	}
	if len(sizes) == 0 {
		return nil, nil, 0
	}
	trun := newBw()
	trun.u8(0x01)
	trun.u24(0x000f01)
	trun.u32(uint32(len(sizes)))
	trun.u32(0)
	for i := range sizes {
		trun.u32(durations[i])
		trun.u32(sizes[i])
		trun.u32(flags[i])
		trun.u32(uint32(offsets[i]))
	}
	tfhd := newBw()
	tfhd.u8(0)
	tfhd.u24(0x020000)
	tfhd.u32(m.trackID)
	base := m.sampleTime(frames[0])
	if base == 0 {
		base = m.baseDTS
	}
	tfdt := newBw()
	tfdt.u8(1)
	tfdt.u24(0)
	tfdt.u64(base)
	traf := box("traf", fullBox("tfhd", tfhd.b), fullBox("tfdt", tfdt.b), fullBox("trun", trun.b))
	return traf, data, total
}

// MultiMuxer writes one fragmented MP4 stream carrying both video and audio,
// which lets a browser play the stream with a single SourceBuffer.
type MultiMuxer struct {
	video *TrackMuxer
	audio *TrackMuxer
	seq   uint32
}

func NewMultiMuxer(video, audio *Track) (*MultiMuxer, error) {
	mm := &MultiMuxer{}
	if video != nil {
		m, err := NewTrackMuxer(video, 1)
		if err != nil {
			return nil, err
		}
		mm.video = m
	}
	if audio != nil {
		m, err := NewTrackMuxer(audio, 2)
		if err != nil {
			return nil, err
		}
		mm.audio = m
	}
	if mm.video == nil && mm.audio == nil {
		return nil, errors.New("media: no tracks for the muxed stream")
	}
	return mm, nil
}

func (m *MultiMuxer) Init() []byte {
	first := m.video
	if first == nil {
		first = m.audio
	}
	var traks [][]byte
	var trexs [][]byte
	if m.video != nil {
		traks = append(traks, m.video.trak())
		trexs = append(trexs, m.video.trex())
	}
	if m.audio != nil {
		traks = append(traks, m.audio.trak())
		trexs = append(trexs, m.audio.trex())
	}
	parts := append([][]byte{first.mvhd()}, traks...)
	parts = append(parts, box("mvex", trexs...))
	moov := box("moov", parts...)
	return append(first.ftyp(), moov...)
}

func (m *MultiMuxer) Fragment(video, audio []Frame) []byte {
	m.seq++
	var trafs [][]byte
	var payloads [][]byte
	var sizes []int
	if m.video != nil && len(video) > 0 {
		traf, data, dur := m.video.buildTraf(video)
		if traf != nil {
			trafs = append(trafs, traf)
			payloads = append(payloads, data)
			sizes = append(sizes, len(data))
			m.video.baseDTS += dur
		}
	}
	if m.audio != nil && len(audio) > 0 {
		traf, data, dur := m.audio.buildTraf(audio)
		if traf != nil {
			trafs = append(trafs, traf)
			payloads = append(payloads, data)
			sizes = append(sizes, len(data))
			m.audio.baseDTS += dur
		}
	}
	if len(trafs) == 0 {
		return nil
	}
	mfhd := newBw()
	mfhd.u8(0)
	mfhd.u24(0)
	mfhd.u32(m.seq)
	parts := append([][]byte{fullBox("mfhd", mfhd.b)}, trafs...)
	moof := box("moof", parts...)
	var data []byte
	offsets := make([]uint32, 0, len(sizes))
	off := uint32(len(moof) + 8)
	for i, p := range payloads {
		offsets = append(offsets, off)
		off += uint32(sizes[i])
		data = append(data, p...)
	}
	patchTruns(moof, offsets)
	return append(moof, box("mdat", data)...)
}

func patchTruns(moof []byte, offsets []uint32) {
	idx := 0
	for i := 0; i+16 < len(moof) && idx < len(offsets); i++ {
		if moof[i] == 't' && moof[i+1] == 'r' && moof[i+2] == 'u' && moof[i+3] == 'n' {
			binary.BigEndian.PutUint32(moof[i+12:], offsets[idx])
			idx++
		}
	}
}

func adtsPayload(b []byte) []byte {
	if len(b) < 7 || b[0] != 0xFF || b[1]&0xF0 != 0xF0 {
		return b
	}
	hdr := 7
	if b[1]&1 == 0 {
		hdr = 9
	}
	if hdr >= len(b) {
		return b
	}
	return b[hdr:]
}

func annexBToAVCC(b []byte) []byte {
	nals := SplitNALsAll(b, false)
	out := newBw()
	for _, n := range nals {
		if n.Type == 7 || n.Type == 8 || n.Type == 9 {
			continue
		}
		body := b[n.Header:n.End]
		out.u32(uint32(len(body)))
		out.b = append(out.b, body...)
	}
	return out.b
}

type bw struct{ b []byte }

func newBw() *bw { return &bw{b: make([]byte, 0, 64)} }

func (w *bw) u8(v byte)    { w.b = append(w.b, v) }
func (w *bw) u16(v uint16) { w.b = append(w.b, byte(v>>8), byte(v)) }
func (w *bw) u24(v uint32) { w.b = append(w.b, byte(v>>16), byte(v>>8), byte(v)) }
func (w *bw) u32(v uint32) { w.b = append(w.b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v)) }
func (w *bw) u64(v uint64) {
	w.b = append(w.b, byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32), byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
func (w *bw) zeros(n int) {
	for i := 0; i < n; i++ {
		w.b = append(w.b, 0)
	}
}
func (w *bw) tag(s string) { w.b = append(w.b, s...) }
func (w *bw) raw(v []byte) { w.b = append(w.b, v...) }

func box(typ string, parts ...[]byte) []byte {
	size := 8
	for _, p := range parts {
		size += len(p)
	}
	out := make([]byte, 0, size)
	out = append(out, byte(size>>24), byte(size>>16), byte(size>>8), byte(size))
	out = append(out, typ...)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func fullBox(typ string, body []byte) []byte {
	return box(typ, body)
}

func (m *TrackMuxer) matrix() []byte {
	w := newBw()
	w.u32(0x00010000)
	w.zeros(12)
	w.u32(0x00010000)
	w.zeros(12)
	w.u32(0x40000000)
	return w.b
}

func (m *TrackMuxer) ftyp() []byte {
	w := newBw()
	w.tag("isom")
	w.u32(512)
	w.tag("isom")
	w.tag("iso5")
	w.tag("iso6")
	w.tag("dash")
	w.tag("mp41")
	return box("ftyp", w.b)
}

func (m *TrackMuxer) mvhd() []byte {
	w := newBw()
	w.u8(0)
	w.u24(0)
	w.u32(0)
	w.u32(0)
	w.u32(1000)
	w.u32(0)
	w.u32(0x00010000)
	w.u16(0x0100)
	w.u16(0)
	w.zeros(8)
	w.raw(m.matrix())
	w.zeros(24)
	w.u32(2)
	return box("mvhd", w.b)
}

func (m *TrackMuxer) tkhd() []byte {
	w := newBw()
	w.u8(0)
	w.u24(0x000003)
	w.u32(0)
	w.u32(0)
	w.u32(m.trackID)
	w.u32(0)
	w.u32(0)
	w.zeros(8)
	w.u16(0)
	w.u16(0)
	if m.audio {
		w.u16(0x0100)
	} else {
		w.u16(0)
	}
	w.u16(0)
	w.raw(m.matrix())
	w.u32(uint32(m.track.Width) << 16)
	w.u32(uint32(m.track.Height) << 16)
	return box("tkhd", w.b)
}

func (m *TrackMuxer) mdhd() []byte {
	w := newBw()
	w.u8(0)
	w.u24(0)
	w.u32(0)
	w.u32(0)
	w.u32(m.Timescale())
	w.u32(0)
	w.u16(0x55c4)
	w.u16(0)
	return box("mdhd", w.b)
}

func (m *TrackMuxer) hdlr() []byte {
	w := newBw()
	w.u8(0)
	w.u24(0)
	w.u32(0)
	if m.audio {
		w.tag("soun")
	} else {
		w.tag("vide")
	}
	w.zeros(12)
	if m.audio {
		w.tag("SoundHandler\x00")
	} else {
		w.tag("VideoHandler\x00")
	}
	return box("hdlr", w.b)
}

func (m *TrackMuxer) dinf() []byte {
	dref := newBw()
	dref.u8(0)
	dref.u24(0)
	dref.u32(1)
	url := newBw()
	url.u8(0)
	url.u24(1)
	dref.raw(box("url ", url.b))
	return box("dinf", box("dref", dref.b))
}

func (m *TrackMuxer) stbl(sampleEntry []byte) []byte {
	stsd := newBw()
	stsd.u8(0)
	stsd.u24(0)
	stsd.u32(1)
	stsd.raw(sampleEntry)
	empty := func(typ string) []byte {
		w := newBw()
		w.u8(0)
		w.u24(0)
		w.u32(0)
		return box(typ, w.b)
	}
	stsz := newBw()
	stsz.u8(0)
	stsz.u24(0)
	stsz.u32(0)
	stsz.u32(0)
	return box("stbl", box("stsd", stsd.b), empty("stts"), empty("stsc"), box("stsz", stsz.b), empty("stco"))
}

func (m *TrackMuxer) avcC() []byte {
	w := newBw()
	w.u8(1)
	w.u8(m.track.SPS[1])
	w.u8(m.track.SPS[2])
	w.u8(m.track.SPS[3])
	w.u8(0xff)
	w.u8(0xe1)
	w.u16(uint16(len(m.track.SPS)))
	w.raw(m.track.SPS)
	w.u8(1)
	w.u16(uint16(len(m.track.PPS)))
	w.raw(m.track.PPS)
	return box("avcC", w.b)
}

func (m *TrackMuxer) avc1() []byte {
	w := newBw()
	w.zeros(6)
	w.u16(1)
	w.zeros(16)
	w.u16(uint16(m.track.Width))
	w.u16(uint16(m.track.Height))
	w.u32(0x00480000)
	w.u32(0x00480000)
	w.u32(0)
	w.u16(1)
	w.zeros(32)
	w.u16(0x0018)
	w.u16(0xffff)
	w.raw(m.avcC())
	return box("avc1", w.b)
}

func (m *TrackMuxer) esds() []byte {
	cfg := ADTSConfig(ADTSFrame{Profile: 2, SampleRate: m.track.SampleRate, Channels: m.track.Channels})
	dsi := newBw()
	dsi.u8(0x05)
	dsi.u8(byte(len(cfg)))
	dsi.raw(cfg)

	dc := newBw()
	dc.u8(0x40)
	dc.u8(0x15)
	dc.u24(0)
	dc.u32(0)
	dc.u32(0)
	dc.raw(dsi.b)

	es := newBw()
	es.u16(1)
	es.u8(0)
	es.u8(0x04)
	es.u8(byte(len(dc.b)))
	es.raw(dc.b)
	es.u8(0x06)
	es.u8(1)
	es.u8(0x02)

	w := newBw()
	w.u8(0)
	w.u24(0)
	w.u8(0x03)
	w.u8(byte(len(es.b)))
	w.raw(es.b)
	return box("esds", w.b)
}

func (m *TrackMuxer) mp4a() []byte {
	w := newBw()
	w.zeros(6)
	w.u16(1)
	w.zeros(8)
	w.u16(uint16(m.track.Channels))
	w.u16(16)
	w.u16(0)
	w.u16(0)
	w.u32(uint32(m.track.SampleRate) << 16)
	w.raw(m.esds())
	return box("mp4a", w.b)
}

func (m *TrackMuxer) trex() []byte {
	w := newBw()
	w.u8(0)
	w.u24(0)
	w.u32(m.trackID)
	w.u32(1)
	w.u32(0)
	w.u32(0)
	w.u32(0)
	return box("trex", w.b)
}

func (m *TrackMuxer) moov() []byte {
	return box("moov", m.mvhd(), m.trak(), box("mvex", m.trex()))
}

func (m *TrackMuxer) trak() []byte {
	return box("trak", m.tkhd(), box("mdia", m.mdhd(), m.hdlr(), m.minf()))
}

func (m *TrackMuxer) minf() []byte {
	var media []byte
	if m.audio {
		w := newBw()
		w.u8(0)
		w.u24(0)
		w.u16(0)
		w.u16(0)
		media = box("smhd", w.b)
	} else {
		w := newBw()
		w.u8(0)
		w.u24(1)
		w.u16(0)
		w.zeros(6)
		media = box("vmhd", w.b)
	}
	return box("minf", media, m.dinf(), m.stbl(m.sampleEntry()))
}

func (m *TrackMuxer) sampleEntry() []byte {
	if m.audio {
		return m.mp4a()
	}
	return m.avc1()
}

func (m *TrackMuxer) sampleTime(f Frame) uint64 {
	ts := int64(m.Timescale())
	if ts == 90000 {
		return uint64(maxInt64(f.DTS, 0))
	}
	return uint64(float64(maxInt64(f.DTS, 0)) * float64(ts) / 90000.0)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
