package store

import (
	"archive/zip"
	"encoding/hex"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/zeebo/blake3"
)

// subtree returns p and, for a folder, everything inside it, parents before children.
func (s *Store) subtree(p string) ([]Entry, error) {
	list, err := s.query(`SELECT `+cols+` FROM files WHERE path = ?1 OR substr(path, 1, length(?1) + 1) = ?1 || '/' ORDER BY path`, p)
	if err == nil && len(list) == 0 {
		err = ErrNotFound
	}
	return list, err
}

// tmpFile writes a new temp file with write and returns its path and BLAKE3 hash. The caller places or removes it.
func (s *Store) tmpFile(write func(io.Writer) error) (string, string, error) {
	tmp := tmpPath(randID())
	out, err := s.root.OpenFile(osPath(tmp), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o664)
	if err != nil {
		return "", "", err
	}
	h := blake3.New()
	err = write(io.MultiWriter(out, h))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		s.root.Remove(osPath(tmp))
		return "", "", err
	}
	return tmp, hex.EncodeToString(h.Sum(nil)), nil
}

// Copy duplicates from (a file, or a folder with everything inside) as to. Every file is copied to a
// temp file, hashed on the way, and placed like an upload, so nobody ever sees half a copy.
// The target must not exist (ConflictError for a file, ErrExists for a folder).
func (s *Store) Copy(from, to string) (Entry, error) {
	from, err1 := CleanPath(from)
	to, err2 := CleanPath(to)
	if err1 != nil || err2 != nil || from == "" || to == "" || strings.HasPrefix(to+"/", from+"/") {
		return Entry{}, ErrBadName
	}
	list, err := s.subtree(from)
	if err != nil {
		return Entry{}, err
	}
	if _, err := s.Stat(to); err == nil {
		return Entry{}, ErrExists
	}
	for _, e := range list {
		dst := to + e.Path[len(from):]
		if e.Dir {
			_, err = s.Mkdir(dst)
		} else {
			_, err = s.copyFile1(e.Path, dst)
		}
		if err != nil {
			return Entry{}, err
		}
	}
	return s.Stat(to)
}

func (s *Store) copyFile1(src, dst string) (Entry, error) {
	f, e, err := s.Open(src)
	if err != nil {
		return Entry{}, err
	}
	defer f.Close()
	tmp, sum, err := s.tmpFile(func(w io.Writer) error { _, err := io.Copy(w, f); return err })
	if err != nil {
		return Entry{}, err
	}
	out, err := s.place(tmp, dst, sum, 0, false, time.Unix(0, e.mtimeNs), s.now()) // a copy keeps its date and is born now, as in Explorer
	if err != nil {
		s.root.Remove(osPath(tmp))
	}
	return out, err
}

// stored are formats that are compressed already: deflating them again only costs NAS CPU.
var stored = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".avif": true, ".heic": true,
	".mp4": true, ".m4v": true, ".mov": true, ".mkv": true, ".webm": true, ".mp3": true, ".m4a": true,
	".ogg": true, ".opus": true, ".flac": true, ".aac": true, ".zip": true, ".7z": true, ".rar": true,
	".gz": true, ".xz": true, ".zst": true, ".docx": true, ".xlsx": true, ".pptx": true, ".3mf": true,
}

// Zip writes paths (files, or folders with everything inside) as one zip archive. Each keeps its own
// name at the top of the archive, so a folder unpacks as that folder.
func (s *Store) Zip(w io.Writer, paths []string) error {
	zw := zip.NewWriter(w)
	for _, p := range paths {
		p, err := CleanPath(p)
		if err != nil || p == "" {
			return ErrBadName
		}
		list, err := s.subtree(p)
		if err != nil {
			return err
		}
		cut := len(parentOf(p))
		if cut > 0 {
			cut++ // and the slash
		}
		for _, e := range list {
			hdr := &zip.FileHeader{Name: e.Path[cut:], Modified: time.Unix(0, e.mtimeNs), Method: zip.Deflate}
			if e.Dir {
				hdr.Name += "/"
				if _, err := zw.CreateHeader(hdr); err != nil {
					return err
				}
				continue
			}
			if stored[strings.ToLower(path.Ext(e.Path))] {
				hdr.Method = zip.Store
			}
			f, _, err := s.Open(e.Path)
			if err != nil {
				return err
			}
			fw, err := zw.CreateHeader(hdr)
			if err == nil {
				_, err = io.Copy(fw, f)
			}
			f.Close()
			if err != nil {
				return err
			}
		}
	}
	return zw.Close()
}

// Compress writes paths into a new zip file at to (which must not exist yet).
func (s *Store) Compress(paths []string, to string) (Entry, error) {
	to, err := CleanPath(to)
	if err != nil || to == "" || len(paths) == 0 {
		return Entry{}, ErrBadName
	}
	tmp, sum, err := s.tmpFile(func(w io.Writer) error { return s.Zip(w, paths) })
	if err != nil {
		return Entry{}, err
	}
	e, err := s.place(tmp, to, sum, 0, false, time.Time{}, time.Time{})
	if err != nil {
		s.root.Remove(osPath(tmp))
	}
	return e, err
}
