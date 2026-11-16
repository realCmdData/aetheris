package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"aetheris/server/internal/store"
)

// Share links are encrypted with AES-256-GCM under the key in the link's #fragment, which browsers never
// send. The Vercel gateway and its logs only ever see the link id and ciphertext. The NAS keeps the key
// (it must encrypt), so this protects the path through the gateway, not the files from the NAS.
//
// Content stream: 8-byte random nonce prefix, 8-byte big-endian plaintext size, then the file in
// 256 KiB chunks, each sealed with nonce = prefix ‖ uint32 BE (index | 1<<31 on the last chunk).
// The last-chunk bit and the size make truncation or extension fail authentication.
// The reader is web/src/lib/sealed.ts; keep both in sync.
const sealChunk = 256 << 10

func gcmFor(key string) (cipher.AEAD, error) {
	k, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func chunks(size int64) int64 { return max(1, (size+sealChunk-1)/sealChunk) }

// sealedSize is the stream length for a file of size bytes.
func sealedSize(size int64) int64 { return 16 + size + 16*chunks(size) }

func sealStream(w io.Writer, r io.Reader, gcm cipher.AEAD, size int64) error {
	head := make([]byte, 16)
	rand.Read(head[:8])
	binary.BigEndian.PutUint64(head[8:], uint64(size))
	if _, err := w.Write(head); err != nil {
		return err
	}
	nonce := make([]byte, 12)
	copy(nonce, head[:8])
	buf := make([]byte, sealChunk)
	out := make([]byte, 0, sealChunk+16)
	n := chunks(size)
	for i := range n {
		m := min(sealChunk, size-i*sealChunk)
		if _, err := io.ReadFull(r, buf[:m]); err != nil {
			return err
		}
		ctr := uint32(i)
		if i == n-1 {
			ctr |= 1 << 31
		}
		binary.BigEndian.PutUint32(nonce[8:], ctr)
		if _, err := w.Write(gcm.Seal(out[:0], nonce, buf[:m], nil)); err != nil {
			return err
		}
	}
	return nil
}

// sealMeta encrypts a small value (the file's name, size, date) as nonce ‖ ciphertext, base64url.
func sealMeta(gcm cipher.AEAD, v any) string {
	plain, _ := json.Marshal(v)
	nonce := make([]byte, 12)
	rand.Read(nonce)
	return base64.RawURLEncoding.EncodeToString(gcm.Seal(nonce, nonce, plain, nil))
}

func previewable(p string) bool {
	_, ok := previewTypes[strings.ToLower(path.Ext(p))]
	return ok
}

// Owner side (behind auth).

func (a *API) createShare(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path      string `json:"path"`
		Views     int64  `json:"views"`
		Downloads int64  `json:"downloads"`
		Hours     int64  `json:"hours"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, err)
		return
	}
	if req.Hours < 1 || req.Hours > 24*90 || req.Views != 0 && !previewable(req.Path) {
		a.fail(w, errBadRequest) // only previewable files can be viewed in the browser
		return
	}
	sh, err := a.s.CreateShare(req.Path, req.Views, req.Downloads, time.Duration(req.Hours)*time.Hour)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.audit(r, "share_created", sh.Path)
	writeJSON(w, http.StatusOK, sh)
}

func (a *API) listShares(w http.ResponseWriter, r *http.Request) {
	list, err := a.s.Shares()
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shares": list})
}

func (a *API) revokeShare(w http.ResponseWriter, r *http.Request) {
	sh, err := a.s.RevokeShare(r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	a.audit(r, "share_revoked", sh.Path)
	w.WriteHeader(http.StatusNoContent)
}

// Public serves share links to anyone holding one: no session, the random id is the capability.
func Public(s *store.Store, log *slog.Logger, audit AuditFunc) http.Handler {
	a := &API{s: s, log: log, audit: audit}
	m := http.NewServeMux()
	m.HandleFunc("GET /api/v1/shared/{id}", a.sharedInfo)
	m.HandleFunc("GET /api/v1/shared/{id}/content", a.sharedContent)
	return m
}

func (a *API) sharedInfo(w http.ResponseWriter, r *http.Request) {
	sh, e, err := a.s.GetShare(r.PathValue("id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	gcm, err := gcmFor(sh.Key)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"meta":    sealMeta(gcm, map[string]any{"name": path.Base(e.Path), "size": e.Size, "mtime": e.Mtime}),
		"views":   sh.Views, "downloads": sh.Downloads, "expires": sh.Expires,
	})
}

func (a *API) sharedContent(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	if sh, _, err := a.s.GetShare(r.PathValue("id")); err == nil && mode == "view" && !previewable(sh.Path) {
		a.fail(w, errBadRequest)
		return
	}
	f, e, sh, done, err := a.s.UseShare(r.PathValue("id"), mode)
	if err != nil {
		a.fail(w, err)
		return
	}
	defer f.Close()
	gcm, err := gcmFor(sh.Key)
	if err != nil {
		done(false)
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(sealedSize(e.Size), 10))
	err = sealStream(w, io.LimitReader(f, e.Size), gcm, e.Size)
	done(err == nil)
	if err != nil {
		a.log.Warn("share stream ended early", "err", err)
		return
	}
	a.audit(r, "share_"+mode, e.Path)
}
