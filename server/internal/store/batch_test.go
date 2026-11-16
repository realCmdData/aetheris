package store

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"testing"
)

func TestCopyAndZip(t *testing.T) {
	s, _ := open(t)
	s.Mkdir("Trip")
	s.Mkdir("Trip/Day 1")
	put(t, s, "Trip/Day 1/a.txt", 0, "alpha")
	put(t, s, "Trip/b.jpg", 0, "jpeg bytes")

	if _, err := s.Copy("Trip/b.jpg", "b copy.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Copy("Trip", "Trip 2"); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]string{"b copy.jpg": "jpeg bytes", "Trip 2/b.jpg": "jpeg bytes", "Trip 2/Day 1/a.txt": "alpha"} {
		e, err := s.Stat(p)
		if err != nil || e.Hash != blake(want) {
			t.Errorf("%s: %+v %v", p, e, err)
		}
	}
	if _, err := s.Copy("Trip", "Trip/inside"); !errors.Is(err, ErrBadName) {
		t.Errorf("copy into itself: %v", err)
	}
	if _, err := s.Copy("Trip/b.jpg", "b copy.jpg"); !errors.Is(err, ErrExists) {
		t.Errorf("copy over an existing file: %v", err)
	}

	if _, err := s.Compress([]string{"Trip", "b copy.jpg"}, "Archive.zip"); err != nil {
		t.Fatal(err)
	}
	f, e, err := s.Open("Archive.zip")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(f)
	f.Close()
	if e.Hash != blake(string(raw)) {
		t.Error("archive hash doesn't match its bytes")
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, zf := range zr.File {
		names = append(names, zf.Name)
		if zf.Name == "Trip/Day 1/a.txt" {
			rc, _ := zf.Open()
			b, _ := io.ReadAll(rc)
			if string(b) != "alpha" || zf.Method != zip.Deflate {
				t.Errorf("a.txt in zip: %q method %d", b, zf.Method)
			}
		}
		if zf.Name == "Trip/b.jpg" && zf.Method != zip.Store {
			t.Error("jpg should be stored, not deflated")
		}
	}
	sort.Strings(names)
	if want := "[Trip/ Trip/Day 1/ Trip/Day 1/a.txt Trip/b.jpg b copy.jpg]"; fmt.Sprint(names) != want {
		t.Errorf("zip entries %v, want %s", names, want)
	}
}
