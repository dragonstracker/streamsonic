package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := ParseText(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Path = path
	return c, nil
}

func ParseText(src string) (*Config, error) {
	nodes, err := Parse(src)
	if err != nil {
		return nil, err
	}
	c := New()
	if err := c.Apply(nodes); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) Apply(nodes []*Node) error {
	for _, n := range nodes {
		if err := c.applyNode(n); err != nil {
			return err
		}
	}
	c.Rev++
	return nil
}

func (c *Config) applyNode(n *Node) error {
	switch n.Name {
	case "http":
		return c.applyHTTP(n)
	case "edit_auth", "admin_auth":
		c.AdminLogin = n.Arg(0)
		c.AdminPassword = n.Arg(1)
	case "session_log":
		c.SessionLog = n.Arg(0)
	case "dvr":
		d, err := parseDVR(n)
		if err != nil {
			return err
		}
		c.DVRs[d.Name] = d
	case "auth_backend":
		b, err := parseAuthBackend(n)
		if err != nil {
			return err
		}
		c.Backends[b.Name] = b
	case "event_sink":
		s, err := parseSink(n)
		if err != nil {
			return err
		}
		c.Sinks = append(c.Sinks, s)
	case "stream":
		s, err := parseStream(n)
		if err != nil {
			return err
		}
		if _, dup := c.Streams[s.Name]; dup {
			return fmt.Errorf("line %d: stream %q is declared twice", n.Line, s.Name)
		}
		c.Streams[s.Name] = s
	default:
		c.Extra = append(c.Extra, n)
	}
	return nil
}

func (c *Config) applyHTTP(n *Node) error {
	if !n.Block {
		if p := n.Arg(0); p != "" {
			port, err := strconv.Atoi(p)
			if err != nil {
				return fmt.Errorf("line %d: http port %q is not a number", n.Line, p)
			}
			c.HTTP.Port = port
		}
		for _, a := range n.Args[1:] {
			switch a {
			case "tls", "https":
				c.HTTP.TLS = true
			}
		}
		return nil
	}
	for _, ch := range n.Children {
		switch ch.Name {
		case "port":
			port, err := strconv.Atoi(ch.Arg(0))
			if err != nil {
				return fmt.Errorf("line %d: http port %q is not a number", ch.Line, ch.Arg(0))
			}
			c.HTTP.Port = port
		case "tls":
			c.HTTP.TLS = boolArg(ch)
		case "cert", "certificate":
			c.HTTP.Cert = ch.Arg(0)
		case "key", "private_key":
			c.HTTP.Key = ch.Arg(0)
		default:
			return fmt.Errorf("line %d: unknown http option %q", ch.Line, ch.Name)
		}
	}
	return nil
}

func parseDVR(n *Node) (*DVR, error) {
	d := &DVR{Root: "/var/lib/streamsonic/dvr"}
	if n.Block {
		if a := n.Arg(0); a != "" {
			d.Name = a
		}
		for _, ch := range n.Children {
			if err := dvrOption(d, ch); err != nil {
				return nil, err
			}
		}
	} else {
		d.Name = n.Arg(0)
		if len(n.Args) > 1 {
			d.Root = n.Args[1]
		}
	}
	if d.Name == "" {
		return nil, fmt.Errorf("line %d: dvr requires a name", n.Line)
	}
	if d.Root == "" {
		d.Root = "/var/lib/streamsonic/dvr"
	}
	return d, nil
}

func dvrOption(d *DVR, ch *Node) error {
	switch ch.Name {
	case "root":
		d.Root = ch.Arg(0)
	case "index":
		d.Index = ch.Arg(0)
	case "check_mount":
		v := boolArg(ch)
		d.CheckMount = &v
	case "active":
		d.Active = intArg(ch)
	case "disk":
		d.Disks = append(d.Disks, ch.Arg(0))
	case "storage_limit":
		d.StorageLimit = int64(intArg(ch))
	case "expiration":
		d.Expiration = intArg(ch)
	case "episodes_expiration":
		d.EpisodesExpiration = intArg(ch)
	case "dvr_offline":
		d.DVROffline = boolArg(ch)
	case "copy":
		d.Copy = ch.Arg(0)
	case "schedule":
		if len(ch.Args) >= 2 {
			d.Schedule = append(d.Schedule, DVRWindow{From: ch.Args[0], To: ch.Args[1]})
		}
	default:
		return fmt.Errorf("line %d: unknown dvr option %q", ch.Line, ch.Name)
	}
	return nil
}

