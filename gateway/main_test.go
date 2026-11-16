package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func token(priv ed25519.PrivateKey, exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"sid":"s1","exp":%d}`, exp.Unix()))
	return payload + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(priv, []byte(payload)))
}

func TestFilter(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now()
	good := token(priv, now.Add(time.Hour))

	cases := []struct {
		name, method, url, cookie, auth string
		want                            int
	}{
		{"login is open", "POST", "/api/v1/auth/login/begin", "", "", 0},
		{"status is open", "GET", "/api/v1/auth/status", "", "", 0},
		{"files need a session", "GET", "/api/v1/tree", "", "", 401},
		{"valid session", "GET", "/api/v1/tree", good, "", 0},
		{"expired session", "GET", "/api/v1/tree", token(priv, now.Add(-time.Minute)), "", 401},
		{"session signed by another key", "GET", "/api/v1/tree", token(other, now.Add(time.Hour)), "", 401},
		{"garbage cookie", "GET", "/api/v1/tree", "abc.def", "", 401},
		{"bearer tokens refused", "GET", "/api/v1/tree", good, "Bearer x", 403},
		{"pairing not reachable", "POST", "/api/v1/devices/claim", "", "", 404},
		{"open endpoint only with its method", "GET", "/api/v1/auth/login/begin", "", "", 401},
		{"dot segments", "GET", "/api/v1/auth/status/../../tree", "", "", 400},
		{"encoded slash", "GET", "/api/v1/auth%2Fstatus", "", "", 400},
		{"outside the API", "GET", "/etc/passwd", "", "", 404},
		{"share links are open", "GET", "/api/v1/shared/0123456789abcdef0123456789abcdef/content", "", "", 0},
		{"share links are read-only", "POST", "/api/v1/shared/0123456789abcdef0123456789abcdef", "", "", 401},
		{"share links can't climb out", "GET", "/api/v1/shared/../tree", "", "", 400},
		{"managing shares needs a session", "GET", "/api/v1/shares", "", "", 401},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.url, nil)
		if c.cookie != "" {
			r.AddCookie(&http.Cookie{Name: cookieName, Value: c.cookie})
		}
		if c.auth != "" {
			r.Header.Set("Authorization", c.auth)
		}
		if got, _ := filter(r, pub, now); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
	// A missing AETHERIS_PUBKEY must fail closed, not open.
	r := httptest.NewRequest("GET", "/api/v1/tree", nil)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: good})
	if got, _ := filter(r, nil, now); got != 401 {
		t.Errorf("without a public key: got %d", got)
	}
}
