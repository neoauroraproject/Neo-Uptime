package api

import (
	"context"
	"net/http"

	"github.com/neoauroraproject/Neo-Uptime/internal/ads"
)

// StartAds begins reading the project's ad feed. The last good copy is kept
// in the settings table, so ads show at once after a restart.
func (s *Server) StartAds(ctx context.Context) {
	svc := ads.NewService(ads.SourcesFromEnv(),
		func() []byte { return []byte(s.db.GetSetting(ctx, "ads.feed", "")) },
		func(b []byte) { _ = s.db.SetSettings(context.Background(), map[string]string{"ads.feed": string(b)}) })
	s.adsMu.Lock()
	s.ads = svc
	s.adsMu.Unlock()
	go svc.Run(ctx)
}

// handleAds lists the ads showing today; the page picks one per place.
func (s *Server) handleAds(w http.ResponseWriter, r *http.Request) {
	s.adsMu.Lock()
	svc := s.ads
	s.adsMu.Unlock()
	writeJSON(w, map[string]any{"ads": svc.Live()})
}
