package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/zeebo/blake3"
)

// Scan reconciles the index with folder dir on disk and journals what changed outside Aetheris
// (SMB, File Station). Hashing runs outside the write lock; each folder is applied in one short
// transaction after re-checking the disk. New folders are always scanned fully; existing ones only
// when recursive is set.
func (s *Store) Scan(dir string, recursive bool) error {
	disk, err := s.readDisk(dir)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrNotDir) {
		return nil // the parent's scan records its removal
	}
	if err != nil {
		return err
	}
	idx, err := s.List(dir)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotDir) {
		return nil
	}
	if err != nil {
		return err
	}
	known := map[string]Entry{}
	for _, e := range idx {
		known[e.Path] = e
	}

	type put struct {
		e   Entry
		was *Entry
	}
	var puts []put
	var mkdirs []Entry
	var dels []string
	var subdirs []string
	fresh := map[string]bool{}
	for name, fi := range disk {
		p := join(dir, name)
		e, ok := known[p]
		if fi.IsDir() {
			subdirs = append(subdirs, p)
			if !ok || !e.Dir {
				if ok {
					dels = append(dels, p)
				}
				mkdirs = append(mkdirs, Entry{Path: p, Dir: true, mtimeNs: fi.ModTime().UnixNano()})
				fresh[p] = true
			}
			continue
		}
		if ok && !e.Dir && e.Size == fi.Size() && e.mtimeNs == fi.ModTime().UnixNano() {
			continue
		}
		if ok && e.Dir {
			dels = append(dels, p)
		}
		sum, err := s.hashFile(p)
		if err != nil {
			s.log.Warn("scan: hash", "path", p, "err", err)
			continue
		}
		var was *Entry
		if ok {
			was = &e
		}
		puts = append(puts, put{Entry{Path: p, Size: fi.Size(), mtimeNs: fi.ModTime().UnixNano(), Hash: sum}, was})
	}
	for p := range known {
		if _, ok := disk[lastName(p)]; !ok {
			dels = append(dels, p)
		}
	}

	if len(puts)+len(mkdirs)+len(dels) > 0 {
		s.mu.Lock()
		err := s.commitTx(func(tx *sql.Tx) error {
			for _, p := range dels {
				cur, err := s.Stat(p)
				if err != nil {
					continue // already gone
				}
				if fi, err := s.root.Lstat(osPath(p)); err == nil && fi.IsDir() == cur.Dir {
					continue // back, or replaced through Aetheris meanwhile
				}
				if _, err := record(tx, "delete", p, ""); err != nil {
					return err
				}
				if err := deleteTree(tx, p); err != nil {
					return err
				}
			}
			for _, e := range mkdirs {
				if fi, err := s.root.Lstat(osPath(e.Path)); err != nil || !fi.IsDir() {
					continue
				}
				var err error
				if e.Rev, err = record(tx, "mkdir", e.Path, ""); err != nil {
					return err
				}
				if err := upsert(tx, e); err != nil {
					return err
				}
			}
			for _, pu := range puts {
				// Skip if the file changed again while hashing (the next scan gets it) or Aetheris wrote it meanwhile.
				fi, err := s.root.Lstat(osPath(pu.e.Path))
				if err != nil || fi.Size() != pu.e.Size || fi.ModTime().UnixNano() != pu.e.mtimeNs {
					continue
				}
				if cur, err := s.Stat(pu.e.Path); err == nil && (pu.was == nil || cur.Rev != pu.was.Rev) {
					continue
				}
				e := pu.e
				if e.Rev, err = record(tx, "put", e.Path, ""); err != nil {
					return err
				}
				if err := upsert(tx, e); err != nil {
					return err
				}
			}
			return nil
		})
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
	for _, d := range subdirs {
		if recursive || fresh[d] {
			if err := s.Scan(d, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func lastName(p string) string { return p[strings.LastIndexByte(p, '/')+1:] }

// readDisk lists folder dir, skipping internal folders, symlinks and devices (never followed or synced),
// and names Windows cannot store.
func (s *Store) readDisk(dir string) (map[string]fs.FileInfo, error) {
	f, err := s.root.Open(osPath(dir))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, ErrNotDir
	}
	disk := map[string]fs.FileInfo{}
	for _, de := range entries {
		if ignoredNames[de.Name()] {
			continue
		}
		if !validName(de.Name()) {
			s.log.Debug("scan: skipping name Windows cannot store", "path", join(dir, de.Name()))
			continue
		}
		fi, err := de.Info()
		if err != nil || !(fi.IsDir() || fi.Mode().IsRegular()) {
			continue
		}
		disk[de.Name()] = fi
	}
	return disk, nil
}

func (s *Store) hashFile(p string) (string, error) {
	f, err := s.root.Open(osPath(p))
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := blake3.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Run keeps the index honest until ctx ends: a full scan at start and every rescan interval,
// debounced folder scans whenever the OS reports a change, and retention once a day.
func (s *Store) Run(ctx context.Context, rescan time.Duration) {
	if err := s.Scan("", true); err != nil {
		s.log.Error("initial scan", "err", err)
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		s.log.Error("watcher unavailable; relying on periodic scans", "err", err)
	} else {
		defer w.Close()
		s.watchTree(w, "")
	}
	var events <-chan fsnotify.Event
	var errs <-chan error
	if w != nil {
		events, errs = w.Events, w.Errors
	}
	dirty := map[string]bool{}
	overflow := false // the watcher lost events: only a full scan is safe
	debounce := time.NewTimer(time.Hour)
	debounce.Stop()
	full := time.NewTicker(rescan)
	defer full.Stop()
	daily := time.NewTicker(24 * time.Hour)
	defer daily.Stop()
	s.Purge()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-events:
			rel, ok := s.rel(ev.Name)
			if !ok {
				continue
			}
			dirty[parentOf(rel)] = true
			if ev.Has(fsnotify.Create) {
				if fi, err := s.root.Lstat(osPath(rel)); err == nil && fi.IsDir() {
					// Never from this loop: on Windows, Add waits for fsnotify's reader, and the reader may be
					// waiting for this loop to take its next event. That deadlock froze the dev server.
					go s.watchTree(w, rel)
				}
			}
			debounce.Reset(2 * time.Second) // let a burst (a big SMB copy) settle first
		case err := <-errs:
			s.log.Warn("watcher", "err", err)
			overflow = true
			debounce.Reset(2 * time.Second)
		case <-debounce.C:
			if overflow {
				if err := s.Scan("", true); err != nil {
					s.log.Error("full scan", "err", err)
				}
			}
			for d := range dirty {
				if overflow {
					break
				}
				if err := s.Scan(d, false); err != nil {
					s.log.Error("scan", "dir", d, "err", err)
				}
			}
			clear(dirty)
			overflow = false
		case <-full.C:
			if err := s.Scan("", true); err != nil {
				s.log.Error("full scan", "err", err)
			}
		case <-daily.C:
			if err := s.Purge(); err != nil {
				s.log.Error("purge", "err", err)
			}
		}
	}
}

// rel turns a watcher path into a root-relative slash path; false for anything internal or outside.
func (s *Store) rel(abs string) (string, bool) {
	r, err := filepath.Rel(s.dir, abs)
	if err != nil || r == "." || strings.HasPrefix(r, "..") {
		return "", false
	}
	r = filepath.ToSlash(r)
	first, _, _ := strings.Cut(r, "/")
	return r, !ignoredNames[first]
}

// watchTree adds a watch for dir and every folder below it.
func (s *Store) watchTree(w *fsnotify.Watcher, dir string) {
	if err := w.Add(filepath.Join(s.dir, osPath(dir))); err != nil {
		s.log.Warn("watch", "dir", dir, "err", err) // e.g. inotify limit reached: periodic scans still cover it
		return
	}
	disk, err := s.readDisk(dir)
	if err != nil {
		return
	}
	for name, fi := range disk {
		if fi.IsDir() {
			s.watchTree(w, join(dir, name))
		}
	}
}
