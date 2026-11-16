// Package store keeps Aetheris's files as plain files under one root folder, with a SQLite index,
// a change journal, versions and trash beside them in .aetheris/. Every file access goes through
// os.Root, so no client path can escape the root, symlinks included.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
	ErrNoParent = errors.New("parent folder does not exist")
	ErrIsDir    = errors.New("is a folder")
	ErrNotDir   = errors.New("not a folder")
	ErrEpoch    = errors.New("index was rebuilt; resync from the full tree")
)

// ConflictError means the client's base revision is stale. Current is the server's entry, nil if none.
type ConflictError struct{ Current *Entry }

func (e *ConflictError) Error() string { return "changed on the server" }

// Entry is one file or folder. Rev is the journal sequence of its last change and serves as its ETag.
type Entry struct {
	Path    string `json:"path"`
	Dir     bool   `json:"dir,omitempty"`
	Size    int64  `json:"size"`
	Mtime   int64  `json:"mtime"` // unix milliseconds
	Hash    string `json:"hash,omitempty"`
	Rev     int64  `json:"rev"`
	// Unix milliseconds: when the item came to be. Uploads from Windows bring the file's own creation time; anything
	// else starts at its modification time. Edits and moves keep it.
	Created int64 `json:"created,omitempty"`
	mtimeNs int64
}

// Change is one journal record. Entry is the path's current state (nil once it is gone).
type Change struct {
	Seq   int64  `json:"seq"`
	Op    string `json:"op"` // put | mkdir | delete | move
	Path  string `json:"path"`
	From  string `json:"from,omitempty"`
	Entry *Entry `json:"entry,omitempty"`
}

type Store struct {
	dir   string
	root  *os.Root
	db    *sql.DB
	log   *slog.Logger
	epoch string
	now   func() time.Time

	mu sync.Mutex // serializes every change to files and index

	cmu     sync.Mutex
	changed chan struct{} // closed after each commit to wake long-polls

	umu     sync.Mutex
	uploads map[string]*upload
}

var migrations = []string{`
CREATE TABLE files (
  path   TEXT PRIMARY KEY,
  parent TEXT NOT NULL,
  lpath  TEXT NOT NULL,     -- lowercase path: Windows clients cannot hold two names differing only in case
  dir    INTEGER NOT NULL,
  size   INTEGER NOT NULL,
  mtime  INTEGER NOT NULL,  -- unix nanoseconds, as on disk
  hash   TEXT NOT NULL,     -- BLAKE3 hex, '' for folders
  rev    INTEGER NOT NULL
);
CREATE INDEX files_parent ON files(parent);
CREATE INDEX files_lpath ON files(lpath);
CREATE TABLE journal (seq INTEGER PRIMARY KEY AUTOINCREMENT, op TEXT NOT NULL, path TEXT NOT NULL, from_path TEXT NOT NULL DEFAULT '');
CREATE TABLE versions (id TEXT PRIMARY KEY, path TEXT NOT NULL, created INTEGER NOT NULL, size INTEGER NOT NULL, hash TEXT NOT NULL);
CREATE INDEX versions_path ON versions(path, created);
CREATE TABLE trash (id TEXT PRIMARY KEY, path TEXT NOT NULL, dir INTEGER NOT NULL, size INTEGER NOT NULL, deleted INTEGER NOT NULL);
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`, `
CREATE INDEX files_mtime ON files(mtime);
`, `
CREATE TABLE shares (id TEXT PRIMARY KEY, key TEXT NOT NULL, path TEXT NOT NULL, created INTEGER NOT NULL,
  expires INTEGER NOT NULL, views INTEGER NOT NULL, downloads INTEGER NOT NULL); -- times in unix ms; -1 = unlimited
CREATE INDEX shares_path ON shares(path);
`, `
ALTER TABLE files ADD COLUMN created INTEGER NOT NULL DEFAULT 0; -- unix ms
UPDATE files SET created = mtime / 1000000;
`}

