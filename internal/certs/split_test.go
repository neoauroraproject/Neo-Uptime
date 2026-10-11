package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"
)

func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "status.test"}, DNSNames: []string{"status.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// The panel on port 443 and automatic HTTPS share one listener.
func TestSplitterSharesOnePort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sp := NewSplitter(ln)
	defer ln.Close()

	plain := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "plain") })}
	go func() { _ = plain.Serve(sp.Plain()) }()
	defer plain.Close()

	get := func(url string, c *http.Client) (string, error) {
		resp, err := c.Get(url)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b), nil
	}
	addr := ln.Addr().String()
	tlsClient := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, ServerName: "status.test"}}}

	// HTTPS off: plain HTTP works, TLS is refused instead of hanging.
	if body, err := get("http://"+addr+"/", &http.Client{Timeout: 3 * time.Second}); err != nil || body != "plain" {
		t.Fatalf("plain before TLS: %q %v", body, err)
	}
	if _, err := get("https://"+addr+"/", tlsClient); err == nil {
		t.Fatal("TLS connection accepted while HTTPS is off")
	}

	// HTTPS on: both on the same port.
	cert := selfSigned(t)
	secure := &http.Server{
		Handler:   http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "tls:"+r.TLS.ServerName) }),
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
	}
	go func() { _ = secure.ServeTLS(sp.TLS(), "", "") }()
	if body, err := get("https://"+addr+"/", tlsClient); err != nil || body != "tls:status.test" {
		t.Fatalf("TLS on the shared port: %q %v", body, err)
	}
	if body, err := get("http://"+addr+"/", &http.Client{Timeout: 3 * time.Second}); err != nil || body != "plain" {
		t.Fatalf("plain next to TLS: %q %v", body, err)
	}

	// HTTPS restarted: a new TLS listener takes over.
	_ = secure.Close()
	secure2 := &http.Server{
		Handler:   http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "again") }),
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
	}
	go func() { _ = secure2.ServeTLS(sp.TLS(), "", "") }()
	defer secure2.Close()
	tlsClient.CloseIdleConnections()
	if body, err := get("https://"+addr+"/", tlsClient); err != nil || body != "again" {
		t.Fatalf("TLS after a restart: %q %v", body, err)
	}
}
