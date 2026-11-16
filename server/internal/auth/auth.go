// Package auth guards the API. People sign in with passkeys (WebAuthn, no passwords); the Windows
// client uses a token from device pairing. Both are Ed25519-signed session tokens, so the Vercel
// gateway can reject forged ones with only the public key, while the NAS also checks revocation.
// Its state (signing key, auth.db) lives in stateDir, outside the share, so SMB users never see it.
package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	_ "modernc.org/sqlite"
)

const (
	CookieName  = "__Host-aetheris"
	webTTL      = 30 * 24 * time.Hour
	deviceTTL   = 365 * 24 * time.Hour
	setupTTL    = 15 * time.Minute
	pairTTL     = 10 * time.Minute
	ceremonyTTL = 5 * time.Minute
)

var errUnauthorized = errors.New("sign in required")

type Session struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // web | device
	Name     string `json:"name"`
	Created  int64  `json:"created"` // unix milliseconds
	LastSeen int64  `json:"lastSeen"`
	Expires  int64  `json:"expires"`
	Current  bool   `json:"current,omitempty"`
}

type ceremony struct {
	data     webauthn.SessionData
	register bool
	setup    string // hash of the setup token that opened it; "" when a signed-in user adds a passkey
	expires  time.Time
}

type Auth struct {
	db      *sql.DB
	key     ed25519.PrivateKey
	wa      *webauthn.WebAuthn
	origins map[string]bool
	web     string // the first origin: where the web UI is
	userID  []byte
	// Connect says how a pairing device reaches this server: its tailnet URL and LAN host:port ("" when unknown).
	Connect func() (server, lan string)
	log     *slog.Logger
	now     func() time.Time

	mu           sync.Mutex
	ceremonies   map[string]ceremony
	pairs        map[string]time.Time // pairing code → expiry
	failedClaims int
	claimTimes   []time.Time
}

var migrations = []string{`
CREATE TABLE credentials (id TEXT PRIMARY KEY, name TEXT NOT NULL, data TEXT NOT NULL, created INTEGER NOT NULL, last_used INTEGER NOT NULL);
CREATE TABLE sessions (id TEXT PRIMARY KEY, kind TEXT NOT NULL, name TEXT NOT NULL, created INTEGER NOT NULL, last_seen INTEGER NOT NULL, expires INTEGER NOT NULL);
CREATE TABLE setup (hash TEXT PRIMARY KEY, expires INTEGER NOT NULL);
CREATE TABLE audit (ts INTEGER NOT NULL, event TEXT NOT NULL, detail TEXT NOT NULL, source TEXT NOT NULL);
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`}

// Open loads or creates the auth state in stateDir. rpID and origins are the web UI's
// (e.g. "drive.example.com", "https://drive.example.com"); passkeys are bound to them.
func Open(stateDir, rpID string, origins []string, log *slog.Logger) (*Auth, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	key, err := loadKey(filepath.Join(stateDir, "session.key"))
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(stateDir, "auth.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	a := &Auth{db: db, key: key, log: log, now: time.Now, origins: map[string]bool{}, web: origins[0],
		ceremonies: map[string]ceremony{}, pairs: map[string]time.Time{}}
	for _, o := range origins {
		a.origins[o] = true
	}
	if err := a.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if a.wa, err = webauthn.New(&webauthn.Config{RPID: rpID, RPDisplayName: "Aetheris", RPOrigins: origins}); err != nil {
		db.Close()
		return nil, err
	}
	return a, nil
}

func (a *Auth) Close() error { return a.db.Close() }

func (a *Auth) migrate() error {
	var v int
	if err := a.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	for ; v < len(migrations); v++ {
		if _, err := a.db.Exec(migrations[v] + fmt.Sprintf(";PRAGMA user_version = %d", v+1)); err != nil {
			return fmt.Errorf("auth migration %d: %w", v+1, err)
		}
	}
	var id string
	err := a.db.QueryRow(`SELECT value FROM meta WHERE key = 'user'`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		id = hex.EncodeToString(random(32))
		_, err = a.db.Exec(`INSERT INTO meta(key, value) VALUES('user', ?)`, id)
	}
	a.userID, _ = hex.DecodeString(id)
	return err
}

