package api

import (
	"context"
	"net/http"

	"github.com/neoauroraproject/Neo-Uptime/internal/radar"
)

// StartRadar begins reading the internet radar while it is turned on under
// Settings. Only a small summary is kept, in memory.
func (s *Server) StartRadar(ctx context.Context) {
	svc := radar.New(func() bool { return s.db.GetSetting(context.Background(), "radarEnabled", "false") == "true" })
	s.radar = svc
	go svc.Run(ctx)
}

// handleRadar is the radar as the panel last read it, for Settings and the
// status page editor.
func (s *Server) handleRadar(w http.ResponseWriter, r *http.Request) {
	on := s.db.GetSetting(r.Context(), "radarEnabled", "false") == "true"
	writeJSON(w, map[string]any{"enabled": on, "every": int(radar.Every.Seconds()), "snapshot": s.radar.Snapshot()})
}
