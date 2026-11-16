package store

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeebo/blake3"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func open(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir, quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}

func put(t *testing.T, s *Store, p string, baseRev int64, content string) (Entry, error) {
	t.Helper()
	id, err := s.BeginUpload()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(id, 0, strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	return s.Commit(id, p, baseRev, time.Time{}, time.Time{})
}

func blake(s string) string {
	h := blake3.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func TestCleanPath(t *testing.T) {
	for _, p := range []string{"", "a", "a/b c/d.txt", "/a/b/", "Ünïcødé.md", ".hidden"} {
		if _, err := CleanPath(p); err != nil {
			t.Errorf("%q rejected: %v", p, err)
		}
	}
	for _, p := range []string{"..", "a/../b", "a//b", `a\b`, "a:b", "con", "CON.txt", "lpt1.log", "trail.", "trail ",
		".aetheris", "x/.aetheris/y", "@Recycle", "nul\x00byte", "tab\tname"} {
		if _, err := CleanPath(p); !errors.Is(err, ErrBadName) {
			t.Errorf("%q accepted", p)
		}
	}
}

func TestUploadConflictAndVersions(t *testing.T) {
	s, dir := open(t)
	e, err := put(t, s, "a.txt", 0, "one")
	if err != nil || e.Hash != blake("one") || e.Size != 3 {
		t.Fatalf("first put: %+v %v", e, err)
	}
	// A second "new file" upload to the same path conflicts instead of overwriting.
	_, err = put(t, s, "a.txt", 0, "two")
	var c *ConflictError
	if !errors.As(err, &c) || c.Current == nil || c.Current.Rev != e.Rev {
		t.Fatalf("want conflict with current rev, got %v", err)
	}
	if _, err := put(t, s, "a.txt", e.Rev, "two"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "two" {
		t.Fatalf("content %q", b)
	}
	vs, _ := s.Versions("a.txt")
	if len(vs) != 1 || vs[0].Hash != blake("one") {
		t.Fatalf("versions %+v", vs)
	}
	if _, err := s.RestoreVersion("a.txt", vs[0].ID); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "one" {
		t.Fatalf("restored content %q", b)
	}
	if vs, _ := s.Versions("a.txt"); len(vs) != 2 {
		t.Fatalf("restore should keep the replaced content as a version, have %d", len(vs))
	}
}

func TestResumeAfterRestart(t *testing.T) {
	dir := t.TempDir()
	s1, err := Open(dir, quiet)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := s1.BeginUpload()
	if _, err := s1.Append(id, 0, strings.NewReader("hello ")); err != nil {
		t.Fatal(err)
	}
	var oe *OffsetError
	if _, err := s1.Append(id, 2, strings.NewReader("x")); !errors.As(err, &oe) || oe.Offset != 6 {
		t.Fatalf("misplaced chunk: want offset 6, got %v", err)
	}
	s1.Close()

	s2, err := Open(dir, quiet) // in-memory hash state is gone and must be rebuilt from the temp file
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if off, _ := s2.UploadOffset(id); off != 6 {
		t.Fatalf("offset after restart %d", off)
	}
	if _, err := s2.Append(id, 6, strings.NewReader("world")); err != nil {
		t.Fatal(err)
	}
	e, err := s2.Commit(id, "h.txt", 0, time.Time{}, time.Time{})
	if err != nil || e.Hash != blake("hello world") {
		t.Fatalf("commit %+v %v", e, err)
	}
}

func TestScanSeesOutsideEdits(t *testing.T) {
	s, dir := open(t)
	os.MkdirAll(filepath.Join(dir, "docs"), 0o755)
	os.WriteFile(filepath.Join(dir, "docs", "n.txt"), []byte("smb"), 0o644)
	if err := s.Scan("", true); err != nil {
		t.Fatal(err)
	}
	if e, err := s.Stat("docs/n.txt"); err != nil || e.Hash != blake("smb") {
		t.Fatalf("new file not indexed: %+v %v", e, err)
	}
	os.WriteFile(filepath.Join(dir, "docs", "n.txt"), []byte("smb, edited"), 0o644)
	s.Scan("docs", false)
	if e, _ := s.Stat("docs/n.txt"); e.Hash != blake("smb, edited") {
		t.Fatal("edit not picked up")
	}
	s.Scan("", true) // unchanged: must not journal anything
	os.RemoveAll(filepath.Join(dir, "docs"))
	s.Scan("", false)
	if _, err := s.Stat("docs/n.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatal("removed folder still indexed")
	}
	_, list, _ := s.Changes(context.Background(), s.Epoch(), 0, 0)
	var ops []string
	for _, c := range list {
		ops = append(ops, c.Op+" "+c.Path)
	}
	if got := strings.Join(ops, ", "); got != "mkdir docs, put docs/n.txt, put docs/n.txt, delete docs" {
		t.Fatalf("journal: %s", got)
	}
}

func TestMoveDeleteRestore(t *testing.T) {
	s, _ := open(t)
	s.Mkdir("a")
	s.Mkdir("a/b")
	if _, err := put(t, s, "a/b/f.txt", 0, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move("a", "a/b/c", 0); !errors.Is(err, ErrBadName) {
		t.Fatalf("move into itself: %v", err)
	}
	if _, err := s.Move("a", "z", 0); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List("z/b"); len(list) != 1 || list[0].Path != "z/b/f.txt" {
		t.Fatalf("after move: %+v", list)
	}
	if _, err := s.Stat("a/b/f.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatal("old path still indexed")
	}
	if err := s.Delete("z", 0); err != nil {
		t.Fatal(err)
	}
	tr, _ := s.Trash()
	if len(tr) != 1 || tr[0].Path != "z" || !tr[0].Dir {
		t.Fatalf("trash %+v", tr)
	}
	if to, err := s.RestoreTrash(tr[0].ID, ""); err != nil || to != "z" {
		t.Fatalf("restore: %q %v", to, err)
	}
	if e, err := s.Stat("z/b/f.txt"); err != nil || e.Hash != blake("x") {
		t.Fatalf("restored file: %+v %v", e, err)
	}
}

func TestCaseCollision(t *testing.T) {
	s, _ := open(t)
	put(t, s, "Report.txt", 0, "r")
	if _, err := put(t, s, "report.txt", 0, "r2"); !errors.Is(err, ErrExists) {
		t.Fatalf("case twin accepted: %v", err)
	}
	if _, err := s.Mkdir("REPORT.TXT"); !errors.Is(err, ErrExists) {
		t.Fatalf("case twin folder accepted: %v", err)
	}
	if _, err := s.Move("Report.txt", "report.txt", 0); err != nil {
		t.Fatalf("case-only rename refused: %v", err)
	}
}

func TestRootCannotBeEscaped(t *testing.T) {
	s, dir := open(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o644)
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skip("symlinks unavailable here:", err)
	}
	s.Scan("", true)
	if _, err := s.Stat("link"); !errors.Is(err, ErrNotFound) {
		t.Fatal("symlink was indexed")
	}
	if f, err := s.root.Open(osPath("link/secret")); err == nil {
		f.Close()
		t.Fatal("os.Root followed a symlink out of the share")
	}
}

func TestLongPollWakes(t *testing.T) {
	s, _ := open(t)
	if _, _, err := s.Changes(context.Background(), "stale-epoch", 0, 0); !errors.Is(err, ErrEpoch) {
		t.Fatalf("foreign epoch accepted: %v", err)
	}
	done := make(chan []Change)
	go func() {
		_, list, _ := s.Changes(context.Background(), s.Epoch(), 0, 10*time.Second)
		done <- list
	}()
	time.Sleep(100 * time.Millisecond)
	s.Mkdir("x")
	select {
	case list := <-done:
		if len(list) != 1 || list[0].Op != "mkdir" || list[0].Entry == nil {
			t.Fatalf("got %+v", list)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("long-poll did not wake on a commit")
	}
}

func TestPurgeRetention(t *testing.T) {
	s, _ := open(t)
	base := time.Now().Add(-60 * 24 * time.Hour)
	clock := base
	s.now = func() time.Time { return clock }
	e, _ := put(t, s, "v.txt", 0, "0")
	for i := 1; i <= 12; i++ {
		clock = base.Add(time.Duration(i) * time.Minute)
		var err error
		if e, err = put(t, s, "v.txt", e.Rev, fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	s.Delete("v.txt", 0)
	clock = time.Now()
	if err := s.Purge(); err != nil {
		t.Fatal(err)
	}
	if vs, _ := s.Versions("v.txt"); len(vs) != keepVersions {
		t.Fatalf("kept %d versions, want the newest %d", len(vs), keepVersions)
	}
	if tr, _ := s.Trash(); len(tr) != 0 {
		t.Fatalf("old trash kept: %+v", tr)
	}
}

// A folder tree copied in one go (an SMB copy): its files exist before the watcher has added the new
// folders, and the burst of events must not stall the watcher either.
func TestWatcherSeesNewTrees(t *testing.T) {
	s, dir := open(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx, time.Hour); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	time.Sleep(300 * time.Millisecond) // initial scan and watches

	const dirs, files = 40, 50
	for d := range dirs {
		sub := filepath.Join(dir, "copy", fmt.Sprintf("d%02d", d), "inner")
		os.MkdirAll(sub, 0o755)
		for f := range files {
			os.WriteFile(filepath.Join(sub, fmt.Sprintf("f%03d.txt", f)), []byte("x"), 0o644)
		}
	}
	indexed := func(p string) bool { _, err := s.Stat(p); return err == nil }
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); !ok(); time.Sleep(100 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%s: not indexed after 15 s", what)
			}
		}
	}
	waitFor("the copied tree", func() bool {
		for d := range dirs {
			for f := range files {
				if !indexed(fmt.Sprintf("copy/d%02d/inner/f%03d.txt", d, f)) {
					return false
				}
			}
		}
		return true
	})
	os.WriteFile(filepath.Join(dir, "copy", "after.txt"), []byte("later"), 0o644)
	waitFor("a file written afterwards", func() bool { return indexed("copy/after.txt") })
}

// Deleting a watched folder moves it into the trash; the watcher must keep working afterwards.
func TestWatcherSurvivesFolderDelete(t *testing.T) {
	s, dir := open(t)
	os.MkdirAll(filepath.Join(dir, "big"), 0o755) // no subfolders: Windows can't move a folder whose subfolders are watched
	os.WriteFile(filepath.Join(dir, "big", "a.bin"), []byte("a"), 0o644)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx, time.Hour); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	time.Sleep(300 * time.Millisecond)
	if err := s.Delete("big", 0); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "after.txt"), []byte("later"), 0o644)
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		if _, err := s.Stat("after.txt"); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("watcher stopped after a folder went to the trash")
		}
	}
}

