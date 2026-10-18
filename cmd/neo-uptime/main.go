// Command neo-uptime is a self-hosted uptime monitor with first-class
// BackPack tunnel monitoring.
package main

import (
	"bufio"
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
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // maintenance windows need zones even on minimal hosts

	"github.com/neoauroraproject/Neo-Uptime/internal/ads"
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
	fmt.Fprintf(os.Stderr, `Neo Uptime %s — uptime monitoring with BackPack, Kariz and NeoTunnel tunnel support

Usage:
  neo-uptime [serve] [flags]          run the server (default)
  neo-uptime reset-password <user> [new-password]
  neo-uptime reset-admin [--user U] [--username NEW] [--password-stdin] [--keep-2fa]
                                       forgotten password: a new password (random unless given
                                       on stdin), 2FA off, signed out everywhere; with no
                                       account yet it creates the owner
  neo-uptime disable-2fa <user>
  neo-uptime list-users
  neo-uptime set-owner <user>          make an admin the owner (who alone issues status API keys)
  neo-uptime ad                        show the current ad (used by the install script)
  neo-uptime version

Flags (or environment):
  --host   NEO_UPTIME_HOST      listen address        (default 0.0.0.0)
  --port   NEO_UPTIME_PORT      listen port           (default 7788)
  --data   NEO_UPTIME_DATA      data directory        (default %s)
  --tls-cert NEO_UPTIME_TLS_CERT  certificate (PEM, with chain)
  --tls-key  NEO_UPTIME_TLS_KEY   private key (PEM)

News and help: https://t.me/neoaurora
`, version.Version, defaultDataDir())
}

func main() {
	log.SetFlags(log.LstdFlags)
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}

	if cmd == "reset-admin" {
		resetAdmin(args)
		return
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
	case "ad":
		ads.Print(os.Stdout, isTerminal())
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
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
	app.SetPanelPort(port)
	app.StartAds(ctx)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	// Automatic HTTPS for the panel's and status pages' own domains. It stays
	// off until it is turned on under Settings → Domains & HTTPS.
	cfg := certs.Config{
		HTTPAddr:  ":" + env("NEO_UPTIME_HTTP_PORT", "80"),
		HTTPSAddr: ":" + env("NEO_UPTIME_HTTPS_PORT", "443"),
		CacheDir:  filepath.Join(dataDir, "certs"),
		Directory: env("NEO_UPTIME_ACME_DIRECTORY", ""),
		CARoots:   env("NEO_UPTIME_ACME_CA_ROOTS", ""),
	}
	ownCert := certFile != "" && keyFile != ""
	var ln net.Listener
	if !ownCert {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			log.Fatalf("listening on %s: %v", addr, err)
		}
		ln = l
		// When the panel itself is on the HTTPS or HTTP port, automatic HTTPS
		// shares it instead of trying to open the same port a second time.
		switch ":" + strconv.Itoa(port) {
		case cfg.HTTPSAddr:
			sp := certs.NewSplitter(l)
			ln, cfg.Shared = sp.Plain(), sp
		case cfg.HTTPAddr:
			cfg.SharedPlain = true
		}
	}
	app.EnableHTTPS(ctx, cfg)
	var handler http.Handler = app
	if cfg.SharedPlain {
		handler = app.PlainHandler()
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errc := make(chan error, 1)
	go func() {
		if ownCert {
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
		errc <- srv.Serve(ln)
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

// resetAdmin recovers access for whoever runs the server: it gives an admin
// (the owner unless --user names another) a new password, and by default turns
// off two-factor sign-in, since a forgotten password often comes with a lost
// phone. With no account at all it creates the owner.
func resetAdmin(args []string) {
	fs := flag.NewFlagSet("reset-admin", flag.ExitOnError)
	data := fs.String("data", env("NEO_UPTIME_DATA", defaultDataDir()), "data directory")
	user := fs.String("user", "", "account to reset (default: the owner)")
	rename := fs.String("username", "", "new username")
	fromStdin := fs.Bool("password-stdin", false, "read the new password from the first line of stdin")
	keep2FA := fs.Bool("keep-2fa", false, "leave two-factor sign-in on")
	_ = fs.Parse(args)

	pw := ""
	if *fromStdin {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			log.Fatal("no password on stdin")
		}
		pw = strings.TrimRight(line, "\r\n")
	}
	generated := pw == ""
	if generated {
		pw = auth.RandomAlnum(16)
	}
	if err := auth.ValidatePassword(pw); err != nil {
		log.Fatal(err)
	}
	if *rename != "" {
		if err := auth.ValidateUsername(*rename); err != nil {
			log.Fatal(err)
		}
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		log.Fatal(err)
	}

	db := openDB(*data)
	defer db.Close()
	ctx := context.Background()
	list, err := db.ListUsers(ctx)
	if err != nil {
		log.Fatal(err)
	}
	var u *store.User
	for _, x := range list {
		if (*user != "" && strings.EqualFold(x.Username, *user)) || (*user == "" && x.IsOwner) {
			u = x
			break
		}
	}
	if u == nil && *user == "" {
		for _, x := range list {
			if x.Role == "admin" {
				u = x
				break
			}
		}
	}
	created := false
	switch {
	case u == nil && len(list) == 0:
		name := *rename
		if name == "" {
			name = "admin"
		}
		u = &store.User{Username: name, PasswordHash: hash, Role: "admin", IsOwner: true}
		if err := db.CreateUser(ctx, u); err != nil {
			log.Fatal(err)
		}
		created = true
	case u == nil:
		log.Fatalf("no user %q — run `neo-uptime list-users`", *user)
	default:
		if *rename != "" {
			if other, err := db.GetUserByName(ctx, *rename); err == nil && other.ID != u.ID {
				log.Fatalf("the username %q is already taken", *rename)
			}
			u.Username = *rename
		}
		u.PasswordHash = hash
		u.Role = "admin"
		if !*keep2FA {
			u.TOTPEnabled, u.TOTPSecret = false, ""
		}
		if err := db.UpdateUser(ctx, u); err != nil {
			log.Fatal(err)
		}
		_ = db.DeleteUserSessions(ctx, u.ID, "")
	}

	if created {
		fmt.Println("Created the owner account.")
	} else {
		fmt.Println("Admin account reset; it was signed out everywhere.")
		if !*keep2FA {
			fmt.Println("Two-factor sign-in is off; turn it on again under Account & security.")
		}
	}
	fmt.Printf("Username: %s\n", u.Username)
	if generated {
		fmt.Printf("Password: %s\n", pw)
	} else {
		fmt.Println("Password: the one you entered")
	}
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
