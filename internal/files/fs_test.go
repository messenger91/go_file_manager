package files

import (
	"errors"
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
