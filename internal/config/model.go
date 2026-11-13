package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type HTTP struct {
	Port int
	TLS  bool
	Cert string
	Key  string
}

type Config struct {
	Path          string
	HTTP          HTTP
	SessionLog    string
	AdminLogin    string
	AdminPassword string
	Streams       map[string]*Stream
	DVRs          map[string]*DVR
	Backends      map[string]*AuthBackend
	Sinks         []*EventSink
	Extra         []*Node
	Rev           int64
}

func New() *Config {
	return &Config{
		HTTP:     HTTP{Port: 8080},
		Streams:  map[string]*Stream{},
		DVRs:     map[string]*DVR{},
		Backends: map[string]*AuthBackend{},
	}
}

func (c *Config) StreamNames() []string {
	out := make([]string, 0, len(c.Streams))
	for k := range c.Streams {
		out = append(out, k)
	}
	return out
}

type Input struct {
	Type                    string            `json:"-"`
	URL                     string            `json:"url"`
	Comment                 string            `json:"comment,omitempty"`
	Priority                int               `json:"priority,omitempty"`
	SourceTimeout           int               `json:"source_timeout,omitempty"`
	VideoTimeout            int               `json:"video_timeout,omitempty"`
	AudioTimeout            int               `json:"audio_timeout,omitempty"`
	FramesTimeout           int               `json:"frames_timeout,omitempty"`
	MaxRetry                int               `json:"max_retry_timeout,omitempty"`
	RetryLimit              int               `json:"retry_limit,omitempty"`
	Headers                 map[string]string `json:"headers,omitempty"`
	UserAgent               string            `json:"user_agent,omitempty"`
	BindIP                  string            `json:"bind_ip,omitempty"`
	Programs                []int             `json:"programs,omitempty"`
	PIDs                    []int             `json:"pids,omitempty"`
	SCTE35                  *bool             `json:"scte35,omitempty"`
	NoClientsReconnectDelay int               `json:"no_clients_reconnect_delay,omitempty"`
}

var inputTypes = []string{"tshttp", "hlss", "hls", "rtsps", "rtsp", "rtmps", "rtmp", "srt", "udp", "rtp", "file", "m4f", "fake"}

func (i *Input) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	for _, t := range inputTypes {
		v, ok := raw[t]
		if !ok {
			continue
		}
		type plain Input
		var p plain
		if err := json.Unmarshal(v, &p); err != nil {
			return fmt.Errorf("%s: %w", t, err)
		}
		*i = Input(p)
		i.Type = t
		if i.URL == "" {
			i.URL = p.URL
		}
		return nil
	}
	type plain Input
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*i = Input(p)
	if t, ok := raw["type"]; ok {
		var s string
		if err := json.Unmarshal(t, &s); err != nil {
			return fmt.Errorf("type: %w", err)
		}
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			i.Type = s
		}
	}
	if i.Type == "" {
		i.Type = TypeOfURL(i.URL)
	}
	normalizeInputURL(i)
	return nil
}

func normalizeInputURL(i *Input) {
	if i.Type == "" || i.URL == "" {
		return
	}
	if rest, ok := strings.CutPrefix(i.URL, i.Type+"://"); ok && strings.Contains(rest, "://") {
		i.URL = rest
	}
}

func (i Input) MarshalJSON() ([]byte, error) {
	type plain Input
	t := i.Type
	if t == "" {
		t = TypeOfURL(i.URL)
	}
	return json.Marshal(map[string]any{t: plain(i)})
}

func (i Input) APIShape() map[string]any {
	b, err := json.Marshal(i)
	if err != nil {
		return map[string]any{"url": i.URL}
	}
	var grouped map[string]map[string]any
	if err := json.Unmarshal(b, &grouped); err != nil {
		return map[string]any{"url": i.URL}
	}
	for t, body := range grouped {
		body["type"] = t
		return body
	}
	return map[string]any{"url": i.URL}
}

func (s *Stream) APIView() map[string]any {
	b, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	inputs := make([]map[string]any, 0, len(s.Inputs))
	for _, in := range s.Inputs {
		inputs = append(inputs, in.APIShape())
	}
	out["inputs"] = inputs
	return out
}

func TypeOfURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "tshttp"
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "tshttp":
		return "tshttp"
	case "hls", "hlss":
		return "hls"
	case "udp", "rtp", "multicast":
		return "udp"
	case "file":
		return "file"
	case "srt":
		return "srt"
	case "rtmp", "rtmps":
		return "rtmp"
	case "rtsp", "rtsps":
		return "rtsp"
	case "m4f":
		return "m4f"
	}
	if raw == "" {
		return "tshttp"
	}
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "./") {
		return "file"
	}
	return "tshttp"
}

type Backup struct {
	URL     string `json:"url"`
	Comment string `json:"comment,omitempty"`
}

type Protocols struct {
	Whitelist bool `json:"whitelist,omitempty"`
	HLS       bool `json:"hls,omitempty"`
	DASH      bool `json:"dash,omitempty"`
	WebRTC    bool `json:"webrtc,omitempty"`
	MSE       bool `json:"mse,omitempty"`
	MSS       bool `json:"mss,omitempty"`
	CMAF      bool `json:"cmaf,omitempty"`
	RTMP      bool `json:"rtmp,omitempty"`
	RTSP      bool `json:"rtsp,omitempty"`
	SRT       bool `json:"srt,omitempty"`
	MPEGTS    bool `json:"mpegts,omitempty"`
}

func DefaultProtocols() *Protocols {
	return &Protocols{HLS: true, DASH: true, MSE: true, CMAF: true, MPEGTS: true}
}

