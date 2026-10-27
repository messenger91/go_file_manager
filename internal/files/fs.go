// Package files provides read-only, root-confined access to the filesystem.
package files

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	// ErrCopyIntoSelf is returned when a folder is copied into its own subtree.
	ErrCopyIntoSelf = errors.New("cannot copy a folder into itself")
	// ErrUnsupported is returned for entries that cannot be copied (sockets,
	// devices, FIFOs).
	ErrUnsupported = errors.New("copying this file type is not supported")
	// ErrInTrash is returned when trashing the trash folder itself or an entry
	// that already lives inside it.
	ErrInTrash = errors.New("path is already in the trash")
	// ErrArchiveTooLarge is returned when an archive declares more
	// uncompressed content than files.MaxExtractSize.
	ErrArchiveTooLarge = errors.New("archive is too large to extract")
)

// MaxReadSize is the largest file the API will return as a text preview.
const MaxReadSize = 2 << 20 // 2 MiB

// MaxUploadSize is the largest total upload accepted by the API.
const MaxUploadSize = 50 << 20 // 50 MiB

// MaxWriteSize is the largest body accepted when saving an edited file.
const MaxWriteSize = 8 << 20 // 8 MiB

// MaxSearchResults caps how many entries Search returns; the API reports a
// truncation when the result length reaches it.
const MaxSearchResults = 200

// TrashDir is the name of the trash folder at the root. It starts with a dot,
// so listings hide it unless hidden entries are requested.
const TrashDir = ".trash"

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

// Copy copies the entry at from to the full path to, recursively for folders.
// Symlinks are recreated as links and never followed, so a link cannot pull
// content in from outside the root. The destination must not exist, its parent
// must exist, and a folder cannot be copied into itself or its own subtree. A
// failed copy removes whatever was written so far.
func (f *FS) Copy(from, to string) error {
	fromFull, err := f.resolveTarget(from)
	if err != nil {
		return err
	}
	toFull, err := f.resolveTarget(to)
	if err != nil {
		return err
	}
	if fromFull == toFull {
		return os.ErrExist
	}
	if !validName(filepath.Base(toFull)) {
		return ErrInvalidName
	}
	info, err := os.Lstat(fromFull)
	if err != nil {
		return err
	}
	fromRel := path.Clean("/" + strings.TrimSpace(from))
	toRel := path.Clean("/" + strings.TrimSpace(to))
	if info.IsDir() && strings.HasPrefix(toRel+"/", fromRel+"/") {
		return ErrCopyIntoSelf
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
	if err := copyEntry(fromFull, toFull, info); err != nil {
		_ = os.RemoveAll(toFull)
		return err
	}
	return nil
}

// copyEntry copies one entry, recursing into directories.
func copyEntry(src, dst string, info os.FileInfo) error {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case info.IsDir():
		// Add owner access while filling so a read-only source directory can
		// still receive its children; the final mode is restored afterwards.
		if err := os.Mkdir(dst, info.Mode().Perm()|0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			childInfo, err := entry.Info()
			if err != nil {
				return err
			}
			if err := copyEntry(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()), childInfo); err != nil {
				return err
			}
		}
		return os.Chmod(dst, info.Mode().Perm())
	case info.Mode().IsRegular():
		return copyFile(src, dst, info)
	default:
		return ErrUnsupported
	}
}

