package api

import (
	"encoding/base64"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"streamsonic/internal/config"
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

func matchStreamFilters(st *stream.Stream, q map[string][]string) bool {
	alive, status := fluStatus(st)
	cfg := st.Config()
	for key, vals := range q {
		if len(vals) == 0 {
			continue
		}
		v := vals[0]
		switch key {
		case "limit", "cursor", "next", "sort", "select", "q":
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
	stats := map[string]any{
		"alive":  alive,
		"status": status,
		"play":   map[string]any{"play_opened_sessions": snap["stats"].(map[string]any)["clients"]},
	}
	if raw, ok := snap["stats"].(map[string]any); ok {
		if v, ok := raw["bitrate"]; ok {
			stats["bitrate"] = v
			stats["input_bitrate"] = v
		}
		if v, ok := raw["bytes_in"]; ok {
			stats["bytes_in"] = v
		}
		if v, ok := raw["bytes_out"]; ok {
			stats["bytes_out"] = v
		}
		if v, ok := raw["input_errors"]; ok {
			stats["input_errors"] = v
		}
		if v, ok := raw["input_switches"]; ok {
			stats["input_switches"] = v
		}
	}
	if err, ok := snap["last_error"].(string); ok && err != "" {
		stats["source_error"] = err
	}
	if info, ok := snap["media_info"]; ok && info != nil {
		stats["media_info"] = info
	}
	if dvr, ok := snap["dvr"]; ok {
		stats["dvr_enabled"] = true
		stats["dvr"] = dvr
	} else {
		stats["dvr_enabled"] = false
	}

	inputs := make([]map[string]any, 0)
	title := ""
	disabled := false
	static := true
	name := st.Name()
	if cfg != nil {
		name = cfg.Name
		title = cfg.Title
		disabled = cfg.Disabled
		static = !cfg.Ondemand
		for _, in := range cfg.Inputs {
			inputs = append(inputs, fluInputView(in))
		}
	}

	out := map[string]any{
		"name":      name,
		"title":     title,
		"position":  position,
		"disabled":  disabled,
		"static":    static,
		"nomedia":   false,
		"named_by":  "config",
		"inputs":    inputs,
		"stats":     stats,
	}
	if cfg != nil {
		out["config_on_disk"] = map[string]any{
			"name":     name,
			"title":    title,
			"disabled": disabled,
			"inputs":   inputs,
		}
		if cfg.Ondemand {
			out["config_on_disk"].(map[string]any)["static"] = false
		}
	}
	return out
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
		"id":              s.serverID(),
		"now":             now.UnixMilli(),
		"hostname":        "streamsonic",
		"build":           0,
		"started_at":      s.started.Unix(),
		"uptime":          int64(now.Sub(s.started).Seconds()),
		"server_version":  s.version,
		"config_version":  []int64{s.started.Unix(), s.store.Rev()},
		"streamer_status": "running",
		"transcoder":      false,
		"is_chassis":      false,
		"license_type":    "open_source",
		"input_kbit":      rateIn / 1000,
		"output_kbit":     rateOut / 1000,
		"online_streams":  online,
		"opened_files":    0,
		"total_clients":   clients,
		"total_streams":   len(list),
	}
}
