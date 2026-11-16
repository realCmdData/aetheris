//go:debug x509usefallbackroots=1

// Command aetheris serves a folder (the NAS share) to Aetheris clients.
//
//	aetheris serve  -data /share/Aetheris        run the server (tailnet :443 + LAN :8443)
//	aetheris setup                               print a 15-minute link to register a passkey
//	aetheris reset                               remove all passkeys and sessions, then setup
//	aetheris pubkey                              print the session-token public key for the gateway
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"
	"time"

	_ "golang.org/x/crypto/x509roots/fallback" // QTS 4.3.4's CA store cannot verify today's chains
	"tailscale.com/client/local"
	"tailscale.com/tsnet"

	"aetheris/server/internal/api"
	"aetheris/server/internal/auth"
	"aetheris/server/internal/store"
)

// version is set at build time by scripts/deploy-nas.sh (git describe).
var version = "dev"

func main() {
	cmd := "serve"
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		cmd = os.Args[1] // separate statement: append below rewrites os.Args in place
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}
	home, _ := os.UserHomeDir()
	data := flag.String("data", "", "folder to serve, e.g. /share/Aetheris")
	state := flag.String("state", filepath.Join(home, ".aetheris"), "private state: signing key, auth.db, tailnet node (keep outside the share)")
	origins := flag.String("origins", "", "comma-separated web UI origins, e.g. https://drive.example.com (saved in -state: later starts may leave it out)")
	rpID := flag.String("rpid", "", "WebAuthn relying party (default: the host of the first origin)")
	hostname := flag.String("hostname", "aetheris", "tailnet node name; empty disables the tailnet and LAN listeners")
	lan := flag.String("lan", ":8443", "LAN TLS listener (same certificate as the tailnet); empty disables it")
	dev := flag.String("dev", "", "plain-HTTP loopback listener for development, e.g. 127.0.0.1:8080")
	rescan := flag.Duration("rescan", time.Hour, "interval between full rescans")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	web, err := webOrigins(*state, *origins)
	if err != nil {
		fail(log, "web address", err)
	}
	if *rpID == "" {
		u, _ := url.Parse(web[0])
		*rpID = u.Hostname()
	}
	a, err := auth.Open(*state, *rpID, web, log)
	if err != nil {
		fail(log, "open auth state", err)
	}
	defer a.Close()

	switch cmd {
	case "reset":
		if err := a.Reset(); err != nil {
			fail(log, "reset", err)
		}
		fallthrough
	case "setup":
		token, err := a.NewSetupToken()
		if err != nil {
			fail(log, "setup", err)
		}
		fmt.Printf("Open this link within 15 minutes to register your first passkey:\n\n  %s/#setup=%s\n\n",
			web[0], token)
		return
	case "pubkey":
		fmt.Println(a.PublicKey())
		return
	case "serve":
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (serve, setup, reset, pubkey)\n", cmd)
		os.Exit(2)
	}

	if *data == "" {
		fail(log, "-data is required", nil)
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(96 << 20) // the NAS has ~1 GB free; keep the GC from letting the heap drift
	}
	st, err := store.Open(*data, log)
	if err != nil {
		fail(log, "open store", err)
	}
	defer st.Close()

	mux := http.NewServeMux()
	a.Routes(mux)
	mux.Handle("/api/v1/", a.Require(api.New(st, log, a.Audit)))
	mux.Handle("/api/v1/shared/", api.Public(st, log, a.Audit)) // share links: no session, see api/share.go
	handler := api.Secure(mux)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go st.Run(ctx, *rescan)

	var servers []*http.Server
	serve := func(srv *http.Server, run func() error) {
		servers = append(servers, srv)
		go func() {
			if err := run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("listener", "addr", srv.Addr, "err", err)
				stop()
			}
		}()
	}
	newServer := func(addr string, h http.Handler) *http.Server {
		return &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	}

	if *dev != "" {
		if host, _, err := net.SplitHostPort(*dev); err != nil || !net.ParseIP(host).IsLoopback() {
			fail(log, "-dev must be a loopback address: it is plain HTTP", nil)
		}
		srv := newServer(*dev, handler)
		serve(srv, srv.ListenAndServe)
	}
	if *hostname != "" {
		ts := &tsnet.Server{
			Hostname: *hostname, Dir: filepath.Join(*state, "tsnet"), AdvertiseTags: []string{"tag:aetheris"},
			Logf:     func(string, ...any) {},                                                    // backend chatter
			UserLogf: func(f string, args ...any) { log.Info("tailscale", "msg", fmt.Sprintf(f, args...)) }, // e.g. the login URL
		}
		defer ts.Close()
		lc, err := ts.LocalClient()
		if err != nil {
			fail(log, "tailscale", err)
		}
		// What the Windows app needs to reach this server, shown when a device is paired: its tailnet name and,
		// for LAN-first connections at home, its LAN address.
		a.Connect = func() (string, string) {
			server := ""
			if d := ts.CertDomains(); len(d) > 0 {
				server = "https://" + d[0]
			}
			home := ""
			if _, port, err := net.SplitHostPort(*lan); err == nil {
				if ip := lanIP(); ip != "" {
					home = net.JoinHostPort(ip, port)
				}
			}
			return server, home
		}
		ln, err := ts.ListenTLS("tcp", ":443")
		if err != nil {
			fail(log, "tailnet listener", err)
		}
		srv := newServer("tailnet:443", tailnetGate(lc, handler))
		serve(srv, func() error { return srv.Serve(ln) })
		if *lan != "" {
			lsrv := newServer(*lan, handler)
			lsrv.TLSConfig = &tls.Config{GetCertificate: lc.GetCertificate, MinVersion: tls.VersionTLS12}
			serve(lsrv, func() error { return lsrv.ListenAndServeTLS("", "") })
		}
	}
	if len(servers) == 0 {
		fail(log, "no listener: set -hostname or -dev", nil)
	}
	log.Info("serving", "version", version, "data", *data, "gatewayKey", a.PublicKey())
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range servers {
		srv.Shutdown(shutdown)
	}
}