func copyFile(src, dst string, info os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
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

// Upload streams src into a new file at dirRel/relPath, failing with
// os.ErrExist if the target already exists and never overwriting. relPath is
// usually a plain file name, but folder uploads pass a relative path such as
// "docs/img/a.png"; intermediate folders are created. The path is cleaned
// with the same clamping as every other path — ".." segments fold back into
// dirRel, so an upload can never escape the root — and backslashes are
// normalised to slashes first.
func (f *FS) Upload(dirRel, relPath string, src io.Reader) error {
	clean := path.Clean("/" + strings.ReplaceAll(relPath, "\\", "/"))
	if clean == "/" {
		return ErrInvalidName
	}
	dirFull, err := f.resolve(dirRel)
	if err != nil {
		return err
	}

	sub := filepath.FromSlash(strings.TrimPrefix(clean, "/"))
	full := filepath.Join(dirFull, sub)
	// The clamping above already keeps the target inside dirFull; the prefix
	// check is belt and braces against future changes.
	if full != dirFull && !strings.HasPrefix(full, dirFull+string(os.PathSeparator)) {
		return ErrForbidden
	}
	if parent := filepath.Dir(full); parent != dirFull {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
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

// Trash moves the entry at rel into the trash folder (TrashDir at the root),
// creating that folder on first use. It behaves like Move: the source goes
// through resolveTarget, so a symlink is trashed as a link and its target is
// left alone. Trashing the root, the trash folder itself or an entry already
// inside it fails. If the trash folder already holds an entry with the same
// name, " 2", " 3", … is inserted before the extension. The return value is
// the trashed entry's new root-relative path.
func (f *FS) Trash(rel string) (string, error) {
	fromFull, err := f.resolveTarget(rel)
	if err != nil {
		return "", err
	}
	fromRel := path.Clean("/" + strings.TrimSpace(rel))
	trashRel := "/" + TrashDir
	// The trailing separator keeps "/.trash-old" from matching "/.trash".
	if fromRel == trashRel || strings.HasPrefix(fromRel+"/", trashRel+"/") {
		return "", ErrInTrash
	}

	// The trash folder is created on demand, and an error other than
	// "already exists" (e.g. a file named .trash) must fail the trashing.
	trashFull := filepath.Join(f.root, TrashDir)
	if err := os.Mkdir(trashFull, 0o755); err != nil && !os.IsExist(err) {
		return "", err
	}

	name := filepath.Base(fromFull)
	if _, err := os.Lstat(filepath.Join(trashFull, name)); err == nil {
		name = uniqueTrashedName(trashFull, name)
	}
	toFull := filepath.Join(trashFull, name)
	if err := os.Rename(fromFull, toFull); err != nil {
		return "", err
	}
	return f.relOf(toFull), nil
}

// uniqueTrashedName returns a name not yet present in dir, derived from name by
// inserting " 2", " 3", … before the extension ("a.txt" → "a 2.txt", a dotfile
// or a folder → ".env 2", "docs 2").
func uniqueTrashedName(dir, name string) string {
	base, ext := name, ""
	if i := strings.LastIndexByte(name, '.'); i > 0 && i < len(name)-1 {
		base, ext = name[:i], name[i:]
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s %d%s", base, n, ext)
		if _, err := os.Lstat(filepath.Join(dir, candidate)); err != nil {
			return candidate
		}
	}
}

// EmptyTrash permanently removes every entry inside the trash folder, leaving
// the folder itself in place. Entries are removed like Delete removes them:
// symlinks are unlinked rather than followed. A missing trash folder is not an
// error — there is simply nothing to remove. It returns how many entries were
// removed.
func (f *FS) EmptyTrash() (int, error) {
	trashFull := filepath.Join(f.root, TrashDir)
	entries, err := os.ReadDir(trashFull)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(trashFull, entry.Name())); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// Search walks the folder at rootRel recursively and returns entries whose
// name contains query, case-insensitively. The result is ordered like List
// (directories first, then case-insensitively by name) and capped at
// MaxSearchResults entries, collected in walk order before sorting. Dot
// entries — and the whole subtree beneath a dot folder such as the trash —
// are skipped unless showHidden is set. Directory symlinks are never
// followed, so a link cannot pull the search outside the root. The query must
// not be empty (ErrInvalidName); the root itself is never a match.
func (f *FS) Search(rootRel, query string, showHidden bool) (string, []Entry, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return "", nil, ErrInvalidName
	}
	full, err := f.resolve(rootRel)
	if err != nil {
		return "", nil, err
	}
	entries := []Entry{}
	err = filepath.WalkDir(full, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if p == full {
				return walkErr // the searched folder itself is unreadable
			}
			return nil // skip unreadable subtrees like List skips dead entries
		}
		if p == full {
			return nil
		}
		name := d.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.Contains(strings.ToLower(name), needle) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil // broken symlink or an entry that just vanished
		}
		entries = append(entries, f.entry(name, f.relOf(p), info))
		if len(entries) >= MaxSearchResults {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return f.relOf(full), entries, nil
}

// Zip writes the folder at rel into w as a zip archive whose root entry is the
// folder itself, so extracting does not splatter files. Symlinks are stored as
// links and never followed, special files (sockets, devices, FIFOs) are left
// out, and unreadable subtrees are skipped like List skips dead entries.
// Errors are only returned while the folder itself is still unreadable: once
// writing has started the caller cannot report a failure in the status code,
// so entries that vanish or refuse to open mid-walk are skipped instead.
func (f *FS) Zip(rel string, w io.Writer) error {
	full, err := f.resolve(rel)
	if err != nil {
		return err
	}
	info, err := os.Stat(full)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", rel)
	}

	zipw := zip.NewWriter(w)
	if err := zipDir(zipw, full, info, filepath.Base(full)); err != nil {
		_ = zipw.Close()
		return err
	}
	return zipw.Close()
}

// ZipMulti writes several entries into one archive: files land at the archive
// root under their own names, folders as subtrees rooted at the folder. Name
// collisions get " 2", " 3", … appended. Everything is resolved and stated
// before the first byte is written, so the caller can still report a bad path
// in its status code; mid-walk problems are skipped like in Zip.
func (f *FS) ZipMulti(rels []string, w io.Writer) error {
	type target struct {
		full string
		info os.FileInfo
	}
	targets := make([]target, 0, len(rels))
	for _, rel := range rels {
		full, err := f.resolve(rel)
		if err != nil {
			return err
		}
		info, err := os.Stat(full)
		if err != nil {
			return err
		}
		targets = append(targets, target{full: full, info: info})
	}

	used := map[string]bool{}
	zipw := zip.NewWriter(w)
	for _, t := range targets {
		arcname := uniqueArcname(used, filepath.Base(t.full))
		var err error
		if t.info.IsDir() {
			err = zipDir(zipw, t.full, t.info, arcname)
		} else {
			err = zipOne(zipw, t.full, t.info, arcname)
		}
		if err != nil {
			_ = zipw.Close()
			return err
		}
	}
	return zipw.Close()
}

// MaxExtractSize caps the total uncompressed size of one extraction.
const MaxExtractSize = 1 << 30 // 1 GiB

// Extract unpacks the zip archive at rel into a new folder next to it, named
// after the archive ("docs.zip" -> "docs"). The destination must not exist,
// and a failed extraction removes whatever was written so far. When every
// entry lives under one common top-level folder, that wrapper is stripped, so
// the archives this app's zip download produces extract without double
// nesting. Safety rules: entry names are validated against path traversal
// (zip-slip) outright, symlink entries are refused (a zip can declare a link
// pointing anywhere), and the declared total size is capped at
// MaxExtractSize before anything is written. Stored permission bits are
// honoured, with owner read/write and directory access forced on.
func (f *FS) Extract(rel string) (string, error) {
	full, err := f.resolve(rel)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", rel)
	}

	base := filepath.Base(full)
	destName := base
	if strings.EqualFold(path.Ext(base), ".zip") {
		destName = strings.TrimSuffix(base, path.Ext(base))
	}
	if !validName(destName) {
		return "", ErrInvalidName
	}
	destFull := filepath.Join(filepath.Dir(full), destName)
	if _, err := os.Lstat(destFull); err == nil {
		return "", os.ErrExist
	}

	archive, err := os.Open(full)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	zr, err := zip.NewReader(archive, info.Size())
	if err != nil {
		return "", err
	}

	// Validate everything before touching the destination: one bad entry
	// refuses the whole extraction.
	var total int64
	for _, zf := range zr.File {
		clean := path.Clean(zf.Name)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			return "", ErrInvalidName
		}
		if zf.FileInfo().Mode()&os.ModeSymlink != 0 {
			return "", ErrUnsupported
		}
		total += int64(zf.UncompressedSize64)
		if total > MaxExtractSize {
			return "", ErrArchiveTooLarge
		}
	}

	names := make([]string, len(zr.File))
	for i, zf := range zr.File {
		names[i] = path.Clean(zf.Name)
	}
	if stripped, ok := stripCommonRoot(zr.File); ok {
		names = stripped
	}

	if err := os.Mkdir(destFull, 0o755); err != nil {
		return "", err
	}
	if err := extractEntries(zr, names, destFull); err != nil {
		_ = os.RemoveAll(destFull)
		return "", err
	}
	return f.relOf(destFull), nil
}

