// Package server wires the HTTP API and static web UI together.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go_file_manager/internal/config"
	"go_file_manager/internal/files"
	"go_file_manager/internal/host"
	"go_file_manager/internal/version"
)

// Server serves the read-only file manager API and web UI.
type Server struct {
	cfg      *config.Config
	fm       *files.FS
	web      fs.FS
	logger   *log.Logger
	sessions *sessionStore
	logins   *loginThrottle
	shares   *shareStore
	// passwordHash is the parsed PASSWORD_HASH; nil falls back to the
	// plaintext PASSWORD comparison. A malformed hash stays nil and refuses
	// every password (fail closed) after logging a warning.
	passwordHash *passwordVerifier
}

// New builds a Server. web is the filesystem holding the static UI assets.
func New(cfg *config.Config, fm *files.FS, web fs.FS, logger *log.Logger) *Server {
	s := &Server{
		cfg:      cfg,
		fm:       fm,
		web:      web,
		logger:   logger,
		sessions: newSessionStore(),
		logins:   newLoginThrottle(),
		shares:   newShareStore(),
	}
	if cfg.SessionsFile != "" {
		s.sessions = newSessionStoreFile(cfg.SessionsFile, logger)
	}
	if cfg.PasswordHash != "" {
		verifier, err := parsePasswordHash(cfg.PasswordHash)
		if err != nil {
			// Fail closed: a broken hash must never downgrade to the
			// plaintext PASSWORD (whose default is admin/admin).
			logger.Printf("PASSWORD_HASH is malformed, password logins are refused until it is fixed: %v", err)
			s.passwordHash = &passwordVerifier{broken: true}
		} else {
			s.passwordHash = verifier
		}
	}
	return s
}

// Handler returns the fully wired HTTP handler.
func (s *Server) Handler() http.Handler {
	app := http.NewServeMux()
	app.HandleFunc("GET /api/list", s.handleList)
	app.HandleFunc("GET /api/search", s.handleSearch)
	// /api/stat is kept as an alias for scripts; the UI uses /api/metadata
	// because ad blockers match the generic filter rule "/api/stat?" and would
	// otherwise make the browser reject the request ("Failed to fetch").
	app.HandleFunc("GET /api/metadata", s.handleStat)
	app.HandleFunc("GET /api/stat", s.handleStat)
	app.HandleFunc("GET /api/read", s.handleRead)
	app.HandleFunc("GET /api/download", s.handleDownload)
	app.HandleFunc("GET /api/download-zip", s.handleDownloadZip)
	app.HandleFunc("GET /api/info", s.handleInfo)
	app.HandleFunc("DELETE /api/delete", s.handleDelete)
	app.HandleFunc("POST /api/mkdir", s.handleMkdir)
	app.HandleFunc("POST /api/rename", s.handleRename)
	app.HandleFunc("POST /api/chmod", s.handleChmod)
	app.HandleFunc("POST /api/move", s.handleMove)
	app.HandleFunc("POST /api/copy", s.handleCopy)
	app.HandleFunc("POST /api/trash", s.handleTrash)
	app.HandleFunc("POST /api/trash/empty", s.handleTrashEmpty)
	app.HandleFunc("POST /api/create", s.handleCreate)
	app.HandleFunc("POST /api/extract", s.handleExtract)
	app.HandleFunc("PUT /api/write", s.handleWrite)
	app.HandleFunc("POST /api/upload", s.handleUpload)
	app.HandleFunc("POST /api/share", s.handleShareCreate)
	app.HandleFunc("GET /api/shares", s.handleShareList)
	app.HandleFunc("DELETE /api/share", s.handleShareRevoke)
	app.Handle("/", http.FileServer(http.FS(s.web)))

	mux := http.NewServeMux()
	// Public routes: the login form, logout, the assets the form needs, and
	// shared links (each token is its own capability).
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLoginSubmit)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.Handle("GET /static/", http.FileServer(http.FS(s.web)))
	mux.HandleFunc("GET /s/", s.handleShareGet)
	// Everything else needs a session (or Basic credentials).
	mux.Handle("/", s.withAuth(app))

	return s.withLogging(mux)
}

