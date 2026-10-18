package server

import (
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// sessionCookie is the name of the cookie that carries the session token.
	sessionCookie = "gfm_session"
	// loginPath serves the login form.
	loginPath = "/login"
	// sessionTTL is how long a session stays valid after login.
	sessionTTL = 24 * time.Hour
)

type session struct {
	user    string
	expires time.Time
}

// sessionStore is a small in-memory session store. Sessions do not survive a
// server restart, so restarting logs everyone out.
type sessionStore struct {
	mu   sync.Mutex
	data map[string]session
}

func newSessionStore() *sessionStore {
	return &sessionStore{data: map[string]session{}}
}

func (st *sessionStore) create(user string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)

	st.mu.Lock()
	defer st.mu.Unlock()
	now := time.Now()
	for token, sess := range st.data {
		if now.After(sess.expires) {
			delete(st.data, token)
		}
	}
	st.data[token] = session{user: user, expires: now.Add(sessionTTL)}
	return token, nil
}

func (st *sessionStore) get(token string) (session, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()

	sess, ok := st.data[token]
	if !ok {
		return session{}, false
	}
	if time.Now().After(sess.expires) {
		delete(st.data, token)
		return session{}, false
	}
	return sess, true
}

func (st *sessionStore) delete(token string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.data, token)
}

// authenticate reports whether the request carries valid credentials, either a
// live session cookie or a valid HTTP Basic header. Basic auth is accepted so
// scripts can keep calling the API with `curl -u`; browsers never see a
// WWW-Authenticate challenge and use the login form instead.
func (s *Server) authenticate(r *http.Request) bool {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if _, ok := s.sessions.get(c.Value); ok {
			return true
		}
	}
	if user, pass, ok := r.BasicAuth(); ok {
		return equal(user, s.cfg.Admin) && equal(pass, s.cfg.Password)
	}
	return false
}

// setSessionCookie writes the session cookie. A negative maxAge clears it.
func (s *Server) setSessionCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
		// Secure is intentionally omitted: the app normally runs over plain
		// HTTP on a trusted network. Set it (and serve over TLS) if you expose
		// the app more widely.
	})
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if _, ok := s.sessions.get(c.Value); ok {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
	}
	page, err := fs.ReadFile(s.web, "login.html")
	if err != nil {
		http.Error(w, "login page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(page)
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	next := safeNext(r.PostFormValue("next"))

	user := r.PostFormValue("username")
	pass := r.PostFormValue("password")
	if !equal(user, s.cfg.Admin) || !equal(pass, s.cfg.Password) {
		s.logger.Printf("failed login for %q from %s", user, r.RemoteAddr)
		http.Redirect(w, r, loginPath+"?error=1&next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}

	token, err := s.sessions.create(user)
	if err != nil {
		s.logger.Printf("session: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.setSessionCookie(w, token, int(sessionTTL/time.Second))
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.delete(c.Value)
	}
	s.setSessionCookie(w, "", -1)
	http.Redirect(w, r, loginPath, http.StatusSeeOther)
}

// safeNext keeps post-login redirects on the same origin: only a rooted path
// that is not protocol-relative is allowed.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}