type StreamDVR struct {
	Reference          string      `json:"reference,omitempty"`
	StorageLimit       int64       `json:"storage_limit,omitempty"`
	Expiration         int         `json:"expiration,omitempty"`
	EpisodesExpiration int         `json:"episodes_expiration,omitempty"`
	DVRReplicate       bool        `json:"dvr_replicate,omitempty"`
	ReplicationSpeed   int         `json:"replication_speed,omitempty"`
	ReplicationPort    int         `json:"replication_port,omitempty"`
	Schedule           []DVRWindow `json:"schedule,omitempty"`
	DVROffline         bool        `json:"dvr_offline,omitempty"`
	Copy               string      `json:"copy,omitempty"`
	Remotes            []string    `json:"remotes,omitempty"`
}

type DVRWindow struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type DVR struct {
	Name               string      `json:"name"`
	Root               string      `json:"root"`
	Index              string      `json:"index,omitempty"`
	CheckMount         *bool       `json:"check_mount,omitempty"`
	Active             int         `json:"active,omitempty"`
	Disks              []string    `json:"disks,omitempty"`
	StorageLimit       int64       `json:"storage_limit,omitempty"`
	Expiration         int         `json:"expiration,omitempty"`
	EpisodesExpiration int         `json:"episodes_expiration,omitempty"`
	Schedule           []DVRWindow `json:"schedule,omitempty"`
	DVROffline         bool        `json:"dvr_offline,omitempty"`
	Copy               string      `json:"copy,omitempty"`
}

type Stream struct {
	Name             string            `json:"name"`
	Comment          string            `json:"comment,omitempty"`
	Title            string            `json:"title,omitempty"`
	Static           bool              `json:"static,omitempty"`
	Ondemand         bool              `json:"ondemand,omitempty"`
	Inputs           []Input           `json:"inputs"`
	Backup           *Backup           `json:"backup,omitempty"`
	ClientsTimeout   *int              `json:"clients_timeout,omitempty"`
	SegmentDuration  float64           `json:"segment_duration,omitempty"`
	SegmentCount     int               `json:"segment_count,omitempty"`
	CMAFEnabled      bool              `json:"cmaf_enabled,omitempty"`
	ChunkDuration    float64           `json:"chunk_duration,omitempty"`
	DashUpdatePeriod float64           `json:"dash_update_period,omitempty"`
	Prepush          float64           `json:"prepush,omitempty"`
	Protocols        *Protocols        `json:"protocols,omitempty"`
	DVR              *StreamDVR        `json:"dvr,omitempty"`
	RetryLimit       int               `json:"retry_limit,omitempty"`
	SourceTimeout    int               `json:"source_timeout,omitempty"`
	VideoTimeout     int               `json:"video_timeout,omitempty"`
	AudioTimeout     int               `json:"audio_timeout,omitempty"`
	MaxRetryTimeout  int               `json:"max_retry_timeout,omitempty"`
	OnPlay           *AuthSpec         `json:"on_play,omitempty"`
	OnPublish        *AuthSpec         `json:"on_publish,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	Pushes           []Push            `json:"pushes,omitempty"`
	JpegSignKey      string            `json:"jpeg_snapshot_sign_key,omitempty"`
	NoMedia          bool              `json:"nomedia,omitempty"`
}

func (s *Stream) Duration() float64 {
	if s.SegmentDuration > 0 {
		return s.SegmentDuration
	}
	return 6
}

func (s *Stream) SegmentCountOrDefault() int {
	if s.SegmentCount > 0 {
		return s.SegmentCount
	}
	return 4
}

type Push struct {
	URL     string `json:"url"`
	Comment string `json:"comment,omitempty"`
}

type AuthSpec struct {
	URL                 string            `json:"url,omitempty"`
	Domains             []string          `json:"domains,omitempty"`
	MaxSessions         int               `json:"max_sessions,omitempty"`
	AllowedCountries    []string          `json:"allowed_countries,omitempty"`
	DisallowedCountries []string          `json:"disallowed_countries,omitempty"`
	SoftLimitation      bool              `json:"soft_limitation,omitempty"`
	SessionKeys         []string          `json:"session_keys,omitempty"`
	Extra               map[string]string `json:"extra,omitempty"`
}

type AuthBackend struct {
	Name           string   `json:"name"`
	AllowTokens    []string `json:"allow_tokens,omitempty"`
	DenyTokens     []string `json:"deny_tokens,omitempty"`
	AllowIPs       []string `json:"allow_ips,omitempty"`
	DenyIPs        []string `json:"deny_ips,omitempty"`
	AllowCountries []string `json:"allow_countries,omitempty"`
	DenyCountries  []string `json:"deny_countries,omitempty"`
	AllowUAs       []string `json:"allow_uas,omitempty"`
	DenyUAs        []string `json:"deny_uas,omitempty"`
	Backends       []string `json:"backends,omitempty"`
	AllowDefault   *bool    `json:"allow_default,omitempty"`
}

type EventSink struct {
	Name          string                `json:"name"`
	URL           string                `json:"url"`
	Level         string                `json:"level,omitempty"`
	Only          []map[string][]string `json:"only,omitempty"`
	Except        []map[string][]string `json:"except,omitempty"`
	ThrottleDelay int                   `json:"throttle_delay,omitempty"`
	MaxSize       int                   `json:"max_size,omitempty"`
	MaxDepth      int                   `json:"max_depth,omitempty"`
	ResendLimit   int                   `json:"resend_limit,omitempty"`
	ResendTimeout int                   `json:"resend_timeout,omitempty"`
	Extra         map[string]string     `json:"extra,omitempty"`
}
