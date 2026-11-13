package media

import (
	"errors"
	"fmt"
)

var ErrNoSync = errors.New("media: no start code found")

var ErrIncomplete = errors.New("media: incomplete frame")

type BitReader struct {
	buf []byte
	pos int
}

func NewBitReader(b []byte) *BitReader { return &BitReader{buf: b} }

func (r *BitReader) Bit() uint {
	if r.pos >= len(r.buf)*8 {
		r.pos++
		return 0
	}
	b := (r.buf[r.pos>>3] >> (7 - uint(r.pos&7))) & 1
	r.pos++
	return uint(b)
}

func (r *BitReader) Bits(n int) uint {
	var v uint
	for i := 0; i < n; i++ {
		v = v<<1 | r.Bit()
	}
	return v
}

func (r *BitReader) U8() uint   { return r.Bits(8) }
func (r *BitReader) U16() uint  { return r.Bits(16) }
func (r *BitReader) U32() uint  { return r.Bits(32) }
func (r *BitReader) Flag() bool { return r.Bit() == 1 }

func (r *BitReader) Skip(n int) { r.pos += n }

func (r *BitReader) UE() uint {
	zeros := 0
	for r.pos < len(r.buf)*8 && r.Bit() == 0 {
		zeros++
		if zeros > 32 {
			return 0
		}
	}
	if zeros == 0 {
		return 0
	}
	return (1 << uint(zeros)) - 1 + r.Bits(zeros)
}

func (r *BitReader) SE() int {
	v := r.UE()
	if v&1 == 1 {
		return int(v+1) / 2
	}
	return -int(v) / 2
}

func (r *BitReader) Err() bool { return r.pos > len(r.buf)*8 }

type NAL struct {
	Type   byte
	Start  int
	End    int
	Header int
}

func SplitNALs(buf []byte, hevc bool) []NAL { return splitNALs(buf, hevc, false) }

func SplitNALsAll(buf []byte, hevc bool) []NAL { return splitNALs(buf, hevc, true) }

func splitNALs(buf []byte, hevc bool, complete bool) []NAL {
	var out []NAL
	i := 0
	var starts []int
	for i+2 < len(buf) {
		if buf[i] == 0 && buf[i+1] == 0 && buf[i+2] == 1 {
			starts = append(starts, i)
			i += 3
			continue
		}
		i++
	}
	hlen := 1
	if hevc {
		hlen = 2
	}
	for k, s := range starts {
		body := s + 3
		if body+hlen > len(buf) {
			continue
		}
		end := len(buf)
		if k+1 < len(starts) {
			end = starts[k+1]
			for end > body && buf[end-1] == 0 {
				end--
			}
		} else if !complete {
			continue
		}
		var typ byte
		if hevc {
			typ = (buf[body] >> 1) & 0x3f
		} else {
			typ = buf[body] & 0x1f
		}
		out = append(out, NAL{Type: typ, Start: s, End: end, Header: body})
	}
	return out
}

func isVCL(typ byte, hevc bool) bool {
	if hevc {
		return typ < 32
	}
	return typ >= 1 && typ <= 5
}

func IsPictureStart(buf []byte, n NAL, hevc bool) bool {
	if !isVCL(n.Type, hevc) {
		return false
	}
	if hevc {
		if n.Header+2 >= len(buf) {
			return false
		}
		return buf[n.Header+2]&0x80 != 0
	}
	br := NewBitReader(buf[n.Header+1:])
	return br.UE() == 0
}

func IsKeyframeNAL(typ byte, hevc bool) bool {
	if hevc {
		return typ >= 16 && typ <= 21
	}
	return typ == 5
}

type VideoParams struct {
	Codec        Codec
	Width        int
	Height       int
	Profile      string
	Level        string
	SPS          []byte
	PPS          []byte
	VPS          []byte
	ChromaFormat int
	BitDepth     int
}

func ParseSPS(nal []byte, hevc bool) (VideoParams, error) {
	if hevc {
		return parseHEVCSPS(nal)
	}
	return parseAVCSPS(nal)
}

func parseAVCSPS(nal []byte) (VideoParams, error) {
	p := VideoParams{Codec: CodecH264, ChromaFormat: 1, BitDepth: 8}
	if len(nal) < 4 {
		return p, ErrNoSync
	}
	body := nal[1:]
	r := NewBitReader(body)
	profile := r.U8()
	r.U8()
	level := r.U8()
	r.UE()
	p.Profile = avcProfileName(profile)
	p.Level = fmt.Sprintf("%d.%d", level/10, level%10)
	high := false
	switch profile {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		high = true
	}
	if high {
		chroma := int(r.UE())
		p.ChromaFormat = chroma
		if chroma == 3 {
			r.Bit()
		}
		p.BitDepth = 8 + int(r.UE())
		r.UE()
		r.Bit()
		if r.Flag() {
			n := 8
			if chroma == 3 {
				n = 12
			}
			for i := 0; i < n; i++ {
				if r.Flag() {
					skipScalingList(r, 16)
				}
			}
		}
	}
	r.UE()
	pocType := r.UE()
	if pocType == 0 {
		r.UE()
	} else if pocType == 1 {
		r.Bit()
		r.SE()
		r.SE()
		n := r.UE()
		for i := uint(0); i < n && i < 256; i++ {
			r.SE()
		}
	}
	r.UE()
	r.Bit()
	widthMBs := int(r.UE()) + 1
	heightMapUnits := int(r.UE()) + 1
	frameMbsOnly := r.Flag()
	if !frameMbsOnly {
		r.Bit()
	}
	r.Bit()
	width := widthMBs * 16
	height := (2 - boolToInt(frameMbsOnly)) * heightMapUnits * 16
	if r.Flag() {
		cropL := int(r.UE())
		cropR := int(r.UE())
		cropT := int(r.UE())
		cropB := int(r.UE())
		sx, sy := 2, 2
		if p.ChromaFormat == 3 {
			sx, sy = 1, 1
		} else if p.ChromaFormat == 0 {
			sx, sy = 1, 1
		}
		width -= (cropL + cropR) * sx
		height -= (cropT + cropB) * sy * (2 - boolToInt(frameMbsOnly))
	}
	p.Width = width
	p.Height = height
	return p, nil
}

