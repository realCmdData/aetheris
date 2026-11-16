package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	maxCeremonies = 100
	maxBody       = 64 << 10
	pairAlphabet  = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O, 1/I: codes are typed by hand
)

// user is the single Aetheris account, as the WebAuthn library sees it.
type user struct {
	id    []byte
	creds []webauthn.Credential
}

func (u user) WebAuthnID() []byte                         { return u.id }
func (u user) WebAuthnName() string                       { return "aetheris" }
func (u user) WebAuthnDisplayName() string                { return "Aetheris" }
func (u user) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func (a *Auth) user() (user, error) {
	u := user{id: a.userID}
	rows, err := a.db.Query(`SELECT data FROM credentials`)
	if err != nil {
		return u, err
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		var c webauthn.Credential
		if err := rows.Scan(&data); err != nil {
			return u, err
		}
		if err := json.Unmarshal([]byte(data), &c); err != nil {
			return u, err
		}
		u.creds = append(u.creds, c)
	}
	return u, rows.Err()
}

func (a *Auth) Routes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/auth/status", a.status)
	m.HandleFunc("POST /api/v1/auth/register/begin", a.registerBegin)
	m.HandleFunc("POST /api/v1/auth/register/finish", a.registerFinish)
	m.HandleFunc("POST /api/v1/auth/login/begin", a.loginBegin)
	m.HandleFunc("POST /api/v1/auth/login/finish", a.loginFinish)
	m.HandleFunc("POST /api/v1/auth/logout", a.logout)
	m.HandleFunc("POST /api/v1/devices/claim", a.claim)
	m.Handle("POST /api/v1/devices/pair", a.Require(http.HandlerFunc(a.pair)))
	m.Handle("GET /api/v1/sessions", a.Require(http.HandlerFunc(a.sessions)))
	m.Handle("DELETE /api/v1/sessions/{id}", a.Require(http.HandlerFunc(a.revoke)))
	m.Handle("GET /api/v1/passkeys", a.Require(http.HandlerFunc(a.passkeys)))
	m.Handle("DELETE /api/v1/passkeys/{id}", a.Require(http.HandlerFunc(a.removePasskey)))
	m.Handle("GET /api/v1/audit", a.Require(http.HandlerFunc(a.auditLog)))
}

func (a *Auth) addCeremony(c ceremony) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for id, old := range a.ceremonies {
		if now.After(old.expires) {
			delete(a.ceremonies, id)
		}
	}
	if len(a.ceremonies) >= maxCeremonies {
		return "", false
	}
	id := randID()
	c.expires = now.Add(ceremonyTTL)
	a.ceremonies[id] = c
	return id, true
}

// takeCeremony removes and returns a pending ceremony: each challenge can be answered once.
func (a *Auth) takeCeremony(id string, register bool) (ceremony, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.ceremonies[id]
	delete(a.ceremonies, id)
	return c, ok && c.register == register && a.now().Before(c.expires)
}

func (a *Auth) status(w http.ResponseWriter, r *http.Request) {
	signedIn := false
	if c, err := r.Cookie(CookieName); err == nil {
		_, _, err := a.session(c.Value)
		signedIn = err == nil
	}
	var n int
	a.db.QueryRow(`SELECT count(*) FROM credentials`).Scan(&n)
	writeJSON(w, http.StatusOK, map[string]bool{"signedIn": signedIn, "hasPasskeys": n > 0})
}

