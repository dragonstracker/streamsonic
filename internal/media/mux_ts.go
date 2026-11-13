package media

import (
	"io"
	"sort"
)

const (
	pmtPID       = 0x1000
	programNum   = 1
	videoPIDBase = 0x0100
)

type TSMuxer struct {
	w       io.Writer
	info    *MediaInfo
	tracks  []*Track
	pidOf   map[Codec]uint16
	cc      map[uint16]byte
	pcrPID  uint16
	buf     []byte
	running bool
}

func NewTSMuxer(w io.Writer, info *MediaInfo) *TSMuxer {
	m := &TSMuxer{w: w, info: info, cc: map[uint16]byte{}, pidOf: map[Codec]uint16{}}
	m.assign()
	return m
}

func (m *TSMuxer) assign() {
	m.tracks = nil
	next := uint16(videoPIDBase)
	for _, t := range m.info.Tracks {
		if t.Codec != CodecH264 && t.Codec != CodecHEVC && t.Codec != CodecAAC &&
			t.Codec != CodecAC3 && t.Codec != CodecEAC3 && t.Codec != CodecMP3 {
			continue
		}
		if t.PID == 0 {
			t.PID = next
			next++
		}
		m.tracks = append(m.tracks, t)
		m.pidOf[t.Codec] = t.PID
		if m.pcrPID == 0 && t.Type == KindVideo {
			m.pcrPID = t.PID
		}
	}
	if m.pcrPID == 0 && len(m.tracks) > 0 {
		m.pcrPID = m.tracks[0].PID
	}
}

func streamType(c Codec) byte {
	switch c {
	case CodecH264:
		return 0x1b
	case CodecHEVC:
		return 0x24
	case CodecAAC:
		return 0x0f
	case CodecMP3:
		return 0x03
	case CodecAC3:
		return 0x81
	case CodecEAC3:
		return 0x87
	}
	return 0x06
}

// WritePSI emits a PAT/PMT pair. Call it at the start of every segment.
func (m *TSMuxer) WritePSI() error {
	m.buf = m.buf[:0]
	m.packet(pidPAT, true, append([]byte{0x00}, m.pat()...), nil)
	m.packet(pmtPID, true, append([]byte{0x00}, m.pmt()...), nil)
	_, err := m.w.Write(m.buf)
	return err
}

func (m *TSMuxer) pat() []byte {
	body := []byte{
		0x00,
		0xb0, 0x0d,
		0x00, 0x01,
		0xc1,
		0x00, 0x00,
		0x00, byte(programNum),
		0xe0 | byte(pmtPID>>8), byte(pmtPID & 0xff),
	}
	return appendCRC(body)
}

func (m *TSMuxer) pmt() []byte {
	body := make([]byte, 0, 32)
	body = append(body,
		0x02,
		0xb0, 0x00,
		0x00, byte(programNum),
		0xc1,
		0x00, 0x00,
		0xe0|byte(m.pcrPID>>8), byte(m.pcrPID),
		0xf0, 0x00,
	)
	for _, t := range m.tracks {
		desc := m.descriptors(t)
		body = append(body, streamType(t.Codec), 0xe0|byte(t.PID>>8), byte(t.PID), 0xf0|byte(len(desc)>>8), byte(len(desc)))
		body = append(body, desc...)
	}
	sectionLen := len(body) - 3 + 4
	body[1] = 0xb0 | byte(sectionLen>>8)&0x0f
	body[2] = byte(sectionLen)
	return appendCRC(body)
}

func (m *TSMuxer) descriptors(t *Track) []byte {
	var d []byte
	if t.Language != "" && len(t.Language) == 3 {
		d = append(d, 0x0a, 4, t.Language[0], t.Language[1], t.Language[2], 0)
	}
	if t.Codec == CodecAC3 {
		d = append(d, 0x6a, 1, 0)
	}
	if t.Codec == CodecEAC3 {
		d = append(d, 0x7a, 1, 0)
	}
	return d
}

