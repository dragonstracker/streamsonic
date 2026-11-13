package api

import (
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
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
	if wantsPlainText(r) {
		writeText(w, http.StatusOK, cfg.RenderText())
		return
	}
	listeners := map[string]any{}
	if cfg.HTTP.Port > 0 {
		listeners["http"] = []map[string]any{{"port": cfg.HTTP.Port}}
	}
	body := map[string]any{
		"server_id":   s.serverID(),
		"stats":       s.fluConfigStats(),
		"listeners":   listeners,
		"session_log": cfg.SessionLog,
	}
	if cfg.AdminLogin != "" {
		body["edit_auth"] = map[string]any{
			"login":    cfg.AdminLogin,
			"password": cfg.AdminPassword,
			"hash":     false,
		}
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) getConfigText(w http.ResponseWriter, r *http.Request) {
	writeText(w, http.StatusOK, s.store.Current().RenderText())
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if ct == "text/plain" || ct == "application/x-streamsonic-config" || ct == "application/x-flussonic-config" {
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

func (s *Server) uiSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"title":   "streamsonic",
		"product": "streamer",
		"brand":   "streamsonic",
		"logo":    brandB64("logo.png"),
		"logoAuth": brandB64("logoAuth.png"),
		"colors": map[string]string{
			"primary":    "#002DAB",
			"secondary":  "#FF0D70",
			"background": "#fff",
		},
		"fonts": map[string]string{
			"regular": brandB64("regular.woff2"),
			"light":   brandB64("light.woff2"),
			"medium":  brandB64("medium.woff2"),
		},
		"favicons": brandFavicons(),
		"locales":  []string{"en", "es", "ru"},
		"menu_items": map[string]any{
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

func (s *Server) systemUpdater(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "disabled",
		"version": s.version,
	})
}

func (s *Server) licenseInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "open_source",
		"license":     "open_source",
		"version":     s.version,
		"activated":   true,
		"online":      false,
		"message":     "streamsonic is open source; no license key is required",
	})
}

func (s *Server) adminViewToken(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"token": ""})
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