// --- middleware -----------------------------------------------------------

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authenticate(r) {
			next.ServeHTTP(w, r)
			return
		}
		// Deliberately no WWW-Authenticate header: it would make browsers pop
		// up their native credential dialog. API calls get JSON, page requests
		// are sent to the login form instead.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		http.Redirect(w, r, loginPath+"?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
	})
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		s.logger.Printf("%s %s %d %s", r.Method, r.URL.RequestURI(), rec.status, time.Since(start).Round(time.Millisecond))
	})
}

func equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// --- handlers -------------------------------------------------------------

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	showHidden := r.URL.Query().Get("hidden") == "1"
	rel, entries, err := s.fm.List(r.URL.Query().Get("path"), showHidden)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":    rel,
		"entries": entries,
	})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "search query is empty"})
		return
	}
	results, err := s.fm.Search(r.URL.Query().Get("path"), query, files.SearchOptions{
		ShowHidden: r.URL.Query().Get("hidden") == "1",
		Content:    r.URL.Query().Get("content") == "1",
	})
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":      results.Base,
		"entries":   results.Matches,
		"truncated": results.Truncated,
		"scanLimit": results.ScanLimit,
	})
}

func (s *Server) handleStat(w http.ResponseWriter, r *http.Request) {
	entry, err := s.fm.Stat(r.URL.Query().Get("path"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleRead(w http.ResponseWriter, r *http.Request) {
	rel, data, err := s.fm.ReadFile(r.URL.Query().Get("path"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":    rel,
		"size":    len(data),
		"content": string(data),
	})
}

// mediaTypes maps file extensions to the Content-Type served by /api/download
// so the browser can preview images, video, audio and PDFs inline. Anything
// unmapped keeps application/octet-stream; browsers sniff <img> and <video>
// sources by content, but <embed> needs the real type.
var mediaTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".bmp":  "image/bmp",
	".avif": "image/avif",
	".ico":  "image/x-icon",
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mov":  "video/quicktime",
	".m4v":  "video/x-m4v",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".oga":  "audio/ogg",
	".m4a":  "audio/mp4",
	".flac": "audio/flac",
	".aac":  "audio/aac",
	".pdf":  "application/pdf",
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	file, info, err := s.fm.Open(r.URL.Query().Get("path"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", contentTypeFor(info.Name()))
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(info.Name(), `"`, "")+`"`)
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

// contentTypeFor maps a file name to its Content-Type for streaming routes.
// Anything unmapped keeps application/octet-stream; browsers sniff <img> and
// <video> sources by content, but <embed> needs the real type.
func contentTypeFor(name string) string {
	if mt, ok := mediaTypes[strings.ToLower(filepath.Ext(name))]; ok {
		return mt
	}
	return "application/octet-stream"
}

func (s *Server) handleDownloadZip(w http.ResponseWriter, r *http.Request) {
	paths := r.URL.Query()["path"]
	if len(paths) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no path given"})
		return
	}
	// Resolve and stat everything up front, so a bad path still maps to its
	// status code instead of corrupting a half-written archive.
	type zipTarget struct {
		rel   string
		entry files.Entry
	}
	targets := make([]zipTarget, 0, len(paths))
	seen := map[string]bool{}
	for _, p := range paths {
		entry, err := s.fm.Stat(p)
		if err != nil {
			s.writeError(w, err)
			return
		}
		if !seen[entry.Path] {
			seen[entry.Path] = true
			targets = append(targets, zipTarget{rel: entry.Path, entry: entry})
		}
	}

	var name string
	switch {
	case len(targets) == 1 && targets[0].entry.IsDir:
		name = targets[0].entry.Name + ".zip"
	case len(targets) == 1:
		// A single file is what /api/download is for.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "not a folder"})
		return
	default:
		name = "selected.zip"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(name, `"`, "")+`"`)

	rels := make([]string, len(targets))
	for i, t := range targets {
		rels[i] = t.rel
	}
	// Once the archive has started, mid-walk problems can no longer change the
	// status code; files.Zip skips what it cannot read and logs the rest here.
	if err := s.fm.ZipMulti(rels, w); err != nil {
		s.logger.Printf("error: zip %v: %v", rels, err)
	}
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	disk, err := s.fm.Disk()
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": version.Version,
		"root":    s.fm.Root(),
		"disk":    disk,
		"host":    host.Collect(),
	})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	recursive := r.URL.Query().Get("recursive") == "true"
	if err := s.fm.Delete(rel, recursive); err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": rel})
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if err := s.fm.Mkdir(rel); err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"created": rel})
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	rel := query.Get("path")
	name := query.Get("name")
	if err := s.fm.Rename(rel, name); err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": rel, "name": name})
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	from := query.Get("from")
	to := query.Get("to")
	if err := s.fm.Move(from, to); err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "path": to})
}

