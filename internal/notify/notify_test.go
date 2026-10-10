package notify

import (
	"strings"
	"testing"
	"time"
)

func TestPersianMessages(t *testing.T) {
	p := 42.0
	e := Event{Kind: "status", MonitorName: "NL", Status: StatusUp, Ping: &p, Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Downtime: 90 * time.Minute, Lang: "fa", TZ: "Asia/Tehran"}
	if !strings.Contains(e.Title(), "دوباره در دسترس است") {
		t.Errorf("title: %s", e.Title())
	}
	text := e.Text()
	for _, want := range []string{"زمان پاسخ", "۴۲", "مدت قطعی", "۱ ساعت و ۳۰ دقیقه", "Asia/Tehran"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	e.Lang = ""
	if !strings.Contains(e.Title(), "is UP") {
		t.Errorf("english title: %s", e.Title())
	}
}

func TestEveryProviderHasLanguage(t *testing.T) {
	for _, p := range Providers() {
		found := false
		for _, f := range p.Fields {
			found = found || f.Key == "language"
		}
		if !found {
			t.Errorf("%s has no language field", p.Type)
		}
	}
}