func parseHEVCSPS(nal []byte) (VideoParams, error) {
	p := VideoParams{Codec: CodecHEVC, ChromaFormat: 1, BitDepth: 8}
	if len(nal) < 4 {
		return p, ErrNoSync
	}
	r := NewBitReader(nal[2:])
	r.Bits(4)
	maxSubLayers := int(r.Bits(3))
	r.Bit()
	profileSpace := r.Bits(2)
	tier := r.Bit()
	profileIDC := r.Bits(5)
	for i := 0; i < 32; i++ {
		r.Bit()
	}
	r.Bits(4)
	r.Bits(43)
	r.Bit()
	level := r.Bits(8)
	p.Profile = fmt.Sprintf("profile(%d,%d,%d)", profileSpace, tier, profileIDC)
	p.Level = fmt.Sprintf("%d.%d", level/30, (level%30)/3)
	if maxSubLayers > 0 {
		profPresent := make([]bool, maxSubLayers)
		levelPresent := make([]bool, maxSubLayers)
		for i := 0; i < maxSubLayers; i++ {
			profPresent[i] = r.Flag()
			levelPresent[i] = r.Flag()
		}
		for i := maxSubLayers; i < 8; i++ {
			r.Bits(2)
		}
		for i := 0; i < maxSubLayers; i++ {
			if profPresent[i] {
				r.Bits(88)
			}
			if levelPresent[i] {
				r.Bits(8)
			}
		}
	}
	r.UE()
	chroma := int(r.UE())
	p.ChromaFormat = chroma
	if chroma == 3 {
		r.Bit()
	}
	width := int(r.UE())
	height := int(r.UE())
	if r.Flag() {
		r.UE()
		r.UE()
		r.UE()
		r.UE()
	}
	p.BitDepth = 8 + int(r.UE())
	r.UE()
	p.Width = width
	p.Height = height
	return p, nil
}

func skipScalingList(r *BitReader, size int) {
	last := 8
	next := 8
	for j := 0; j < size; j++ {
		if next != 0 {
			next = (last + int(r.SE()) + 256) % 256
		}
		if next != 0 {
			last = next
		}
	}
}

func avcProfileName(idc uint) string {
	switch idc {
	case 66:
		return "Baseline"
	case 77:
		return "Main"
	case 88:
		return "Extended"
	case 100:
		return "High"
	case 110:
		return "High 10"
	case 122:
		return "High 4:2:2"
	case 244:
		return "High 4:4:4"
	}
	return fmt.Sprintf("Profile%d", idc)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type ADTSFrame struct {
	Data       []byte
	Profile    int
	SampleRate int
	Channels   int
	Size       int
}

var aacRates = []int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050,
	16000, 12000, 11025, 8000, 7350}

func ParseADTS(b []byte) (ADTSFrame, int, error) {
	var f ADTSFrame
	if len(b) < 7 {
		return f, 0, ErrNoSync
	}
	if b[0] != 0xFF || b[1]&0xF0 != 0xF0 {
		return f, 0, ErrNoSync
	}
	protectionAbsent := b[1]&1 == 1
	profile := int((b[2] >> 6) & 0x3)
	rateIdx := int((b[2] >> 2) & 0xf)
	chanCfg := int((b[2]&1)<<2 | (b[3]>>6)&0x3)
	frameLen := int(b[3]&0x3)<<11 | int(b[4])<<3 | int(b[5]>>5)
	headerLen := 7
	if !protectionAbsent {
		headerLen = 9
	}
	if frameLen < headerLen {
		return f, 0, fmt.Errorf("media: bad adts frame length %d (have %d)", frameLen, len(b))
	}
	if frameLen > len(b) {
		return f, 0, ErrIncomplete
	}
	rate := 0
	if rateIdx < len(aacRates) {
		rate = aacRates[rateIdx]
	}
	f.Profile = profile + 1
	f.SampleRate = rate
	f.Channels = chanCfg
	f.Size = frameLen
	f.Data = b[headerLen:frameLen]
	return f, frameLen, nil
}

func ADTSConfig(f ADTSFrame) []byte {
	rateIdx := 4
	for i, r := range aacRates {
		if r == f.SampleRate {
			rateIdx = i
			break
		}
	}
	cfg := uint16(f.Profile)<<11 | uint16(rateIdx)<<7 | uint16(f.Channels)<<3
	return []byte{byte(cfg >> 8), byte(cfg)}
}
