// Package api exposes the store over HTTP: JSON for metadata, raw bytes for content.
// The contract is in docs/protocol.md; main wraps it in auth.Require.
package api

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"aetheris/server/internal/store"
)

// ChunkSize is what clients should send per upload request on direct paths (the gateway path uses ≤ 4 MB).
const ChunkSize = 16 << 20

var errBadRequest = errors.New("bad request")

// AuditFunc records a destructive or restoring action in the audit log.
type AuditFunc func(r *http.Request, event, detail string)

type API struct {
	s     *store.Store
	log   *slog.Logger
	audit AuditFunc

	zmu  sync.Mutex
	zips map[string]zipJob // one-use download tickets for zip archives
}

func New(s *store.Store, log *slog.Logger, audit AuditFunc) http.Handler {
	a := &API{s: s, log: log, audit: audit, zips: map[string]zipJob{}}
	m := http.NewServeMux()
	m.HandleFunc("GET /api/v1/tree", a.tree)
	m.HandleFunc("GET /api/v1/changes", a.changes)
	m.HandleFunc("GET /api/v1/changes/head", a.head)
	m.HandleFunc("GET /api/v1/file", a.file)
	m.HandleFunc("GET /api/v1/search", a.search)
	m.HandleFunc("GET /api/v1/recent", a.recent)
	m.HandleFunc("GET /api/v1/thumb", a.thumb)
	m.HandleFunc("GET /api/v1/icon", a.icon)
	m.HandleFunc("PUT /api/v1/icon", a.setIcon)
	m.HandleFunc("GET /api/v1/versions/file", a.versionFile)
	m.HandleFunc("POST /api/v1/upload", a.beginUpload)
	m.HandleFunc("GET /api/v1/upload/{id}", a.uploadOffset)
	m.HandleFunc("PUT /api/v1/upload/{id}", a.uploadChunk)
	m.HandleFunc("POST /api/v1/upload/{id}/commit", a.commit)
	m.HandleFunc("DELETE /api/v1/upload/{id}", a.abort)
	m.HandleFunc("POST /api/v1/mkdir", a.mkdir)
	m.HandleFunc("POST /api/v1/move", a.move)
	m.HandleFunc("POST /api/v1/copy", a.copy)
	m.HandleFunc("POST /api/v1/compress", a.compress)
	m.HandleFunc("POST /api/v1/zip", a.zipTicket)
	m.HandleFunc("GET /api/v1/zip/{id}", a.zip)
	m.HandleFunc("POST /api/v1/shares", a.createShare)
	m.HandleFunc("GET /api/v1/shares", a.listShares)
	m.HandleFunc("DELETE /api/v1/shares/{id}", a.revokeShare)
	m.HandleFunc("POST /api/v1/delete", a.del)
	m.HandleFunc("GET /api/v1/versions", a.versions)
	m.HandleFunc("POST /api/v1/versions/restore", a.restoreVersion)
	m.HandleFunc("GET /api/v1/trash", a.trash)
	m.HandleFunc("POST /api/v1/trash/restore", a.restoreTrash)
	return m
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (a *API) fail(w http.ResponseWriter, err error) {
	var conflict *store.ConflictError
	var offset *store.OffsetError
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &conflict):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "conflict", "current": conflict.Current})
	case errors.As(err, &offset):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "offset", "offset": offset.Offset})
	case errors.As(err, &tooBig):
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "chunk too large"})
	case errors.Is(err, store.ErrBadName), errors.Is(err, errBadRequest):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
	case errors.Is(err, store.ErrEpoch):
		writeJSON(w, http.StatusGone, map[string]any{"error": err.Error()})
	case errors.Is(err, store.ErrBusy):
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
	case errors.Is(err, store.ErrExists), errors.Is(err, store.ErrNoParent), errors.Is(err, store.ErrIsDir), errors.Is(err, store.ErrNotDir):
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
	default:
		a.log.Error("request failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal error"})
	}
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		return errBadRequest
	}
	return nil
}

func (a *API) tree(w http.ResponseWriter, r *http.Request) {
	p, err := store.CleanPath(r.URL.Query().Get("path"))
	if err != nil {
		a.fail(w, err)
		return
	}
	if r.URL.Query().Get("recursive") != "1" {
		list, err := a.s.List(p)
		if err != nil {
			a.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": list})
		return
	}
	if p != "" {
		a.fail(w, fmt.Errorf("%w: recursive listings start at the root", errBadRequest))
		return
	}
	// NDJSON, one entry per line; the last line carries epoch and cursor. A stream without it was cut off.
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	cursor, err := a.s.Walk(r.Context(), func(e store.Entry) error { return enc.Encode(e) })
	if err != nil {
		a.log.Warn("tree stream ended early", "err", err)
		return
	}
	enc.Encode(map[string]any{"epoch": a.s.Epoch(), "cursor": cursor, "done": true})
}

func (a *API) changes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cursor, err := strconv.ParseInt(q.Get("cursor"), 10, 64)
	if err != nil {
		a.fail(w, errBadRequest)
		return
	}
	wait, _ := strconv.Atoi(q.Get("wait"))
	wait = min(max(wait, 0), 55) // stay under proxy and gateway timeouts
	next, list, err := a.s.Changes(r.Context(), q.Get("epoch"), cursor, time.Duration(wait)*time.Second)
	if err != nil {
		a.fail(w, err)
		return
	}
	if list == nil {
		list = []store.Change{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"epoch": a.s.Epoch(), "cursor": next, "changes": list})
}

