package api

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"streamsonic/internal/media"
	"streamsonic/internal/packager"
	"streamsonic/internal/session"
	"streamsonic/internal/stream"
)

type countingWriter struct {
	http.ResponseWriter
	n int64
}

func (c *countingWriter) Write(b []byte) (int, error) {
	n, err := c.ResponseWriter.Write(b)
	c.n += int64(n)
	return n, err
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/stream/")
	name, file, ok := strings.Cut(rest, "/")
	if !ok || name == "" {
		writeErr(w, http.StatusNotFound, "stream not found")
		return
	}
	s.serveMediaPath(w, r, name, file)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p != "" {
		name, file, ok := strings.Cut(p, "/")
		if ok && name != "" && looksLikeMedia(file) {
			s.serveMediaPath(w, r, name, file)
			return
		}
	}
	s.uiHandler().ServeHTTP(w, r)
}

func looksLikeMedia(file string) bool {
	if file == "" {
		return false
	}
	if file == "info.json" || file == "media_info.json" || file == "ranges.json" || file == "preview.json" {
		return true
	}
	for _, ext := range []string{".m3u8", ".mpd", ".mp4", ".m4s", ".ts"} {
		if strings.HasSuffix(file, ext) {
			return true
		}
	}
	return strings.HasPrefix(file, "archive-") || strings.HasPrefix(file, "timeshift_") ||
		strings.HasPrefix(file, "rewind-") || strings.HasPrefix(file, "dvr-")
}

func (s *Server) serveMediaPath(w http.ResponseWriter, r *http.Request, name, file string) {
	st := s.mgr.Get(name)
	if st == nil {
		writeErr(w, http.StatusNotFound, "stream not found")
		return
	}
	if !s.authorizePlay(w, r, st) {
		return
	}
	st = s.mgr.EnsureStarted(name)
	proto := "hls"
	if strings.HasSuffix(file, ".mpd") {
		proto = "dash"
	} else if strings.HasSuffix(file, ".mp4") || strings.HasSuffix(file, ".m4s") {
		proto = "dash"
	}
	sess := s.trackSession(w, r, st, proto)
	cw := &countingWriter{ResponseWriter: w}
	defer func() {
		sess.AddBytes(cw.n)
		st.AddOutputBytes(cw.n)
	}()
	files := &mediaHandler{server: s, stream: st, file: file, w: cw, r: r}
	files.serve()
}

