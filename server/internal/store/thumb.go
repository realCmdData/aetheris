package store

import (
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// ErrNoThumb means p is not an image we can make a thumbnail of (unknown format, too big, broken).
var ErrNoThumb = errors.New("no thumbnail")

const (
	thumbSize      = 320      // longest side, px: sharp on a 2x screen at the grid's 160 px tiles
	thumbMaxPixels = 50 << 20 // larger images are skipped: decoding them would not fit the NAS's memory budget
)

// One decode at a time: a 12 MP photo needs ~20 MB while it decodes, and the J1900 is slow anyway.
// shortcut: the first view of a big photo folder fills the cache one image at a time; pre-generate in Scan if that hurts.
var thumbSlot = make(chan struct{}, 1)

var thumbExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}

// Thumb returns a JPEG thumbnail of image p. It is made once per content hash and kept in .aetheris/thumbs,
// so a changed file gets a new one and an unchanged one is never decoded twice.
func (s *Store) Thumb(p string) (*os.File, string, error) {
	e, err := s.Stat(p)
	if err != nil {
		return nil, "", err
	}
	dot := strings.LastIndexByte(p, '.')
	if e.Dir || e.Hash == "" || dot < 0 || !thumbExts[strings.ToLower(p[dot:])] {
		return nil, "", ErrNoThumb
	}
	name := metaDir + "/thumbs/" + e.Hash + ".jpg"
	if f, err := s.root.Open(osPath(name)); err == nil {
		return f, e.Hash, nil
	}
	thumbSlot <- struct{}{}
	defer func() { <-thumbSlot }()
	if f, err := s.root.Open(osPath(name)); err == nil { // made while we waited
		return f, e.Hash, nil
	}
	if err := s.makeThumb(p, name); err != nil {
		return nil, "", ErrNoThumb
	}
	f, err := s.root.Open(osPath(name))
	return f, e.Hash, err
}

func (s *Store) makeThumb(p, name string) error {
	f, err := s.root.Open(osPath(p))
	if err != nil {
		return err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width*cfg.Height > thumbMaxPixels || cfg.Width == 0 || cfg.Height == 0 {
		return ErrNoThumb
	}
	f.Seek(0, io.SeekStart)
	orient := exifOrientation(f)
	f.Seek(0, io.SeekStart)
	src, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	b := src.Bounds()
	w, h := thumbSize, thumbSize
	if b.Dx() >= b.Dy() {
		h = max(1, b.Dy()*thumbSize/b.Dx())
	} else {
		w = max(1, b.Dx()*thumbSize/b.Dy())
	}
	if b.Dx() <= thumbSize && b.Dy() <= thumbSize {
		w, h = b.Dx(), b.Dy() // never upscale
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	out := orientImage(dst, orient)

	if err := s.root.MkdirAll(osPath(metaDir+"/thumbs"), 0o700); err != nil {
		return err
	}
	tmp := name + "." + randID()
	tf, err := s.root.OpenFile(osPath(tmp), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := jpeg.Encode(tf, out, &jpeg.Options{Quality: 82}); err != nil {
		tf.Close()
		s.root.Remove(osPath(tmp))
		return err
	}
	tf.Close()
	return s.root.Rename(osPath(tmp), osPath(name))
}

// exifOrientation reads the EXIF orientation tag (1–8) from a JPEG; 1 (as stored) when absent.
// Phones save photos sideways and set this tag; without it portrait thumbnails would lie on their side.
func exifOrientation(r io.Reader) int {
	head := make([]byte, 64<<10) // EXIF lives in an APP1 segment near the start
	n, _ := io.ReadFull(r, head)
	b := head[:n]
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(b) && b[i] == 0xFF; {
		marker, size := b[i+1], int(binary.BigEndian.Uint16(b[i+2:]))
		if size < 2 { // corrupt length: stop rather than slice out of range on untrusted bytes
			break
		}
		seg := b[i+4 : min(i+2+size, len(b))]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		if marker == 0xDA { // start of image data: no EXIF before it
			break
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder = binary.LittleEndian
	if string(t[:2]) == "MM" {
		bo = binary.BigEndian
	}
	ifd := int(bo.Uint32(t[4:]))
	if ifd+2 > len(t) {
		return 1
	}
	for k, count := 0, int(bo.Uint16(t[ifd:])); k < count; k++ {
		e := ifd + 2 + 12*k
		if e+12 > len(t) {
			break
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			if v := int(bo.Uint16(t[e+8:])); v >= 1 && v <= 8 {
				return v
			}
		}
	}
	return 1
}

// orientImage applies an EXIF orientation to a (small) image.
func orientImage(src *image.RGBA, o int) image.Image {
	if o <= 1 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	swap := o >= 5 // 5–8 turn the image by 90°
	dw, dh := w, h
	if swap {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.SetRGBA(dx, dy, src.RGBAAt(x, y))
		}
	}
	return dst
}
