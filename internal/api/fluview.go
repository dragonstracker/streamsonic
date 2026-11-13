package api

import (
	"encoding/base64"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"streamsonic/internal/config"
	"streamsonic/internal/media"
	"streamsonic/internal/stream"
)

func (s *Server) serverID() string {
	if s.id != "" {
		return s.id
	}
	return "00000000-0000-0000-0000-000000000000"
}

func timingOK(loadMs int) map[string]int {
	return map[string]int{"select": 0, "sort": 0, "load": loadMs, "filter": 0, "limit": 0}
}

func (s *Server) collection(w http.ResponseWriter, key string, items any, total int, next string, loadMs int) {
	body := map[string]any{
		"server_id":       s.serverID(),
		key:               items,
		"estimated_count": total,
		"timing":          timingOK(loadMs),
	}
	if next != "" {
		body["next"] = next
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) emptyCollection(key string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.collection(w, key, []any{}, 0, "", 0)
	}
}

func fluInputURL(in config.Input) string {
	url := in.URL
	if url == "" {
		return ""
	}
	t := in.Type
	if t == "" {
		t = config.TypeOfURL(url)
	}
	if t == "tshttp" && strings.HasPrefix(url, "http://") {
		return "tshttp://" + strings.TrimPrefix(url, "http://")
	}
	if t == "tshttp" && strings.HasPrefix(url, "https://") {
		return "tshttps://" + strings.TrimPrefix(url, "https://")
	}
	return url
}

func fluInputView(in config.Input) map[string]any {
	url := fluInputURL(in)
	if url == "" {
		return map[string]any{}
	}
	t := in.Type
	if t == "" {
		t = config.TypeOfURL(in.URL)
	}
	inner := map[string]any{"url": url}
	if in.SCTE35 != nil {
		inner["scte35"] = *in.SCTE35
	}
	out := map[string]any{"url": url, t: inner}
	if in.SCTE35 != nil {
		out["scte35"] = *in.SCTE35
	}
	return out
}

func streamSearchHaystack(st *stream.Stream) string {
	cfg := st.Config()
	parts := []string{st.Name()}
	if cfg != nil {
		parts = append(parts, cfg.Name, cfg.Title, cfg.Comment)
		for _, in := range cfg.Inputs {
			parts = append(parts, in.URL, fluInputURL(in))
		}
	}
	return strings.ToLower(strings.Join(parts, "\n"))
}

func matchStreamQuery(st *stream.Stream, raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	return strings.Contains(streamSearchHaystack(st), strings.ToLower(raw))
}

func matchStreamFilters(st *stream.Stream, q map[string][]string) bool {
	alive, status := fluStatus(st)
	cfg := st.Config()
	if vals := q["q"]; len(vals) > 0 && !matchStreamQuery(st, vals[0]) {
		return false
	}
	if vals := q["query"]; len(vals) > 0 && !matchStreamQuery(st, vals[0]) {
		return false
	}
	for key, vals := range q {
		if len(vals) == 0 {
			continue
		}
		v := vals[0]
		switch key {
		case "limit", "cursor", "next", "sort", "select", "q", "query", "page", "view":
			continue
		case "disabled":
			want := v == "true"
			if cfg == nil || cfg.Disabled != want {
				return false
			}
		case "named_by":
			if v != "config" {
				return false
			}
		case "stats.status":
			if v == "running" && !(alive || status == "running") {
				return false
			}
			if v == "waiting" && status != "waiting" {
				return false
			}
			if v == "error" && status != "error" {
				return false
			}
		case "dvr_is_not":
			if v == "null" {
				snap := st.Snapshot()
				if _, ok := snap["dvr"]; !ok {
					return false
				}
			}
		case "transcoder_is_not":
			return false
		}
	}
	return true
}

func fluStatus(st *stream.Stream) (alive bool, status string) {
	switch st.Status() {
	case "online", "running":
		return true, "running"
	case "waiting":
		return false, "waiting"
	default:
		cfg := st.Config()
		if cfg != nil && cfg.Ondemand {
			return false, "waiting"
		}
		return false, "error"
	}
}

func fluStreamView(st *stream.Stream, position int) map[string]any {
	cfg := st.Config()
	snap := st.Snapshot()
	alive, status := fluStatus(st)
	raw, _ := snap["stats"].(map[string]any)
	if raw == nil {
		raw = map[string]any{}
	}
	clients := intOf(raw["clients"])
	rate := intOf(raw["bitrate"])
	info := st.Info()

	name := st.Name()
	title := ""
	disabled := false
	static := true
	inputs := []config.Input{}
	if cfg != nil {
		name = cfg.Name
		title = cfg.Title
		disabled = cfg.Disabled
		static = !cfg.Ondemand
		inputs = cfg.Inputs
	}

	mediaView := fluMediaInfoView(info, title)
	proto := ""
	if len(inputs) > 0 {
		proto = inputs[0].Type
		if proto == "" {
			proto = config.TypeOfURL(inputs[0].URL)
		}
	}

	inputStats := map[string]any{
		"active":         alive,
		"proto":          proto,
		"bytes":          raw["bytes_in"],
		"errors":         raw["input_errors"],
		"retries":        0,
		"input_switches": raw["input_switches"],
		"media_info":     mediaView,
	}

	inputsView := make([]map[string]any, 0, len(inputs))
	for i, in := range inputs {
		view := fluInputView(in)
		inProto := proto
		if i > 0 {
			inProto = in.Type
			if inProto == "" {
				inProto = config.TypeOfURL(in.URL)
			}
		}
		stats := map[string]any{
			"active":         alive && i == 0,
			"proto":          inProto,
			"bytes":          raw["bytes_in"],
			"retries":        0,
			"input_switches": raw["input_switches"],
			"media_info":     mediaView,
		}
		view["stats"] = stats
		if inner, ok := view[inProto].(map[string]any); ok {
			inner["stats"] = stats
		}
		inputsView = append(inputsView, view)
	}

	stats := map[string]any{
		"alive":              alive,
		"status":             status,
		"input":              inputStats,
		"inputs":             inputsView,
		"bytes_out":          raw["bytes_out"],
		"bitrate":            rate / 1000,
		"input_bitrate":      rate / 1000,
		"media_info":         mediaView,
		"lifetime":           lifetimeMs(snap),
		"last_access_at":     intOf(raw["last_publish"]),
		"running_transcoder": false,
		"play": map[string]any{
			"play_bytes":           raw["bytes_out"],
			"play_opened_sessions": clients,
		},
	}
	if dvr, ok := snap["dvr"]; ok && dvr != nil {
		stats["dvr_enabled"] = true
		stats["dvr"] = dvr
	} else {
		stats["dvr_enabled"] = false
	}

	out := map[string]any{
		"name":     name,
		"title":    title,
		"position": position,
		"disabled": disabled,
		"static":   static,
		"nomedia":  false,
		"named_by": "config",
		"inputs":   inputsView,
		"stats":    stats,
	}
	if cfg != nil {
		out["config_on_disk"] = map[string]any{
			"name":     name,
			"title":    title,
			"position": position,
			"disabled": disabled,
			"static":   static,
			"inputs":   inputsView,
		}
	}
	return out
}

func fluMediaInfoView(info media.MediaInfo, title string) map[string]any {
	tracks := make([]map[string]any, 0, len(info.Tracks))
	var video, audio int
	for _, t := range info.Tracks {
		view := map[string]any{
			"pid":     t.PID,
			"content": t.Type,
			"codec":   string(t.Codec),
		}
		switch t.Type {
		case media.KindVideo:
			video++
			view["track_id"] = "v" + strconv.Itoa(video)
			view["width"] = t.Width
			view["height"] = t.Height
			view["pixel_width"] = t.Width
			view["pixel_height"] = t.Height
			view["sar_width"] = 1
			view["sar_height"] = 1
			view["fps"] = t.FPS
			view["avg_fps"] = t.FPS
			view["is_progressive"] = true
			view["closed_captions"] = []string{}
			if t.Profile != "" {
				view["profile"] = t.Profile
			}
			if t.Level != "" {
				view["level"] = t.Level
			}
		case media.KindAudio:
			audio++
			view["track_id"] = "a" + strconv.Itoa(audio)
			view["sample_rate"] = t.SampleRate
			view["channels"] = t.Channels
		}
		if t.Bitrate > 0 {
			view["bitrate"] = t.Bitrate / 1000
		}
		if t.Language != "" {
			view["language"] = t.Language
		}
		tracks = append(tracks, view)
	}
	out := map[string]any{
		"tracks":    tracks,
		"provider":  "Streamsonic",
		"stream_id": 0,
	}
	if title != "" {
		out["title"] = title
	}
	if len(info.Programs) > 0 {
		out["program_id"] = info.Programs[0].ID
	}
	return out
}