func appendCRC(b []byte) []byte {
	crc := uint32(0xffffffff)
	for _, v := range b {
		crc ^= uint32(v) << 24
		for i := 0; i < 8; i++ {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return append(b, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
}

func (m *TSMuxer) WriteFrame(f Frame) error {
	pid := f.PID
	if pid == 0 {
		var ok bool
		pid, ok = m.pidOf[f.Codec]
		if !ok {
			return nil
		}
	}
	m.buf = m.buf[:0]
	pes := m.pes(f)
	var pcr *int64
	if f.Kind == KindVideo {
		pts := f.DTS
		pcr = &pts
	}
	first := true
	for len(pes) > 0 {
		n := len(pes)
		if n > 184 {
			n = 184
		}
		chunk := pes[:n]
		pes = pes[n:]
		m.packet(pid, first, chunk, pcr)
		first = false
		pcr = nil
	}
	_, err := m.w.Write(m.buf)
	return err
}

func (m *TSMuxer) pes(f Frame) []byte {
	var hdr []byte
	if f.Kind == KindVideo {
		hdr = make([]byte, 0, 19)
		hdr = append(hdr, 0x00, 0x00, 0x01, 0xe0, 0x00, 0x00, 0x80)
		if f.DTS != 0 && f.DTS != f.PTS {
			hdr = append(hdr, 0xc0, 10)
			hdr = append(hdr, encodePTS(f.PTS, 0x31)...)
			hdr = append(hdr, encodePTS(f.DTS, 0x11)...)
		} else {
			hdr = append(hdr, 0x80, 5)
			hdr = append(hdr, encodePTS(f.PTS, 0x21)...)
		}
	} else {
		size := 8 + len(f.Data)
		hdr = make([]byte, 0, 14)
		hdr = append(hdr, 0x00, 0x00, 0x01, 0xc0, byte(size>>8), byte(size), 0x80, 0x80, 5)
		hdr = append(hdr, encodePTS(f.PTS, 0x21)...)
	}
	out := make([]byte, 0, len(hdr)+len(f.Data))
	out = append(out, hdr...)
	out = append(out, f.Data...)
	return out
}

func encodePTS(pts int64, prefix byte) []byte {
	return []byte{
		prefix | byte((pts>>29)&0x0e),
		byte(pts >> 22),
		byte(pts>>14) | 1,
		byte(pts >> 7),
		byte(pts<<1) | 1,
	}
}

func (m *TSMuxer) packet(pid uint16, pusi bool, payload []byte, pcr *int64) {
	n := len(payload)
	if n > 184 {
		n = 184
	}
	afLen := 0
	if n < 184 {
		afLen = 184 - n
	}
	if pcr != nil && n > 176 {
		n = 176
		afLen = 184 - n
	}
	afc := byte(1)
	if afLen > 0 {
		afc = 3
	}
	cc := m.cc[pid]
	p := make([]byte, PacketSize)
	p[0] = 0x47
	if pusi {
		p[1] = 0x40
	}
	p[1] |= byte(pid>>8) & 0x1f
	p[2] = byte(pid)
	p[3] = afc<<4 | cc
	m.cc[pid] = (cc + 1) & 0x0f
	off := 4
	if afLen > 0 {
		p[off] = byte(afLen - 1)
		q := off + 1
		p[q] = 0
		if pcr != nil {
			p[q] = 0x10
		}
		q++
		if pcr != nil {
			v := uint64(*pcr)<<15 | 0x7e00
			p[q] = byte(v >> 40)
			p[q+1] = byte(v >> 32)
			p[q+2] = byte(v >> 24)
			p[q+3] = byte(v >> 16)
			p[q+4] = byte(v >> 8)
			p[q+5] = byte(v)
			q += 6
		}
		for q < off+afLen {
			p[q] = 0xff
			q++
		}
		off += afLen
	}
	copy(p[off:], payload[:n])
	m.buf = append(m.buf, p...)
}

func (m *TSMuxer) SortedPIDs() []int {
	pids := make([]int, 0, len(m.tracks))
	for _, t := range m.tracks {
		pids = append(pids, int(t.PID))
	}
	sort.Ints(pids)
	return pids
}
