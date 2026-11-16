package store

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"
	"time"
)

func TestThumb(t *testing.T) {
	s, _ := open(t)
	src := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	var buf bytes.Buffer
	png.Encode(&buf, src)
	if _, err := put(t, s, "wide.png", 0, buf.String()); err != nil {
		t.Fatal(err)
	}
	f, hash, err := s.Thumb("wide.png")
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(f)
	f.Close()
	if err != nil || img.Bounds().Dx() != 320 || img.Bounds().Dy() != 160 {
		t.Fatalf("thumbnail %v %v", img.Bounds(), err)
	}
	if f2, h2, err := s.Thumb("wide.png"); err != nil || h2 != hash { // second call is the cached file
		t.Fatalf("cached thumb: %v", err)
	} else {
		f2.Close()
	}
	put(t, s, "notes.txt", 0, "not an image")
	if _, _, err := s.Thumb("notes.txt"); !errors.Is(err, ErrNoThumb) {
		t.Fatalf("text file: %v", err)
	}
	put(t, s, "broken.jpg", 0, "\xff\xd8 not really")
	if _, _, err := s.Thumb("broken.jpg"); !errors.Is(err, ErrNoThumb) {
		t.Fatalf("broken jpeg: %v", err)
	}
}

func TestOrientation(t *testing.T) {
	// A minimal JPEG start with an EXIF block saying "rotate 90° clockwise" (6), little-endian TIFF.
	tiff := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 0x01, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	jpg := append([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0, byte(len(app1) + 2)}, app1...)
	if o := exifOrientation(bytes.NewReader(jpg)); o != 6 {
		t.Fatalf("orientation %d, want 6", o)
	}
	if o := exifOrientation(strings.NewReader("\xff\xd8\xff\xe1\x00\x00junk")); o != 1 { // corrupt length must not panic
		t.Fatalf("corrupt header: %d", o)
	}

	src := image.NewRGBA(image.Rect(0, 0, 2, 1)) // [red, blue]
	src.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	src.SetRGBA(1, 0, color.RGBA{0, 0, 255, 255})
	out := orientImage(src, 6).(*image.RGBA) // turned clockwise: red on top, blue below
	if out.Bounds().Dx() != 1 || out.Bounds().Dy() != 2 || out.RGBAAt(0, 0).R != 255 || out.RGBAAt(0, 1).B != 255 {
		t.Fatalf("rotated %v: top %v bottom %v", out.Bounds(), out.RGBAAt(0, 0), out.RGBAAt(0, 1))
	}
}

func TestRecentAndVersionDownload(t *testing.T) {
	s, _ := open(t)
	commit := func(p string, base int64, body string, mtime time.Time) Entry {
		t.Helper()
		id, _ := s.BeginUpload()
		s.Append(id, 0, strings.NewReader(body))
		e, err := s.Commit(id, p, base, mtime, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	old := commit("old.txt", 0, "v1", time.Now().Add(-48*time.Hour))
	commit("new.txt", 0, "x", time.Now())
	if list, _ := s.Recent(10); len(list) != 2 || list[0].Path != "new.txt" {
		t.Fatalf("recent %+v", list)
	}
	commit("old.txt", old.Rev, "v2", time.Now())
	vs, _ := s.Versions("old.txt")
	f, _, err := s.OpenVersion("old.txt", vs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "v1" {
		t.Fatalf("version content %q", b)
	}
	if _, _, err := s.OpenVersion("new.txt", vs[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("a version opened under another file's path")
	}
}