// A file keeps the creation time it arrived with through edits and moves; folders report what is inside them.
func TestCreatedAndFolderSize(t *testing.T) {
	s, _ := open(t)
	if _, err := s.Mkdir("docs"); err != nil {
		t.Fatal(err)
	}
	born := time.UnixMilli(1_600_000_000_000)
	id, _ := s.BeginUpload()
	s.Append(id, 0, strings.NewReader("hello"))
	e, err := s.Commit(id, "docs/a.txt", 0, time.Time{}, born)
	if err != nil || e.Created != born.UnixMilli() {
		t.Fatalf("given: %+v %v", e, err)
	}
	if e, err = put(t, s, "docs/a.txt", e.Rev, "hello, world"); err != nil || e.Created != born.UnixMilli() {
		t.Fatalf("after an edit: %+v %v", e, err)
	}
	if _, err = s.Move("docs/a.txt", "docs/b.txt", e.Rev); err != nil {
		t.Fatal(err)
	}
	if e, _ = s.Stat("docs/b.txt"); e.Created != born.UnixMilli() {
		t.Fatalf("after a move: %+v", e)
	}
	if _, err := put(t, s, "docsx.txt", 0, "not inside"); err != nil { // sorts right after "docs/": must not count
		t.Fatal(err)
	}
	list, err := s.List("")
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range list {
		if x.Path == "docs" && x.Size != int64(len("hello, world")) {
			t.Fatalf("folder size %d", x.Size)
		}
	}
}

// A folder's icon is a PNG kept out of sight; anything else, or a file's icon, is refused.
func TestFolderIcon(t *testing.T) {
	s, dir := open(t)
	s.Mkdir("Downloads")
	put(t, s, "a.txt", 0, "x")
	png := append([]byte("\x89PNG\r\n\x1a\n"), "pixels"...)
	if err := s.SetIcon("a.txt", png); !errors.Is(err, ErrNotDir) {
		t.Fatalf("file: %v", err)
	}
	if err := s.SetIcon("Downloads", []byte("GIF89a")); !errors.Is(err, ErrBadName) {
		t.Fatalf("not a PNG: %v", err)
	}
	if _, err := s.Icon("Downloads"); !errors.Is(err, ErrNoIcon) {
		t.Fatalf("none yet: %v", err)
	}
	if err := s.SetIcon("Downloads", png); err != nil {
		t.Fatal(err)
	}
	f, err := s.Icon("Downloads")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if string(got) != string(png) {
		t.Fatal("icon changed on the way")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 3 { // .aetheris, Downloads, a.txt: nothing else among the files
		t.Fatalf("share holds %d entries", len(entries))
	}
}
