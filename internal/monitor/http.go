package monitor

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/store"
	"github.com/neoauroraproject/Neo-Uptime/internal/version"
)

func checkHTTP(ctx context.Context, m *store.Monitor, _ *Env) Result {
	c := m.Config
	if c.URL == "" {
		return down("no URL configured")
	}
	method := strings.ToUpper(strings.TrimSpace(c.Method))
	if method == "" {
		method = http.MethodGet
	}

	var body io.Reader
	if c.Body != "" && method != http.MethodGet && method != http.MethodHead {
		body = strings.NewReader(c.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL, body)
	if err != nil {
		return down("invalid request: " + err.Error())
	}
	req.Header.Set("User-Agent", "NeoUptime/"+version.Version)
	req.Header.Set("Accept", "*/*")
	if body != nil {
		switch c.BodyEncoding {
		case "form":
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		case "xml":
			req.Header.Set("Content-Type", "text/xml; charset=utf-8")
		case "raw":
			req.Header.Set("Content-Type", "text/plain; charset=utf-8")
		default:
			req.Header.Set("Content-Type", "application/json")
		}
	}
	if strings.TrimSpace(c.Headers) != "" {
		var hs map[string]any
		if err := json.Unmarshal([]byte(c.Headers), &hs); err != nil {
			return down("headers are not a JSON object: " + err.Error())
		}
		for k, v := range hs {
			req.Header.Set(k, fmt.Sprint(v))
		}
	}
	if c.BasicAuthUser != "" || c.BasicAuthPass != "" {
		req.SetBasicAuth(c.BasicAuthUser, c.BasicAuthPass)
	}

	maxRedirects := 10
	if c.MaxRedirects != nil {
		maxRedirects = *c.MaxRedirects
	}
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
		TLSHandshakeTimeout: 15 * time.Second,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: c.IgnoreTLS}, //nolint:gosec // explicit per-monitor option
		DisableKeepAlives:   true,
		ForceAttemptHTTP2:   true,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{
		Transport: tr,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return nil
		},
	}
	if maxRedirects == 0 {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return down(shortNetErr(err))
	}
	defer resp.Body.Close()

	needBody := m.Type == TypeKeyword || m.Type == TypeJSONQuery
	var data []byte
	if needBody {
		data, err = io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		if err != nil {
			return down("reading body: " + shortNetErr(err))
		}
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	}
	elapsed := time.Since(start)

	var cert *CertInfo
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		pc := resp.TLS.PeerCertificates[0]
		cert = &CertInfo{
			Subject:       pc.Subject.CommonName,
			Issuer:        firstNonEmpty(pc.Issuer.CommonName, strings.Join(pc.Issuer.Organization, ", ")),
			ValidFrom:     pc.NotBefore.Unix(),
			ValidTo:       pc.NotAfter.Unix(),
			DaysRemaining: int(time.Until(pc.NotAfter).Hours() / 24),
			Valid:         time.Now().Before(pc.NotAfter) && time.Now().After(pc.NotBefore),
			DNSNames:      pc.DNSNames,
		}
	}

	statusLine := fmt.Sprintf("%d - %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	if !statusAccepted(resp.StatusCode, c.AcceptedStatusCodes) {
		r := down(statusLine)
		r.Cert = cert
		return r
	}

	var r Result
	switch m.Type {
	case TypeKeyword:
		found := strings.Contains(string(data), c.Keyword)
		if found != c.InvertKeyword {
			word := "found"
			if c.InvertKeyword {
				word = "not found"
			}
			r = up(fmt.Sprintf("%s, keyword %q %s", statusLine, c.Keyword, word), elapsed)
		} else {
			excerpt := strings.Join(strings.Fields(string(data)), " ")
			if len(excerpt) > 60 {
				excerpt = excerpt[:60] + "…"
			}
			if c.InvertKeyword {
				r = down(fmt.Sprintf("keyword %q is present: [%s]", c.Keyword, excerpt))
			} else {
				r = down(fmt.Sprintf("keyword %q not found: [%s]", c.Keyword, excerpt))
			}
		}
	case TypeJSONQuery:
		ok, got, err := evalJSONQuery(data, c.JSONPath, c.JSONOperator, c.ExpectedValue)
		switch {
		case err != nil:
			r = down(err.Error())
		case ok:
			r = up(fmt.Sprintf("%s, %s = %s", statusLine, c.JSONPath, got), elapsed)
		default:
			r = down(fmt.Sprintf("%s is %s, expected %s %s", c.JSONPath, got, opLabel(c.JSONOperator), c.ExpectedValue))
		}
	default:
		r = up(statusLine, elapsed)
	}
	r.Cert = cert
	return r
}

func statusAccepted(code int, accepted []string) bool {
	if len(accepted) == 0 {
		accepted = []string{"200-299"}
	}
	for _, a := range accepted {
		a = strings.TrimSpace(strings.ToLower(a))
		if len(a) == 3 && strings.HasSuffix(a, "xx") {
			if d, err := strconv.Atoi(a[:1]); err == nil && code/100 == d {
				return true
			}
			continue
		}
		if lo, hi, ok := strings.Cut(a, "-"); ok {
			l, e1 := strconv.Atoi(strings.TrimSpace(lo))
			h, e2 := strconv.Atoi(strings.TrimSpace(hi))
			if e1 == nil && e2 == nil && code >= l && code <= h {
				return true
			}
			continue
		}
		if n, err := strconv.Atoi(a); err == nil && n == code {
			return true
		}
	}
	return false
}

func shortNetErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout: no response in time"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout: " + err.Error()
	}
	return err.Error()
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
