package store

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"time"
)

// Share is a link that opens one file without an account. Key encrypts what the link serves; it lives
// in the link's #fragment, so browsers never send it and no server or log between ever sees it.
type Share struct {
	ID        string `json:"id"`
	Key       string `json:"key"` // AES-256, base64url; only the owner's API returns it
	Path      string `json:"path"`
	Created   int64  `json:"created"` // unix milliseconds
	Expires   int64  `json:"expires"`
	Views     int64  `json:"views"`     // left; -1 = unlimited
	Downloads int64  `json:"downloads"` // left; -1 = unlimited
}

const shareCols = `id, key, path, created, expires, views, downloads`

func scanShare(row interface{ Scan(...any) error }) (Share, error) {
	var sh Share
	err := row.Scan(&sh.ID, &sh.Key, &sh.Path, &sh.Created, &sh.Expires, &sh.Views, &sh.Downloads)
	return sh, err
}

// CreateShare makes a link to file p, valid for ttl and the given number of views and downloads.
func (s *Store) CreateShare(p string, views, downloads int64, ttl time.Duration) (Share, error) {
	p, err := CleanPath(p)
	if err != nil || p == "" || views < -1 || downloads < -1 || views == 0 && downloads == 0 || ttl <= 0 {
		return Share{}, ErrBadName
	}
	e, err := s.Stat(p)
	if err != nil {
		return Share{}, err
	}
	if e.Dir {
		return Share{}, ErrIsDir
	}
	key := make([]byte, 32)
	rand.Read(key)
	now := s.now()
	sh := Share{ID: randID(), Key: base64.RawURLEncoding.EncodeToString(key), Path: p,
		Created: now.UnixMilli(), Expires: now.Add(ttl).UnixMilli(), Views: views, Downloads: downloads}
	_, err = s.db.Exec(`INSERT INTO shares(`+shareCols+`) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		sh.ID, sh.Key, sh.Path, sh.Created, sh.Expires, sh.Views, sh.Downloads)
	return sh, err
}

// Shares lists the links that still work, newest first.
func (s *Store) Shares() ([]Share, error) {
	rows, err := s.db.Query(`SELECT `+shareCols+` FROM shares WHERE expires > ? AND (views != 0 OR downloads != 0) ORDER BY created DESC`, s.now().UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Share{}
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, sh)
	}
	return list, rows.Err()
}

func (s *Store) RevokeShare(id string) (Share, error) {
	sh, err := scanShare(s.db.QueryRow(`SELECT `+shareCols+` FROM shares WHERE id = ?`, id))
	if err != nil {
		return sh, ErrNotFound
	}
	_, err = s.db.Exec(`DELETE FROM shares WHERE id = ?`, id)
	return sh, err
}

// GetShare returns a link that still works and the file it opens. Expired, used up, revoked, or
// its file gone: all ErrNotFound, so a visitor learns nothing about which.
func (s *Store) GetShare(id string) (Share, Entry, error) {
	sh, err := scanShare(s.db.QueryRow(`SELECT `+shareCols+` FROM shares WHERE id = ? AND expires > ? AND (views != 0 OR downloads != 0)`, id, s.now().UnixMilli()))
	if err != nil {
		return sh, Entry{}, ErrNotFound
	}
	e, err := s.Stat(sh.Path)
	if err != nil || e.Dir {
		return sh, e, ErrNotFound
	}
	return sh, e, nil
}

// UseShare spends one view or download (mode) and opens the file. Call done(false) if sending failed:
// that gives the view or download back, so a dropped connection doesn't burn a one-time link.
func (s *Store) UseShare(id, mode string) (f *os.File, e Entry, sh Share, done func(ok bool), err error) {
	col := map[string]string{"view": "views", "download": "downloads"}[mode] // never user text in SQL
	if col == "" {
		return nil, e, sh, nil, ErrBadName
	}
	res, err := s.db.Exec(`UPDATE shares SET `+col+` = `+col+` - (`+col+` > 0) WHERE id = ? AND expires > ? AND `+col+` != 0`, id, s.now().UnixMilli())
	if err != nil {
		return nil, e, sh, nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, e, sh, nil, ErrNotFound
	}
	refund := func() { s.db.Exec(`UPDATE shares SET `+col+` = `+col+` + 1 WHERE id = ? AND `+col+` >= 0`, id) }
	sh, err = scanShare(s.db.QueryRow(`SELECT `+shareCols+` FROM shares WHERE id = ?`, id))
	if err == nil {
		f, e, err = s.Open(sh.Path)
	}
	if err != nil {
		refund()
		return nil, e, sh, nil, ErrNotFound
	}
	return f, e, sh, func(ok bool) {
		if !ok {
			refund()
		}
	}, nil
}