// stripCommonRoot maps entry names into a common top-level folder, if there is
// one: every entry must either be that folder itself (a directory entry) or
// live inside it. Otherwise ok is false and the caller keeps the plain names.
func stripCommonRoot(entries []*zip.File) ([]string, bool) {
	if len(entries) == 0 {
		return nil, false
	}
	clean := path.Clean(entries[0].Name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return nil, false
	}
	// The wrapper folder is the first entry's first segment — unless the first
	// entry is the wrapper's own directory entry, whose name is the folder.
	root, _, found := strings.Cut(clean, "/")
	if !found {
		if !entries[0].FileInfo().IsDir() {
			return nil, false
		}
		root = clean
	}
	names := make([]string, len(entries))
	for i, zf := range entries {
		c := path.Clean(zf.Name)
		if c == root {
			if !zf.FileInfo().IsDir() {
				return nil, false // a file where the wrapper folder should be
			}
			names[i] = "."
			continue
		}
		rest, ok := strings.CutPrefix(c, root+"/")
		if !ok || rest == "" {
			return nil, false
		}
		names[i] = rest
	}
	return names, true
}

// extractEntries writes the (already validated) entries into dest. A name of
// "." is the stripped wrapper folder itself and creates nothing.
func extractEntries(zr *zip.Reader, names []string, dest string) error {
	for i, zf := range zr.File {
		if names[i] == "." {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(names[i]))
		if target != dest && !strings.HasPrefix(target, dest+string(os.PathSeparator)) {
			return ErrForbidden
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		perm := zf.FileInfo().Mode().Perm() | 0o600
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		closeErr := out.Close()
		rc.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

// uniqueArcname returns name, or name with " 2", " 3", … inserted before the
// extension, so two selected entries with the same name both land in the
// archive.
func uniqueArcname(used map[string]bool, name string) string {
	if !used[name] {
		used[name] = true
		return name
	}
	base, ext := name, ""
	if i := strings.LastIndexByte(name, '.'); i > 0 && i < len(name)-1 {
		base, ext = name[:i], name[i:]
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s %d%s", base, n, ext)
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}

// zipDir writes the tree rooted at full under the archive name arcname (the
// folder itself first, with a trailing slash).
func zipDir(zipw *zip.Writer, full string, info os.FileInfo, arcname string) error {
	if err := zipOne(zipw, full, info, arcname); err != nil {
		return err
	}
	return filepath.WalkDir(full, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if p == full {
				return walkErr
			}
			return nil // skip unreadable subtrees
		}
		if p == full {
			return nil // the root entry is already written
		}
		relToRoot, err := filepath.Rel(full, p)
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil // broken symlink or an entry that just vanished
		}
		return zipOne(zipw, p, info, arcname+"/"+filepath.ToSlash(relToRoot))
	})
}

// zipOne writes a single entry (symlink, directory or regular file) and skips
// anything it cannot read or represent.
func zipOne(zipw *zip.Writer, path string, info os.FileInfo, arcname string) error {
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return nil
	}
	header.Name = arcname
	if info.IsDir() {
		header.Name += "/"
	}
	header.Modified = info.ModTime()

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return nil
		}
		writer, err := zipw.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = io.WriteString(writer, target)
		return err
	case info.IsDir():
		_, err := zipw.CreateHeader(header)
		return err
	case info.Mode().IsRegular():
		file, err := os.Open(path)
		if err != nil {
			return nil // skip files that refuse to open
		}
		defer file.Close()
		writer, err := zipw.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = io.Copy(writer, file)
		return err
	default:
		return nil
	}
}
