package store

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ErrBadName means a path a Windows client could not store, or one that tries to leave the root.
var ErrBadName = errors.New("invalid name")

// metaDir holds Aetheris's own state inside the data root; it is never listed or synced.
const metaDir = ".aetheris"

// Folders QTS creates inside shares, plus our own state. Never indexed, synced or accepted from clients.
var ignoredNames = map[string]bool{
	metaDir: true, "@Recycle": true, "@Recently-Snapshot": true, ".@__thumb": true, ".@__desc": true, ".streams": true,
}

var reservedNames = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true}

func init() {
	for _, d := range "123456789" {
		reservedNames["COM"+string(d)] = true
		reservedNames["LPT"+string(d)] = true
	}
}

// CleanPath validates a client path like "Photos/2026/a.jpg" ("" is the root) and returns its canonical form.
// It rejects anything Windows cannot store, so the PC client never meets a file it cannot create.
func CleanPath(p string) (string, error) {
	p = strings.Trim(p, "/")
	if p == "" {
		return "", nil
	}
	if len(p) > 4096 || !utf8.ValidString(p) {
		return "", ErrBadName
	}
	for _, seg := range strings.Split(p, "/") {
		if !validName(seg) {
			return "", ErrBadName
		}
	}
	return p, nil
}

func validName(n string) bool {
	if n == "" || n == "." || n == ".." || len(n) > 255 || ignoredNames[n] ||
		strings.ContainsAny(n, `<>:"\|?*`) || strings.HasSuffix(n, ".") || strings.HasSuffix(n, " ") {
		return false
	}
	for _, r := range n {
		if r < 0x20 {
			return false
		}
	}
	base, _, _ := strings.Cut(strings.ToUpper(n), ".")
	return !reservedNames[base]
}

func parentOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

func join(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// osPath converts a slash path inside the root to the form os.Root expects.
func osPath(p string) string {
	if p == "" {
		return "."
	}
	return filepath.FromSlash(p)
}
