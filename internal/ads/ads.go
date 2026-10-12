// Package ads shows the project's text ads: a small JSON feed kept in the
// GitHub repository (ads.json), read every few hours by every installation.
// Nothing is sent along: it is a plain GET of a public file.
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
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Sources are tried in turn: GitHub first, a CDN copy of the same file when
// GitHub is slow or filtered.
var Sources = []string{
	"https://raw.githubusercontent.com/neoauroraproject/Neo-Uptime/main/ads.json",
	"https://cdn.jsdelivr.net/gh/neoauroraproject/Neo-Uptime@main/ads.json",
}

// Placements where an ad can appear.
const (
	PlaceDashboard = "dashboard" // a card at the top of the dashboard
	PlaceSidebar   = "sidebar"   // a small card in the side menu
	PlaceInstall   = "install"   // the install and update scripts, in the terminal
)

// Ad is one text ad: a title, a line or two, and a button with a link.
type Ad struct {
	ID         string   `json:"id"`
	Placements []string `json:"placements,omitempty"` // empty: everywhere
	Lang       string   `json:"lang,omitempty"`       // fa | en; empty: any language
	Badge      string   `json:"badge,omitempty"`      // e.g. "Sponsored"; empty: the default
	Title      string   `json:"title"`
	Text       string   `json:"text,omitempty"`
	Button     string   `json:"button,omitempty"`
	URL        string   `json:"url,omitempty"`
	Color      string   `json:"color,omitempty"` // #rrggbb for the button and edge
	Start      string   `json:"start,omitempty"` // YYYY-MM-DD, first day shown
	End        string   `json:"end,omitempty"`   // YYYY-MM-DD, last day shown
	Weight     int      `json:"weight,omitempty"`
}

// Feed is the file as published.
type Feed struct {
	Ads []Ad `json:"ads"`
}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// check rejects ads that would not show well or safely.
func (a *Ad) check() error {
	a.ID, a.Title, a.Text = strings.TrimSpace(a.ID), strings.TrimSpace(a.Title), strings.TrimSpace(a.Text)
	a.Button, a.Badge, a.URL = strings.TrimSpace(a.Button), strings.TrimSpace(a.Badge), strings.TrimSpace(a.URL)
	switch {
	case a.ID == "" || len(a.ID) > 64:
		return errors.New("id is required (up to 64 characters)")
	case a.Title == "" || utf8.RuneCountInString(a.Title) > 90:
		return errors.New("title is required (up to 90 characters)")
	case utf8.RuneCountInString(a.Text) > 280:
		return errors.New("text is limited to 280 characters")
	case utf8.RuneCountInString(a.Button) > 30 || utf8.RuneCountInString(a.Badge) > 20:
		return errors.New("button text is limited to 30 characters, the badge to 20")
	case a.Color != "" && !colorRe.MatchString(a.Color):
		return errors.New("color must look like #22c55e")
	case a.Lang != "" && a.Lang != "fa" && a.Lang != "en":
		return errors.New(`lang is "fa", "en" or empty`)
	}
	if a.URL != "" {
		u, err := url.Parse(a.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "tg") || (u.Host == "" && u.Scheme != "tg") {
			return errors.New("url must be an http(s) or tg: link")
		}
	}
	for _, d := range []string{a.Start, a.End} {
		if d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return fmt.Errorf("date %q must look like 2026-10-31", d)
			}
		}
	}
	return nil
}

// Parse reads a feed, keeping the ads that pass the checks.
func Parse(body []byte) (*Feed, []error) {
	var f Feed
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, []error{err}
	}
	var errs []error
	kept := f.Ads[:0]
	for _, a := range f.Ads {
		if err := a.check(); err != nil {
			errs = append(errs, fmt.Errorf("ad %q: %w", a.ID, err))
			continue
		}
		kept = append(kept, a)
	}
	f.Ads = kept
	return &f, errs
}

// Live keeps the ads within their dates.
func (f *Feed) Live(now time.Time) []Ad {
	out := []Ad{}
	if f == nil {
		return out
	}
	day := now.UTC().Format("2006-01-02")
	for _, a := range f.Ads {
		if (a.Start == "" || a.Start <= day) && (a.End == "" || day <= a.End) {
			out = append(out, a)
		}
	}
	return out
}

// Pick chooses one ad for a placement, by weight; preferring the language
// given and falling back to ads for any language.
func Pick(list []Ad, placement, lang string) *Ad {
	var fit []Ad
	for _, a := range list {
		if len(a.Placements) > 0 && !slices.Contains(a.Placements, placement) {
			continue
		}
		if a.Lang == "" || a.Lang == lang {
			fit = append(fit, a)
		}
	}
	if len(fit) == 0 {
		return nil
	}
	total := 0
	for _, a := range fit {
		total += max(a.Weight, 1)
	}
	n := rand.IntN(total)
	for i := range fit {
		if n -= max(fit[i].Weight, 1); n < 0 {
			return &fit[i]
		}
	}
	return &fit[0]
}

// Fetch downloads the feed from the first source that answers.
func Fetch(ctx context.Context, client *http.Client, sources []string) (*Feed, []byte, error) {
	var last error
	for _, src := range sources {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
		if err != nil {
			last = err
			continue
		}
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

// Service keeps the feed fresh for the panel and remembers the last good
// copy, so ads show right after a restart and while offline.
type Service struct {
	client  *http.Client
	sources []string
	load    func() []byte
	save    func([]byte)

	mu   sync.RWMutex
	feed *Feed
}

// NewService prepares a service; load and save keep the cached copy.
func NewService(sources []string, load func() []byte, save func([]byte)) *Service {
	s := &Service{client: &http.Client{Timeout: 20 * time.Second}, sources: sources, load: load, save: save}
	if b := load(); len(b) > 0 {
		s.feed, _ = Parse(b)
	}
	return s
}

// Run refreshes the feed every few hours until ctx ends.
func (s *Service) Run(ctx context.Context) {
	wait := 15 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		fctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		f, body, err := Fetch(fctx, s.client, s.sources)
		cancel()
		if err != nil {
			wait = 30 * time.Minute
			continue
		}
		s.mu.Lock()
		s.feed = f
		s.mu.Unlock()
		s.save(body)
		wait = 3*time.Hour + time.Duration(rand.IntN(1800))*time.Second
	}
}

// Live returns the ads showing today.
func (s *Service) Live() []Ad {
	if s == nil {
		return []Ad{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.feed.Live(time.Now())
}