func (a *Auth) registerBegin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SetupToken string `json:"setupToken"`
	}
	json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&req)
	setup := ""
	if req.SetupToken != "" {
		setup = hashToken(req.SetupToken)
		if !a.setupValid(setup) {
			a.audit(r, "setup_token_rejected", "")
			jsonError(w, http.StatusUnauthorized, "setup link is invalid or expired")
			return
		}
	} else if _, ok := a.authenticate(w, r); !ok { // adding another passkey needs a signed-in session
		return
	}
	u, err := a.user()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var exclude []protocol.CredentialDescriptor
	for _, c := range u.creds {
		exclude = append(exclude, c.Descriptor())
	}
	opts, data, err := a.wa.BeginRegistration(u,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey: protocol.ResidentKeyRequirementRequired, RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification: protocol.VerificationRequired}),
		webauthn.WithExclusions(exclude))
	if err != nil {
		a.log.Error("begin registration", "err", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	id, ok := a.addCeremony(ceremony{data: *data, register: true, setup: setup})
	if !ok {
		jsonError(w, http.StatusTooManyRequests, "too many pending sign-ins; try again shortly")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ceremony": id, "options": opts})
}

func (a *Auth) registerFinish(w http.ResponseWriter, r *http.Request) {
	c, ok := a.takeCeremony(r.URL.Query().Get("ceremony"), true)
	if !ok {
		jsonError(w, http.StatusBadRequest, "this attempt expired; start again")
		return
	}
	if c.setup != "" && !a.setupValid(c.setup) {
		jsonError(w, http.StatusUnauthorized, "setup link is invalid or expired")
		return
	}
	if c.setup == "" {
		if _, ok := a.authenticate(w, r); !ok {
			return
		}
	}
	u, err := a.user()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	cred, err := a.wa.FinishRegistration(u, c.data, r)
	if err != nil {
		a.audit(r, "passkey_rejected", err.Error())
		jsonError(w, http.StatusBadRequest, "passkey not accepted")
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" || len(name) > 60 {
		name = "Passkey"
	}
	data, _ := json.Marshal(cred)
	now := a.now().UnixMilli()
	if _, err := a.db.Exec(`INSERT INTO credentials(id, name, data, created, last_used) VALUES(?, ?, ?, ?, ?)`,
		base64.RawURLEncoding.EncodeToString(cred.ID), name, string(data), now, now); err != nil {
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, "passkey_registered", name)
	if c.setup != "" { // first passkey: the setup link is spent and this browser is signed in
		a.db.Exec(`DELETE FROM setup WHERE hash = ?`, c.setup)
		s, token, err := a.newSession("web", clientName(r))
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "internal error")
			return
		}
		setCookie(w, token, ttl(s.Kind))
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *Auth) loginBegin(w http.ResponseWriter, r *http.Request) {
	opts, data, err := a.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		a.log.Error("begin login", "err", err)
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	id, ok := a.addCeremony(ceremony{data: *data})
	if !ok {
		jsonError(w, http.StatusTooManyRequests, "too many pending sign-ins; try again shortly")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ceremony": id, "options": opts})
}

func (a *Auth) loginFinish(w http.ResponseWriter, r *http.Request) {
	c, ok := a.takeCeremony(r.URL.Query().Get("ceremony"), false)
	if !ok {
		jsonError(w, http.StatusBadRequest, "this attempt expired; start again")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	_, cred, err := a.wa.FinishPasskeyLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		if !bytes.Equal(userHandle, a.userID) {
			return nil, errors.New("unknown user")
		}
		return a.user()
	}, c.data, r)
	if err == nil && cred.Authenticator.CloneWarning {
		err = errors.New("signature counter went backwards: possible cloned authenticator")
	}
	if err != nil {
		a.audit(r, "login_failed", err.Error())
		jsonError(w, http.StatusUnauthorized, "passkey not accepted")
		return
	}
	data, _ := json.Marshal(cred)
	a.db.Exec(`UPDATE credentials SET data = ?, last_used = ? WHERE id = ?`,
		string(data), a.now().UnixMilli(), base64.RawURLEncoding.EncodeToString(cred.ID))
	s, token, err := a.newSession("web", clientName(r))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	setCookie(w, token, ttl(s.Kind))
	a.audit(r, "login", s.Name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	s, ok := a.authenticate(w, r)
	if !ok {
		return
	}
	a.db.Exec(`DELETE FROM sessions WHERE id = ?`, s.ID)
	setCookie(w, "", -time.Second)
	a.audit(r, "logout", s.Name)
	w.WriteHeader(http.StatusNoContent)
}

// pair shows a short code that a new device exchanges for its own token within 10 minutes.
func (a *Auth) pair(w http.ResponseWriter, r *http.Request) {
	b := random(8)
	for i := range b {
		b[i] = pairAlphabet[int(b[i])%len(pairAlphabet)]
	}
	code := string(b)
	a.mu.Lock()
	a.pairs[code] = a.now().Add(pairTTL)
	a.failedClaims = 0
	a.mu.Unlock()
	a.audit(r, "pairing_started", "")
	writeJSON(w, http.StatusOK, a.connection(map[string]any{"code": code, "expires": a.now().Add(pairTTL).UnixMilli()}))
}

// claim trades a pairing code for a device token. Codes are short, so guessing is capped twice:
// 10 attempts a minute overall, and 5 wrong guesses void every open code.
func (a *Auth) claim(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "bad request")
		return
	}
	code := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(req.Code), "-", ""))
	now := a.now()
	a.mu.Lock()
	recent := a.claimTimes[:0]
	for _, t := range a.claimTimes {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	a.claimTimes = recent
	if len(recent) >= 10 {
		a.mu.Unlock()
		jsonError(w, http.StatusTooManyRequests, "too many attempts; wait a minute")
		return
	}
	a.claimTimes = append(a.claimTimes, now)
	exp, ok := a.pairs[code]
	if !ok || now.After(exp) {
		a.failedClaims++
		if a.failedClaims >= 5 {
			clear(a.pairs)
		}
		a.mu.Unlock()
		a.audit(r, "pairing_failed", "")
		jsonError(w, http.StatusUnauthorized, "code is wrong or expired")
		return
	}
	delete(a.pairs, code)
	a.mu.Unlock()
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 60 {
		name = "Device"
	}
	_, token, err := a.newSession("device", name)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.audit(r, "device_paired", name)
	writeJSON(w, http.StatusOK, a.connection(map[string]any{"token": token, "web": a.web}))
}

