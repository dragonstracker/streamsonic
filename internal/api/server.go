package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"streamsonic/internal/stream"
)

const APIPrefix = "/streamer/api/v3"

type Server struct {
	store   *Store
	mgr     *stream.Manager
	version string
	started time.Time
	id      string
}

func NewServer(store *Store, mgr *stream.Manager, version string) *Server {
	var b [16]byte
	_, _ = rand.Read(b[:])
	id := hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" + hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:])
	return &Server{store: store, mgr: mgr, version: version, started: time.Now(), id: id}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET "+APIPrefix+"/config", s.auth(s.getConfig))
	mux.HandleFunc("PUT "+APIPrefix+"/config", s.auth(s.putConfig))
	mux.HandleFunc("GET "+APIPrefix+"/config/text", s.auth(s.getConfigText))

	mux.HandleFunc("GET "+APIPrefix+"/streams", s.auth(s.listStreams))
	mux.HandleFunc("POST "+APIPrefix+"/streams", s.auth(s.createStream))
	mux.HandleFunc("GET "+APIPrefix+"/streams/{name}", s.auth(s.getStream))
	mux.HandleFunc("PUT "+APIPrefix+"/streams/{name}", s.auth(s.putStream))
	mux.HandleFunc("DELETE "+APIPrefix+"/streams/{name}", s.auth(s.deleteStream))
	mux.HandleFunc("PUT "+APIPrefix+"/streams/{name}/stop", s.auth(s.stopStream))
	mux.HandleFunc("DELETE "+APIPrefix+"/streams/{name}/stop", s.auth(s.stopStream))
	mux.HandleFunc("PUT "+APIPrefix+"/streams/{name}/start", s.auth(s.startStream))
	mux.HandleFunc("POST "+APIPrefix+"/streams/{name}/inputs/{index}/select", s.auth(s.selectInput))
	mux.HandleFunc("GET "+APIPrefix+"/streams/{name}/stats", s.auth(s.streamStats))
	mux.HandleFunc("GET "+APIPrefix+"/streams/{name}/sessions", s.auth(s.streamSessions))
	mux.HandleFunc("PUT "+APIPrefix+"/streams/{name}/sessions", s.auth(s.putPlayStats))
	mux.HandleFunc("GET "+APIPrefix+"/streams/{name}/dvr", s.auth(s.streamDVR))
	mux.HandleFunc("POST "+APIPrefix+"/streams/{name}/dvr/lock", s.auth(s.dvrLock))
	mux.HandleFunc("DELETE "+APIPrefix+"/streams/{name}/dvr/lock", s.auth(s.dvrUnlock))
	mux.HandleFunc(APIPrefix+"/", s.auth(s.apiNotFound))

	mux.HandleFunc("PUT /{name}/sessions", s.putPlayStats)
	mux.HandleFunc("GET /{name}/sessions", s.streamSessionsByPath)

	mux.HandleFunc("GET "+APIPrefix+"/sessions", s.auth(s.listSessions))
	mux.HandleFunc("GET "+APIPrefix+"/server/stats", s.auth(s.serverStats))
	mux.HandleFunc("GET "+APIPrefix+"/ui_settings", s.uiSettings)
	mux.HandleFunc("GET "+APIPrefix+"/templates", s.auth(s.emptyCollection("templates")))
	mux.HandleFunc("GET "+APIPrefix+"/vods", s.auth(s.emptyCollection("vods")))
	mux.HandleFunc("GET "+APIPrefix+"/multiplexers", s.auth(s.emptyCollection("multiplexers")))
	mux.HandleFunc("GET "+APIPrefix+"/dvrs", s.auth(s.emptyCollection("dvrs")))
	mux.HandleFunc("GET "+APIPrefix+"/transponders", s.auth(s.emptyCollection("transponders")))
	mux.HandleFunc("GET "+APIPrefix+"/dvb_cards", s.auth(s.emptyCollection("dvb_cards")))
	mux.HandleFunc("GET "+APIPrefix+"/auth_backends", s.auth(s.emptyCollection("auth_backends")))
	mux.HandleFunc("GET "+APIPrefix+"/event_sinks", s.auth(s.emptyCollection("event_sinks")))
	mux.HandleFunc("GET "+APIPrefix+"/cluster/peers", s.auth(s.emptyCollection("peers")))
	mux.HandleFunc("GET "+APIPrefix+"/cluster/sources", s.auth(s.emptyCollection("sources")))
	mux.HandleFunc("GET "+APIPrefix+"/system/updater", s.auth(s.systemUpdater))
	mux.HandleFunc("GET "+APIPrefix+"/admin_view_token", s.auth(s.adminViewToken))
	mux.HandleFunc("PUT "+APIPrefix+"/admin_session_save/{session_id}", s.auth(s.adminSessionSave))
	mux.HandleFunc("DELETE "+APIPrefix+"/admin_view_token", s.auth(s.adminViewToken))
	mux.HandleFunc("GET "+APIPrefix+"/license", s.auth(s.licenseInfo))
	mux.HandleFunc("GET "+APIPrefix+"/license/activations", s.auth(s.licenseInfo))
	mux.HandleFunc("GET "+APIPrefix+"/monitoring/liveness", s.liveness)
	mux.HandleFunc("GET "+APIPrefix+"/monitoring/readiness", s.readiness)
	mux.HandleFunc("GET "+APIPrefix+"/version", s.versionHandler)

	mux.HandleFunc("/", s.handleRoot)
	return withCORS(withLog(withGzip(mux)))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func withLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusWriter{ResponseWriter: w}
		started := time.Now()
		next.ServeHTTP(rec, r)
		if rec.status >= http.StatusBadRequest {
			log.Printf("%s %s %d %s", r.Method, r.URL.RequestURI(), rec.status, time.Since(started).Round(time.Millisecond))
		}
	})
}

