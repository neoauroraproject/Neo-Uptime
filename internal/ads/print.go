package ads

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Print writes an ad for a terminal, as the install and update scripts show
// it. It gives up after a few seconds and prints nothing when there is no ad.
func Print(w io.Writer, color bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	f, _, err := Fetch(ctx, &http.Client{}, SourcesFromEnv())
	if err != nil {
		return
	}
	a := Pick(f.Ads)
	if a == nil {
		return
	}
	bold, dim, accent, reset := "", "", "", ""
	if color {
		bold, dim, accent, reset = "\033[1m", "\033[2m", "\033[32m", "\033[0m"
	}
	badge := a.Badge
	if badge == "" {
		badge = "Sponsored"
	}
	rule := strings.Repeat("─", 56)
	fmt.Fprintf(w, "\n  %s── %s %s%s\n", dim, badge, rule[:max(0, len(rule)-3*(utf8.RuneCountInString(badge)+4))], reset)
	fmt.Fprintf(w, "  %s%s%s\n", bold, a.Title, reset)
	for _, line := range wrap(a.Text, 64) {
		fmt.Fprintf(w, "  %s\n", line)
	}
	if a.URL != "" {
		label := a.Button
		if label == "" {
			label = "More"
		}
		fmt.Fprintf(w, "  %s→ %s:%s %s\n", accent, label, reset, a.URL)
	}
	fmt.Fprintf(w, "  %s%s%s\n\n", dim, rule, reset)
}

func wrap(s string, width int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			if line != "" && utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) > width {
				out = append(out, line)
				line = ""
			}
			if line != "" {
				line += " "
			}
			line += word
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
