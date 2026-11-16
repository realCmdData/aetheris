package api

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aetheris/server/internal/store"
)

// TestUploadDownloadFlow drives the HTTP contract a client depends on: chunked upload,
// commit conflict, ranged download with ETag, full tree stream and the change feed.
func TestUploadDownloadFlow(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := store.Open(t.TempDir(), quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	srv := httptest.NewServer(Secure(New(s, quiet, func(*http.Request, string, string) {})))
	defer srv.Close()

	call := func(method, path, body string, out any) int {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if out != nil {
			json.NewDecoder(resp.Body).Decode(out)
		}
		return resp.StatusCode
	}
	upload := func(path string, baseRev int64, parts ...string) (int, map[string]any) {
		var begin struct{ ID string }
		call("POST", "/api/v1/upload", "", &begin)
		off := 0
		for _, p := range parts {
			call("PUT", "/api/v1/upload/"+begin.ID+"?offset="+itoa(off), p, nil)
			off += len(p)
		}
		var out map[string]any
		code := call("POST", "/api/v1/upload/"+begin.ID+"/commit", `{"path":"`+path+`","baseRev":`+itoa(int(baseRev))+`}`, &out)
		return code, out
	}

	if code := call("POST", "/api/v1/mkdir", `{"path":"docs"}`, nil); code != 200 {
		t.Fatalf("mkdir %d", code)
	}
	code, e := upload("docs/a.txt", 0, "hello ", "world")
	if code != 200 || e["size"].(float64) != 11 {
		t.Fatalf("commit %d %v", code, e)
	}
	if code, out := upload("docs/a.txt", 0, "other"); code != 409 || out["current"] == nil {
		t.Fatalf("stale commit: %d %v", code, out)
	}

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/file?path=docs/a.txt", nil)
	req.Header.Set("Range", "bytes=6-")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || string(b) != "world" || resp.Header.Get("ETag") == "" ||
		!strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("range download: %d %q %v", resp.StatusCode, b, resp.Header)
	}

	resp, _ = http.Get(srv.URL + "/api/v1/tree?recursive=1")
	sc := bufio.NewScanner(resp.Body)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	resp.Body.Close()
	if len(lines) != 3 || !strings.Contains(lines[2], `"done":true`) {
		t.Fatalf("tree stream: %v", lines)
	}

	var ch struct {
		Cursor  int64
		Changes []store.Change
	}
	if code := call("GET", "/api/v1/changes?cursor=0&epoch="+s.Epoch(), "", &ch); code != 200 || len(ch.Changes) != 2 {
		t.Fatalf("changes %d %+v", code, ch)
	}
	if code := call("GET", "/api/v1/file?path=../etc/passwd", "", nil); code != 400 {
		t.Fatalf("traversal path: %d", code)
	}

	// Previews: allow-listed types inline in a sandbox; anything that could run script is only downloaded.
	upload("docs/evil.html", 0, "<script>alert(1)</script>")
	upload("docs/evil.svg", 0, "<svg onload=alert(1)>")
	upload("docs/pic.png", 0, "\x89PNG")
	for name, want := range map[string]string{"evil.html": "attachment", "evil.svg": "attachment", "pic.png": "inline"} {
		resp, err := http.Get(srv.URL + "/api/v1/file?inline=1&path=docs/" + name)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		h := resp.Header
		if !strings.HasPrefix(h.Get("Content-Disposition"), want) || h.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: %v", name, h)
		}
		if want == "attachment" && h.Get("Content-Type") != "application/octet-stream" {
			t.Errorf("%s served as %s", name, h.Get("Content-Type"))
		}
		if want == "inline" && (h.Get("Content-Type") != "image/png" || !strings.HasPrefix(h.Get("Content-Security-Policy"), "sandbox")) {
			t.Errorf("%s preview headers: %v", name, h)
		}
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }
