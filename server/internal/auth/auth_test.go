package auth

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

const origin = "http://localhost"

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// softKey is a software passkey: it produces the same attestation and assertion messages a phone does.
type softKey struct {
	id   []byte
	priv *ecdsa.PrivateKey
}

func newSoftKey() *softKey {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	return &softKey{id: random(16), priv: priv}
}

func (k *softKey) authData(flags byte, attested bool) []byte {
	rp := sha256.Sum256([]byte("localhost"))
	b := append(rp[:], flags, 0, 0, 0, 0)
	if attested {
		pub, _ := k.priv.PublicKey.ECDH()
		raw := pub.Bytes() // 0x04 || X || Y
		cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: raw[1:33], -3: raw[33:]})
		b = append(b, make([]byte, 16)...) // AAGUID
		b = binary.BigEndian.AppendUint16(b, uint16(len(k.id)))
		b = append(append(b, k.id...), cose...)
	}
	return b
}

func clientData(typ, challenge string) []byte {
	b, _ := json.Marshal(map[string]string{"type": typ, "challenge": challenge, "origin": origin})
	return b
}

func (k *softKey) create(challenge string) string {
	att, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": k.authData(0x45, true)}) // UP|UV|AT
	b, _ := json.Marshal(map[string]any{"id": b64(k.id), "rawId": b64(k.id), "type": "public-key",
		"response": map[string]string{"clientDataJSON": b64(clientData("webauthn.create", challenge)), "attestationObject": b64(att)}})
	return string(b)
}

func (k *softKey) get(challenge string, userHandle []byte) string {
	cd := clientData("webauthn.get", challenge)
	ad := k.authData(0x05, false) // UP|UV
	cdHash := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), cdHash[:]...))
	sig, _ := ecdsa.SignASN1(rand.Reader, k.priv, digest[:])
	b, _ := json.Marshal(map[string]any{"id": b64(k.id), "rawId": b64(k.id), "type": "public-key",
		"response": map[string]string{"clientDataJSON": b64(cd), "authenticatorData": b64(ad), "signature": b64(sig), "userHandle": b64(userHandle)}})
	return string(b)
}

type harness struct {
	t   *testing.T
	a   *Auth
	srv *httptest.Server
	c   *http.Client // a browser: keeps cookies and sends our Origin
}

func newHarness(t *testing.T) *harness {
	a, err := Open(t.TempDir(), "localhost", []string{origin}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	m := http.NewServeMux()
	a.Routes(m)
	m.Handle("/api/v1/", a.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("secret")) })))
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &harness{t, a, srv, &http.Client{Jar: jar}}
}

// do sends a request as the browser; hdr may override Origin or add Authorization.
func (h *harness) do(method, path, body string, hdr map[string]string, out any) int {
	h.t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Origin", origin)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := h.c.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

type begun struct {
	Ceremony string
	Options  struct {
		PublicKey struct{ Challenge string } `json:"publicKey"`
	}
}

func (h *harness) register(k *softKey, setupToken string) int {
	var b begun
	if code := h.do("POST", "/api/v1/auth/register/begin", `{"setupToken":"`+setupToken+`"}`, nil, &b); code != 200 {
		return code
	}
	return h.do("POST", "/api/v1/auth/register/finish?ceremony="+b.Ceremony+"&name=Test", k.create(b.Options.PublicKey.Challenge), nil, nil)
}

func (h *harness) login(k *softKey) int {
	var b begun
	h.do("POST", "/api/v1/auth/login/begin", "", nil, &b)
	return h.do("POST", "/api/v1/auth/login/finish?ceremony="+b.Ceremony, k.get(b.Options.PublicKey.Challenge, h.a.userID), nil, nil)
}

func TestTokens(t *testing.T) {
	h := newHarness(t)
	_, tok, err := h.a.newSession("device", "pc")
	if err != nil {
		t.Fatal(err)
	}
	pub := h.a.key.Public().(ed25519.PublicKey)
	if _, _, ok := Verify(pub, tok, time.Now()); !ok {
		t.Fatal("valid token rejected")
	}
	payload, sig, _ := strings.Cut(tok, ".")
	forged := b64([]byte(`{"sid":"someone-else","exp":9999999999}`)) + "." + sig
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	for name, ok := range map[string]bool{
		"tampered payload": func() bool { _, _, ok := Verify(pub, forged, time.Now()); return ok }(),
		"wrong key":        func() bool { _, _, ok := Verify(otherPub, tok, time.Now()); return ok }(),
		"expired":          func() bool { _, _, ok := Verify(pub, tok, time.Now().Add(400*24*time.Hour)); return ok }(),
		"garbage":          func() bool { _, _, ok := Verify(pub, payload, time.Now()); return ok }(),
	} {
		if ok {
			t.Errorf("%s token accepted", name)
		}
	}
	bearer := map[string]string{"Authorization": "Bearer " + tok, "Origin": ""}
	if code := h.do("GET", "/api/v1/files", "", bearer, nil); code != 200 {
		t.Fatalf("device token refused: %d", code)
	}
	h.a.db.Exec(`DELETE FROM sessions`) // revoked: the signature still verifies, the NAS must still refuse
	if code := h.do("GET", "/api/v1/files", "", bearer, nil); code != 401 {
		t.Fatalf("revoked token accepted: %d", code)
	}
}

func TestPasskeySetupAndLogin(t *testing.T) {
	h := newHarness(t)
	key := newSoftKey()
	if code := h.do("GET", "/api/v1/files", "", nil, nil); code != 401 {
		t.Fatalf("anonymous request: %d", code)
	}
	if code := h.register(key, ""); code != 401 {
		t.Fatalf("registration without setup link or session: %d", code)
	}
	if code := h.register(key, "not-a-real-token"); code != 401 {
		t.Fatalf("registration with a made-up token: %d", code)
	}
	token, _ := h.a.NewSetupToken()
	if code := h.register(key, token); code != 200 {
		t.Fatalf("first passkey: %d", code)
	}
	if code := h.do("GET", "/api/v1/files", "", nil, nil); code != 200 {
		t.Fatalf("not signed in after setup: %d", code)
	}
	if code := h.register(newSoftKey(), token); code != 401 {
		t.Fatalf("setup link reused: %d", code)
	}

	// CSRF: a cookie request that changes state from a foreign origin is refused.
	if code := h.do("POST", "/api/v1/files", "", map[string]string{"Origin": "https://evil.example"}, nil); code != 403 {
		t.Fatalf("cross-site POST: %d", code)
	}
	if code := h.do("POST", "/api/v1/auth/logout", "", nil, nil); code != 204 {
		t.Fatalf("logout: %d", code)
	}
	if code := h.do("GET", "/api/v1/files", "", nil, nil); code != 401 {
		t.Fatalf("still signed in after logout: %d", code)
	}

	if code := h.login(newSoftKey()); code != 401 {
		t.Fatalf("unknown passkey signed in: %d", code)
	}
	impostor := newSoftKey()
	impostor.id = key.id // knows the credential ID, not the private key
	if code := h.login(impostor); code != 401 {
		t.Fatalf("forged signature signed in: %d", code)
	}
	if code := h.login(key); code != 200 {
		t.Fatalf("login: %d", code)
	}
	if code := h.do("GET", "/api/v1/files", "", nil, nil); code != 200 {
		t.Fatalf("not signed in after login: %d", code)
	}
}

func TestPairing(t *testing.T) {
	h := newHarness(t)
	token, _ := h.a.NewSetupToken()
	h.register(newSoftKey(), token)

	var p struct{ Code string }
	h.do("POST", "/api/v1/devices/pair", "", nil, &p)
	device := &http.Client{} // the Windows client: no cookies
	claim := func(code string) (int, string) {
		resp, err := device.Post(h.srv.URL+"/api/v1/devices/claim", "application/json", strings.NewReader(`{"code":"`+code+`","name":"PC"}`))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct{ Token string }
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out.Token
	}
	for range 5 {
		if code, _ := claim("WRONGONE"); code != 401 {
			t.Fatalf("wrong code: %d", code)
		}
	}
	if code, _ := claim(p.Code); code != 401 {
		t.Fatalf("code survived 5 wrong guesses: %d", code)
	}

	h.do("POST", "/api/v1/devices/pair", "", nil, &p)
	code, tok := claim(strings.ToLower(p.Code[:4] + "-" + p.Code[4:])) // typed by hand
	if code != 200 || tok == "" {
		t.Fatalf("claim: %d", code)
	}
	if code, _ := claim(p.Code); code != 401 {
		t.Fatalf("code used twice: %d", code)
	}
	bearer := map[string]string{"Authorization": "Bearer " + tok, "Origin": ""}
	if code := h.do("PUT", "/api/v1/files", "", bearer, nil); code != 200 {
		t.Fatalf("device token: %d", code)
	}
	var list struct{ Sessions []Session }
	h.do("GET", "/api/v1/sessions", "", nil, &list)
	for _, s := range list.Sessions {
		if s.Kind == "device" {
			h.do("DELETE", "/api/v1/sessions/"+s.ID, "", nil, nil)
		}
	}
	if code := h.do("GET", "/api/v1/files", "", bearer, nil); code != 401 {
		t.Fatalf("revoked device still in: %d", code)
	}
}
