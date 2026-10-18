// Package server wires the HTTP API and static web UI together.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
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

func (s *Server) writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, files.ErrForbidden), errors.Is(err, os.ErrPermission):
		status = http.StatusForbidden
	case errors.Is(err, os.ErrNotExist):
		status = http.StatusNotFound
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
