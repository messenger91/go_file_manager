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
	"os"
	"path/filepath"
	"strings"
	"time"

	"go_file_manager/internal/config"
	"go_file_manager/internal/files"
)

// Server serves the read-only file manager API and web UI.
type Server struct {
	cfg    *config.Config
	fm     *files.FS
	web    fs.FS
	logger *log.Logger
}

// New builds a Server. web is the filesystem holding the static UI assets.
func New(cfg *config.Config, fm *files.FS, web fs.FS, logger *log.Logger) *Server {
	return &Server{cfg: cfg, fm: fm, web: web, logger: logger}
}

// Handler returns the fully wired HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/list", s.handleList)
	mux.HandleFunc("GET /api/stat", s.handleStat)
	mux.HandleFunc("GET /api/read", s.handleRead)
	mux.HandleFunc("GET /api/download", s.handleDownload)
	mux.HandleFunc("DELETE /api/delete", s.handleDelete)
	mux.HandleFunc("POST /api/mkdir", s.handleMkdir)
	mux.HandleFunc("POST /api/rename", s.handleRename)
	mux.HandleFunc("POST /api/create", s.handleCreate)
	mux.HandleFunc("PUT /api/write", s.handleWrite)
	mux.HandleFunc("POST /api/upload", s.handleUpload)
	mux.Handle("/", http.FileServer(http.FS(s.web)))

	return s.withLogging(s.withAuth(mux))
}

// --- middleware -----------------------------------------------------------

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || !equal(user, s.cfg.Admin) || !equal(pass, s.cfg.Password) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Go File Manager", charset="UTF-8"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
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
