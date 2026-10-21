// Package files provides read-only, root-confined access to the filesystem.
package files

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

var (
	// ErrForbidden is returned when a requested path escapes the configured root.
	ErrForbidden = errors.New("path is outside the configured root")
	// ErrRootOperation is returned when an operation targets the root itself.
	ErrRootOperation = errors.New("operation is not allowed on the root directory")
	// ErrDirNotEmpty is returned for a non-recursive delete of a non-empty directory.
	ErrDirNotEmpty = errors.New("directory is not empty")
	// ErrInvalidName is returned when a new name is empty or contains path separators.
	ErrInvalidName = errors.New("invalid name")
	// ErrMoveIntoSelf is returned when a folder is moved into its own subtree.
	ErrMoveIntoSelf = errors.New("cannot move a folder into itself")
)

// MaxReadSize is the largest file the API will return as a text preview.
const MaxReadSize = 2 << 20 // 2 MiB

// MaxUploadSize is the largest total upload accepted by the API.
const MaxUploadSize = 50 << 20 // 50 MiB

// MaxWriteSize is the largest body accepted when saving an edited file.
const MaxWriteSize = 8 << 20 // 8 MiB

// Entry is a serialisable description of a file or directory.
type Entry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"` // slash-separated, relative to the root
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	Mode    string    `json:"mode"`
}

// FS confines all operations to a single root directory.
type FS struct {
	root string
}

// New creates an FS rooted at root. The path is made absolute and symlinks in
// the root itself are resolved so containment checks compare real paths.
func New(root string) *FS {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return &FS{root: root}
}

// Root returns the absolute root directory.
func (f *FS) Root() string { return f.root }

// resolve turns a slash-separated, root-relative request path into an absolute
// filesystem path, rejecting anything that escapes the root (including via
// symlinks).
func (f *FS) resolve(rel string) (string, error) {
	clean := path.Clean("/" + strings.TrimSpace(rel))
	full := filepath.Join(f.root, filepath.FromSlash(clean))
	if full != f.root && !strings.HasPrefix(full, f.root+string(os.PathSeparator)) {
		return "", ErrForbidden
	}
	// A symlink inside the root can still point outside of it.
	if resolved, err := filepath.EvalSymlinks(full); err == nil {
		if resolved != f.root && !strings.HasPrefix(resolved, f.root+string(os.PathSeparator)) {
			return "", ErrForbidden
		}
	}
	return full, nil
}

// relOf converts an absolute path back to its root-relative slash form.
func (f *FS) relOf(full string) string {
	rel, err := filepath.Rel(f.root, full)
	if err != nil || rel == "." {
		return "/"
	}
	return "/" + filepath.ToSlash(rel)
}

func (f *FS) entry(name, rel string, info os.FileInfo) Entry {
	return Entry{
		Name:    name,
		Path:    rel,
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		ModTime: info.ModTime().UTC(),
		Mode:    info.Mode().String(),
	}
}

// List returns the cleaned relative path and the sorted contents of a
// directory. Directories sort before files, then case-insensitively by name.
// Dot entries are skipped unless showHidden is set.
func (f *FS) List(rel string, showHidden bool) (string, []Entry, error) {
	full, err := f.resolve(rel)
	if err != nil {
		return "", nil, err
	}
	dir, err := os.ReadDir(full)
	if err != nil {
		return "", nil, err
	}

	base := f.relOf(full)
	entries := make([]Entry, 0, len(dir))
	for _, d := range dir {
		if !showHidden && strings.HasPrefix(d.Name(), ".") {
			continue
		}
		info, err := d.Info()
		if err != nil {
			continue // e.g. broken symlink or permission error
		}
		entries = append(entries, f.entry(d.Name(), path.Join(base, d.Name()), info))
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return base, entries, nil
}

// Stat returns metadata for a single path.
func (f *FS) Stat(rel string) (Entry, error) {
	full, err := f.resolve(rel)
	if err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return Entry{}, err
	}
	return f.entry(info.Name(), f.relOf(full), info), nil
}

// ReadFile returns the cleaned relative path and the contents of a file. Files
// larger than MaxReadSize are rejected; callers should offer a download.
func (f *FS) ReadFile(rel string) (string, []byte, error) {
	full, err := f.resolve(rel)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", nil, err
	}
	if info.IsDir() {
		return "", nil, fmt.Errorf("%s is a directory", rel)
	}
	if info.Size() > MaxReadSize {
		return "", nil, fmt.Errorf("file is too large to preview (%d bytes); download it instead", info.Size())
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", nil, err
	}
	return f.relOf(full), data, nil
}

// Open returns an open file handle and its info for streaming downloads.
func (f *FS) Open(rel string) (*os.File, os.FileInfo, error) {
	full, err := f.resolve(rel)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		return nil, nil, fmt.Errorf("%s is a directory", rel)
	}
	file, err := os.Open(full)
	if err != nil {
		return nil, nil, err
	}
	return file, info, nil
}

