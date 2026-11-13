package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

func (c *Config) RenderText() string {
	return Render(c.Nodes())
}

func (c *Config) Nodes() []*Node {
	nodes := make([]*Node, 0, 8)
	if c.HTTP.Flat && !c.HTTP.TLS && c.HTTP.Cert == "" && c.HTTP.Key == "" {
		nodes = append(nodes, &Node{Name: "http", Args: []string{strconv.Itoa(c.HTTP.Port)}})
	} else {
		http := &Node{Name: "http", Block: true}
		http.Children = append(http.Children, &Node{Name: "port", Args: []string{strconv.Itoa(c.HTTP.Port)}})
		if c.HTTP.TLS {
			http.Children = append(http.Children, &Node{Name: "tls", Args: []string{"true"}})
		}
		if c.HTTP.Cert != "" {
			http.Children = append(http.Children, &Node{Name: "cert", Args: []string{c.HTTP.Cert}})
		}
		if c.HTTP.Key != "" {
			http.Children = append(http.Children, &Node{Name: "key", Args: []string{c.HTTP.Key}})
		}
		nodes = append(nodes, http)
	}
	if c.AdminLogin != "" {
		nodes = append(nodes, &Node{Name: "edit_auth", Args: []string{c.AdminLogin, c.AdminPassword}})
	}
	if c.SessionLog != "" {
		nodes = append(nodes, &Node{Name: "session_log", Args: []string{c.SessionLog}})
	}
	for _, name := range sortedKeys(c.DVRs) {
		nodes = append(nodes, dvrNode(c.DVRs[name]))
	}
	for _, name := range sortedKeys(c.Backends) {
		nodes = append(nodes, backendNode(c.Backends[name]))
	}
	for _, s := range c.Sinks {
		nodes = append(nodes, sinkNode(s))
	}
	for _, name := range c.OrderedStreams() {
		nodes = append(nodes, streamNode(c.Streams[name]))
	}
	nodes = append(nodes, c.Extra...)
	return nodes
}

func dvrNode(d *DVR) *Node {
	n := &Node{Name: "dvr", Args: []string{d.Name}, Block: true}
	if d.Root != "" {
		n.Children = append(n.Children, &Node{Name: "root", Args: []string{d.Root}})
	}
	if d.Index != "" {
		n.Children = append(n.Children, &Node{Name: "index", Args: []string{d.Index}})
	}
	if d.StorageLimit > 0 {
		n.Children = append(n.Children, &Node{Name: "storage_limit", Args: []string{strconv.FormatInt(d.StorageLimit, 10)}})
	}
	if d.Expiration > 0 {
		n.Children = append(n.Children, &Node{Name: "expiration", Args: []string{strconv.Itoa(d.Expiration)}})
	}
	if d.EpisodesExpiration > 0 {
		n.Children = append(n.Children, &Node{Name: "episodes_expiration", Args: []string{strconv.Itoa(d.EpisodesExpiration)}})
	}
	if d.DVROffline {
		n.Children = append(n.Children, &Node{Name: "dvr_offline", Args: []string{"true"}})
	}
	if d.Copy != "" {
		n.Children = append(n.Children, &Node{Name: "copy", Args: []string{d.Copy}})
	}
	for _, s := range d.Schedule {
		n.Children = append(n.Children, &Node{Name: "schedule", Args: []string{s.From, s.To}})
	}
	return n
}

func backendNode(b *AuthBackend) *Node {
	n := &Node{Name: "auth_backend", Args: []string{b.Name}, Block: true}
	addAll := func(key string, vals []string) {
		for _, v := range vals {
			n.Children = append(n.Children, &Node{Name: key, Args: []string{v}})
		}
	}
	addAll("allow_token", b.AllowTokens)
	addAll("deny_token", b.DenyTokens)
	addAll("allow_ip", b.AllowIPs)
	addAll("deny_ip", b.DenyIPs)
	addAll("allow_country", b.AllowCountries)
	addAll("deny_country", b.DenyCountries)
	addAll("allow_ua", b.AllowUAs)
	addAll("deny_ua", b.DenyUAs)
	addAll("backend", b.Backends)
	return n
}

func sinkNode(s *EventSink) *Node {
	n := &Node{Name: "event_sink", Args: []string{s.Name}, Block: true}
	if s.URL != "" {
		n.Children = append(n.Children, &Node{Name: "url", Args: []string{s.URL}})
	}
	if s.Level != "" {
		n.Children = append(n.Children, &Node{Name: "level", Args: []string{s.Level}})
	}
	if s.ThrottleDelay > 0 {
		n.Children = append(n.Children, &Node{Name: "throttle_delay", Args: []string{strconv.Itoa(s.ThrottleDelay)}})
	}
	if s.ResendLimit > 0 {
		n.Children = append(n.Children, &Node{Name: "resend_limit", Args: []string{strconv.Itoa(s.ResendLimit)}})
	}
	if s.ResendTimeout > 0 {
		n.Children = append(n.Children, &Node{Name: "resend_timeout", Args: []string{strconv.Itoa(s.ResendTimeout)}})
	}
	for _, m := range s.Only {
		for k, v := range m {
			n.Children = append(n.Children, &Node{Name: "only", Args: append([]string{k}, v...)})
		}
	}
	return n
}

