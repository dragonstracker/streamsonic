package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"streamsonic/internal/config"
)

func (s *Server) listStreams(w http.ResponseWriter, r *http.Request) {
	list := s.mgr.List()
	out := make([]map[string]any, 0, len(list))
	for _, st := range list {
		snap := st.Snapshot()
		snap["config"] = st.Config().APIView()
		out = append(out, snap)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getStream(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	st := s.mgr.Get(name)
	if st == nil {
		writeErr(w, http.StatusNotFound, "stream not found")
		return
	}
	snap := st.Snapshot()
	snap["config"] = st.Config().APIView()
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) createStream(w http.ResponseWriter, r *http.Request) {
	var st config.Stream
	if err := decodeBody(r, &st); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if st.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := s.store.Update(func(c *config.Config) error {
		if _, exists := c.Streams[st.Name]; exists {
			return errConflict
		}
		c.Streams[st.Name] = &st
		return nil
	}); err != nil {
		status := http.StatusBadRequest
		if err == errConflict {
			status = http.StatusConflict
		}
		writeErr(w, status, err.Error())
		return
	}
	s.apply()
	writeJSON(w, http.StatusCreated, map[string]any{"name": st.Name})
}

func (s *Server) putStream(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var st config.Stream
	if err := decodeBody(r, &st); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if st.Name != "" && st.Name != name {
		writeErr(w, http.StatusBadRequest, "name in body does not match the URL")
		return
	}
	st.Name = name
	if err := s.store.Update(func(c *config.Config) error {
		if _, exists := c.Streams[name]; !exists {
			return errNotFound
		}
		c.Streams[name] = &st
		return nil
	}); err != nil {
		status := http.StatusBadRequest
		if err == errNotFound {
			status = http.StatusNotFound
		}
		writeErr(w, status, err.Error())
		return
	}
	s.apply()
	writeJSON(w, http.StatusOK, map[string]any{"name": name})
}

func (s *Server) deleteStream(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.store.Update(func(c *config.Config) error {
		if _, exists := c.Streams[name]; !exists {
			return errNotFound
		}
		delete(c.Streams, name)
		return nil
	}); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	s.apply()
	writeJSON(w, http.StatusOK, map[string]any{"deleted": name})
}

func (s *Server) stopStream(w http.ResponseWriter, r *http.Request) {
	st := s.mgr.Get(r.PathValue("name"))
	if st == nil {
		writeErr(w, http.StatusNotFound, "stream not found")
		return
	}
	st.Stop()
	writeJSON(w, http.StatusOK, map[string]any{"status": "stopped"})
}

func (s *Server) startStream(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.mgr.Get(name) == nil {
		writeErr(w, http.StatusNotFound, "stream not found")
		return
	}
	s.mgr.Restart(name)
	writeJSON(w, http.StatusOK, map[string]any{"status": "started"})
}

func (s *Server) streamStats(w http.ResponseWriter, r *http.Request) {
	st := s.mgr.Get(r.PathValue("name"))
	if st == nil {
		writeErr(w, http.StatusNotFound, "stream not found")
		return
	}
	snap := st.Snapshot()
	snap["config"] = st.Config().APIView()
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) streamSessions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sessions := s.mgr.Sessions().List(name)
	out := make([]map[string]any, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, sess.View())
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) streamDVR(w http.ResponseWriter, r *http.Request) {
	st := s.mgr.Get(r.PathValue("name"))
	if st == nil {
		writeErr(w, http.StatusNotFound, "stream not found")
		return
	}
	arch := st.Archive()
	if arch == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	meta := arch.Metadata()
	meta["enabled"] = true
	meta["locks"] = arch.Locks()
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) dvrLock(w http.ResponseWriter, r *http.Request) {
	st := s.mgr.Get(r.PathValue("name"))
	if st == nil || st.Archive() == nil {
		writeErr(w, http.StatusNotFound, "dvr not enabled for this stream")
		return
	}
	var body struct {
		From     string  `json:"from"`
		Duration float64 `json:"duration"`
		Comment  string  `json:"comment"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	from, err := parseTime(body.From)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid from: "+err.Error())
		return
	}
	dur := time.Duration(body.Duration * float64(time.Second))
	if dur <= 0 {
		writeErr(w, http.StatusBadRequest, "duration must be positive")
		return
	}
	if err := st.Archive().Lock(from, dur, body.Comment); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"locked": true})
}

func (s *Server) dvrUnlock(w http.ResponseWriter, r *http.Request) {
	st := s.mgr.Get(r.PathValue("name"))
	if st == nil || st.Archive() == nil {
		writeErr(w, http.StatusNotFound, "dvr not enabled for this stream")
		return
	}
	from, err := parseTime(r.URL.Query().Get("from"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid from: "+err.Error())
		return
	}
	if err := st.Archive().Unlock(from); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"locked": false})
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	streamName := r.URL.Query().Get("stream")
	sessions := s.mgr.Sessions().List(streamName)
	out := make([]map[string]any, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, sess.View())
	}
	writeJSON(w, http.StatusOK, out)
}

func decodeBody(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return errEmptyBody
	}
	return json.Unmarshal(body, v)
}

func parseTime(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, errEmptyBody
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		if n > 1e12 {
			return time.UnixMilli(n), nil
		}
		return time.Unix(n, 0), nil
	}
	return time.Parse(time.RFC3339, v)
}
