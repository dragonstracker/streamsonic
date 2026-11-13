package api

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
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
}

func NewServer(store *Store, mgr *stream.Manager, version string) *Server {
	return &Server{store: store, mgr: mgr, version: version, started: time.Now()}
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
	mux.HandleFunc("PUT "+APIPrefix+"/streams/{name}/start", s.auth(s.startStream))
	mux.HandleFunc("GET "+APIPrefix+"/streams/{name}/stats", s.auth(s.streamStats))
	mux.HandleFunc("GET "+APIPrefix+"/streams/{name}/sessions", s.auth(s.streamSessions))
	mux.HandleFunc("GET "+APIPrefix+"/streams/{name}/dvr", s.auth(s.streamDVR))
	mux.HandleFunc("POST "+APIPrefix+"/streams/{name}/dvr/lock", s.auth(s.dvrLock))
	mux.HandleFunc("DELETE "+APIPrefix+"/streams/{name}/dvr/lock", s.auth(s.dvrUnlock))

	mux.HandleFunc("GET "+APIPrefix+"/sessions", s.auth(s.listSessions))
	mux.HandleFunc("GET "+APIPrefix+"/server/stats", s.auth(s.serverStats))
	mux.HandleFunc("GET "+APIPrefix+"/ui_settings", s.uiSettings)
	mux.HandleFunc("GET "+APIPrefix+"/monitoring/liveness", s.liveness)
	mux.HandleFunc("GET "+APIPrefix+"/monitoring/readiness", s.readiness)
	mux.HandleFunc("GET "+APIPrefix+"/version", s.versionHandler)

	mux.HandleFunc("/stream/", s.handleMedia)
	mux.Handle("/", s.uiHandler())
	return mux
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
		w.Header().Set("WWW-Authenticate", `Basic realm="streamsonic"`)
		writeErr(w, http.StatusUnauthorized, "unauthorized")
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

// bearerCredentials decodes the Flussonic admin convention:
// Authorization: Bearer base64(login:password)
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
	enc.SetIndent("", "  ")
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
