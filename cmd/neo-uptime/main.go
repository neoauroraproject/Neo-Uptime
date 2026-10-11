// Command neo-uptime is a self-hosted uptime monitor with first-class
// BackPack tunnel monitoring.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
	_ "time/tzdata" // maintenance windows need zones even on minimal hosts

	"github.com/neoauroraproject/Neo-Uptime/internal/api"
	"github.com/neoauroraproject/Neo-Uptime/internal/auth"
	"github.com/neoauroraproject/Neo-Uptime/internal/backpack"
	"github.com/neoauroraproject/Neo-Uptime/internal/certs"
	"github.com/neoauroraproject/Neo-Uptime/internal/monitor"
	"github.com/neoauroraproject/Neo-Uptime/internal/store"
	"github.com/neoauroraproject/Neo-Uptime/internal/version"
	"github.com/neoauroraproject/Neo-Uptime/web"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func defaultDataDir() string {
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		return "/var/lib/neo-uptime"
	}
	return "data"
}

func usage() {
	fmt.Fprintf(os.Stderr, `Neo Uptime %s — uptime monitoring with BackPack tunnel support

Usage:
  neo-uptime [serve] [flags]          run the server (default)
  neo-uptime reset-password <user> [new-password]
  neo-uptime disable-2fa <user>
  neo-uptime list-users
  neo-uptime set-owner <user>          make an admin the owner (who alone issues status API keys)
  neo-uptime version

Flags (or environment):
  --host   NEO_UPTIME_HOST      listen address        (default 0.0.0.0)
  --port   NEO_UPTIME_PORT      listen port           (default 7788)
  --data   NEO_UPTIME_DATA      data directory        (default %s)
  --tls-cert NEO_UPTIME_TLS_CERT  certificate (PEM, with chain)
  --tls-key  NEO_UPTIME_TLS_KEY   private key (PEM)
`, version.Version, defaultDataDir())
}