// Open serves dir, creating .aetheris/ and the index on first use.
func Open(dir string, log *slog.Logger) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	for _, d := range []string{metaDir, metaDir + "/tmp", metaDir + "/versions", metaDir + "/trash"} {
		if err := root.MkdirAll(osPath(d), 0o700); err != nil {
			root.Close()
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(abs, metaDir, "index.db")+
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		root.Close()
		return nil, err
	}
	s := &Store{dir: abs, root: root, db: db, log: log, now: time.Now, changed: make(chan struct{}), uploads: map[string]*upload{}}
	if err := s.migrate(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return errors.Join(s.db.Close(), s.root.Close())
}

func (s *Store) Epoch() string { return s.epoch }

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	for ; v < len(migrations); v++ {
		if _, err := s.db.Exec(migrations[v] + fmt.Sprintf(";PRAGMA user_version = %d", v+1)); err != nil {
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
	}
	// The epoch identifies this index; clients holding a cursor from another epoch must resync.
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'epoch'`).Scan(&s.epoch)
	if errors.Is(err, sql.ErrNoRows) {
		s.epoch = randID()
		_, err = s.db.Exec(`INSERT INTO meta(key, value) VALUES('epoch', ?)`, s.epoch)
	}
	return err
}

func randID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

const cols = `path, dir, size, mtime, hash, rev, created`

func scanEntry(row interface{ Scan(...any) error }) (Entry, error) {
	var e Entry
	err := row.Scan(&e.Path, &e.Dir, &e.Size, &e.mtimeNs, &e.Hash, &e.Rev, &e.Created)
	e.Mtime = e.mtimeNs / 1e6
	return e, err
}

func (s *Store) Stat(p string) (Entry, error) {
	e, err := scanEntry(s.db.QueryRow(`SELECT `+cols+` FROM files WHERE path = ?`, p))
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// ErrBusy means a file keeps changing under us (e.g. an SMB copy in progress).
var ErrBusy = errors.New("file is changing; try again")

// Open returns p's content with the index entry that describes exactly those bytes, so an ETag
// never labels different content. If the disk is ahead of the index, the folder is rescanned first.
func (s *Store) Open(p string) (*os.File, Entry, error) {
	for range 3 {
		e, err := s.Stat(p)
		if err != nil {
			return nil, e, err
		}
		if e.Dir {
			return nil, e, ErrIsDir
		}
		f, err := s.root.Open(osPath(p))
		if err != nil {
			return nil, e, ErrNotFound
		}
		if fi, err := f.Stat(); err == nil && fi.Size() == e.Size && fi.ModTime().UnixNano() == e.mtimeNs {
			return f, e, nil
		}
		f.Close()
		if err := s.Scan(parentOf(p), false); err != nil {
			return nil, e, err
		}
	}
	return nil, Entry{}, ErrBusy
}

func (s *Store) isDir(p string) bool {
	e, err := s.Stat(p)
	return p == "" || (err == nil && e.Dir)
}

// List returns the children of folder dir ("" is the root), folders first.
func (s *Store) List(dir string) ([]Entry, error) {
	if dir != "" {
		e, err := s.Stat(dir)
		if err != nil {
			return nil, err
		}
		if !e.Dir {
			return nil, ErrNotDir
		}
	}
	list, err := s.query(`SELECT `+cols+` FROM files WHERE parent = ? ORDER BY dir DESC, lpath`, dir)
	if err != nil {
		return nil, err
	}
	// A folder's size is everything inside it ('0' follows '/', so the range is exactly the folder's contents).
	for i := range list {
		if list[i].Dir {
			s.db.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM files WHERE path > ?1 || '/' AND path < ?1 || '0' AND dir = 0`, list[i].Path).Scan(&list[i].Size)
		}
	}
	return list, nil
}

// Recent returns the most recently modified files, newest first.
func (s *Store) Recent(limit int) ([]Entry, error) {
	return s.query(`SELECT `+cols+` FROM files WHERE dir = 0 ORDER BY mtime DESC LIMIT ?`, limit)
}

// Search matches q against whole paths, case-insensitively.
func (s *Store) Search(q string, limit int) ([]Entry, error) {
	return s.query(`SELECT `+cols+` FROM files WHERE instr(lpath, ?) > 0 ORDER BY dir DESC, lpath LIMIT ?`, strings.ToLower(q), limit)
}

func (s *Store) query(q string, args ...any) ([]Entry, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, rows.Err()
}

// Walk streams every entry, parents before children, from one consistent snapshot.
// The returned cursor is where /changes continues from that snapshot.
func (s *Store) Walk(ctx context.Context, fn func(Entry) error) (cursor int64, err error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := tx.QueryRow(`SELECT coalesce(max(seq), 0) FROM journal`).Scan(&cursor); err != nil {
		return 0, err
	}
	rows, err := tx.Query(`SELECT ` + cols + ` FROM files ORDER BY path`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return 0, err
		}
		if err := fn(e); err != nil {
			return 0, err
		}
	}
	return cursor, rows.Err()
}

