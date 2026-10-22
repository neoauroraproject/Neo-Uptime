package radar

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

// Reads the real radar once (NEO_UPTIME_RADAR_LIVE=1); shows the summary.
func TestLiveRadar(t *testing.T) {
	if os.Getenv("NEO_UPTIME_RADAR_LIVE") == "" {
		t.Skip("set NEO_UPTIME_RADAR_LIVE=1 to read radar.chabokan.net")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	snap, err := Fetch(ctx, http.DefaultClient, SourceURL(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(snap, "", " ")
	t.Logf("%d bytes summary:\n%s", len(b), b)
}