func (a *API) head(w http.ResponseWriter, r *http.Request) {
	epoch, cursor, err := a.s.Head()
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"epoch": epoch, "cursor": cursor})
}

func (a *API) file(w http.ResponseWriter, r *http.Request) {
	p, err := store.CleanPath(r.URL.Query().Get("path"))
	if err == nil && p == "" {
		err = store.ErrIsDir
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	f, e, err := a.s.Open(p)
	if err != nil {
		a.fail(w, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("ETag", fmt.Sprintf(`"%d"`, e.Rev))
	h.Set("Cache-Control", "private, no-cache")
	disposition := "attachment"
	h.Set("Content-Type", "application/octet-stream")
	if ct, ok := previewTypes[strings.ToLower(path.Ext(p))]; ok && r.URL.Query().Get("inline") == "1" {
		disposition = "inline"
		h.Set("Content-Type", ct)
		if ct == "application/pdf" {
			h.Set("Content-Security-Policy", "frame-ancestors 'self'") // the browser's PDF viewer breaks inside a sandbox
		} else {
			h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; frame-ancestors 'self'")
		}
	}
	h.Set("Content-Disposition", disposition+"; filename*=UTF-8''"+url.PathEscape(path.Base(p)))
	http.ServeContent(w, r, "", time.UnixMilli(e.Mtime), f)
}

// previewTypes may be shown inline, always with this fixed type (never sniffed) and, except PDF,
// inside a CSP sandbox. HTML, SVG and anything else that can run script is only ever downloaded.
var previewTypes = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif",
	".bmp": "image/bmp", ".ico": "image/x-icon",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime", ".ogv": "video/ogg",
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".ogg": "audio/ogg", ".opus": "audio/ogg", ".flac": "audio/flac", ".wav": "audio/wav", ".aac": "audio/aac",
	".pdf": "application/pdf",
}

func init() {
	// Text and source code show as plain text, never as what they are (HTML and SVG stay download-only).
	// Keep in sync with the text kinds in web/src/lib/format.ts.
	for _, e := range strings.Fields("txt md log csv tsv json xml yaml yml toml ini cfg conf env js mjs ts tsx jsx css scss py go rs c h cpp hpp cs java kt swift rb php sh bash ps1 bat cmd sql srt vtt tex gcode") {
		previewTypes["."+e] = "text/plain; charset=utf-8"
	}
}

// Secure sets the headers every response carries. File previews override the CSP for themselves.
func Secure(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("Cache-Control", "no-store")
		hd.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		hd.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		h.ServeHTTP(w, r)
	})
}

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		a.fail(w, errBadRequest)
		return
	}
	list, err := a.s.Search(q, 200)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": list})
}

func (a *API) recent(w http.ResponseWriter, r *http.Request) {
	list, err := a.s.Recent(100)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": list})
}

func (a *API) thumb(w http.ResponseWriter, r *http.Request) {
	p, err := store.CleanPath(r.URL.Query().Get("path"))
	if err != nil {
		a.fail(w, err)
		return
	}
	f, hash, err := a.s.Thumb(p)
	if errors.Is(err, store.ErrNoThumb) {
		http.Error(w, "no thumbnail", http.StatusNotFound) // the UI falls back to the file-type icon
		return
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", "image/jpeg") // we made it ourselves: always a JPEG
	h.Set("ETag", `"`+hash+`"`)
	h.Set("Cache-Control", "private, max-age=31536000, immutable") // the UI adds the file's rev to the URL
	http.ServeContent(w, r, "", time.Time{}, f)
}

// icon serves a folder's own icon (see store.SetIcon); 404 when it has none, and the UI shows the usual folder.
func (a *API) icon(w http.ResponseWriter, r *http.Request) {
	f, err := a.s.Icon(r.URL.Query().Get("path"))
	if errors.Is(err, store.ErrNoIcon) {
		http.Error(w, "no icon", http.StatusNotFound)
		return
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/png") // checked to be a PNG when it was stored
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, "", time.Time{}, f)
}

// setIcon stores a folder's icon: the body is a PNG of at most 256 KB.
func (a *API) setIcon(w http.ResponseWriter, r *http.Request) {
	png, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if err == nil {
		err = a.s.SetIcon(r.URL.Query().Get("path"), png)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) versionFile(w http.ResponseWriter, r *http.Request) {
	p, err := store.CleanPath(r.URL.Query().Get("path"))
	if err != nil {
		a.fail(w, err)
		return
	}
	f, v, err := a.s.OpenVersion(p, r.URL.Query().Get("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(path.Base(p)))
	http.ServeContent(w, r, "", time.UnixMilli(v.Created), f)
}

func (a *API) beginUpload(w http.ResponseWriter, r *http.Request) {
	id, err := a.s.BeginUpload()
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "chunkSize": ChunkSize})
}

func (a *API) uploadOffset(w http.ResponseWriter, r *http.Request) {
	off, err := a.s.UploadOffset(r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"offset": off})
}

func (a *API) uploadChunk(w http.ResponseWriter, r *http.Request) {
	off, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil || off < 0 {
		a.fail(w, errBadRequest)
		return
	}
	n, err := a.s.Append(r.PathValue("id"), off, http.MaxBytesReader(w, r.Body, store.MaxChunk))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"offset": n})
}

