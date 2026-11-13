package api

import (
	"errors"
	"io"
	"net/http"
	"runtime"
	"time"

	"streamsonic/internal/config"
)

var (
	errConflict   = errors.New("resource already exists")
	errNotFound   = errors.New("resource not found")
	errEmptyBody  = errors.New("request body is empty")
	errBadRequest = errors.New("bad request")
)

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Current()
	writeJSON(w, http.StatusOK, map[string]any{
		"config_version": []int64{s.started.Unix(), s.store.Rev()},
		"config":         cfg.Snapshot().APIView(),
		"text":           cfg.RenderText(),
	})
}

func (s *Server) getConfigText(w http.ResponseWriter, r *http.Request) {
	writeText(w, http.StatusOK, s.store.Current().RenderText())
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if ct == "text/plain" || ct == "application/x-streamsonic-config" {
		body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		next, err := config.ParseText(string(body))
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		next.Path = s.store.Current().Path
		if err := s.store.Replace(next); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		s.apply()
		writeJSON(w, http.StatusOK, map[string]any{"config_version": []int64{s.started.Unix(), s.store.Rev()}})
		return
	}
	var snap config.Snapshot
	if err := decodeBody(r, &snap); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	next := config.FromSnapshot(snap)
	next.Path = s.store.Current().Path
	if err := s.store.Replace(next); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.apply()
	writeJSON(w, http.StatusOK, map[string]any{"config_version": []int64{s.started.Unix(), s.store.Rev()}})
}

func (s *Server) serverStats(w http.ResponseWriter, r *http.Request) {
	list := s.mgr.List()
	var clients, segments int64
	var bytesIn, bytesOut, rateIn, rateOut int64
	for _, st := range list {
		snap := st.Snapshot()
		stats := snap["stats"].(map[string]any)
		clients += stats["clients"].(int64)
		segments += stats["segments"].(int64)
		bytesIn += stats["bytes_in"].(int64)
		bytesOut += stats["bytes_out"].(int64)
		rateIn += stats["bitrate"].(int64)
		if c, ok := stats["clients"].(int64); ok && c > 0 {
			rateOut += stats["bitrate"].(int64) * c
		}
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	writeJSON(w, http.StatusOK, map[string]any{
		"version":        s.version,
		"uptime":         int64(time.Since(s.started).Seconds()),
		"streams":        len(list),
		"stream_limit":   999,
		"files":          0,
		"clients":        clients,
		"segments":       segments,
		"bytes_in":       bytesIn,
		"bytes_out":      bytesOut,
		"bytes_in_rate":  rateIn,
		"bytes_out_rate": rateOut,
		"goroutines":     runtime.NumGoroutine(),
		"heap_alloc":     mem.HeapAlloc,
		"sys":            mem.Sys,
		"num_cpu":        runtime.NumCPU(),
		"config_rev":     s.store.Rev(),
		"go_version":     runtime.Version(),
		"sessions":       s.mgr.Sessions().Count(""),
		"admin_login":    s.store.Current().AdminLogin,
	})
}

// uiSettings reproduces the Flussonic white-label theme contract.
// The admin SPA reads title, brand, colors, fonts, favicons and menu_items
// from this endpoint, so a deployment can be re-branded without a rebuild.
func (s *Server) uiSettings(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Current()
	writeJSON(w, http.StatusOK, map[string]any{
		"title":   "streamsonic",
		"product": "streamer",
		"brand":   "streamsonic",
		"version": s.version,
		"colors": map[string]string{
			"primary":    "#002DAB",
			"secondary":  "#FF0D70",
			"background": "#fff",
		},
		"fonts": map[string]string{
			"regular": "fira-sans-400.woff2",
			"light":   "fira-sans-300.woff2",
			"medium":  "fira-sans-500.woff2",
		},
		"locales":      []string{"en"},
		"edit_auth":    cfg.AdminLogin != "",
		"default_page": "streams",
		"locale":       "en",
		"timezone":     "UTC",
		"menu_items": map[string]bool{
			"cluster":              true,
			"iptv":                 true,
			"chassis":              false,
			"ipcam":                true,
			"main_cluster_sources": true,
			"main_dvb_cards":       true,
			"main_templates":       true,
			"main_transponders":    true,
			"main_vod_locations":   true,
			"pins":                 false,
			"pulse":                true,
			"streamer_config":      true,
			"upload_logs":          true,
		},
	})
}

func (s *Server) liveness(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Current()
	online := 0
	for _, st := range s.mgr.List() {
		if st.Status() == "online" {
			online++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"streams":        len(cfg.Streams),
		"streams_online": online,
	})
}

func (s *Server) versionHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version": s.version,
		"build":   runtime.Version(),
		"edition": "streamsonic",
	})
}