func (s *Server) trackSession(w http.ResponseWriter, r *http.Request, st *stream.Stream, proto string) *session.Session {
	reg := s.mgr.Sessions()
	if c, err := r.Cookie("ss-session"); err == nil && c.Value != "" {
		if existing := reg.Get(c.Value); existing != nil && existing.Stream == st.Name() {
			existing.Touch()
			return existing
		}
	}
	sess := &session.Session{
		Stream:    st.Name(),
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		Protocol:  proto,
		Referer:   r.Referer(),
		Started:   time.Now(),
	}
	reg.Add(sess)
	st.AddClient()
	http.SetCookie(w, &http.Cookie{Name: "ss-session", Value: sess.ID, Path: "/", MaxAge: 3600})
	return sess
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) authorizePlay(w http.ResponseWriter, r *http.Request, st *stream.Stream) bool {
	cfg := st.Config()
	if cfg == nil || cfg.OnPlay == nil || cfg.OnPlay.URL == "" {
		return true
	}
	u := cfg.OnPlay.URL
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	target := u + sep + "stream=" + st.Name() + "&ip=" + clientIP(r)
	if tok := tokenOf(r); tok != "" {
		target += "&token=" + tok
	}
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		writeErr(w, http.StatusBadGateway, "authorization backend unavailable")
		return false
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeErr(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

type mediaHandler struct {
	server *Server
	stream *stream.Stream
	file   string
	w      http.ResponseWriter
	r      *http.Request
}

func (h *mediaHandler) serve() {
	file := h.file
	switch {
	case file == "info.json":
		writeJSON(h.w, http.StatusOK, h.stream.Snapshot())
	case file == "media_info.json":
		writeJSON(h.w, http.StatusOK, h.stream.Info())
	case file == "ranges.json":
		h.serveRanges()
	case file == "index.m3u8" || file == "master.m3u8" || file == "playlist.m3u8":
		h.serveHLS()
	case file == "index.mpd" || file == "manifest.mpd":
		h.serveMPD()
	case file == "init-video.mp4":
		h.serveInit(h.stream.DASHVideo())
	case file == "init-audio.mp4":
		h.serveInit(h.stream.DASHAudio())
	case file == "preview.json":
		h.servePreviewManifest()
	case file == "preview-init.mp4":
		h.serveInit(h.stream.Preview())
	case strings.HasPrefix(file, "preview-") && strings.HasSuffix(file, ".m4s"):
		h.serveFragment(h.stream.Preview(), strings.TrimSuffix(strings.TrimPrefix(file, "preview-"), ".m4s"))
	case strings.HasPrefix(file, "video-") && strings.HasSuffix(file, ".m4s"):
		h.serveFragment(h.stream.DASHVideo(), strings.TrimSuffix(strings.TrimPrefix(file, "video-"), ".m4s"))
	case strings.HasPrefix(file, "audio-") && strings.HasSuffix(file, ".m4s"):
		h.serveFragment(h.stream.DASHAudio(), strings.TrimSuffix(strings.TrimPrefix(file, "audio-"), ".m4s"))
	case strings.HasPrefix(file, "archive-"):
		h.serveArchive()
	case strings.HasPrefix(file, "timeshift_abs-"):
		h.serveTimeshift(strings.TrimPrefix(file, "timeshift_abs-"), "abs")
	case strings.HasPrefix(file, "timeshift_rel-"):
		h.serveTimeshift(strings.TrimPrefix(file, "timeshift_rel-"), "rel")
	case strings.HasPrefix(file, "rewind-"):
		h.serveTimeshift(strings.TrimPrefix(file, "rewind-"), "rewind")
	case strings.HasPrefix(file, "dvr-") && strings.HasSuffix(file, ".ts"):
		h.serveDVRChunk(strings.TrimSuffix(strings.TrimPrefix(file, "dvr-"), ".ts"))
	case strings.HasSuffix(file, ".ts") && strings.Contains(file, "/"):
		h.serveLiveSegment(file)
	default:
		writeErr(h.w, http.StatusNotFound, "unknown media resource")
	}
}

func (h *mediaHandler) serveHLS() {
	body := packager.LivePlaylist(h.stream.HLS(), func(c *packager.Chunk) string {
		return packager.SegmentName(c.From, c.Seq) + ".ts"
	})
	h.w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	h.w.Header().Set("Cache-Control", "no-cache")
	h.w.Write([]byte(body))
}

func (h *mediaHandler) serveMPD() {
	info := h.stream.Info()
	p := packager.DashParams{
		Window:        time.Duration(h.stream.Config().SegmentCountOrDefault()*6) * time.Second,
		AvailableFrom: time.Now().Add(-time.Minute),
	}
	for _, tr := range info.Tracks {
		if tr.Codec == media.CodecH264 && p.Video.ID == 0 {
			p.Video = packager.TrackInfo{
				ID:        1,
				MimeType:  "video/mp4",
				Codec:     avcCodecString(tr),
				Timescale: 90000,
				Width:     first(tr.Width, 1920),
				Height:    first(tr.Height, 1080),
				FPS:       defaultFPS(tr.FPS),
				Bandwidth: 3000000,
			}
		}
		if tr.Codec == media.CodecAAC && p.Audio.ID == 0 {
			p.Audio = packager.TrackInfo{
				ID:        2,
				MimeType:  "audio/mp4",
				Codec:     "mp4a.40.2",
				Timescale: uint32(first(tr.SampleRate, 48000)),
				Channels:  first(tr.Channels, 2),
				Bandwidth: 128000,
			}
		}
	}
	body := packager.MPD(p, h.stream.DASHVideo(), h.stream.DASHAudio())
	h.w.Header().Set("Content-Type", "application/dash+xml")
	h.w.Header().Set("Cache-Control", "no-cache")
	h.w.Write([]byte(body))
}

func avcCodecString(t *media.Track) string {
	if len(t.SPS) >= 4 {
		return fmt.Sprintf("avc1.%02x%02x%02x", t.SPS[1], t.SPS[2], t.SPS[3])
	}
	return "avc1.4d401f"
}

func first(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

func defaultFPS(f float64) float64 {
	if f > 0 {
		return f
	}
	return 25
}

func (h *mediaHandler) servePreviewManifest() {
	chunks := h.stream.Preview().Snapshot()
	segs := make([]map[string]any, 0, len(chunks))
	for _, c := range chunks {
		segs = append(segs, map[string]any{
			"n":        c.Seq + 1,
			"from":     c.From.UTC().Format(time.RFC3339),
			"duration": c.Duration.Seconds(),
			"size":     len(c.Data),
		})
	}
	target := 6.0
	if c := h.stream.Config(); c != nil {
		target = c.Duration()
	}
	writeJSON(h.w, http.StatusOK, map[string]any{
		"stream":   h.stream.Name(),
		"init":     "preview-init.mp4",
		"codecs":   h.stream.Codecs(),
		"segments": segs,
		"target":   target,
		"live":     h.stream.Status() == "online",
	})
}

func (h *mediaHandler) serveInit(ring *packager.Ring) {
	init := ring.Init()
	if len(init) == 0 {
		writeErr(h.w, http.StatusNotFound, "initialization segment is not ready")
		return
	}
	h.w.Header().Set("Content-Type", "video/mp4")
	h.w.Header().Set("Cache-Control", "public, max-age=60")
	h.w.Write(init)
}

func (h *mediaHandler) serveFragment(ring *packager.Ring, num string) {
	n, err := strconv.Atoi(num)
	if err != nil {
		writeErr(h.w, http.StatusBadRequest, "invalid segment number")
		return
	}
	c := ring.Get(n - 1)
	if c == nil {
		writeErr(h.w, http.StatusNotFound, "segment not found")
		return
	}
	h.w.Header().Set("Content-Type", "video/mp4")
	h.w.Header().Set("Cache-Control", "public, max-age=30")
	h.w.Write(c.Data)
}

func (h *mediaHandler) serveRanges() {
	arch := h.stream.Archive()
	if arch == nil {
		writeJSON(h.w, http.StatusOK, map[string]any{"enabled": false, "ranges": []any{}})
		return
	}
	writeJSON(h.w, http.StatusOK, map[string]any{
		"enabled": true,
		"ranges":  arch.Ranges(),
		"from":    arch.From().UTC().Format(time.RFC3339),
		"to":      arch.To().UTC().Format(time.RFC3339),
	})
}

func (h *mediaHandler) serveArchive() {
	arch := h.stream.Archive()
	if arch == nil {
		writeErr(h.w, http.StatusNotFound, "dvr is not enabled for this stream")
		return
	}
	spec := strings.TrimPrefix(h.file, "archive-")
	isPlaylist := strings.HasSuffix(spec, ".m3u8")
	isTS := strings.HasSuffix(spec, ".ts")
	spec = strings.TrimSuffix(strings.TrimSuffix(spec, ".m3u8"), ".ts")
	fromStr, durStr, ok := strings.Cut(spec, "-")
	if !ok {
		writeErr(h.w, http.StatusBadRequest, "expected archive-<from>-<duration>")
		return
	}
	from, err := parseTime(fromStr)
	if err != nil {
		writeErr(h.w, http.StatusBadRequest, "invalid archive start")
		return
	}
	durSec, err := strconv.ParseFloat(durStr, 64)
	if err != nil || durSec <= 0 {
		writeErr(h.w, http.StatusBadRequest, "invalid archive duration")
		return
	}
	dur := time.Duration(durSec * float64(time.Second))
	entries := arch.Select(from, dur)
	if len(entries) == 0 {
		writeErr(h.w, http.StatusNotFound, "no data in the requested range")
		return
	}
	if isPlaylist {
		var b strings.Builder
		b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-PLAYLIST-TYPE:VOD\n")
		maxDur := 0.0
		for _, e := range entries {
			if d := float64(e.Dur) / 1000; d > maxDur {
				maxDur = d
			}
		}
		fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n", int(maxDur)+1)
		for _, e := range entries {
			fmt.Fprintf(&b, "#EXTINF:%.3f,\ndvr-%d-%d.ts\n", float64(e.Dur)/1000, e.Start, e.Dur)
		}
		b.WriteString("#EXT-X-ENDLIST\n")
		h.w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		h.w.Header().Set("Cache-Control", "no-cache")
		h.w.Write([]byte(b.String()))
		return
	}
	if isTS {
		rc, _, err := arch.OpenRange(from, dur)
		if err != nil {
			writeErr(h.w, http.StatusNotFound, err.Error())
			return
		}
		defer rc.Close()
		h.w.Header().Set("Content-Type", "video/mp2t")
		h.w.Header().Set("Cache-Control", "public, max-age=86400")
		io.Copy(h.w, rc)
		return
	}
	writeErr(h.w, http.StatusNotFound, "unsupported archive format")
}

func (h *mediaHandler) serveDVRChunk(spec string) {
	arch := h.stream.Archive()
	if arch == nil {
		writeErr(h.w, http.StatusNotFound, "dvr is not enabled for this stream")
		return
	}
	fromStr, _, ok := strings.Cut(spec, "-")
	if !ok {
		writeErr(h.w, http.StatusBadRequest, "expected dvr-<from>-<duration>")
		return
	}
	from, err := parseTime(fromStr)
	if err != nil {
		writeErr(h.w, http.StatusBadRequest, "invalid dvr start")
		return
	}
	entries := arch.Select(from, time.Second)
	if len(entries) == 0 {
		writeErr(h.w, http.StatusNotFound, "segment not found")
		return
	}
	e := entries[0]
	rc, _, err := arch.OpenRange(e.From(), time.Duration(e.Dur)*time.Millisecond)
	if err != nil {
		writeErr(h.w, http.StatusNotFound, err.Error())
		return
	}
	defer rc.Close()
	h.w.Header().Set("Content-Type", "video/mp2t")
	h.w.Header().Set("Cache-Control", "public, max-age=86400")
	io.Copy(h.w, rc)
}

func (h *mediaHandler) serveTimeshift(spec, mode string) {
	arch := h.stream.Archive()
	if arch == nil {
		writeErr(h.w, http.StatusNotFound, "dvr is not enabled for this stream")
		return
	}
	spec = strings.TrimSuffix(spec, ".m3u8")
	var from time.Time
	switch mode {
	case "abs":
		t, err := parseTime(spec)
		if err != nil {
			writeErr(h.w, http.StatusBadRequest, "invalid timeshift start")
			return
		}
		from = t
	case "rel", "rewind":
		sec, err := strconv.ParseFloat(spec, 64)
		if err != nil {
			writeErr(h.w, http.StatusBadRequest, "invalid timeshift offset")
			return
		}
		from = time.Now().Add(-time.Duration(sec * float64(time.Second)))
	}
	to := time.Now()
	if arch.To().Before(to) {
		to = arch.To()
	}
	dur := to.Sub(from)
	if dur <= 0 {
		writeErr(h.w, http.StatusNotFound, "no data in the requested range")
		return
	}
	entries := arch.Select(from, dur)
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-PLAYLIST-TYPE:EVENT\n")
	maxDur := 0.0
	for _, e := range entries {
		if d := float64(e.Dur) / 1000; d > maxDur {
			maxDur = d
		}
	}
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n", int(maxDur)+1)
	for _, e := range entries {
		fmt.Fprintf(&b, "#EXTINF:%.3f,\ndvr-%d-%d.ts\n", float64(e.Dur)/1000, e.Start, e.Dur)
	}
	h.w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	h.w.Header().Set("Cache-Control", "no-cache")
	h.w.Write([]byte(b.String()))
}

func (h *mediaHandler) serveLiveSegment(file string) {
	chunks := h.stream.HLS().Snapshot()
	for _, c := range chunks {
		name := packager.SegmentName(c.From, c.Seq) + ".ts"
		if name == file {
			h.w.Header().Set("Content-Type", "video/mp2t")
			h.w.Header().Set("Cache-Control", "public, max-age=30")
			h.w.Write(c.Data)
			return
		}
	}
	writeErr(h.w, http.StatusNotFound, "segment not found")
}
