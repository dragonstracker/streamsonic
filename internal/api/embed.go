package api

import (
	"encoding/json"
	"net/http"

	"streamsonic/internal/webui"
)

type embedConfig struct {
	StreamName        string            `json:"streamName"`
	StreamingPrefix   *string           `json:"streamingPrefix"`
	HasPreview        bool              `json:"hasPreview"`
	Poster            bool              `json:"poster"`
	Proto             string            `json:"proto"`
	AuthTokenName     string            `json:"authTokenName"`
	ThumbnailsEnabled bool              `json:"thumbnails_enabled"`
	Type              string            `json:"type"`
	DVRInfo           any               `json:"dvr_info"`
	Protocols         map[string]bool   `json:"protocols"`
	Extra             map[string]string `json:"-"`
}

func (s *Server) serveEmbed(w http.ResponseWriter, r *http.Request, name string) {
	st := s.mgr.Get(name)
	if st == nil {
		writeErr(w, http.StatusNotFound, "stream not found")
		return
	}
	cfg := embedConfig{
		StreamName:    name,
		Proto:         "hls",
		AuthTokenName: "token",
		Type:          "live",
		Protocols: map[string]bool{
			"hls":    true,
			"dash":   true,
			"webrtc": false,
			"mse":    false,
			"mss":    false,
			"cmaf":   false,
		},
	}
	if arch := st.Archive(); arch != nil {
		cfg.HasPreview = true
		cfg.DVRInfo = map[string]any{
			"from": arch.From().UnixMilli(),
			"to":   arch.To().UnixMilli(),
		}
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	webui.WriteEmbed(w, body)
}
