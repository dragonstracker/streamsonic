package api

import (
	"net/http"

	"streamsonic/internal/webui"
)

func (s *Server) uiHandler() http.Handler { return webui.Handler() }
