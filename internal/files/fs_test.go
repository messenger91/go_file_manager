package files

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Traversal segments are either rejected outright or clamped into the root;
// either way the result must never escape it.
func TestResolveStaysWithinRoot(t *testing.T) {
	f := New(t.TempDir())
	for _, p := range []string{"../etc/passwd", "/../../etc/passwd", "a/../../b", "/.."} {
		got, err := f.resolve(p)
		if err != nil {
			continue // rejecting is also acceptable
		}
		if got != f.Root() && !strings.HasPrefix(got, f.Root()+string(os.PathSeparator)) {
			t.Errorf("resolve(%q) = %q escaped root %q", p, got, f.Root())
		}
	}
}

func TestResolveSymlinkEscapeRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	f := New(root)
	if _, err := f.resolve("/link.txt"); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestListSortsDirsFirst(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "zzz-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "aaa.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := New(root)
	_, entries, err := f.List("/", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if !entries[0].IsDir || entries[0].Name != "zzz-dir" {
		t.Errorf("first entry = %+v, want directory zzz-dir", entries[0])
	}
	if entries[1].Path != "/aaa.txt" {
		t.Errorf("second entry path = %q, want /aaa.txt", entries[1].Path)
	}
}

func TestListHidesDotEntriesUnlessAsked(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"visible.txt", ".hidden", ".config"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	f := New(root)
	_, visible, err := f.List("/", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].Name != "visible.txt" {
		t.Fatalf("visible = %+v, want only visible.txt", visible)
	}

	_, all, err := f.List("/", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("want 4 entries with hidden files, got %d: %+v", len(all), all)
	}
	// Directories still sort first, dot directory included.
	if !all[0].IsDir || all[0].Name != ".git" {
		t.Errorf("first entry = %+v, want directory .git", all[0])
	}
}

func TestReadFileRejectsLargeFile(t *testing.T) {
	root := t.TempDir()
	big := filepath.Join(root, "big.bin")
	if err := os.WriteFile(big, make([]byte, MaxReadSize+1), 0o644); err != nil {
		t.Fatal(err)
	}

	f := New(root)
	if _, _, err := f.ReadFile("/big.bin"); err == nil {
		t.Fatal("expected large file to be rejected")
	}
}

func TestDeleteRootRejected(t *testing.T) {
	f := New(t.TempDir())
	for _, p := range []string{"/", "", "/.", "/sub/.."} {
		if err := f.Delete(p, true); !errors.Is(err, ErrRootOperation) {
			t.Errorf("Delete(%q) error = %v, want ErrRootOperation", p, err)
		}
	}
}

func TestDeleteNonEmptyDirRequiresRecursive(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := New(root)
	if err := f.Delete("/sub", false); !errors.Is(err, ErrDirNotEmpty) {
		t.Fatalf("non-recursive delete error = %v, want ErrDirNotEmpty", err)
	}
	if err := f.Delete("/sub", true); err != nil {
		t.Fatalf("recursive delete: %v", err)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatalf("sub still exists after recursive delete: %v", err)
	}
}

func TestDeleteFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := New(root)
	if err := f.Delete("/a.txt", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
}

// Deleting a symlink removes the link, never its target outside the root.
func TestDeleteSymlinkRemovesLinkOnly(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	f := New(root)
	if err := f.Delete("/link.txt", false); err != nil {
		t.Fatalf("delete symlink: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("symlink still exists: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target was removed: %v", err)
	}
}

func TestMkdirCreatesDirectory(t *testing.T) {
	root := t.TempDir()
	f := New(root)

	if err := f.Mkdir("/new"); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if info, err := os.Stat(filepath.Join(root, "new")); err != nil || !info.IsDir() {
		t.Fatalf("directory not created: %v", err)
	}
}

func TestMkdirNested(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Mkdir("/sub/inner"); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if info, err := os.Stat(filepath.Join(root, "sub", "inner")); err != nil || !info.IsDir() {
		t.Fatalf("nested directory not created: %v", err)
	}
}

func TestMkdirExistingFails(t *testing.T) {
	root := t.TempDir()
	f := New(root)
	if err := f.Mkdir("/dup"); err != nil {
		t.Fatal(err)
	}
	if err := f.Mkdir("/dup"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("second mkdir error = %v, want os.ErrExist", err)
	}
}

func TestMkdirMissingParentFails(t *testing.T) {
	f := New(t.TempDir())
	if err := f.Mkdir("/missing/child"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mkdir with missing parent error = %v, want os.ErrNotExist", err)
	}
}

func TestMkdirRootRejected(t *testing.T) {
	f := New(t.TempDir())
	for _, p := range []string{"/", "", "/sub/.."} {
		if err := f.Mkdir(p); !errors.Is(err, ErrRootOperation) {
			t.Errorf("Mkdir(%q) error = %v, want ErrRootOperation", p, err)
		}
	}
}

// Traversal in a create path is clamped into the root, never escaping it.
func TestMkdirStaysWithinRoot(t *testing.T) {
	root := t.TempDir()
	f := New(root)

	if err := f.Mkdir("/../evil"); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "evil")); err != nil {
		t.Fatalf("expected clamped directory inside root: %v", err)
	}
}

func TestRenameFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "old.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Rename("/old.txt", "new.txt"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("old name still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err != nil {
		t.Fatalf("new name missing: %v", err)
	}
}

func TestRenameDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Rename("/sub", "renamed"); err != nil {
		t.Fatalf("rename dir: %v", err)
	}
	if info, err := os.Stat(filepath.Join(root, "renamed")); err != nil || !info.IsDir() {
		t.Fatalf("renamed directory missing: %v", err)
	}
}

func TestRenameToExistingFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Rename("/a.txt", "b.txt"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("rename onto existing error = %v, want os.ErrExist", err)
	}
	// The original file must be untouched.
	if data, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(data) != "a" {
		t.Fatalf("source changed despite failed rename")
	}
}

func TestRenameInvalidName(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	for _, name := range []string{"", ".", "..", "sub/b", "../b"} {
		if err := f.Rename("/a.txt", name); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Rename to %q error = %v, want ErrInvalidName", name, err)
		}
	}
}

func TestRenameRootRejected(t *testing.T) {
	f := New(t.TempDir())
	if err := f.Rename("/", "x"); !errors.Is(err, ErrRootOperation) {
		t.Fatalf("rename root error = %v, want ErrRootOperation", err)
	}
}

func TestRenameMissing(t *testing.T) {
	f := New(t.TempDir())
	if err := f.Rename("/nope.txt", "x.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rename missing error = %v, want os.ErrNotExist", err)
	}
}

func TestRenameSameNameNoop(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Rename("/a.txt", "a.txt"); err != nil {
		t.Fatalf("rename to same name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("file missing after no-op rename: %v", err)
	}
}

func TestCreateFile(t *testing.T) {
	root := t.TempDir()
	f := New(root)

	if err := f.Create("/new.txt"); err != nil {
		t.Fatalf("create: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "new.txt"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if !info.Mode().IsRegular() || info.Size() != 0 {
		t.Fatalf("expected empty regular file, got mode=%v size=%d", info.Mode(), info.Size())
	}
}

func TestCreateExistingFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Create("/a.txt"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("create existing error = %v, want os.ErrExist", err)
	}
}

func TestCreateRootRejected(t *testing.T) {
	f := New(t.TempDir())
	if err := f.Create("/"); !errors.Is(err, ErrRootOperation) {
		t.Fatalf("create root error = %v, want ErrRootOperation", err)
	}
}

func TestWriteFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Write("/a.txt", []byte("new content")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "new content" {
		t.Fatalf("content = %q, want %q", data, "new content")
	}
}

func TestWriteMissingFails(t *testing.T) {
	f := New(t.TempDir())
	if err := f.Write("/nope.txt", []byte("x")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("write missing error = %v, want os.ErrNotExist", err)
	}
}

func TestWriteThroughSymlinkRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	f := New(root)
	if err := f.Write("/link.txt", []byte("hacked")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("write through symlink error = %v, want ErrForbidden", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "original" {
		t.Fatalf("symlink target was modified: %q", data)
	}
}

func TestUpload(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Upload("/sub", "up.txt", strings.NewReader("payload")); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "sub", "up.txt")); string(data) != "payload" {
		t.Fatalf("uploaded content = %q", data)
	}
}

func TestUploadExistingFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Upload("/", "a.txt", strings.NewReader("new")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("upload existing error = %v, want os.ErrExist", err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(data) != "keep" {
		t.Fatalf("existing file was overwritten: %q", data)
	}
}

func TestUploadInvalidName(t *testing.T) {
	f := New(t.TempDir())
	for _, name := range []string{"", ".", "..", "a/b"} {
		if err := f.Upload("/", name, strings.NewReader("x")); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Upload(%q) error = %v, want ErrInvalidName", name, err)
		}
	}
}

func TestDisk(t *testing.T) {
	f := New(t.TempDir())
	info, err := f.Disk()
	if err != nil {
		t.Fatalf("disk: %v", err)
	}
	if info.TotalBytes == 0 {
		t.Fatal("total bytes is zero")
	}
	if info.FreeBytes > info.TotalBytes {
		t.Fatalf("free (%d) > total (%d)", info.FreeBytes, info.TotalBytes)
	}
	if info.UsedBytes > info.TotalBytes {
		t.Fatalf("used (%d) > total (%d)", info.UsedBytes, info.TotalBytes)
	}
}

func TestMoveFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Move("/a.txt", "/dest/a.txt"); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("source still exists: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dest", "a.txt"))
	if err != nil || string(data) != "payload" {
		t.Fatalf("moved file = %q, %v; want payload", data, err)
	}
}