// resolveTarget resolves a path for filesystem mutations. Unlike resolve, it
// does not follow the final path element, so operating on a symlink affects the
// link itself rather than its target. The parent directory is resolved and must
// stay inside the root. The root itself is rejected.
func (f *FS) resolveTarget(rel string) (string, error) {
	clean := path.Clean("/" + strings.TrimSpace(rel))
	full := filepath.Join(f.root, filepath.FromSlash(clean))
	if full != f.root && !strings.HasPrefix(full, f.root+string(os.PathSeparator)) {
		return "", ErrForbidden
	}
	if full == f.root {
		return "", ErrRootOperation
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(full))
	if err != nil {
		return "", err
	}
	if resolvedParent != f.root && !strings.HasPrefix(resolvedParent, f.root+string(os.PathSeparator)) {
		return "", ErrForbidden
	}
	return full, nil
}

// Mkdir creates a new directory at rel. Its parent must already exist and be
// inside the root, and the directory must not already exist.
func (f *FS) Mkdir(rel string) error {
	full, err := f.resolveTarget(rel)
	if err != nil {
		return err
	}
	return os.Mkdir(full, 0o755)
}

// Rename changes the base name of the entry at rel to newName, keeping it in the
// same directory. newName must be a plain name with no path separators. Renaming
// onto an existing path fails with os.ErrExist rather than overwriting it.
func (f *FS) Rename(rel, newName string) error {
	if !validName(newName) {
		return ErrInvalidName
	}
	oldFull, err := f.resolveTarget(rel)
	if err != nil {
		return err
	}
	newFull := filepath.Join(filepath.Dir(oldFull), newName)
	if newFull == oldFull {
		return nil // nothing to do
	}
	if _, err := os.Lstat(newFull); err == nil {
		return os.ErrExist
	}
	return os.Rename(oldFull, newFull)
}

// Move moves the entry at from to the full path to, which may be in another
// directory. Both paths go through resolveTarget, so a symlink is moved as a
// link and never resolves its target. The root cannot be moved, a folder cannot
// be moved into itself or its own subtree, and an existing destination is
// refused rather than overwritten.
func (f *FS) Move(from, to string) error {
	fromFull, err := f.resolveTarget(from)
	if err != nil {
		return err
	}
	toFull, err := f.resolveTarget(to)
	if err != nil {
		return err
	}
	if fromFull == toFull {
		return nil // nothing to do
	}
	if !validName(filepath.Base(toFull)) {
		return ErrInvalidName
	}
	info, err := os.Lstat(fromFull)
	if err != nil {
		return err
	}
	// Compare cleaned relative paths so "docs/../docs/sub" cannot dodge the
	// check. The trailing separator keeps "/docs2" from matching "/docs".
	fromRel := path.Clean("/" + strings.TrimSpace(from))
	toRel := path.Clean("/" + strings.TrimSpace(to))
	if info.IsDir() && strings.HasPrefix(toRel+"/", fromRel+"/") {
		return ErrMoveIntoSelf
	}
	parent, err := os.Stat(filepath.Dir(toFull))
	if err != nil {
		return err
	}
	if !parent.IsDir() {
		return os.ErrNotExist
	}
	if _, err := os.Lstat(toFull); err == nil {
		return os.ErrExist
	}
	return os.Rename(fromFull, toFull)
}

// validName reports whether name is usable as a single file/folder name.
func validName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsRune(name, '/') && !strings.ContainsRune(name, os.PathSeparator)
}

// Create makes a new, empty file at rel. It fails with os.ErrExist if the path
// already exists.
func (f *FS) Create(rel string) error {
	full, err := f.resolveTarget(rel)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return file.Close()
}

// Write replaces the contents of the file at rel. Writing through a symlink is
// refused so a link can never be used to modify a file outside the root.
func (f *FS) Write(rel string, data []byte) error {
	full, err := f.resolveTarget(rel)
	if err != nil {
		return err
	}
	info, err := os.Lstat(full)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrForbidden
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", rel)
	}
	return os.WriteFile(full, data, 0o644)
}

// Upload streams src into a new file called name inside dirRel. It fails with
// os.ErrExist if the target already exists and never overwrites.
func (f *FS) Upload(dirRel, name string, src io.Reader) error {
	if !validName(name) {
		return ErrInvalidName
	}
	full, err := f.resolveTarget(path.Join(dirRel, name))
	if err != nil {
		return err
	}
	file, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, src)
	return err
}

// DiskInfo describes the capacity of the filesystem holding the root.
type DiskInfo struct {
	Path       string `json:"path"`
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
}

// Disk reports the capacity of the filesystem containing the root. FreeBytes is
// the space available to the current (unprivileged) process.
func (f *FS) Disk() (DiskInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(f.root, &stat); err != nil {
		return DiskInfo{}, err
	}
	blockSize := uint64(stat.Bsize)
	total := stat.Blocks * blockSize
	free := stat.Bavail * blockSize
	used := (stat.Blocks - stat.Bfree) * blockSize
	return DiskInfo{
		Path:       f.root,
		TotalBytes: total,
		FreeBytes:  free,
		UsedBytes:  used,
	}, nil
}

// Delete removes a file or directory. Directories are removed non-recursively
// unless recursive is true, in which case their entire contents are removed.
func (f *FS) Delete(rel string, recursive bool) error {
	full, err := f.resolveTarget(rel)
	if err != nil {
		return err
	}

	info, err := os.Lstat(full)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		// Files and symlinks (even ones pointing outside the root) are unlinked.
		return os.Remove(full)
	}

	if recursive {
		return os.RemoveAll(full)
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return ErrDirNotEmpty
	}
	return os.Remove(full)
}