func streamNode(s *Stream) *Node {
	kw := s.Keyword
	if kw != "stream" && kw != "ondemand" {
		kw = "stream"
		if s.Ondemand {
			kw = "ondemand"
		}
	}
	n := &Node{Name: kw, Args: []string{s.Name}, Block: true}
	add := func(name string, args ...string) {
		n.Children = append(n.Children, &Node{Name: name, Args: args})
	}
	if s.Disabled {
		add("disabled", "true")
	}
	for _, in := range s.Inputs {
		args := []string{inputURL(in)}
		if in.Priority != 0 {
			args = append(args, fmt.Sprintf("priority=%d", in.Priority))
		}
		if in.SourceTimeout != 0 {
			args = append(args, fmt.Sprintf("source_timeout=%d", in.SourceTimeout))
		}
		if in.RetryLimit != 0 {
			args = append(args, fmt.Sprintf("retry_limit=%d", in.RetryLimit))
		}
		for _, k := range headerKeys(in) {
			args = append(args, k+"="+in.Headers[k])
		}
		n.Children = append(n.Children, &Node{Name: "input", Args: args})
	}
	if s.Backup != nil && s.Backup.URL != "" {
		add("backup", s.Backup.URL)
	}
	if s.Title != "" {
		add("title", s.Title)
	}
	if s.Comment != "" {
		add("comment", s.Comment)
	}
	if s.Static {
		add("static", "true")
	}
	if s.Ondemand && kw != "ondemand" {
		add("ondemand", "true")
	}
	if s.NoMedia {
		add("nomedia", "true")
	}
	if s.SegmentDuration > 0 {
		add("segment_duration", trimFloat(s.SegmentDuration))
	}
	if s.SegmentCount > 0 {
		add("segment_count", strconv.Itoa(s.SegmentCount))
	}
	if s.CMAFEnabled {
		add("cmaf_enabled", "true")
	}
	if s.ChunkDuration > 0 {
		add("chunk_duration", trimFloat(s.ChunkDuration))
	}
	if s.DashUpdatePeriod > 0 {
		add("dash_update_period", trimFloat(s.DashUpdatePeriod))
	}
	if s.ClientsTimeout != nil {
		add("clients_timeout", strconv.Itoa(*s.ClientsTimeout))
	}
	if s.SourceTimeout > 0 {
		add("source_timeout", strconv.Itoa(s.SourceTimeout))
	}
	if s.RetryLimit > 0 {
		add("retry_limit", strconv.Itoa(s.RetryLimit))
	}
	if s.DVR != nil {
		d := &Node{Name: "dvr", Block: true}
		if s.DVR.Reference != "" {
			d.Args = []string{s.DVR.Reference}
		}
		if s.DVR.StorageLimit > 0 {
			d.Children = append(d.Children, &Node{Name: "storage_limit", Args: []string{strconv.FormatInt(s.DVR.StorageLimit, 10)}})
		}
		if s.DVR.Expiration > 0 {
			d.Children = append(d.Children, &Node{Name: "expiration", Args: []string{strconv.Itoa(s.DVR.Expiration)}})
		}
		if s.DVR.DVROffline {
			d.Children = append(d.Children, &Node{Name: "dvr_offline", Args: []string{"true"}})
		}
		if s.DVR.Reference != "" && len(d.Children) == 0 {
			add("dvr", s.DVR.Reference)
		} else {
			n.Children = append(n.Children, d)
		}
	}
	if s.Protocols != nil {
		p := &Node{Name: "protocols", Block: true}
		for name, v := range map[string]bool{
			"hls": s.Protocols.HLS, "dash": s.Protocols.DASH, "mse": s.Protocols.MSE,
			"cmaf": s.Protocols.CMAF, "mpegts": s.Protocols.MPEGTS, "mss": s.Protocols.MSS,
			"webrtc": s.Protocols.WebRTC, "rtmp": s.Protocols.RTMP, "rtsp": s.Protocols.RTSP,
			"srt": s.Protocols.SRT,
		} {
			p.Children = append(p.Children, &Node{Name: name, Args: []string{strconv.FormatBool(v)}})
		}
		sort.Slice(p.Children, func(i, j int) bool { return p.Children[i].Name < p.Children[j].Name })
		n.Children = append(n.Children, p)
	}
	if s.OnPlay != nil {
		n.Children = append(n.Children, &Node{Name: "on_play", Args: []string{s.OnPlay.URL}})
	}
	for _, p := range s.Pushes {
		if p.Name != "" {
			push := &Node{Name: "push", Args: []string{p.Name}, Block: true}
			if p.URL != "" {
				push.Children = append(push.Children, &Node{Name: "url", Args: []string{p.URL}})
			}
			if p.Comment != "" {
				push.Children = append(push.Children, &Node{Name: "comment", Args: []string{p.Comment}})
			}
			n.Children = append(n.Children, push)
			continue
		}
		add("push", p.URL)
	}
	if s.JpegSignKey != "" {
		add("jpeg_snapshot_sign_key", s.JpegSignKey)
	}
	n.Children = append(n.Children, s.Extras...)
	return n
}

func trimFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func headerKeys(in Input) []string {
	out := make([]string, 0, len(in.Headers))
	seen := make(map[string]bool, len(in.Headers))
	for _, k := range in.HeaderOrder {
		if _, ok := in.Headers[k]; ok && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for _, k := range sortedKeys(in.Headers) {
		if !seen[k] {
			out = append(out, k)
		}
	}
	return out
}

func (c *Config) OrderedStreams() []string {
	out := make([]string, 0, len(c.Streams))
	seen := make(map[string]bool, len(c.Streams))
	for _, name := range c.Order {
		if _, ok := c.Streams[name]; ok && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, name := range sortedKeys(c.Streams) {
		if !seen[name] {
			out = append(out, name)
		}
	}
	return out
}

type Snapshot struct {
	HTTP          HTTP           `json:"http"`
	AdminLogin    string         `json:"edit_auth,omitempty"`
	AdminPassword string         `json:"-"`
	SessionLog    string         `json:"session_log,omitempty"`
	Streams       []*Stream      `json:"streams"`
	DVRs          []*DVR         `json:"dvrs,omitempty"`
	AuthBackends  []*AuthBackend `json:"auth_backends,omitempty"`
	EventSinks    []*EventSink   `json:"event_sinks,omitempty"`
}

func (c *Config) Snapshot() Snapshot {
	s := Snapshot{
		HTTP:          c.HTTP,
		AdminLogin:    c.AdminLogin,
		AdminPassword: c.AdminPassword,
		SessionLog:    c.SessionLog,
		Streams:       make([]*Stream, 0, len(c.Streams)),
	}
	for _, name := range c.OrderedStreams() {
		s.Streams = append(s.Streams, c.Streams[name])
	}
	for _, k := range sortedKeys(c.DVRs) {
		s.DVRs = append(s.DVRs, c.DVRs[k])
	}
	for _, k := range sortedKeys(c.Backends) {
		s.AuthBackends = append(s.AuthBackends, c.Backends[k])
	}
	s.EventSinks = append(s.EventSinks, c.Sinks...)
	return s
}

func (s Snapshot) APIView() map[string]any {
	out := map[string]any{
		"http":        s.HTTP,
		"edit_auth":   s.AdminLogin,
		"session_log": s.SessionLog,
	}
	streams := make([]map[string]any, 0, len(s.Streams))
	for _, st := range s.Streams {
		streams = append(streams, st.APIView())
	}
	out["streams"] = streams
	dvrs := make([]map[string]any, 0, len(s.DVRs))
	for _, d := range s.DVRs {
		dvrs = append(dvrs, map[string]any{"name": d.Name, "root": d.Root, "expiration": d.Expiration})
	}
	out["dvrs"] = dvrs
	out["auth_backends"] = len(s.AuthBackends)
	out["event_sinks"] = len(s.EventSinks)
	return out
}

func FromSnapshot(s Snapshot) *Config {
	c := New()
	c.HTTP = s.HTTP
	c.AdminLogin = s.AdminLogin
	c.AdminPassword = s.AdminPassword
	c.SessionLog = s.SessionLog
	for _, st := range s.Streams {
		c.Streams[st.Name] = st
		c.Order = append(c.Order, st.Name)
	}
	for _, d := range s.DVRs {
		c.DVRs[d.Name] = d
	}
	for _, b := range s.AuthBackends {
		c.Backends[b.Name] = b
	}
	c.Sinks = append(c.Sinks, s.EventSinks...)
	return c
}

func (c *Config) Clone() *Config {
	blob, err := json.Marshal(c.Snapshot())
	if err != nil {
		return c
	}
	var snap Snapshot
	if err := json.Unmarshal(blob, &snap); err != nil {
		return c
	}
	out := FromSnapshot(snap)
	out.AdminPassword = c.AdminPassword
	out.Path = c.Path
	out.Rev = c.Rev
	out.Extra = c.Extra
	return out
}