func (s *Server) handleCopy(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	from := query.Get("from")
	to := query.Get("to")
	if err := s.fm.Copy(from, to); err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "path": to})
}

func (s *Server) handleTrash(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	to, err := s.fm.Trash(rel)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trashed": rel, "path": to})
}

func (s *Server) handleTrashEmpty(w http.ResponseWriter, r *http.Request) {
	removed, err := s.fm.EmptyTrash()
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": removed})
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if err := s.fm.Create(rel); err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"created": rel})
}

func (s *Server) handleExtract(w http.ResponseWriter, r *http.Request) {
	dest, err := s.fm.Extract(r.URL.Query().Get("path"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"extracted": dest})
}

// handleChmod sets the permission bits of one entry; mode is octal ("644",
// "0755") and limited to the permission bits.
func (s *Server) handleChmod(w http.ResponseWriter, r *http.Request) {
	mode, err := strconv.ParseUint(r.URL.Query().Get("mode"), 8, 32)
	if err != nil || mode > 0o777 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be octal permission bits like 644 or 0755"})
		return
	}
	rel := r.URL.Query().Get("path")
	if err := s.fm.Chmod(rel, os.FileMode(mode)); err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": rel, "mode": strconv.FormatUint(mode, 8)})
}

func (s *Server) handleWrite(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, files.MaxWriteSize))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "file is too large to save"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.fm.Write(rel, data); err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": rel, "size": len(data)})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	r.Body = http.MaxBytesReader(w, r.Body, files.MaxUploadSize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
				"error": "upload is larger than the " + formatLimit(files.MaxUploadSize) + " limit",
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	defer r.MultipartForm.RemoveAll()

	parts := r.MultipartForm.File["file"]
	if len(parts) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no files in request"})
		return
	}

	uploaded := []string{}
	failed := []map[string]string{}
	// Folder uploads send one "path" field per file, aligned by order: Go's
	// multipart parser strips directories from part filenames, so the
	// relative path has to travel separately.
	paths := r.MultipartForm.Value["path"]
	for i, part := range parts {
		name := part.Filename
		if i < len(paths) && paths[i] != "" {
			name = paths[i]
		}
		src, err := part.Open()
		if err != nil {
			failed = append(failed, map[string]string{"name": name, "error": err.Error()})
			continue
		}
		err = s.fm.Upload(dir, name, src)
		src.Close()
		if err != nil {
			failed = append(failed, map[string]string{"name": name, "error": err.Error()})
			continue
		}
		uploaded = append(uploaded, name)
	}

	writeJSON(w, http.StatusOK, map[string]any{"uploaded": uploaded, "failed": failed})
}

func (s *Server) writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, files.ErrForbidden), errors.Is(err, os.ErrPermission):
		status = http.StatusForbidden
	case errors.Is(err, os.ErrNotExist):
		status = http.StatusNotFound
	case errors.Is(err, files.ErrRootOperation), errors.Is(err, files.ErrInvalidName),
		errors.Is(err, files.ErrMoveIntoSelf), errors.Is(err, files.ErrCopyIntoSelf),
		errors.Is(err, files.ErrUnsupported), errors.Is(err, files.ErrInTrash):
		status = http.StatusBadRequest
	case errors.Is(err, files.ErrArchiveTooLarge):
		status = http.StatusRequestEntityTooLarge
	case errors.Is(err, files.ErrDirNotEmpty), errors.Is(err, os.ErrExist):
		status = http.StatusConflict
	}
	if status >= http.StatusInternalServerError {
		s.logger.Printf("error: %v", err)
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// formatLimit renders a byte limit for error messages ("50 MB").
func formatLimit(bytes int64) string {
	return strconv.FormatInt(bytes/(1<<20), 10) + " MB"
}
