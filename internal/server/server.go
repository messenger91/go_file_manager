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
	"strings"
	"time"

	"go_file_manager/internal/config"
	"go_file_manager/internal/files"
	"go_file_manager/internal/host"
)

// Server serves the read-only file manager API and web UI.
type Server struct {
	cfg      *config.Config
	fm       *files.FS
	web      fs.FS
	logger   *log.Logger
	sessions *sessionStore
}

// New builds a Server. web is the filesystem holding the static UI assets.
func New(cfg *config.Config, fm *files.FS, web fs.FS, logger *log.Logger) *Server {
	return &Server{
		cfg:      cfg,
		fm:       fm,
		web:      web,
		logger:   logger,
		sessions: newSessionStore(),
	}
}

// Handler returns the fully wired HTTP handler.
func (s *Server) Handler() http.Handler {
	app := http.NewServeMux()
	app.HandleFunc("GET /api/list", s.handleList)
	app.HandleFunc("GET /api/stat", s.handleStat)
	app.HandleFunc("GET /api/read", s.handleRead)
	app.HandleFunc("GET /api/download", s.handleDownload)
	app.HandleFunc("GET /api/info", s.handleInfo)
	app.HandleFunc("DELETE /api/delete", s.handleDelete)
	app.HandleFunc("POST /api/mkdir", s.handleMkdir)
	app.HandleFunc("POST /api/rename", s.handleRename)
	app.HandleFunc("POST /api/create", s.handleCreate)
	app.HandleFunc("PUT /api/write", s.handleWrite)
	app.HandleFunc("POST /api/upload", s.handleUpload)
	app.Handle("/", http.FileServer(http.FS(s.web)))

	mux := http.NewServeMux()
	// Public routes: the login form, logout, and the assets the form needs.
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLoginSubmit)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.Handle("GET /static/", http.FileServer(http.FS(s.web)))
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
	rel, entries, err := s.fm.List(r.URL.Query().Get("path"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":    rel,
		"entries": entries,
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

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	file, info, err := s.fm.Open(r.URL.Query().Get("path"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	defer file.Close()

	name := strings.ReplaceAll(info.Name(), `"`, "")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	disk, err := s.fm.Disk()
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"root": s.fm.Root(),
		"disk": disk,
		"host": host.Collect(),
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

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if err := s.fm.Create(rel); err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"created": rel})
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
	for _, part := range parts {
		name := filepath.Base(part.Filename)
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
	case errors.Is(err, files.ErrRootOperation), errors.Is(err, files.ErrInvalidName):
		status = http.StatusBadRequest
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
