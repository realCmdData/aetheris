package api

import (
	"bytes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aetheris/server/internal/store"
)

// openStream is the Go twin of web/src/lib/sealed.ts: it must reject any cut, extension or edit.
func openStream(gcm cipher.AEAD, b []byte) ([]byte, error) {
	if len(b) < 16 {
		return nil, errors.New("short")
	}
	size := int64(binary.BigEndian.Uint64(b[8:16]))
	nonce := append([]byte{}, b[:8]...)
	nonce = append(nonce, 0, 0, 0, 0)
	rest, out := b[16:], []byte{}
	n := chunks(size)
	for i := range n {
		m := min(sealChunk, size-i*sealChunk) + 16
		if int64(len(rest)) < m {
			return nil, errors.New("cut off")
		}
		ctr := uint32(i)
		if i == n-1 {
			ctr |= 1 << 31
		}
		binary.BigEndian.PutUint32(nonce[8:], ctr)
		plain, err := gcm.Open(nil, nonce, rest[:m], nil)
		if err != nil {
			return nil, err
		}
		out, rest = append(out, plain...), rest[m:]
	}
	if len(rest) != 0 {
		return nil, errors.New("trailing data")
	}
	return out, nil
}

func TestShareLinks(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := store.Open(t.TempDir(), quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var audited []string
	audit := func(_ *http.Request, ev, _ string) { audited = append(audited, ev) }
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", New(s, quiet, audit))
	mux.Handle("/api/v1/shared/", Public(s, quiet, audit))
	srv := httptest.NewServer(Secure(mux))
	defer srv.Close()

	body := bytes.Repeat([]byte("0123456789abcdef"), sealChunk/16*2+5) // two full chunks and a bit
	id, _ := s.BeginUpload()
	s.Append(id, 0, bytes.NewReader(body))
	s.Mkdir("docs")
	s.Commit(id, "docs/report.pdf", 0, time.Time{}, time.Time{})
	put := func(p, c string) { id, _ := s.BeginUpload(); s.Append(id, 0, strings.NewReader(c)); s.Commit(id, p, 0, time.Time{}, time.Time{}) }
	put("docs/data.bin", "x")

	post := func(path, body string) (int, map[string]any) {
		resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	get := func(path string) (int, []byte) {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}

	if code, _ := post("/api/v1/shares", `{"path":"docs/data.bin","views":1,"downloads":1,"hours":1}`); code != 400 {
		t.Errorf("a view of an unpreviewable file: %d", code)
	}
	code, sh := post("/api/v1/shares", `{"path":"docs/report.pdf","views":1,"downloads":0,"hours":24}`)
	if code != 200 {
		t.Fatalf("create share: %d %v", code, sh)
	}
	sid, key := sh["id"].(string), sh["key"].(string)
	gcm, err := gcmFor(key)
	if err != nil {
		t.Fatal(err)
	}

	// The link follows its file when it moves.
	s.Move("docs", "papers", 0)

	code, raw := get("/api/v1/shared/" + sid)
	var info struct {
		Meta             string
		Views, Downloads int64
	}
	json.Unmarshal(raw, &info)
	sealed, _ := base64.RawURLEncoding.DecodeString(info.Meta)
	meta, err := gcm.Open(nil, sealed[:12], sealed[12:], nil)
	if code != 200 || err != nil || !strings.Contains(string(meta), `"name":"report.pdf"`) || info.Views != 1 || info.Downloads != 0 {
		t.Fatalf("info: %d %s %v %+v", code, meta, err, info)
	}
	if bytes.Contains(raw, []byte("report")) {
		t.Error("the file name went out in the clear")
	}

	if code, _ := get("/api/v1/shared/" + sid + "/content?mode=download"); code != 404 {
		t.Errorf("download on a view-only link: %d", code)
	}
	code, stream := get("/api/v1/shared/" + sid + "/content?mode=view")
	plain, err := openStream(gcm, stream)
	if code != 200 || err != nil || !bytes.Equal(plain, body) || int64(len(stream)) != sealedSize(int64(len(body))) {
		t.Fatalf("view: %d %v, %d bytes", code, err, len(plain))
	}
	if bytes.Contains(stream, body[:64]) {
		t.Error("content went out in the clear")
	}
	// Tampering: a cut, a flipped bit, a dropped last chunk.
	for name, bad := range map[string][]byte{
		"cut":     stream[:len(stream)-1],
		"flipped": append(append([]byte{}, stream[:100]...), append([]byte{stream[100] ^ 1}, stream[101:]...)...),
		"dropped": append(append([]byte{}, stream[:8]...), append(binary.BigEndian.AppendUint64(nil, uint64(2*sealChunk)), stream[16:16+2*(sealChunk+16)]...)...),
	} {
		if _, err := openStream(gcm, bad); err == nil {
			t.Errorf("%s stream accepted", name)
		}
	}

	if code, _ := get("/api/v1/shared/" + sid + "/content?mode=view"); code != 404 {
		t.Errorf("second view of a one-time link: %d", code)
	}
	if code, _ := get("/api/v1/shared/" + sid); code != 404 {
		t.Errorf("a used-up link still answers: %d", code)
	}
	if code, _ := get("/api/v1/shared/0123456789abcdef0123456789abcdef"); code != 404 {
		t.Errorf("made-up id: %d", code)
	}

	// Unlimited downloads, then revoked.
	_, sh = post("/api/v1/shares", `{"path":"papers/data.bin","views":0,"downloads":-1,"hours":1}`)
	for range 3 {
		if code, _ := get("/api/v1/shared/" + sh["id"].(string) + "/content?mode=download"); code != 200 {
			t.Fatalf("unlimited download: %d", code)
		}
	}
	req, _ := http.NewRequest("DELETE", srv.URL+"/api/v1/shares/"+sh["id"].(string), nil)
	http.DefaultClient.Do(req)
	if code, _ := get("/api/v1/shared/" + sh["id"].(string)); code != 404 {
		t.Errorf("revoked link: %d", code)
	}
	if strings.Join(audited, ",") != "share_created,share_view,share_created,share_download,share_download,share_download,share_revoked" {
		t.Errorf("audit: %v", audited)
	}
}