func lifetimeMs(snap map[string]any) int64 {
	started, ok := snap["started"].(string)
	if !ok || started == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, started)
	if err != nil || t.IsZero() {
		return 0
	}
	return time.Since(t).Milliseconds()
}

func intOf(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case uint64:
		return int64(n)
	}
	return 0
}

func parseLimit(r *http.Request, def int) int {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	if n > 1000 {
		return 1000
	}
	return n
}

func parseCursor(r *http.Request) int {
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		raw = r.URL.Query().Get("next")
	}
	if raw == "" {
		return 0
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(raw)
		if err != nil {
			n, _ := strconv.Atoi(raw)
			return n
		}
	}
	text := string(decoded)
	text = strings.ReplaceAll(text, "%24", "$")
	for _, key := range []string{"$position_gt=", "position_gt="} {
		if i := strings.Index(text, key); i >= 0 {
			n, _ := strconv.Atoi(strings.TrimPrefix(text[i:], key))
			return n + 1
		}
	}
	n, _ := strconv.Atoi(raw)
	return n
}

func nextCursor(lastPos int) string {
	return base64.StdEncoding.EncodeToString([]byte("%24position_gt=" + strconv.Itoa(lastPos)))
}

func projectStream(view map[string]any, selectParam string) map[string]any {
	selectParam = strings.TrimSpace(selectParam)
	if selectParam == "" {
		return view
	}
	out := map[string]any{}
	for _, field := range strings.Split(selectParam, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if v, ok := view[field]; ok {
			out[field] = v
		}
	}
	return out
}

func sortedStreams(list []*stream.Stream) []*stream.Stream {
	out := append([]*stream.Stream(nil), list...)
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name()) < strings.ToLower(out[j].Name())
	})
	return out
}

func (s *Server) fluConfigStats() map[string]any {
	list := s.mgr.List()
	var clients int64
	var rateIn, rateOut int64
	online := 0
	for _, st := range list {
		snap := st.Snapshot()
		stats := snap["stats"].(map[string]any)
		if c, ok := stats["clients"].(int64); ok {
			clients += c
		}
		if b, ok := stats["bitrate"].(int64); ok {
			rateIn += b
			if c, ok := stats["clients"].(int64); ok && c > 0 {
				rateOut += b * c
			}
		}
		if alive, status := fluStatus(st); alive || status == "running" {
			online++
		}
	}
	now := time.Now()
	return map[string]any{
		"id":                 s.serverID(),
		"now":                now.UnixMilli(),
		"hostname":           "streamsonic",
		"build":              0,
		"started_at":         s.started.Unix(),
		"uptime":             int64(now.Sub(s.started).Seconds()),
		"cpu_usage":          cpuUsage(),
		"memory_usage":       memoryUsage(),
		"server_version":     s.version,
		"config_version":     []int64{s.started.Unix(), s.store.Rev()},
		"rproxy":             false,
		"rproxy_running":     false,
		"vsaas":              false,
		"vsaas_branding":     false,
		"transcoder":         true,
		"license_type":       "open_source",
		"license_txt":        "",
		"scheduler_load":     schedulerLoad(),
		"is_chassis":         false,
		"partitions":         diskPartitions(),
		"streamer_status":    "running",
		"text_alerts":        map[string]any{},
		"transcoder_devices": []map[string]any{{"id": 0, "name": "CPU Encoder", "type": "cpu"}},
		"video_devices":      []any{},
		"input_kbit":         rateIn / 1000,
		"output_kbit":        rateOut / 1000,
		"online_streams":     online,
		"opened_files":       0,
		"total_clients":      clients,
		"total_streams":      len(list),
	}
}