func TestMoveDirectoryKeepsContents(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "nested", "note.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Move("/docs", "/dest/docs"); err != nil {
		t.Fatalf("move dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Fatalf("source dir still exists: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dest", "docs", "nested", "note.txt"))
	if err != nil || string(data) != "hi" {
		t.Fatalf("nested file = %q, %v; want hi", data, err)
	}
}

func TestMoveFolderIntoItselfFails(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "docs2"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	for _, to := range []string{"/docs/sub/docs", "/docs/./sub/docs", "/docs/../docs/sub/docs"} {
		if err := f.Move("/docs", to); !errors.Is(err, ErrMoveIntoSelf) {
			t.Fatalf("move into itself (%s) error = %v, want ErrMoveIntoSelf", to, err)
		}
	}
	// A sibling whose name merely starts with the source name is fine.
	if err := f.Move("/docs", "/docs2/docs"); err != nil {
		t.Fatalf("move to sibling prefix: %v", err)
	}
}

func TestMoveOntoExistingFails(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.txt", "dest/a.txt"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f := New(root)

	if err := f.Move("/a.txt", "/dest/a.txt"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("move onto existing error = %v, want os.ErrExist", err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(data) != "a.txt" {
		t.Fatalf("source changed despite failed move")
	}
	if data, _ := os.ReadFile(filepath.Join(root, "dest", "a.txt")); string(data) != "dest/a.txt" {
		t.Fatalf("destination changed despite failed move")
	}
}

func TestMoveMissingPathsFail(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Move("/missing.txt", "/a.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing source error = %v, want os.ErrNotExist", err)
	}
	if err := f.Move("/a.txt", "/nope/a.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing destination error = %v, want os.ErrNotExist", err)
	}
}

func TestMoveRootRejected(t *testing.T) {
	f := New(t.TempDir())
	if err := f.Move("/", "/dest"); !errors.Is(err, ErrRootOperation) {
		t.Fatalf("move root error = %v, want ErrRootOperation", err)
	}
	if err := f.Move("/a.txt", "/"); !errors.Is(err, ErrRootOperation) {
		t.Fatalf("move onto root error = %v, want ErrRootOperation", err)
	}
}

func TestMoveSamePathNoop(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Move("/a.txt", "/a.txt"); err != nil {
		t.Fatalf("move onto itself: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("file disappeared: %v", err)
	}
}

func TestMoveSymlinkMovesLinkOnly(t *testing.T) {
	outside := t.TempDir()
	target := filepath.Join(outside, "target.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Move("/link", "/dest/link"); err != nil {
		t.Fatalf("move symlink: %v", err)
	}
	info, err := os.Lstat(filepath.Join(root, "dest", "link"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("destination is not a symlink: %v, %v", info, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "outside" {
		t.Fatalf("link target changed: %q, %v", data, err)
	}
}

func TestCopyFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Copy("/a.txt", "/dest/a.txt"); err != nil {
		t.Fatalf("copy: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dest", "a.txt"))
	if err != nil || string(data) != "payload" {
		t.Fatalf("copied file = %q, %v; want payload", data, err)
	}
	// The source stays in place.
	if data, err := os.ReadFile(filepath.Join(root, "a.txt")); err != nil || string(data) != "payload" {
		t.Fatalf("source changed: %q, %v", data, err)
	}
	info, err := os.Stat(filepath.Join(root, "dest", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("copied mode = %v, want 0640", info.Mode().Perm())
	}
}

func TestCopyDirectoryRecursive(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "nested", "note.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A read-only directory must still receive its children. Restore write
	// access afterwards so TempDir cleanup can remove the tree.
	if err := os.Chmod(filepath.Join(root, "docs", "nested"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(filepath.Join(root, "docs", "nested"), 0o755)
		_ = os.Chmod(filepath.Join(root, "dest", "docs", "nested"), 0o755)
	})
	if err := os.Mkdir(filepath.Join(root, "dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Copy("/docs", "/dest/docs"); err != nil {
		t.Fatalf("copy dir: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dest", "docs", "nested", "note.txt"))
	if err != nil || string(data) != "hi" {
		t.Fatalf("nested copy = %q, %v; want hi", data, err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "nested", "note.txt")); err != nil {
		t.Fatalf("source lost a file: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "dest", "docs", "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o555 {
		t.Fatalf("copied dir mode = %v, want 0555", info.Mode().Perm())
	}
}

func TestCopyFolderIntoItselfFails(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "docs2"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Copy("/docs", "/docs/sub/docs"); !errors.Is(err, ErrCopyIntoSelf) {
		t.Fatalf("copy into itself error = %v, want ErrCopyIntoSelf", err)
	}
	if err := f.Copy("/docs", "/docs2/docs"); err != nil {
		t.Fatalf("copy to sibling prefix: %v", err)
	}
}

func TestCopyOntoExistingFails(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.txt", "dest/a.txt"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f := New(root)

	if err := f.Copy("/a.txt", "/dest/a.txt"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("copy onto existing error = %v, want os.ErrExist", err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "dest", "a.txt")); string(data) != "dest/a.txt" {
		t.Fatalf("existing destination changed")
	}
}

func TestCopyMissingPathsFail(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Copy("/missing.txt", "/a.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing source error = %v, want os.ErrNotExist", err)
	}
	if err := f.Copy("/a.txt", "/nope/a.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing destination error = %v, want os.ErrNotExist", err)
	}
}

func TestCopyRootRejected(t *testing.T) {
	f := New(t.TempDir())
	if err := f.Copy("/", "/dest"); !errors.Is(err, ErrRootOperation) {
		t.Fatalf("copy root error = %v, want ErrRootOperation", err)
	}
	if err := f.Copy("/a.txt", "/"); !errors.Is(err, ErrRootOperation) {
		t.Fatalf("copy onto root error = %v, want ErrRootOperation", err)
	}
}

func TestCopySamePathFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Copy("/a.txt", "/a.txt"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("copy onto itself error = %v, want os.ErrExist", err)
	}
}

func TestCopySymlinkCopiesLink(t *testing.T) {
	outside := t.TempDir()
	target := filepath.Join(outside, "target.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if err := f.Copy("/link", "/dest/link"); err != nil {
		t.Fatalf("copy symlink: %v", err)
	}
	info, err := os.Lstat(filepath.Join(root, "dest", "link"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("copy is not a symlink: %v, %v", info, err)
	}
	got, err := os.Readlink(filepath.Join(root, "dest", "link"))
	if err != nil || got != target {
		t.Fatalf("copied link target = %q, %v; want %q", got, err, target)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "outside" {
		t.Fatalf("link target changed: %q, %v", data, err)
	}
}

func TestTrashMovesFileAndCreatesTrashFolder(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	to, err := f.Trash("/a.txt")
	if err != nil {
		t.Fatalf("trash: %v", err)
	}
	if to != "/"+TrashDir+"/a.txt" {
		t.Fatalf("trashed path = %q, want /%s/a.txt", to, TrashDir)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("source still exists: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, TrashDir, "a.txt"))
	if err != nil || string(data) != "payload" {
		t.Fatalf("trashed file = %q, %v; want payload", data, err)
	}
}

func TestTrashDirectoryKeepsContents(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "nested", "note.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if _, err := f.Trash("/docs"); err != nil {
		t.Fatalf("trash dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Fatalf("source dir still exists: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, TrashDir, "docs", "nested", "note.txt"))
	if err != nil || string(data) != "hi" {
		t.Fatalf("nested file = %q, %v; want hi", data, err)
	}
}

func TestTrashNameCollisionGetsUniqueName(t *testing.T) {
	root := t.TempDir()
	f := New(root)

	for _, content := range []string{"first", "second", "third"} {
		if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Trash("/a.txt"); err != nil {
			t.Fatalf("trash %s: %v", content, err)
		}
	}
	first, err := os.ReadFile(filepath.Join(root, TrashDir, "a.txt"))
	if err != nil || string(first) != "first" {
		t.Fatalf("a.txt = %q, %v; want first", first, err)
	}
	second, err := os.ReadFile(filepath.Join(root, TrashDir, "a 2.txt"))
	if err != nil || string(second) != "second" {
		t.Fatalf("a 2.txt = %q, %v; want second", second, err)
	}
	third, err := os.ReadFile(filepath.Join(root, TrashDir, "a 3.txt"))
	if err != nil || string(third) != "third" {
		t.Fatalf("a 3.txt = %q, %v; want third", third, err)
	}
}

func TestTrashRootAndTrashItselfRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, TrashDir, "old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, TrashDir+"-old"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	if _, err := f.Trash("/"); !errors.Is(err, ErrRootOperation) {
		t.Fatalf("trash root error = %v, want ErrRootOperation", err)
	}
	if _, err := f.Trash("/" + TrashDir); !errors.Is(err, ErrInTrash) {
		t.Fatalf("trash folder error = %v, want ErrInTrash", err)
	}
	if _, err := f.Trash("/" + TrashDir + "/old"); !errors.Is(err, ErrInTrash) {
		t.Fatalf("entry inside trash error = %v, want ErrInTrash", err)
	}
	// A sibling whose name merely starts with the trash name is fine.
	if _, err := f.Trash("/" + TrashDir + "-old"); err != nil {
		t.Fatalf("trash sibling prefix: %v", err)
	}
}

func TestTrashMissingFails(t *testing.T) {
	f := New(t.TempDir())

	if _, err := f.Trash("/missing.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("trash missing error = %v, want os.ErrNotExist", err)
	}
}

func TestTrashSymlinkTrashesLinkOnly(t *testing.T) {
	outside := t.TempDir()
	target := filepath.Join(outside, "target.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	f := New(root)

	if _, err := f.Trash("/link"); err != nil {
		t.Fatalf("trash symlink: %v", err)
	}
	info, err := os.Lstat(filepath.Join(root, TrashDir, "link"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("trashed entry is not a symlink: %v, %v", info, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "outside" {
		t.Fatalf("link target changed: %q, %v", data, err)
	}
}

func TestEmptyTrash(t *testing.T) {
	root := t.TempDir()
	f := New(root)
	// keep.txt stays at the root and must survive; gone.txt is trashed.
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gone.txt"), []byte("gone"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Trash("/gone.txt"); err != nil {
		t.Fatal(err)
	}

	removed, err := f.EmptyTrash()
	if err != nil {
		t.Fatalf("empty trash: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(filepath.Join(root, TrashDir, "gone.txt")); !os.IsNotExist(err) {
		t.Fatalf("trashed file survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, TrashDir)); err != nil {
		t.Fatalf("trash folder itself must survive: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "keep.txt")); err != nil || string(data) != "keep" {
		t.Fatalf("file outside the trash must be untouched: %q, %v", data, err)
	}

	// Emptying again is a no-op...
	removed, err = f.EmptyTrash()
	if err != nil || removed != 0 {
		t.Fatalf("empty again = (%d, %v), want (0, nil)", removed, err)
	}
	// ...and so is emptying without a trash folder.
	if err := os.Remove(filepath.Join(root, TrashDir)); err != nil {
		t.Fatal(err)
	}
	removed, err = f.EmptyTrash()
	if err != nil || removed != 0 {
		t.Fatalf("empty without trash folder = (%d, %v), want (0, nil)", removed, err)
	}
}

func TestEmptyTrashRemovesNestedFoldersAndSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "target.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "inner.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "target.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := f.Trash("/docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Trash("/link.txt"); err != nil {
		t.Fatal(err)
	}

	if removed, err := f.EmptyTrash(); err != nil || removed != 2 {
		t.Fatalf("empty = (%d, %v), want (2, nil)", removed, err)
	}
	if data, err := os.ReadFile(filepath.Join(outside, "target.txt")); err != nil || string(data) != "outside" {
		t.Fatalf("symlink target must be untouched: %q, %v", data, err)
	}
}

func TestSearchMatchesCaseInsensitivelyAndSortsLikeList(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "2024 report.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unrelated.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	base, entries, err := f.Search("/", "REPORT", false)
	if err != nil {
		t.Fatal(err)
	}
	if base != "/" {
		t.Fatalf("base = %q, want /", base)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}
	// Directories sort before files regardless of name.
	if !entries[0].IsDir || entries[0].Name != "Reports" {
		t.Fatalf("first entry = %+v, want directory Reports", entries[0])
	}
	if entries[1].Name != "2024 report.txt" || entries[1].IsDir {
		t.Fatalf("second entry = %+v, want file 2024 report.txt", entries[1])
	}
	if entries[1].Path != "/2024 report.txt" {
		t.Fatalf("path = %q, want /2024 report.txt", entries[1].Path)
	}
}

func TestSearchSkipsHiddenUnlessAsked(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".hiddendir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".hiddendir", "needle.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "visible-needle.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)
	if _, err := f.Trash("/visible-needle.txt"); err != nil {
		t.Fatal(err)
	}

	// Hidden: the dotfile, the dot folder's subtree and the trash all stay out.
	_, entries, err := f.Search("/", "needle", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("got %d entries without showHidden, want 0: %+v", len(entries), entries)
	}

	_, entries, err = f.Search("/", "needle", true)
	if err != nil {
		t.Fatal(err)
	}
	// With showHidden the dotfile and the trash entry appear, but the dot
	// folder itself ("hiddendir" does not contain "needle") does not.
	if len(entries) != 2 {
		t.Fatalf("got %d entries with showHidden, want 2: %+v", len(entries), entries)
	}
	if entries[0].Path != "/.hiddendir/needle.txt" || entries[1].Path != "/.trash/visible-needle.txt" {
		t.Fatalf("entries = %+v, want the hidden-dir and trash matches", entries)
	}
}

func TestSearchScopesToStartFolder(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "needle.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other", "needle.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	base, entries, err := f.Search("/docs", "needle", false)
	if err != nil {
		t.Fatal(err)
	}
	if base != "/docs" {
		t.Fatalf("base = %q, want /docs", base)
	}
	if len(entries) != 1 || entries[0].Path != "/docs/needle.txt" {
		t.Fatalf("entries = %+v, want only /docs/needle.txt", entries)
	}
}

func TestSearchSkipsSymlinkedDirectories(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "needle.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linkdir")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	f := New(root)

	_, entries, err := f.Search("/", "needle", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("search followed a symlinked directory: %+v", entries)
	}
}

func TestSearchCapsResults(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < MaxSearchResults+10; i++ {
		name := fmt.Sprintf("capfile-%03d.txt", i)
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f := New(root)

	_, entries, err := f.Search("/", "capfile", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != MaxSearchResults {
		t.Fatalf("got %d entries, want the %d cap", len(entries), MaxSearchResults)
	}
}

func TestSearchEmptyQueryFails(t *testing.T) {
	f := New(t.TempDir())
	for _, q := range []string{"", "   "} {
		if _, _, err := f.Search("/", q, false); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("Search(%q) error = %v, want ErrInvalidName", q, err)
		}
	}
}

func TestZipFolder(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "target.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "sub", "b.txt"), []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An empty folder and a symlink must survive the round trip.
	if err := os.Mkdir(filepath.Join(root, "docs", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "target.txt"), filepath.Join(root, "docs", "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	f := New(root)

	var buf bytes.Buffer
	if err := f.Zip("/docs", &buf); err != nil {
		t.Fatalf("zip: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip output is not a valid archive: %v", err)
	}

	byName := make(map[string]*zip.File, len(zr.File))
	for _, file := range zr.File {
		byName[file.Name] = file
	}
	for _, name := range []string{"docs/", "docs/a.txt", "docs/sub/", "docs/sub/b.txt", "docs/empty/", "docs/link.txt"} {
		if byName[name] == nil {
			t.Errorf("archive is missing %q (has %v)", name, names(zr.File))
		}
	}
	if len(zr.File) != 6 {
		t.Errorf("archive holds %d entries, want 6: %v", len(zr.File), names(zr.File))
	}
	if data := readZip(t, byName["docs/a.txt"]); string(data) != "alpha" {
		t.Errorf("docs/a.txt = %q, want alpha", data)
	}
	if link := byName["docs/link.txt"]; link != nil {
		if link.Mode()&fs.ModeSymlink == 0 {
			t.Errorf("docs/link.txt mode = %v, want symlink", link.Mode())
		}
		if target := string(readZip(t, link)); target != filepath.Join(outside, "target.txt") {
			t.Errorf("docs/link.txt target = %q", target)
		}
	}
}

func names(files []*zip.File) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Name
	}
	return out
}

func readZip(t *testing.T, file *zip.File) []byte {
	t.Helper()
	if file == nil {
		t.Fatal("nil zip entry")
	}
	rc, err := file.Open()
	if err != nil {
		t.Fatalf("open %s: %v", file.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read %s: %v", file.Name, err)
	}
	return data
}

func TestZipRejectsFilesAndEscapes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plain.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	var buf bytes.Buffer
	if err := f.Zip("/plain.txt", &buf); err == nil {
		t.Fatal("zipping a file should fail")
	}
	if buf.Len() != 0 {
		t.Fatalf("failed zip wrote %d bytes, want 0", buf.Len())
	}
	if err := f.Zip("/missing", &buf); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("zip missing error = %v, want os.ErrNotExist", err)
	}
	// Traversal is clamped into the root, so it fails with "not exist" rather
	// than an escape; either way it must not zip anything outside.
	if err := f.Zip("/../etc", &buf); err == nil {
		t.Fatal("zipping a clamped traversal path should fail")
	}
}

func TestZipMulti(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "one"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "two"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "one", "readme.txt"), []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "two", "readme.txt"), []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "two", "extra.txt"), []byte("extra"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(root)

	var buf bytes.Buffer
	// Both folders hold a readme.txt: the second lands as "readme 2.txt",
	// and the whole two/ folder comes along as a subtree.
	if err := f.ZipMulti([]string{"/one/readme.txt", "/two/readme.txt", "/two"}, &buf); err != nil {
		t.Fatalf("zip multi: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]string)
	for _, file := range zr.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		byName[file.Name] = string(data)
	}
	want := map[string]string{
		"readme.txt":     "first",
		"readme 2.txt":   "second",
		"two/":           "",
		"two/readme.txt": "second",
		"two/extra.txt":  "extra",
	}
	if len(byName) != len(want) {
		t.Fatalf("archive has %v, want exactly %v", byName, want)
	}
	for name, content := range want {
		if got, ok := byName[name]; !ok || got != content {
			t.Errorf("entry %q = %q (present: %v), want %q", name, got, ok, content)
		}
	}
}