func main() {
	log.SetFlags(log.LstdFlags)
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}

	fs := flag.NewFlagSet("neo-uptime", flag.ExitOnError)
	fs.Usage = usage
	host := fs.String("host", env("NEO_UPTIME_HOST", "0.0.0.0"), "listen address")
	port := fs.Int("port", atoi(env("NEO_UPTIME_PORT", "7788"), 7788), "listen port")
	data := fs.String("data", env("NEO_UPTIME_DATA", defaultDataDir()), "data directory")
	cert := fs.String("tls-cert", env("NEO_UPTIME_TLS_CERT", ""), "TLS certificate file")
	key := fs.String("tls-key", env("NEO_UPTIME_TLS_KEY", ""), "TLS key file")
	_ = fs.Parse(args)
	rest := fs.Args()

	switch cmd {
	case "serve":
		serve(*host, *port, *data, *cert, *key)
	case "version", "--version", "-v":
		fmt.Println("neo-uptime", version.Version, runtime.GOOS+"/"+runtime.GOARCH)
	case "reset-password":
		resetPassword(*data, rest)
	case "disable-2fa":
		disable2FA(*data, rest)
	case "list-users":
		listUsers(*data)
	case "set-owner":
		setOwner(*data, rest)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

func atoi(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func openDB(dataDir string) *store.DB {
	db, err := store.Open(filepath.Join(dataDir, "neo-uptime.db"))
	if err != nil {
		log.Fatalf("opening database in %s: %v", dataDir, err)
	}
	return db
}

func serve(host string, port int, dataDir, certFile, keyFile string) {
	db := openDB(dataDir)
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hub := api.NewHub()
	bp := backpack.NewCache()
	agents := monitor.NewAgentHub()
	mgr := monitor.NewManager(db, bp, agents, hub)
	if err := mgr.Start(ctx); err != nil {
		log.Fatalf("starting monitors: %v", err)
	}
	app := api.New(db, mgr, bp, agents, hub, web.FS(), dataDir)
	// Automatic HTTPS for the panel's and status pages' own domains. It stays
	// off until it is turned on under Settings → Domains & HTTPS.
	app.EnableHTTPS(ctx, certs.Config{
		HTTPAddr:  ":" + env("NEO_UPTIME_HTTP_PORT", "80"),
		HTTPSAddr: ":" + env("NEO_UPTIME_HTTPS_PORT", "443"),
		CacheDir:  filepath.Join(dataDir, "certs"),
		Directory: env("NEO_UPTIME_ACME_DIRECTORY", ""),
		CARoots:   env("NEO_UPTIME_ACME_CA_ROOTS", ""),
	})
	srv := &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:           app,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errc := make(chan error, 1)
	go func() {
		if certFile != "" && keyFile != "" {
			// Read the pair on every handshake so certbot renewals apply
			// without a restart.
			srv.TLSConfig = &tls.Config{
				MinVersion: tls.VersionTLS12,
				GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
					c, err := tls.LoadX509KeyPair(certFile, keyFile)
					return &c, err
				},
			}
			log.Printf("Neo Uptime %s listening on https://%s (data: %s)", version.Version, srv.Addr, dataDir)
			errc <- srv.ListenAndServeTLS("", "")
			return
		}
		log.Printf("Neo Uptime %s listening on http://%s (data: %s)", version.Version, srv.Addr, dataDir)
		errc <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Printf("shutting down")
	case err := <-errc:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}

func resetPassword(dataDir string, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: neo-uptime reset-password <username> [new-password]")
		os.Exit(2)
	}
	db := openDB(dataDir)
	defer db.Close()
	ctx := context.Background()
	u, err := db.GetUserByName(ctx, args[0])
	if err != nil {
		log.Fatalf("no user %q — run `neo-uptime list-users`", args[0])
	}
	pw := ""
	if len(args) > 1 {
		pw = args[1]
	} else {
		pw = auth.RandomAlnum(16)
	}
	if err := auth.ValidatePassword(pw); err != nil {
		log.Fatal(err)
	}
	if u.PasswordHash, err = auth.HashPassword(pw); err != nil {
		log.Fatal(err)
	}
	if err := db.UpdateUser(ctx, u); err != nil {
		log.Fatal(err)
	}
	_ = db.DeleteUserSessions(ctx, u.ID, "")
	fmt.Printf("Password for %s reset. New password: %s\n", u.Username, pw)
}

func disable2FA(dataDir string, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: neo-uptime disable-2fa <username>")
		os.Exit(2)
	}
	db := openDB(dataDir)
	defer db.Close()
	ctx := context.Background()
	u, err := db.GetUserByName(ctx, args[0])
	if err != nil {
		log.Fatalf("no user %q", args[0])
	}
	u.TOTPEnabled, u.TOTPSecret = false, ""
	if err := db.UpdateUser(ctx, u); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Two-factor sign-in turned off for %s.\n", u.Username)
}

func listUsers(dataDir string) {
	db := openDB(dataDir)
	defer db.Close()
	list, err := db.ListUsers(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	if len(list) == 0 {
		fmt.Println("No users yet — open the web panel to create the first admin.")
		return
	}
	for _, u := range list {
		tf := ""
		if u.TOTPEnabled {
			tf = " (2FA)"
		}
		if u.IsOwner {
			tf += " (owner)"
		}
		fmt.Printf("%-24s %s%s\n", u.Username, u.Role, tf)
	}
}

func setOwner(dataDir string, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: neo-uptime set-owner <username>")
		os.Exit(2)
	}
	db := openDB(dataDir)
	defer db.Close()
	ctx := context.Background()
	u, err := db.GetUserByName(ctx, args[0])
	if err != nil {
		log.Fatalf("no user %q", args[0])
	}
	if u.Role != "admin" {
		u.Role = "admin"
		if err := db.UpdateUser(ctx, u); err != nil {
			log.Fatal(err)
		}
	}
	if err := db.SetOwner(ctx, u.ID); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s is now the owner.\n", u.Username)
}