func loadKey(path string) (ed25519.PrivateKey, error) {
	seed, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		seed = random(ed25519.SeedSize)
		if err := os.WriteFile(path, seed, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s is corrupt", path)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func random(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func randID() string { return hex.EncodeToString(random(16)) }

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// PublicKey is what the Vercel gateway needs to verify session tokens (base64).
func (a *Auth) PublicKey() string {
	return base64.StdEncoding.EncodeToString(a.key.Public().(ed25519.PublicKey))
}

func (a *Auth) sign(sid string, exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"sid":%q,"exp":%d}`, sid, exp.Unix()))
	return payload + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(a.key, []byte(payload)))
}

// Verify checks a token's signature and expiry. It does not know about revocation; only the NAS does.
func Verify(pub ed25519.PublicKey, token string, now time.Time) (sid string, exp time.Time, ok bool) {
	payload, sig, found := strings.Cut(token, ".")
	s, err := base64.RawURLEncoding.DecodeString(sig)
	if !found || err != nil || !ed25519.Verify(pub, []byte(payload), s) {
		return "", time.Time{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	var c struct {
		Sid string `json:"sid"`
		Exp int64  `json:"exp"`
	}
	if err != nil || json.Unmarshal(raw, &c) != nil || c.Sid == "" || now.Unix() >= c.Exp {
		return "", time.Time{}, false
	}
	return c.Sid, time.Unix(c.Exp, 0), true
}

func ttl(kind string) time.Duration {
	if kind == "device" {
		return deviceTTL
	}
	return webTTL
}

// NewWebSession signs a browser in without a passkey. Only local tools that already hold the state
// dir call it (cmd/devsession); it is never reachable over HTTP.
func (a *Auth) NewWebSession(name string) (string, error) {
	_, token, err := a.newSession("web", name)
	return token, err
}

func (a *Auth) newSession(kind, name string) (Session, string, error) {
	now := a.now()
	s := Session{ID: randID(), Kind: kind, Name: name, Created: now.UnixMilli(), LastSeen: now.UnixMilli(), Expires: now.Add(ttl(kind)).UnixMilli()}
	a.db.Exec(`DELETE FROM sessions WHERE expires < ?`, now.UnixMilli())
	_, err := a.db.Exec(`INSERT INTO sessions(id, kind, name, created, last_seen, expires) VALUES(?, ?, ?, ?, ?, ?)`,
		s.ID, s.Kind, s.Name, s.Created, s.LastSeen, s.Expires)
	return s, a.sign(s.ID, time.UnixMilli(s.Expires)), err
}

// session resolves a token to a live session. Past half its lifetime it is renewed, and the new
// token is returned so the caller can hand it back (cookie or X-Aetheris-Token header).
func (a *Auth) session(token string) (Session, string, error) {
	now := a.now()
	sid, exp, ok := Verify(a.key.Public().(ed25519.PublicKey), token, now)
	if !ok {
		return Session{}, "", errUnauthorized
	}
	var s Session
	err := a.db.QueryRow(`SELECT id, kind, name, created, last_seen, expires FROM sessions WHERE id = ?`, sid).
		Scan(&s.ID, &s.Kind, &s.Name, &s.Created, &s.LastSeen, &s.Expires)
	if err != nil || now.UnixMilli() >= s.Expires {
		return Session{}, "", errUnauthorized
	}
	renewed := ""
	if exp.Sub(now) < ttl(s.Kind)/2 {
		s.Expires = now.Add(ttl(s.Kind)).UnixMilli()
		renewed = a.sign(s.ID, time.UnixMilli(s.Expires))
	}
	if renewed != "" || now.UnixMilli()-s.LastSeen > 5*60*1000 {
		s.LastSeen = now.UnixMilli()
		a.db.Exec(`UPDATE sessions SET last_seen = ?, expires = ? WHERE id = ?`, s.LastSeen, s.Expires, s.ID)
	}
	return s, renewed, nil
}

type sessionKey struct{}

// Current returns the session of an authenticated request.
func Current(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(Session)
	return s, ok
}

// Require admits only requests with a live session.
func (a *Auth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s, ok := a.authenticate(w, r); ok {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, s)))
		}
	})
}

// authenticate resolves the session (bearer token for devices, cookie for browsers) and writes the
// error response itself when there is none. Cookie requests that change state must come from one of
// our origins: SameSite=Strict already blocks cross-site use; this is the second lock against CSRF.
func (a *Auth) authenticate(w http.ResponseWriter, r *http.Request) (Session, bool) {
	token, fromCookie := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), false
	if token == "" || token == r.Header.Get("Authorization") {
		token = ""
		if c, err := r.Cookie(CookieName); err == nil {
			token, fromCookie = c.Value, true
		}
	}
	s, renewed, err := a.session(token)
	if err != nil {
		jsonError(w, http.StatusUnauthorized, err.Error())
		return s, false
	}
	if fromCookie && r.Method != http.MethodGet && r.Method != http.MethodHead && !a.origins[r.Header.Get("Origin")] {
		jsonError(w, http.StatusForbidden, "cross-site request refused")
		return s, false
	}
	if renewed != "" {
		if fromCookie {
			setCookie(w, renewed, ttl(s.Kind))
		} else {
			w.Header().Set("X-Aetheris-Token", renewed)
		}
	}
	return s, true
}

func setCookie(w http.ResponseWriter, token string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: token, Path: "/", MaxAge: int(maxAge.Seconds()),
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// Peer is who reached the server over the tailnet, as Tailscale identifies it.
type Peer struct {
	Name    string
	Gateway bool // the Vercel gateway; X-Forwarded-For is trusted only from it
}

type peerKey struct{}

func WithPeer(ctx context.Context, p Peer) context.Context { return context.WithValue(ctx, peerKey{}, p) }

func source(r *http.Request) string {
	p, ok := r.Context().Value(peerKey{}).(Peer)
	switch {
	case ok && p.Gateway:
		return "web via gateway, client " + r.Header.Get("X-Forwarded-For")
	case ok:
		return p.Name + " (tailnet)"
	}
	return r.RemoteAddr
}

// Audit records event in the audit log, attributed to the request's source.
func (a *Auth) Audit(r *http.Request, event, detail string) { a.audit(r, event, detail) }

func (a *Auth) audit(r *http.Request, event, detail string) {
	src := "cli"
	if r != nil {
		src = source(r)
	}
	a.log.Info("audit", "event", event, "detail", detail, "source", src)
	a.db.Exec(`INSERT INTO audit(ts, event, detail, source) VALUES(?, ?, ?, ?)`, a.now().UnixMilli(), event, detail, src)
}

// NewSetupToken opens a 15-minute window to register a passkey without signing in.
// Only the admin CLI calls it, over SSH from the LAN.
func (a *Auth) NewSetupToken() (string, error) {
	t := randID()
	a.db.Exec(`DELETE FROM setup WHERE expires < ?`, a.now().UnixMilli())
	_, err := a.db.Exec(`INSERT INTO setup(hash, expires) VALUES(?, ?)`, hashToken(t), a.now().Add(setupTTL).UnixMilli())
	a.audit(nil, "setup_token_created", "")
	return t, err
}

func (a *Auth) setupValid(hash string) bool {
	var exp int64
	return a.db.QueryRow(`SELECT expires FROM setup WHERE hash = ?`, hash).Scan(&exp) == nil && a.now().UnixMilli() < exp
}

// Reset removes every passkey and signs out every browser and device (admin CLI, for a lost passkey).
func (a *Auth) Reset() error {
	_, err := a.db.Exec(`DELETE FROM credentials; DELETE FROM sessions; DELETE FROM setup`)
	a.audit(nil, "reset", "all passkeys and sessions removed")
	return err
}
