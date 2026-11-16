package store

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zeebo/blake3"
)

// MaxChunk bounds one upload request. The Vercel gateway path sends ≤ 4 MB anyway.
const MaxChunk = 64 << 20

const (
	retention    = 30 * 24 * time.Hour
	keepVersions = 10
	staleUpload  = 7 * 24 * time.Hour
)

// OffsetError means an upload chunk did not start where the server's copy ends; resume from Offset.
type OffsetError struct{ Offset int64 }

func (e *OffsetError) Error() string { return fmt.Sprintf("upload is at offset %d", e.Offset) }

type upload struct {
	mu   sync.Mutex
	size int64
	h    *blake3.Hasher // nil until rebuilt from the temp file (after a restart or a failed chunk)
}

func tmpPath(id string) string { return metaDir + "/tmp/" + id }

func validID(id string) bool {
	_, err := hex.DecodeString(id)
	return len(id) == 32 && err == nil
}

// BeginUpload starts a resumable upload and returns its id.
func (s *Store) BeginUpload() (string, error) {
	id := randID()
	f, err := s.root.OpenFile(osPath(tmpPath(id)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o664)
	if err != nil {
		return "", err
	}
	f.Close()
	s.umu.Lock()
	s.uploads[id] = &upload{h: blake3.New()}
	s.umu.Unlock()
	return id, nil
}

func (s *Store) getUpload(id string) (*upload, error) {
	if !validID(id) {
		return nil, ErrNotFound
	}
	if _, err := s.root.Stat(osPath(tmpPath(id))); err != nil {
		return nil, ErrNotFound
	}
	s.umu.Lock()
	defer s.umu.Unlock()
	u := s.uploads[id]
	if u == nil { // survived a server restart; hash state is rebuilt on first use
		u = &upload{}
		s.uploads[id] = u
	}
	return u, nil
}

// rehash rebuilds u's hash state from the temp file. Caller holds u.mu.
func (s *Store) rehash(id string, u *upload) error {
	f, err := s.root.Open(osPath(tmpPath(id)))
	if err != nil {
		return err
	}
	defer f.Close()
	h := blake3.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	u.h, u.size = h, n
	return nil
}

func (s *Store) UploadOffset(id string) (int64, error) {
	if _, err := s.getUpload(id); err != nil {
		return 0, err
	}
	fi, err := s.root.Stat(osPath(tmpPath(id)))
	if err != nil {
		return 0, ErrNotFound
	}
	return fi.Size(), nil
}

// Append writes a chunk that must start at the end of what the server already has.
func (s *Store) Append(id string, offset int64, r io.Reader) (int64, error) {
	u, err := s.getUpload(id)
	if err != nil {
		return 0, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.h == nil {
		if err := s.rehash(id, u); err != nil {
			return 0, err
		}
	}
	if offset != u.size {
		return u.size, &OffsetError{u.size}
	}
	f, err := s.root.OpenFile(osPath(tmpPath(id)), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return offset, err
	}
	defer f.Close()
	n, err := io.Copy(io.MultiWriter(f, u.h), io.LimitReader(r, MaxChunk))
	if err != nil {
		// Drop the partial chunk so file and hash state stay in step; the client retries from offset.
		f.Truncate(offset)
		u.h = nil
		return offset, err
	}
	u.size += n
	return u.size, nil
}

func (s *Store) AbortUpload(id string) error {
	if !validID(id) {
		return ErrNotFound
	}
	s.umu.Lock()
	delete(s.uploads, id)
	s.umu.Unlock()
	if err := s.root.Remove(osPath(tmpPath(id))); errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	} else {
		return err
	}
}

// Commit places a finished upload at p. baseRev is the revision the client edited (0 = new file);
// if the server's copy moved on since, nothing changes and a ConflictError is returned.
// created: when the file came to be on the device that sent it (zero: unknown).
func (s *Store) Commit(id, p string, baseRev int64, mtime, created time.Time) (Entry, error) {
	p, err := CleanPath(p)
	if err != nil || p == "" {
		return Entry{}, ErrBadName
	}
	u, err := s.getUpload(id)
	if err != nil {
		return Entry{}, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.h == nil {
		if err := s.rehash(id, u); err != nil {
			return Entry{}, err
		}
	}
	e, err := s.place(tmpPath(id), p, hex.EncodeToString(u.h.Sum(nil)), baseRev, false, mtime, created)
	if err == nil {
		s.umu.Lock()
		delete(s.uploads, id)
		s.umu.Unlock()
	}
	return e, err
}

// place moves the finished temp file tmp to p in one atomic rename. The content it replaces stays
// reachable as a version (a hard link, so p never disappears). force skips the revision check.
func (s *Store) place(tmp, p, sum string, baseRev int64, force bool, mtime, created time.Time) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isDir(parentOf(p)) {
		return Entry{}, ErrNoParent
	}
	cur, err := s.Stat(p)
	exists := err == nil
	switch {
	case err != nil && !errors.Is(err, ErrNotFound):
		return Entry{}, err
	case exists && cur.Dir:
		return Entry{}, ErrIsDir
	case !force && exists && cur.Rev != baseRev:
		return Entry{}, &ConflictError{Current: &cur}
	case !force && !exists && baseRev != 0:
		return Entry{}, &ConflictError{}
	}
	if err := s.checkCase(p, ""); err != nil {
		return Entry{}, err
	}
	if f, err := s.root.OpenFile(osPath(tmp), os.O_RDWR, 0); err == nil {
		f.Sync()
		f.Close()
	}
	version := ""
	if exists {
		version = randID()
		if err := s.root.Link(osPath(p), osPath(metaDir+"/versions/"+version)); err != nil {
			return Entry{}, fmt.Errorf("keep version: %w", err)
		}
	}
	if err := s.root.Rename(osPath(tmp), osPath(p)); err != nil {
		return Entry{}, err
	}
	if !mtime.IsZero() {
		s.root.Chtimes(osPath(p), mtime, mtime)
	}
	s.syncDir(parentOf(p))
	fi, err := s.root.Stat(osPath(p))
	if err != nil {
		return Entry{}, err
	}
	e := Entry{Path: p, Size: fi.Size(), mtimeNs: fi.ModTime().UnixNano(), Hash: sum}
	switch {
	case exists:
		e.Created = cur.Created // replacing the content keeps the file's birth
	case !created.IsZero():
		e.Created = created.UnixMilli()
	default:
		e.Created = e.mtimeNs / 1e6
	}
	err = s.commitTx(func(tx *sql.Tx) error {
		if e.Rev, err = record(tx, "put", p, ""); err != nil {
			return err
		}
		if version != "" {
			if _, err := tx.Exec(`INSERT INTO versions(id, path, created, size, hash) VALUES(?, ?, ?, ?, ?)`,
				version, p, s.now().UnixNano(), cur.Size, cur.Hash); err != nil {
				return err
			}
		}
		return upsert(tx, e)
	})
	e.Mtime = e.mtimeNs / 1e6
	return e, err
}

// Mkdir creates folder p. Creating an existing folder is not an error.
func (s *Store) Mkdir(p string) (Entry, error) {
	p, err := CleanPath(p)
	if err != nil || p == "" {
		return Entry{}, ErrBadName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, err := s.Stat(p); err == nil {
		if e.Dir {
			return e, nil
		}
		return Entry{}, ErrExists
	}
	if !s.isDir(parentOf(p)) {
		return Entry{}, ErrNoParent
	}
	if err := s.checkCase(p, ""); err != nil {
		return Entry{}, err
	}
	if err := s.root.Mkdir(osPath(p), 0o775); err != nil && !errors.Is(err, fs.ErrExist) {
		return Entry{}, err
	}
	fi, err := s.root.Lstat(osPath(p))
	if err != nil || !fi.IsDir() {
		return Entry{}, ErrExists
	}
	e := Entry{Path: p, Dir: true, mtimeNs: fi.ModTime().UnixNano()}
	err = s.commitTx(func(tx *sql.Tx) error {
		if e.Rev, err = record(tx, "mkdir", p, ""); err != nil {
			return err
		}
		return upsert(tx, e)
	})
	e.Mtime = e.mtimeNs / 1e6
	return e, err
}

// Move renames from to to; folders move with everything inside. baseRev 0 skips the revision check.
func (s *Store) Move(from, to string, baseRev int64) (Entry, error) {
	from, err1 := CleanPath(from)
	to, err2 := CleanPath(to)
	if err1 != nil || err2 != nil || from == "" || to == "" || strings.HasPrefix(to+"/", from+"/") && to != from {
		return Entry{}, ErrBadName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.Stat(from)
	if err != nil {
		return Entry{}, err
	}
	if to == from {
		return cur, nil
	}
	if baseRev != 0 && cur.Rev != baseRev {
		return Entry{}, &ConflictError{Current: &cur}
	}
	if !s.isDir(parentOf(to)) {
		return Entry{}, ErrNoParent
	}
	if _, err := s.Stat(to); err == nil {
		return Entry{}, ErrExists
	}
	if err := s.checkCase(to, from); err != nil {
		return Entry{}, err
	}
	if err := s.root.Rename(osPath(from), osPath(to)); err != nil {
		return Entry{}, err
	}
	s.syncDir(parentOf(to))
	err = s.commitTx(func(tx *sql.Tx) error {
		seq, err := record(tx, "move", to, from)
		if err != nil {
			return err
		}
		// SET expressions see the old row, so "path = ?2" still identifies the moved entry itself.
		if _, err := tx.Exec(`UPDATE files SET
			path   = ?1 || substr(path, length(?2) + 1),
			lpath  = ?3 || substr(lpath, length(?4) + 1),
			parent = CASE WHEN path = ?2 THEN ?5 ELSE ?1 || substr(parent, length(?2) + 1) END,
			rev    = CASE WHEN path = ?2 THEN ?6 ELSE rev END
			WHERE path = ?2 OR substr(path, 1, length(?2) + 1) = ?2 || '/'`,
			to, from, strings.ToLower(to), strings.ToLower(from), parentOf(to), seq); err != nil {
			return err
		}
		for _, table := range []string{"versions", "shares"} { // history and share links follow the file
			if _, err := tx.Exec(`UPDATE `+table+` SET path = ?1 || substr(path, length(?2) + 1)
				WHERE path = ?2 OR substr(path, 1, length(?2) + 1) = ?2 || '/'`, to, from); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Entry{}, err
	}
	return s.Stat(to)
}

// Delete moves p (a file, or a folder with everything inside) to the trash.
func (s *Store) Delete(p string, baseRev int64) error {
	p, err := CleanPath(p)
	if err != nil || p == "" {
		return ErrBadName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.Stat(p)
	if err != nil {
		return err
	}
	if baseRev != 0 && cur.Rev != baseRev {
		return &ConflictError{Current: &cur}
	}
	id := randID()
	if err := s.root.Rename(osPath(p), osPath(metaDir+"/trash/"+id)); err != nil {
		return err
	}
	s.syncDir(parentOf(p))
	return s.commitTx(func(tx *sql.Tx) error {
		if _, err := record(tx, "delete", p, ""); err != nil {
			return err
		}
		if err := deleteTree(tx, p); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO trash(id, path, dir, size, deleted) VALUES(?, ?, ?, ?, ?)`, id, p, cur.Dir, cur.Size, s.now().UnixNano())
		return err
	})
}

type Version struct {
	ID      string `json:"id"`
	Created int64  `json:"created"` // unix milliseconds
	Size    int64  `json:"size"`
	Hash    string `json:"hash"`
}

func (s *Store) Versions(p string) ([]Version, error) {
	rows, err := s.db.Query(`SELECT id, created / 1000000, size, hash FROM versions WHERE path = ? ORDER BY created DESC`, p)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Version{}
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.ID, &v.Created, &v.Size, &v.Hash); err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	return list, rows.Err()
}

// OpenVersion returns the content of version id of p, for downloading.
func (s *Store) OpenVersion(p, id string) (*os.File, Version, error) {
	var v Version
	if err := s.db.QueryRow(`SELECT id, created / 1000000, size, hash FROM versions WHERE id = ? AND path = ?`, id, p).
		Scan(&v.ID, &v.Created, &v.Size, &v.Hash); err != nil {
		return nil, v, ErrNotFound
	}
	f, err := s.root.Open(osPath(metaDir + "/versions/" + v.ID))
	if err != nil {
		return nil, v, ErrNotFound
	}
	return f, v, nil
}

// RestoreVersion makes version id the current content of p; what it replaces becomes a version too.
func (s *Store) RestoreVersion(p, id string) (Entry, error) {
	var hash string
	if err := s.db.QueryRow(`SELECT hash FROM versions WHERE id = ? AND path = ?`, id, p).Scan(&hash); err != nil {
		return Entry{}, ErrNotFound
	}
	tmp := tmpPath(randID())
	if err := s.copyFile(metaDir+"/versions/"+id, tmp); err != nil {
		return Entry{}, err
	}
	e, err := s.place(tmp, p, hash, 0, true, time.Time{}, time.Time{})
	if err != nil {
		s.root.Remove(osPath(tmp))
	}
	return e, err
}

func (s *Store) copyFile(src, dst string) error {
	in, err := s.root.Open(osPath(src))
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := s.root.OpenFile(osPath(dst), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o664)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

type Trashed struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Dir     bool   `json:"dir,omitempty"`
	Size    int64  `json:"size"`
	Deleted int64  `json:"deleted"` // unix milliseconds
}

func (s *Store) Trash() ([]Trashed, error) {
	rows, err := s.db.Query(`SELECT id, path, dir, size, deleted / 1000000 FROM trash ORDER BY deleted DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Trashed{}
	for rows.Next() {
		var t Trashed
		if err := rows.Scan(&t.ID, &t.Path, &t.Dir, &t.Size, &t.Deleted); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// RestoreTrash puts a trashed item back at its old path, or at to if given, and returns where it went.
// The restored files are indexed by a scan of the target folder.
func (s *Store) RestoreTrash(id, to string) (string, error) {
	s.mu.Lock()
	var orig string
	if err := s.db.QueryRow(`SELECT path FROM trash WHERE id = ?`, id).Scan(&orig); err != nil {
		s.mu.Unlock()
		return "", ErrNotFound
	}
	if to == "" {
		to = orig
	}
	to, err := CleanPath(to)
	if err == nil && to == "" {
		err = ErrBadName
	}
	if err == nil && !s.isDir(parentOf(to)) {
		err = ErrNoParent
	}
	if err == nil {
		if _, serr := s.Stat(to); serr == nil {
			err = ErrExists
		}
	}
	if err == nil {
		err = s.checkCase(to, "")
	}
	if err == nil {
		err = s.root.Rename(osPath(metaDir+"/trash/"+id), osPath(to))
	}
	if err == nil {
		_, err = s.db.Exec(`DELETE FROM trash WHERE id = ?`, id)
	}
	s.mu.Unlock()
	if err != nil {
		return "", err
	}
	return to, s.Scan(parentOf(to), false)
}

// Purge applies retention: versions older than 30 days beyond the newest 10 per file,
// trash older than 30 days, and uploads untouched for 7 days.
func (s *Store) Purge() error {
	now := s.now()
	cutoff := now.Add(-retention).UnixNano()
	var versions, trash []string
	s.mu.Lock()
	err := s.commitTx(func(tx *sql.Tx) error {
		var err error
		if versions, err = ids(tx, `SELECT id FROM versions v WHERE created < ?1 AND
			(SELECT count(*) FROM versions w WHERE w.path = v.path AND w.created > v.created) >= ?2`, cutoff, keepVersions); err != nil {
			return err
		}
		if trash, err = ids(tx, `SELECT id FROM trash WHERE deleted < ?`, cutoff); err != nil {
			return err
		}
		for _, id := range versions {
			if _, err := tx.Exec(`DELETE FROM versions WHERE id = ?`, id); err != nil {
				return err
			}
		}
		for _, id := range trash {
			if _, err := tx.Exec(`DELETE FROM trash WHERE id = ?`, id); err != nil {
				return err
			}
		}
		_, err = tx.Exec(`DELETE FROM shares WHERE expires < ? OR (views = 0 AND downloads = 0)`, now.UnixMilli())
		return err
	})
	s.mu.Unlock()
	if err != nil {
		return err
	}
	// Files go after the rows, outside the lock: a large trashed folder can take a while.
	for _, id := range versions {
		s.root.Remove(osPath(metaDir + "/versions/" + id))
	}
	for _, id := range trash {
		s.root.RemoveAll(osPath(metaDir + "/trash/" + id))
	}
	if f, err := s.root.Open(osPath(metaDir + "/tmp")); err == nil {
		entries, _ := f.ReadDir(-1)
		f.Close()
		for _, de := range entries {
			if fi, err := de.Info(); err == nil && now.Sub(fi.ModTime()) > staleUpload {
				s.AbortUpload(de.Name())
			}
		}
	}
	return nil
}

func ids(tx *sql.Tx, q string, args ...any) ([]string, error) {
	rows, err := tx.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		list = append(list, id)
	}
	return list, rows.Err()
}
