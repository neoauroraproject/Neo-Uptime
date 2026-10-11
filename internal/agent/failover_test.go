package agent

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/neoauroraproject/Neo-Uptime/internal/agentproto"
)

// Reports go to the first address that answers, and stay there.
func TestSendFailsOver(t *testing.T) {
	hits := 0
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(agentproto.Ack{OK: true, Name: "edge"})
	}))
	defer ok.Close()
	// A port nobody listens on (a tunnel that is down) and a status page
	// domain answering 404.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := "http://" + l.Addr().String()
	l.Close()
	notFound := httptest.NewServer(http.NotFoundHandler())
	defer notFound.Close()

	a := New(Config{Server: dead + ", " + notFound.URL + "," + ok.URL + "/", Token: "na_x"})
	if got := a.Servers(); len(got) != 3 || got[2] != ok.URL {
		t.Fatalf("servers: %v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ack, err := a.Send(ctx, &agentproto.Report{})
	if err != nil || ack.Name != "edge" || a.Current() != ok.URL {
		t.Fatalf("send: %v %+v current=%s", err, ack, a.Current())
	}
	// The next report starts with the address that worked.
	if _, err := a.Send(ctx, &agentproto.Report{}); err != nil || hits != 2 {
		t.Fatalf("second send: %v hits=%d", err, hits)
	}

	// A refused token is final: other addresses are the same server.
	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer refused.Close()
	b := New(Config{Server: refused.URL + "," + ok.URL, Token: "na_x"})
	if _, err := b.Send(ctx, &agentproto.Report{}); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("refused token should stop: %v", err)
	}

	// Everything down: one error naming each address.
	c := New(Config{Server: dead + "," + notFound.URL, Token: "na_x"})
	if _, err := c.Send(ctx, &agentproto.Report{}); err == nil || !strings.Contains(err.Error(), dead) || !strings.Contains(err.Error(), "404") {
		t.Fatalf("all down: %v", err)
	}
}
