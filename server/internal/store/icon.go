package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
)

// Folder icons as the PC they come from shows them: the Windows app sends Downloads' arrow for a synced Downloads
// folder, say. They live in .aetheris/icons by folder path, so nothing appears among the files.
var ErrNoIcon = errors.New("no icon")

const maxIcon = 256 << 10

func iconName(p string) string {
	sum := sha256.Sum256([]byte(p))
	return metaDir + "/icons/" + hex.EncodeToString(sum[:16]) + ".png"
}

// SetIcon keeps png as folder p's icon. Only PNGs up to 256 KB, only for existing folders.
func (s *Store) SetIcon(p string, png []byte) error {
	p, err := CleanPath(p)
	if err != nil || p == "" {
		return ErrBadName
	}
	if e, err := s.Stat(p); err != nil {
		return err
	} else if !e.Dir {
		return ErrNotDir
	}
	if len(png) > maxIcon || !bytes.HasPrefix(png, []byte("\x89PNG\r\n\x1a\n")) {
		return ErrBadName
	}
	if err := s.root.MkdirAll(osPath(metaDir+"/icons"), 0o700); err != nil {
		return err
	}
	name := iconName(p)
	tmp := name + "." + randID()
	if err := s.root.WriteFile(osPath(tmp), png, 0o600); err != nil {
		return err
	}
	if err := s.root.Rename(osPath(tmp), osPath(name)); err != nil {
		s.root.Remove(osPath(tmp))
		return err
	}
	return nil
}

// Icon opens folder p's icon; ErrNoIcon when it has none.
func (s *Store) Icon(p string) (*os.File, error) {
	p, err := CleanPath(p)
	if err != nil {
		return nil, err
	}
	f, err := s.root.Open(osPath(iconName(p)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoIcon
	}
	return f, err
}