// tailnetGate admits only your own (untagged) devices and the Vercel gateway. The tailnet policy
// already allows nothing else; this is the second lock, and it labels the peer for the audit log.
func tailnetGate(lc *local.Client, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		who, err := lc.WhoIs(r.Context(), r.RemoteAddr)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		peer := auth.Peer{Name: who.Node.ComputedName}
		if len(who.Node.Tags) > 0 {
			if !slices.Contains(who.Node.Tags, "tag:gateway") {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			peer.Gateway = true
		}
		h.ServeHTTP(w, r.WithContext(auth.WithPeer(r.Context(), peer)))
	})
}

// The web address belongs to this installation, so it is kept with the private state: given once (aetheris setup
// -origins https://drive.example.com), later starts find it there and start scripts stay the same everywhere.
func webOrigins(state, given string) ([]string, error) {
	file := filepath.Join(state, "origins.txt")
	if given == "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, errors.New("not set yet: run  aetheris setup -origins https://drive.example.com  once (your web address)")
		}
		given = strings.TrimSpace(string(b))
	}
	var list []string
	for _, o := range strings.Split(given, ",") {
		u, err := url.Parse(strings.TrimSpace(o))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || (u.Path != "" && u.Path != "/") {
			return nil, fmt.Errorf("%q: want an origin like https://drive.example.com", o)
		}
		list = append(list, u.Scheme+"://"+u.Host)
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		return nil, err
	}
	return list, os.WriteFile(file, []byte(strings.Join(list, ",")+"\n"), 0o600)
}

// The LAN address this machine reaches other networks from (on a home NAS, its address at home). Only the
// routing table is asked: no packet goes to the TEST-NET address.
func lanIP() string {
	c, err := net.Dial("udp4", "192.0.2.1:9")
	if err != nil {
		return ""
	}
	defer c.Close()
	if ip := c.LocalAddr().(*net.UDPAddr).IP; ip.IsPrivate() {
		return ip.String()
	}
	return ""
}

func fail(log *slog.Logger, msg string, err error) {
	log.Error(msg, "err", err)
	os.Exit(1)
}
