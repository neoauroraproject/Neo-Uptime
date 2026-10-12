package ads

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRepositoryFeedIsValid(t *testing.T) {
	body, err := os.ReadFile("../../ads.json")
	if err != nil {
		t.Fatal(err)
	}
	f, errs := Parse(body)
	if f == nil || len(errs) > 0 || len(f.Ads) == 0 {
		t.Fatalf("ads.json: %v %v", f, errs)
	}
}

func TestFeed(t *testing.T) {
	f, errs := Parse([]byte(`{"ads":[
		{"id":"a","title":"Fa only","lang":"fa","placements":["dashboard"],"url":"https://example.com"},
		{"id":"b","title":"Any language","weight":3},
		{"id":"old","title":"Over","end":"2020-01-01"},
		{"id":"later","title":"Not yet","start":"2999-01-01"},
		{"id":"bad-url","title":"x","url":"javascript:alert(1)"},
		{"id":"bad-color","title":"x","color":"red"},
		{"title":"no id"}
	]}`))
	if len(errs) != 3 || len(f.Ads) != 4 {
		t.Fatalf("parse: %d ads, errors %v", len(f.Ads), errs)
	}
	live := f.Live(time.Now())
	if len(live) != 2 {
		t.Fatalf("live: %v", live)
	}
	if a := Pick(live, PlaceSidebar, "fa"); a == nil || a.ID != "b" {
		t.Fatalf("sidebar in Persian should get the any-language ad: %v", a)
	}
	seen := map[string]bool{}
	for range 200 {
		seen[Pick(live, PlaceDashboard, "fa").ID] = true
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("both ads should come up: %v", seen)
	}
	if a := Pick(live, PlaceDashboard, "en"); a == nil || a.ID != "b" {
		t.Fatalf("English dashboard: %v", a)
	}
}

func TestFetchFallsBack(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ads":[{"id":"x","title":"Hello","text":"World","button":"Go","url":"https://example.com","placements":["install"]}]}`))
	}))
	defer good.Close()
	f, _, err := Fetch(context.Background(), good.Client(), []string{"http://127.0.0.1:1/ads.json", good.URL})
	if err != nil || len(f.Ads) != 1 {
		t.Fatalf("fetch: %v %v", f, err)
	}
	t.Setenv("NEO_UPTIME_ADS_URL", good.URL)
	var out bytes.Buffer
	Print(&out, "fa", false)
	if s := out.String(); !strings.Contains(s, "Hello") || !strings.Contains(s, "Go: https://example.com") {
		t.Fatalf("terminal ad: %q", s)
	}
}