// Head is where a client that only wants changes from now on starts (the web UI; sync clients use Walk).
func (s *Store) Head() (string, int64, error) {
	var cursor int64
	err := s.db.QueryRow(`SELECT coalesce(max(seq), 0) FROM journal`).Scan(&cursor)
	return s.epoch, cursor, err
}

// Changes returns journal records after cursor, waiting up to wait for the first one.
func (s *Store) Changes(ctx context.Context, epoch string, cursor int64, wait time.Duration) (int64, []Change, error) {
	if epoch != s.epoch {
		return 0, nil, ErrEpoch
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		s.cmu.Lock()
		ch := s.changed // taken before the query so no commit can slip between query and wait
		s.cmu.Unlock()
		next, list, err := s.changesSince(cursor)
		if err != nil || len(list) > 0 || wait <= 0 {
			return next, list, err
		}
		select {
		case <-ch:
		case <-timer.C:
			wait = 0
		case <-ctx.Done():
			return cursor, nil, nil
		}
	}
}

func (s *Store) changesSince(cursor int64) (int64, []Change, error) {
	var last int64
	if err := s.db.QueryRow(`SELECT coalesce(max(seq), 0) FROM journal`).Scan(&last); err != nil {
		return cursor, nil, err
	}
	if cursor > last {
		return 0, nil, ErrEpoch
	}
	rows, err := s.db.Query(`SELECT seq, op, path, from_path FROM journal WHERE seq > ? ORDER BY seq LIMIT 1000`, cursor)
	if err != nil {
		return cursor, nil, err
	}
	var list []Change
	for rows.Next() {
		var c Change
		if err := rows.Scan(&c.Seq, &c.Op, &c.Path, &c.From); err != nil {
			rows.Close()
			return cursor, nil, err
		}
		list = append(list, c)
		cursor = c.Seq
	}
	rows.Close()
	for i := range list {
		if e, err := s.Stat(list[i].Path); err == nil {
			list[i].Entry = &e
		}
	}
	return cursor, list, rows.Err()
}

// commitTx runs fn in one transaction and wakes waiting long-polls. Callers hold s.mu.
func (s *Store) commitTx(fn func(tx *sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.cmu.Lock()
	close(s.changed)
	s.changed = make(chan struct{})
	s.cmu.Unlock()
	return nil
}

func record(tx *sql.Tx, op, p, from string) (int64, error) {
	r, err := tx.Exec(`INSERT INTO journal(op, path, from_path) VALUES(?, ?, ?)`, op, p, from)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func upsert(tx *sql.Tx, e Entry) error {
	created := e.Created
	if created == 0 {
		created = e.mtimeNs / 1e6
	}
	// An existing item keeps its created time: an edit is not a new file.
	_, err := tx.Exec(`INSERT INTO files(path, parent, lpath, dir, size, mtime, hash, rev, created) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET dir = excluded.dir, size = excluded.size, mtime = excluded.mtime, hash = excluded.hash, rev = excluded.rev`,
		e.Path, parentOf(e.Path), strings.ToLower(e.Path), e.Dir, e.Size, e.mtimeNs, e.Hash, e.Rev, created)
	return err
}

func deleteTree(tx *sql.Tx, p string) error {
	_, err := tx.Exec(`DELETE FROM files WHERE path = ?1 OR substr(path, 1, length(?1) + 1) = ?1 || '/'`, p)
	return err
}

// checkCase rejects p when a sibling differs from it only in case (skip names p itself, e.g. for a case-only rename).
func (s *Store) checkCase(p, skip string) error {
	var other string
	err := s.db.QueryRow(`SELECT path FROM files WHERE lpath = ? AND path != ? AND path != ?`, strings.ToLower(p), p, skip).Scan(&other)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err == nil:
		return fmt.Errorf("%w: %q differs only in case", ErrExists, path.Base(other))
	}
	return err
}

// syncDir makes a rename in folder p durable (best effort: not every OS can fsync a folder).
func (s *Store) syncDir(p string) {
	if f, err := s.root.Open(osPath(p)); err == nil {
		f.Sync()
		f.Close()
	}
}
