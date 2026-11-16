// Command gateway runs on Vercel in front of your Aetheris web address. It joins the tailnet as an ephemeral
// tag:gateway node and forwards /api/v1/* to the NAS over WireGuard. Anything that needs a session
// is forwarded only with a validly signed, unexpired session cookie, so junk never reaches home.
// The NAS still checks everything itself (revocation, passkeys, paths); this is the outer lock.
//
// Env: TS_AUTHKEY (Tailscale OAuth client secret or auth key), AETHERIS_PUBKEY (`aetheris pubkey` on the NAS),
// AETHERIS_NAS (optional: the NAS's tailnet name; by default the node "aetheris" in the gateway's own tailnet).
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"tailscale.com/tsnet"
)

const cookieName = "__Host-aetheris"

// open may be reached without a session: signing in and the first-passkey setup. The NAS checks them itself.
var open = map[string]bool{
	"GET /api/v1/auth/status":           true,
	"POST /api/v1/auth/login/begin":     true,
	"POST /api/v1/auth/login/finish":    true,
	"POST /api/v1/auth/register/begin":  true,
	"POST /api/v1/auth/register/finish": true,
}

// filter decides whether a request may go to the NAS: 0 means forward.
func filter(r *http.Request, pub ed25519.PublicKey, now time.Time) (int, string) {
	p := r.URL.Path
	switch {
	case !strings.HasPrefix(p, "/api/v1/"):
		return http.StatusNotFound, "not found"
	case r.URL.RawPath != "" || path.Clean(p) != p:
		return http.StatusBadRequest, "malformed path" // the API never needs encoded or dot segments
	case r.Header.Get("Authorization") != "":
		return http.StatusForbidden, "device tokens only work at home or over the tailnet"
	case p == "/api/v1/devices/claim":
		return http.StatusNotFound, "not found" // pairing happens on the LAN or tailnet only
	case open[r.Method+" "+p]:
		return 0, ""
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/api/v1/shared/"):
		return 0, "" // share links: the random id is the capability; the NAS checks it and serves only ciphertext
	}
	c, err := r.Cookie(cookieName)
	if err != nil || len(pub) != ed25519.PublicKeySize || !verify(pub, c.Value, now) {
		return http.StatusUnauthorized, "sign in required"
	}
	return 0, ""
}

// verify checks a session token's signature and expiry. Keep in sync with server/internal/auth.Verify.
func verify(pub ed25519.PublicKey, token string, now time.Time) bool {
	payload, sig, found := strings.Cut(token, ".")
	s, err := base64.RawURLEncoding.DecodeString(sig)
	if !found || err != nil || !ed25519.Verify(pub, []byte(payload), s) {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	var c struct {
		Sid string `json:"sid"`
		Exp int64  `json:"exp"`
	}
	return err == nil && json.Unmarshal(raw, &c) == nil && c.Sid != "" && now.Unix() < c.Exp
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func main() {
	// Vercel runs functions on Lambda-style VMs whose only address is link-local (169.254.x.x). Tailscale
	// accepts that only when it detects Lambda via these variables, which Vercel hides; without them it
	// decides the network is down and never logs in (found in P1). Re-check after tailscale.com upgrades.
	for _, k := range []string{"AWS_LAMBDA_FUNCTION_NAME", "AWS_LAMBDA_FUNCTION_VERSION", "AWS_LAMBDA_INITIALIZATION_TYPE", "AWS_LAMBDA_RUNTIME_API"} {
		if os.Getenv(k) == "" {
			os.Setenv(k, "vercel")
		}
	}
	key := os.Getenv("TS_AUTHKEY")
	if strings.HasPrefix(key, "tskey-client-") && !strings.Contains(key, "?") {
		key += "?preauthorized=true" // OAuth client: mint an ephemeral, pre-approved tag:gateway key per instance
	}
	pub, _ := base64.StdEncoding.DecodeString(os.Getenv("AETHERIS_PUBKEY")) // missing or bad: every session check fails closed
	ts := &tsnet.Server{
		Hostname: "aetheris-gateway", Dir: "/tmp/tsnet", Ephemeral: true, AuthKey: key,
		AdvertiseTags: []string{"tag:gateway"},
		Logf:          func(string, ...any) {},
	}

	// Vercel freezes the instance whenever no request is open, so the join runs inside a request.
	var mu sync.Mutex
	up := false
	nas := os.Getenv("AETHERIS_NAS")
	join := func(ctx context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if up {
			return nil
		}
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		st, err := ts.Up(ctx)
		if err != nil {
			return err
		}
		if nas == "" {
			if st.CurrentTailnet == nil || st.CurrentTailnet.MagicDNSSuffix == "" {
				return errors.New("no MagicDNS name for the tailnet: set AETHERIS_NAS to the NAS's tailnet name")
			}
			nas = "aetheris." + st.CurrentTailnet.MagicDNSSuffix // the server's default node name
		}
		up = true
		return nil
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme, pr.Out.URL.Host, pr.Out.Host = "https", nas, nas
			pr.Out.Header.Set("X-Forwarded-For", pr.In.Header.Get("X-Real-Ip")) // Vercel's verified client IP
		},
		Transport:     &http.Transport{DialContext: ts.Dial, ForceAttemptHTTP2: true, IdleConnTimeout: 90 * time.Second},
		FlushInterval: -1, // stream downloads and long-polls as they arrive
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("proxy %s: %v", r.URL.Path, err)
			jsonError(w, http.StatusBadGateway, "your NAS is not reachable right now")
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if code, msg := filter(r, pub, time.Now()); code != 0 {
			jsonError(w, code, msg)
			return
		}
		if err := join(r.Context()); err != nil {
			log.Printf("tailnet join: %v", err)
			jsonError(w, http.StatusBadGateway, "your NAS is not reachable right now")
			return
		}
		w.Header().Del("X-Content-Type-Options") // the NAS sends these itself; the proxy would add a second copy
		w.Header().Del("Referrer-Policy")
		proxy.ServeHTTP(w, r)
	})
	// The web UI is public/ on Vercel's CDN. Should routing ever hand "/" to this function, send it there.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/index.html", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	srv := &http.Server{Addr: ":" + port, ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		mux.ServeHTTP(w, r)
	})}
	log.Fatal(srv.ListenAndServe())
}