func parseAuthBackend(n *Node) (*AuthBackend, error) {
	b := &AuthBackend{Name: n.Arg(0)}
	if b.Name == "" {
		return nil, fmt.Errorf("line %d: auth_backend requires a name", n.Line)
	}
	if !n.Block {
		b.Backends = n.Args[1:]
		return b, nil
	}
	for _, ch := range n.Children {
		switch ch.Name {
		case "allow_token":
			b.AllowTokens = append(b.AllowTokens, ch.Arg(0))
		case "deny_token":
			b.DenyTokens = append(b.DenyTokens, ch.Arg(0))
		case "allow_ip":
			b.AllowIPs = append(b.AllowIPs, ch.Arg(0))
		case "deny_ip":
			b.DenyIPs = append(b.DenyIPs, ch.Arg(0))
		case "allow_country":
			b.AllowCountries = append(b.AllowCountries, ch.Arg(0))
		case "deny_country":
			b.DenyCountries = append(b.DenyCountries, ch.Arg(0))
		case "allow_ua":
			b.AllowUAs = append(b.AllowUAs, ch.Arg(0))
		case "deny_ua":
			b.DenyUAs = append(b.DenyUAs, ch.Arg(0))
		case "backend", "url":
			b.Backends = append(b.Backends, ch.Arg(0))
		case "allow_default":
			v := boolArg(ch)
			b.AllowDefault = &v
		default:
			return nil, fmt.Errorf("line %d: unknown auth_backend option %q", ch.Line, ch.Name)
		}
	}
	return b, nil
}

func parseSink(n *Node) (*EventSink, error) {
	s := &EventSink{Name: n.Arg(0)}
	if !n.Block {
		s.URL = n.Arg(1)
		return s, nil
	}
	for _, ch := range n.Children {
		switch ch.Name {
		case "url":
			s.URL = ch.Arg(0)
		case "level":
			s.Level = ch.Arg(0)
		case "throttle_delay":
			s.ThrottleDelay = intArg(ch)
		case "max_size":
			s.MaxSize = intArg(ch)
		case "max_depth":
			s.MaxDepth = intArg(ch)
		case "resend_limit":
			s.ResendLimit = intArg(ch)
		case "resend_timeout":
			s.ResendTimeout = intArg(ch)
		case "only":
			if len(ch.Args) > 0 {
				s.Only = append(s.Only, map[string][]string{ch.Args[0]: ch.Args[1:]})
			}
		case "except":
			if len(ch.Args) > 0 {
				s.Except = append(s.Except, map[string][]string{ch.Args[0]: ch.Args[1:]})
			}
		default:
			if s.Extra == nil {
				s.Extra = map[string]string{}
			}
			s.Extra[ch.Name] = ch.Arg(0)
		}
	}
	return s, nil
}

