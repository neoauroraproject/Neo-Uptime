// Package ads shows Neo Aurora's text ads, served by its ad service for
// each product (this one is "neo-uptime"). Nothing is sent along but the
// product's name: it is a plain GET.
package ads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Product is how this program is known to the ad service.
const Product = "neo-uptime"

// Sources are tried in turn.
var Sources = []string{
	"https://license.hossein-m466.workers.dev/v1/ads?product=" + Product,
}

// Ad is one text ad: a title, a line or two, and a button with a link.
type Ad struct {
	ID     string `json:"id"`
	Badge  string `json:"badge,omitempty"`
	Title  string `json:"title"`
	Text   string `json:"text,omitempty"`
	Button string `json:"button,omitempty"`
	URL    string `json:"url,omitempty"`
}

// apiAd is an ad as the service sends it.
type apiAd struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	ButtonText string `json:"buttonText"`
	LinkURL    string `json:"linkUrl"`
	Badge      string `json:"badge"`
}

// Feed is the answer of the ad service.
type Feed struct {
	Ads []Ad `json:"ads"`
}

// check rejects ads that would not show well or safely.
func (a *Ad) check() error {
	a.ID, a.Title, a.Text = strings.TrimSpace(a.ID), strings.TrimSpace(a.Title), strings.TrimSpace(a.Text)
	a.Button, a.Badge, a.URL = strings.TrimSpace(a.Button), strings.TrimSpace(a.Badge), strings.TrimSpace(a.URL)
	switch {
	case a.ID == "" || len(a.ID) > 100:
		return errors.New("id is required")
	case a.Title == "" || utf8.RuneCountInString(a.Title) > 120:
		return errors.New("title is required (up to 120 characters)")
	case utf8.RuneCountInString(a.Text) > 400:
		return errors.New("text is limited to 400 characters")
	case utf8.RuneCountInString(a.Button) > 40 || utf8.RuneCountInString(a.Badge) > 24:
		return errors.New("button text is limited to 40 characters, the badge to 24")
	}
	if a.URL != "" {
		u, err := url.Parse(a.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "tg") || (u.Host == "" && u.Scheme != "tg") {
			return errors.New("the link must be an http(s) or tg: link")
		}
	}
	return nil
}

// Parse reads the ad service's answer, keeping the ads that pass the checks.
func Parse(body []byte) (*Feed, []error) {
	var in struct {
		Ads []apiAd `json:"ads"`
		Ad  *apiAd  `json:"ad"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, []error{err}
	}
	list := in.Ads
	if len(list) == 0 && in.Ad != nil {
		list = []apiAd{*in.Ad}
	}
	f := &Feed{Ads: []Ad{}}
	var errs []error
	for _, x := range list {
		a := Ad{ID: x.ID, Badge: x.Badge, Title: x.Title, Text: x.Body, Button: x.ButtonText, URL: x.LinkURL}
		if err := a.check(); err != nil {
			errs = append(errs, fmt.Errorf("ad %q: %w", x.ID, err))
			continue
		}
		f.Ads = append(f.Ads, a)
	}
	return f, errs
}

// Pick chooses one ad at random, or nil when there is none.
func Pick(list []Ad) *Ad {
	if len(list) == 0 {
		return nil
	}
	return &list[rand.IntN(len(list))]
}

// Fetch asks the ad service, trying each source in turn.
func Fetch(ctx context.Context, client *http.Client, sources []string) (*Feed, []byte, error) {
	var last error
	for _, src := range sources {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(src), nil)
		if err != nil {
			last = err
			continue
		}
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			last = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			last = fmt.Errorf("%s: HTTP %d", src, resp.StatusCode)
			continue
		}
		f, errs := Parse(body)
		if f == nil {
			last = fmt.Errorf("%s: %v", src, errs[0])
			continue
		}
		return f, body, nil
	}
	if last == nil {
		last = errors.New("no ad sources")
	}
	return nil, nil, last
}

// SourcesFromEnv returns NEO_UPTIME_ADS_URL (comma-separated) when set.
func SourcesFromEnv() []string {
	if v := strings.TrimSpace(os.Getenv("NEO_UPTIME_ADS_URL")); v != "" {
		return strings.Split(v, ",")
	}
	return Sources
}

// Service keeps the ads fresh for the panel and remembers the last good
// answer, so ads show right after a restart and while the service is out of
// reach.
type Service struct {
	client  *http.Client
	sources []string
	save    func([]byte)

	mu   sync.RWMutex
	feed *Feed
}

// NewService prepares a service; load and save keep the cached answer.
func NewService(sources []string, load func() []byte, save func([]byte)) *Service {
	s := &Service{client: &http.Client{Timeout: 20 * time.Second}, sources: sources, save: save}
	if b := load(); len(b) > 0 {
		s.feed, _ = Parse(b)
	}
	return s
}

// Run asks for the ads every half hour until ctx ends.
func (s *Service) Run(ctx context.Context) {
	wait := 5 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		fctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		f, body, err := Fetch(fctx, s.client, s.sources)
		cancel()
		if err != nil {
			wait = 10 * time.Minute
			continue
		}
		s.mu.Lock()
		s.feed = f
		s.mu.Unlock()
		s.save(body)
		wait = 30*time.Minute + time.Duration(rand.IntN(300))*time.Second
	}
}

// Live returns the current ads.
func (s *Service) Live() []Ad {
	if s == nil {
		return []Ad{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.feed == nil {
		return []Ad{}
	}
	return s.feed.Ads
}
