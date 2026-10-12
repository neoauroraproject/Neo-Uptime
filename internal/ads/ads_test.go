package ads

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// As the ad service answers (see license.hossein-m466.workers.dev/v1/ads).
const sample = `{"product":"neo-uptime","mode":"rotate","ads":[
	{"id":"ad_1","title":"تبلیغات","body":"برای رزرو تبلیغات اینجا پیام دهید","buttonText":"ثبت تبلیغ","linkUrl":"https://t.me/example","badge":"Ads","isDefault":true},
	{"id":"ad_2","title":"Fast servers","body":"","buttonText":"","linkUrl":"","badge":""},
	{"id":"ad_bad","title":"x","linkUrl":"javascript:alert(1)"},
	{"id":"","title":"no id"}
],"ad":{"id":"ad_1","title":"تبلیغات"},"fallback":true,"poolSize":0}`

func TestParse(t *testing.T) {
	f, errs := Parse([]byte(sample))
	if len(f.Ads) != 2 || len(errs) != 2 {
		t.Fatalf("parse: %+v %v", f.Ads, errs)
	}
	a := f.Ads[0]
	if a.Title != "تبلیغات" || a.Text == "" || a.Button != "ثبت تبلیغ" || a.URL != "https://t.me/example" || a.Badge != "Ads" {
		t.Fatalf("mapping: %+v", a)
	}
	// Only "ad" given: that one.
	f, _ = Parse([]byte(`{"ad":{"id":"ad_9","title":"One"}}`))
	if len(f.Ads) != 1 || f.Ads[0].ID != "ad_9" {
		t.Fatalf("single ad: %+v", f.Ads)
	}
	if f, _ := Parse([]byte(`{"ads":[]}`)); len(f.Ads) != 0 || Pick(f.Ads) != nil {
		t.Fatal("no ads, no pick")
	}
}

func TestFetchAndPrint(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"ads":[{"id":"x","title":"Hello","body":"World","buttonText":"Go","linkUrl":"https://example.com"}]}`))
	}))
	defer srv.Close()
	f, _, err := Fetch(context.Background(), srv.Client(), []string{"http://127.0.0.1:1/v1/ads", srv.URL + "/v1/ads?product=" + Product})
	if err != nil || len(f.Ads) != 1 || asked != "product=neo-uptime" {
		t.Fatalf("fetch: %v %v %q", f, err, asked)
	}
	t.Setenv("NEO_UPTIME_ADS_URL", srv.URL+"/v1/ads?product=neo-uptime")
	var out bytes.Buffer
	Print(&out, false)
	if s := out.String(); !strings.Contains(s, "Hello") || !strings.Contains(s, "Go: https://example.com") {
		t.Fatalf("terminal ad: %q", s)
	}
}

func TestDefaultSourceNamesTheProduct(t *testing.T) {
	if !strings.HasSuffix(Sources[0], "/v1/ads?product=neo-uptime") {
		t.Fatal(Sources[0])
	}
}
