package media

import "fmt"

type MediaInfo struct {
	Tracks   []*Track  `json:"tracks"`
	Programs []Program `json:"programs,omitempty"`
	Bitrate  int       `json:"-"`
	Frames   int64     `json:"-"`
}

type Program struct {
	ID     int      `json:"id"`
	PMTPID uint16   `json:"pmt_pid"`
	Tracks []*Track `json:"tracks"`
}

type Track struct {
	ID         int     `json:"id"`
	PID        uint16  `json:"pid"`
	Type       string  `json:"type"`
	Codec      Codec   `json:"codec"`
	Width      int     `json:"width,omitempty"`
	Height     int     `json:"height,omitempty"`
	FPS        float64 `json:"fps,omitempty"`
	Profile    string  `json:"profile,omitempty"`
	Level      string  `json:"level,omitempty"`
	SampleRate int     `json:"sample_rate,omitempty"`
	Channels   int     `json:"channels,omitempty"`
	Bitrate    int     `json:"bitrate,omitempty"`
	Language   string  `json:"language,omitempty"`
	Program    int     `json:"program,omitempty"`

	SPS []byte `json:"-"`
	PPS []byte `json:"-"`
	VPS []byte `json:"-"`

	params *VideoParams
}

type Codec string

const (
	CodecH264  Codec = "h264"
	CodecHEVC  Codec = "hevc"
	CodecMPEG2 Codec = "mpeg2video"
	CodecAAC   Codec = "aac"
	CodecMP3   Codec = "mp3"
	CodecAC3   Codec = "ac3"
	CodecEAC3  Codec = "eac3"
	CodecSCTE  Codec = "scte35"
	CodecData  Codec = "data"
)

func (t *Track) String() string {
	switch t.Type {
	case "video":
		return fmt.Sprintf("%s %s %dx%d", t.Type, t.Codec, t.Width, t.Height)
	case "audio":
		return fmt.Sprintf("%s %s %dHz %dch", t.Type, t.Codec, t.SampleRate, t.Channels)
	}
	return fmt.Sprintf("%s %s", t.Type, t.Codec)
}

// Frame is one coded access unit (video) or audio frame.
type Frame struct {
	PID      uint16
	Kind     string
	PTS      int64
	DTS      int64
	Keyframe bool
	Data     []byte
	Rate     int
	Channels int
	Codec    Codec
}

const (
	KindVideo = "video"
	KindAudio = "audio"
	KindData  = "data"
)