func (a *API) commit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		BaseRev int64  `json:"baseRev"`
		Mtime   int64  `json:"mtime"`   // unix milliseconds; 0 keeps the server's time
		Created int64  `json:"created"` // unix milliseconds: the file's creation time where it came from; 0 unknown
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, err)
		return
	}
	var mtime, created time.Time
	if req.Mtime > 0 {
		mtime = time.UnixMilli(req.Mtime)
	}
	if req.Created > 0 {
		created = time.UnixMilli(req.Created)
	}
	e, err := a.s.Commit(r.PathValue("id"), req.Path, req.BaseRev, mtime, created)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (a *API) abort(w http.ResponseWriter, r *http.Request) {
	if err := a.s.AbortUpload(r.PathValue("id")); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) mkdir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, err)
		return
	}
	e, err := a.s.Mkdir(req.Path)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (a *API) move(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From    string `json:"from"`
		To      string `json:"to"`
		BaseRev int64  `json:"baseRev"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, err)
		return
	}
	e, err := a.s.Move(req.From, req.To, req.BaseRev)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (a *API) del(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		BaseRev int64  `json:"baseRev"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, err)
		return
	}
	if err := a.s.Delete(req.Path, req.BaseRev); err != nil {
		a.fail(w, err)
		return
	}
	a.audit(r, "delete", req.Path)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) versions(w http.ResponseWriter, r *http.Request) {
	p, err := store.CleanPath(r.URL.Query().Get("path"))
	if err != nil {
		a.fail(w, err)
		return
	}
	list, err := a.s.Versions(p)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": list})
}

func (a *API) restoreVersion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		ID   string `json:"id"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, err)
		return
	}
	e, err := a.s.RestoreVersion(req.Path, req.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.audit(r, "restore_version", req.Path)
	writeJSON(w, http.StatusOK, e)
}

func (a *API) trash(w http.ResponseWriter, r *http.Request) {
	list, err := a.s.Trash()
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (a *API) restoreTrash(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Path string `json:"path"` // optional: restore somewhere else
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, err)
		return
	}
	restored, err := a.s.RestoreTrash(req.ID, req.Path)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.audit(r, "restore_trash", restored)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) copy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, err)
		return
	}
	e, err := a.s.Copy(req.From, req.To)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (a *API) compress(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
		To    string   `json:"to"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, err)
		return
	}
	e, err := a.s.Compress(req.Paths, req.To)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

type zipJob struct {
	paths   []string
	name    string
	expires time.Time
}

// zipTicket checks a selection and returns a one-use URL that downloads it as one zip. Two steps
// because a browser download is a plain GET, and a long selection doesn't fit in a URL.
func (a *API) zipTicket(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
		Name  string   `json:"name"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, err)
		return
	}
	if len(req.Paths) == 0 || !strings.HasSuffix(strings.ToLower(req.Name), ".zip") || strings.ContainsAny(req.Name, "/\\") {
		a.fail(w, errBadRequest)
		return
	}
	for _, p := range req.Paths {
		p, err := store.CleanPath(p)
		if err == nil && p == "" {
			err = errBadRequest
		}
		if err == nil {
			_, err = a.s.Stat(p)
		}
		if err != nil {
			a.fail(w, err)
			return
		}
	}
	id := rand.Text()
	a.zmu.Lock()
	now := time.Now()
	for k, j := range a.zips {
		if now.After(j.expires) {
			delete(a.zips, k)
		}
	}
	a.zips[id] = zipJob{req.Paths, req.Name, now.Add(time.Minute)}
	a.zmu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"url": "/api/v1/zip/" + id})
}

func (a *API) zip(w http.ResponseWriter, r *http.Request) {
	a.zmu.Lock()
	j, ok := a.zips[r.PathValue("id")]
	delete(a.zips, r.PathValue("id"))
	a.zmu.Unlock()
	if !ok || time.Now().After(j.expires) {
		a.fail(w, store.ErrNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(j.name))
	if err := a.s.Zip(w, j.paths); err != nil {
		a.log.Warn("zip download ended early", "err", err) // headers are out; the browser sees a broken download
	}
}