func parseStream(n *Node) (*Stream, error) {
	s := &Stream{Name: n.Arg(0)}
	if s.Name == "" {
		return nil, fmt.Errorf("line %d: stream requires a name", n.Line)
	}
	if !n.Block {
		if u := n.Arg(1); u != "" {
			in := Input{Type: TypeOfURL(u), URL: u}
			normalizeInputURL(&in)
			s.Inputs = append(s.Inputs, in)
		}
		return s, nil
	}
	for _, ch := range n.Children {
		if err := streamOption(s, ch); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func streamOption(s *Stream, ch *Node) error {
	switch ch.Name {
	case "input":
		in, err := parseInput(ch)
		if err != nil {
			return err
		}
		s.Inputs = append(s.Inputs, in)
	case "backup":
		s.Backup = &Backup{URL: ch.Arg(0)}
	case "static":
		s.Static = boolArg(ch)
	case "ondemand":
		s.Ondemand = boolArg(ch)
	case "nomedia":
		s.NoMedia = boolArg(ch)
	case "title":
		s.Title = ch.Arg(0)
	case "comment":
		s.Comment = ch.Arg(0)
	case "segment_duration":
		s.SegmentDuration = floatArg(ch)
	case "segment_count":
		s.SegmentCount = intArg(ch)
	case "cmaf_enabled":
		s.CMAFEnabled = boolArg(ch)
	case "chunk_duration":
		s.ChunkDuration = floatArg(ch)
	case "dash_update_period":
		s.DashUpdatePeriod = floatArg(ch)
	case "prepush":
		s.Prepush = floatArg(ch)
	case "clients_timeout":
		v := intArg(ch)
		s.ClientsTimeout = &v
	case "retry_limit":
		s.RetryLimit = intArg(ch)
	case "source_timeout":
		s.SourceTimeout = intArg(ch)
	case "video_timeout":
		s.VideoTimeout = intArg(ch)
	case "audio_timeout":
		s.AudioTimeout = intArg(ch)
	case "max_retry_timeout":
		s.MaxRetryTimeout = intArg(ch)
	case "dvr":
		if ch.Block {
			d := &StreamDVR{}
			for _, o := range ch.Children {
				switch o.Name {
				case "reference":
					d.Reference = o.Arg(0)
				case "storage_limit":
					d.StorageLimit = int64(intArg(o))
				case "expiration":
					d.Expiration = intArg(o)
				case "episodes_expiration":
					d.EpisodesExpiration = intArg(o)
				case "replicate":
					d.DVRReplicate = boolArg(o)
				case "replication_speed":
					d.ReplicationSpeed = intArg(o)
				case "replication_port":
					d.ReplicationPort = intArg(o)
				case "dvr_offline":
					d.DVROffline = boolArg(o)
				case "copy":
					d.Copy = o.Arg(0)
				}
			}
			s.DVR = d
		} else {
			s.DVR = &StreamDVR{Reference: ch.Arg(0)}
		}
	case "protocols":
		s.Protocols = parseProtocols(ch)
	case "on_play":
		s.OnPlay = parseAuthSpec(ch)
	case "on_publish":
		s.OnPublish = parseAuthSpec(ch)
	case "push":
		s.Pushes = append(s.Pushes, Push{URL: ch.Arg(0)})
	case "jpeg_snapshot_sign_key":
		s.JpegSignKey = ch.Arg(0)
	case "labels":
		if s.Labels == nil {
			s.Labels = map[string]string{}
		}
		if ch.Block {
			for _, o := range ch.Children {
				s.Labels[o.Name] = o.Arg(0)
			}
		} else if len(ch.Args) >= 2 {
			s.Labels[ch.Args[0]] = ch.Args[1]
		}
	default:
		return fmt.Errorf("line %d: unknown stream option %q", ch.Line, ch.Name)
	}
	return nil
}

func parseInput(ch *Node) (Input, error) {
	in := Input{}
	args := ch.Args
	if len(args) == 0 {
		return in, fmt.Errorf("line %d: input requires a URL", ch.Line)
	}
	if isInputType(args[0]) && len(args) > 1 {
		in.Type = args[0]
		args = args[1:]
	}
	in.URL = args[0]
	if in.Type == "" {
		in.Type = TypeOfURL(in.URL)
	}
	normalizeInputURL(&in)
	for _, a := range args[1:] {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			return in, fmt.Errorf("line %d: input option %q must be key=value", ch.Line, a)
		}
		switch k {
		case "priority":
			in.Priority = atoiSafe(v)
		case "source_timeout":
			in.SourceTimeout = atoiSafe(v)
		case "video_timeout":
			in.VideoTimeout = atoiSafe(v)
		case "audio_timeout":
			in.AudioTimeout = atoiSafe(v)
		case "frames_timeout":
			in.FramesTimeout = atoiSafe(v)
		case "max_retry_timeout":
			in.MaxRetry = atoiSafe(v)
		case "retry_limit":
			in.RetryLimit = atoiSafe(v)
		case "user_agent", "user-agent":
			in.UserAgent = v
		case "bind_ip":
			in.BindIP = v
		case "comment":
			in.Comment = v
		default:
			if in.Headers == nil {
				in.Headers = map[string]string{}
			}
			in.Headers[k] = v
		}
	}
	return in, nil
}

func parseProtocols(ch *Node) *Protocols {
	p := &Protocols{}
	if !ch.Block {
		for _, a := range ch.Args {
			setProtocol(p, a, true)
		}
		return p
	}
	for _, o := range ch.Children {
		switch o.Name {
		case "whitelist":
			p.Whitelist = boolArg(o)
		case "all":
			v := boolArg(o)
			*p = Protocols{Whitelist: p.Whitelist, HLS: v, DASH: v, MSE: v, CMAF: v, MPEGTS: v, MSS: v, RTMP: v, RTSP: v, SRT: v, WebRTC: v}
		default:
			setProtocol(p, o.Name, boolArg(o))
		}
	}
	return p
}

func setProtocol(p *Protocols, name string, v bool) {
	switch name {
	case "hls":
		p.HLS = v
	case "dash":
		p.DASH = v
	case "mse":
		p.MSE = v
	case "cmaf":
		p.CMAF = v
	case "mpegts":
		p.MPEGTS = v
	case "mss":
		p.MSS = v
	case "rtmp":
		p.RTMP = v
	case "rtsp":
		p.RTSP = v
	case "srt":
		p.SRT = v
	case "webrtc":
		p.WebRTC = v
	case "whitelist":
		p.Whitelist = v
	}
}

func parseAuthSpec(ch *Node) *AuthSpec {
	if !ch.Block && len(ch.Args) == 1 {
		return &AuthSpec{URL: ch.Arg(0)}
	}
	a := &AuthSpec{}
	for _, o := range ch.Children {
		switch o.Name {
		case "url":
			a.URL = o.Arg(0)
		case "domain":
			a.Domains = append(a.Domains, o.Arg(0))
		case "max_sessions":
			a.MaxSessions = intArg(o)
		case "allow_country":
			a.AllowedCountries = append(a.AllowedCountries, o.Arg(0))
		case "deny_country":
			a.DisallowedCountries = append(a.DisallowedCountries, o.Arg(0))
		case "soft_limitation":
			a.SoftLimitation = boolArg(o)
		case "session_key":
			a.SessionKeys = append(a.SessionKeys, o.Arg(0))
		}
	}
	return a
}

func isInputType(s string) bool {
	for _, t := range inputTypes {
		if s == t {
			return true
		}
	}
	return false
}

func boolArg(n *Node) bool {
	if len(n.Args) == 0 {
		return true
	}
	switch strings.ToLower(n.Arg(0)) {
	case "true", "yes", "on", "1", "enable", "enabled":
		return true
	}
	return false
}

func intArg(n *Node) int {
	return atoiSafe(n.Arg(0))
}

func floatArg(n *Node) float64 {
	v, err := strconv.ParseFloat(n.Arg(0), 64)
	if err != nil {
		return 0
	}
	return v
}

func atoiSafe(s string) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return v
}