// connection adds where this server is (see Connect) to a pairing answer: the web UI shows it next to the code,
// and the device keeps it with its token.
func (a *Auth) connection(m map[string]any) map[string]any {
	if a.Connect != nil {
		server, lan := a.Connect()
		if server != "" {
			m["server"] = server
		}
		if lan != "" {
			m["lan"] = lan
		}
	}
	return m
}

func (a *Auth) sessions(w http.ResponseWriter, r *http.Request) {
	cur, _ := Current(r.Context())
	rows, err := a.db.Query(`SELECT id, kind, name, created, last_seen, expires FROM sessions WHERE expires > ? ORDER BY last_seen DESC`, a.now().UnixMilli())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()
	list := []Session{}
	for rows.Next() {
		var s Session
		rows.Scan(&s.ID, &s.Kind, &s.Name, &s.Created, &s.LastSeen, &s.Expires)
		s.Current = s.ID == cur.ID
		list = append(list, s)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

func (a *Auth) revoke(w http.ResponseWriter, r *http.Request) {
	res, _ := a.db.Exec(`DELETE FROM sessions WHERE id = ?`, r.PathValue("id"))
	if n, _ := res.RowsAffected(); n == 0 {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	a.audit(r, "session_revoked", r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (a *Auth) passkeys(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT id, name, created, last_used FROM credentials ORDER BY created`)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()
	type passkey struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Created  int64  `json:"created"`
		LastUsed int64  `json:"lastUsed"`
	}
	list := []passkey{}
	for rows.Next() {
		var p passkey
		rows.Scan(&p.ID, &p.Name, &p.Created, &p.LastUsed)
		list = append(list, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": list})
}

func (a *Auth) removePasskey(w http.ResponseWriter, r *http.Request) {
	var n int
	a.db.QueryRow(`SELECT count(*) FROM credentials`).Scan(&n)
	if n <= 1 {
		jsonError(w, http.StatusConflict, "this is your last passkey; add another before removing it")
		return
	}
	res, _ := a.db.Exec(`DELETE FROM credentials WHERE id = ?`, r.PathValue("id"))
	if k, _ := res.RowsAffected(); k == 0 {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	a.audit(r, "passkey_removed", r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (a *Auth) auditLog(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT ts, event, detail, source FROM audit ORDER BY ts DESC LIMIT 200`)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()
	type event struct {
		TS     int64  `json:"ts"`
		Event  string `json:"event"`
		Detail string `json:"detail"`
		Source string `json:"source"`
	}
	list := []event{}
	for rows.Next() {
		var e event
		rows.Scan(&e.TS, &e.Event, &e.Detail, &e.Source)
		list = append(list, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": list})
}

// clientName labels a browser session in the session list.
func clientName(r *http.Request) string {
	ua := r.UserAgent()
	browser, os := "Browser", ""
	for _, b := range []string{"Edg/", "Firefox/", "Chrome/", "Safari/"} {
		if strings.Contains(ua, b) {
			browser = map[string]string{"Edg/": "Edge", "Firefox/": "Firefox", "Chrome/": "Chrome", "Safari/": "Safari"}[b]
			break
		}
	}
	for _, o := range []string{"Windows", "Android", "iPhone", "iPad", "Mac OS", "Linux"} {
		if strings.Contains(ua, o) {
			os = " on " + strings.Replace(o, "Mac OS", "Mac", 1)
			break
		}
	}
	return browser + os
}