func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := s.store.Current()
		if cfg.AdminLogin == "" {
			h(w, r)
			return
		}
		if s.checkCredentials(r, cfg.AdminLogin, cfg.AdminPassword) {
			h(w, r)
			return
		}
		if login, password, ok := bearerCredentials(r); ok {
			if subtle.ConstantTimeCompare([]byte(login), []byte(cfg.AdminLogin)) == 1 &&
				subtle.ConstantTimeCompare([]byte(password), []byte(cfg.AdminPassword)) == 1 {
				h(w, r)
				return
			}
		}
		if tok := tokenOf(r); tok != "" {
			if subtle.ConstantTimeCompare([]byte(tok), []byte(cfg.AdminPassword)) == 1 {
				h(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "authorization_failed"})
	}
}

func (s *Server) checkCredentials(r *http.Request, login, password string) bool {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(user), []byte(login)) == 1 &&
		subtle.ConstantTimeCompare([]byte(pass), []byte(password)) == 1
}

func tokenOf(r *http.Request) string {
	if v := r.URL.Query().Get("token"); v != "" {
		return v
	}
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return ""
}

func bearerCredentials(r *http.Request) (string, string, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return "", "", false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
	if err != nil {
		return "", "", false
	}
	login, password, ok := strings.Cut(string(raw), ":")
	if !ok {
		return "", "", false
	}
	return login, password, true
}

func (s *Server) apply() {
	cfg := s.store.Current()
	s.mgr.Apply(cfg)
	s.mgr.Events().Update(cfg.Sinks)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg, "status": status})
}

func writeText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(body))
}

func wantsPlainText(r *http.Request) bool {
	accept := strings.ToLower(r.Header.Get("Accept"))
	if accept == "" {
		return false
	}
	if strings.Contains(accept, "text/plain") {
		return true
	}
	return false
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Access-Control-Expose-Headers", "*")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
