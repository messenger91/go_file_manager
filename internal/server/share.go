package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// shareURLPrefix is the public, unauthenticated route that serves shared
// entries. It sits outside the /api/ namespace on purpose: shared links are
// meant for other people, not for the API's session/Basic-auth wall.
const shareURLPrefix = "/s/"

// shareTokenLen is the byte length of a share token (128 bit of randomness,
// hex-encoded to 32 characters).
const shareTokenLen = 16

// share is a public, read-only link to one entry inside the root. Shares live
// in memory: a restart revokes every link, which is the safe default.
type share struct {
	Path    string    `json:"path"` // root-relative path at share time
	Name    string    `json:"name"` // entry name at share time
	IsDir   bool      `json:"isDir"`
	Token   string    `json:"token"`
	URL     string    `json:"url"`
	Expires time.Time `json:"expires"` // zero means "never"
}

type shareStore struct {
	mu     sync.Mutex
	shares map[string]share
}

func newShareStore() *shareStore {
	return &shareStore{shares: map[string]share{}}
}

func (st *shareStore) create(path, name string, isDir bool, ttl time.Duration) (share, error) {
	raw := make([]byte, shareTokenLen)
	if _, err := rand.Read(raw); err != nil {
		return share{}, err
	}
	token := hex.EncodeToString(raw)
	s := share{
		Path:  path,
		Name:  name,
		IsDir: isDir,
		Token: token,
		URL:   shareURLPrefix + token,
	}
	if ttl > 0 {
		s.Expires = time.Now().Add(ttl)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.shares[token] = s
	return s, nil
}

// get returns the live share for token; an expired share is removed and
// reported as missing.
func (st *shareStore) get(token string) (share, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.shares[token]
	if !ok {
		return share{}, false
	}
	if !s.Expires.IsZero() && time.Now().After(s.Expires) {
		delete(st.shares, token)
		return share{}, false
	}
	return s, true
}

func (st *shareStore) revoke(token string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.shares[token]; !ok {
		return false
	}
	delete(st.shares, token)
	return true
}

func (st *shareStore) list() []share {
	st.mu.Lock()
	defer st.mu.Unlock()
	now := time.Now()
	out := make([]share, 0, len(st.shares))
	for token, s := range st.shares {
		if !s.Expires.IsZero() && now.After(s.Expires) {
			delete(st.shares, token)
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// parseShareTTL reads an optional lifetime: empty is "never", "<n>d" is n
// days, and anything time.ParseDuration accepts works too. Negative and zero
// lengths other than the empty string are rejected.
func parseShareTTL(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	if days := strings.TrimSuffix(raw, "d"); days != raw {
		n, err := strconv.Atoi(days)
		if err != nil || n <= 0 {
			return 0, errors.New("bad ttl: use like 7d, 12h or 30m")
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, errors.New("bad ttl: use like 7d, 12h or 30m")
	}
	return d, nil
}

func (s *Server) handleShareCreate(w http.ResponseWriter, r *http.Request) {
	ttl, err := parseShareTTL(r.URL.Query().Get("ttl"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	entry, err := s.fm.Stat(r.URL.Query().Get("path"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	share, err := s.shares.create(entry.Path, entry.Name, entry.IsDir, ttl)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, share)
}

func (s *Server) handleShareList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"shares": s.shares.list()})
}

func (s *Server) handleShareRevoke(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if !s.shares.revoke(token) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown share"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

// handleShareGet serves a shared entry without authentication. Files stream
// inline when their type is previewable and as downloads otherwise; folders
// stream as zip archives, exactly like /api/download-zip does. A share whose
// entry has been renamed or deleted meanwhile is simply gone (404).
func (s *Server) handleShareGet(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, shareURLPrefix)
	if token == "" || strings.ContainsAny(token, "/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown or expired link"})
		return
	}
	shared, ok := s.shares.get(token)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown or expired link"})
		return
	}

	if shared.IsDir {
		entry, err := s.fm.Stat(shared.Path)
		if err != nil {
			s.writeError(w, err)
			return
		}
		name := strings.ReplaceAll(entry.Name, `"`, "")
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.zip"`)
		if err := s.fm.Zip(shared.Path, w); err != nil {
			s.logger.Printf("error: zip %s (share %s): %v", shared.Path, shared.Token, err)
		}
		return
	}

	file, info, err := s.fm.Open(shared.Path)
	if err != nil {
		s.writeError(w, err)
		return
	}
	defer file.Close()
	contentType := contentTypeFor(info.Name())
	w.Header().Set("Content-Type", contentType)
	disposition := "inline"
	if contentType == "application/octet-stream" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", disposition+`; filename="`+strings.ReplaceAll(info.Name(), `"`, "")+`"`)
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}
