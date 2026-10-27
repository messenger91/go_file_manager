package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
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

// sessionStore is a small session store. Without a file it is memory only and
// a restart logs everyone out; with SESSIONS_FILE set it survives restarts by
// writing its live sessions to that file (JSON, mode 0600) after every change.
type sessionStore struct {
	mu   sync.Mutex
	data map[string]session
	file string
	log  *log.Logger
}

// storedSession is the on-disk shape of one session.
type storedSession struct {
	Token   string    `json:"token"`
	User    string    `json:"user"`
	Expires time.Time `json:"expires"`
}

func newSessionStore() *sessionStore {
	return &sessionStore{data: map[string]session{}}
}

// newSessionStoreFile loads sessions from path (dropping expired ones) and
// keeps the file in sync from then on.
func newSessionStoreFile(path string, logger *log.Logger) *sessionStore {
	st := &sessionStore{data: map[string]session{}, file: path, log: logger}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) && logger != nil {
			logger.Printf("sessions: starting with an empty store (%v)", err)
		}
		return st
	}
	var stored []storedSession
	if err := json.Unmarshal(raw, &stored); err != nil {
		if logger != nil {
			logger.Printf("sessions: %s is unreadable, starting empty: %v", path, err)
		}
		return st
	}
	now := time.Now()
	for _, s := range stored {
		if s.Token != "" && now.Before(s.Expires) {
			st.data[s.Token] = session{user: s.User, expires: s.Expires}
		}
	}
	return st
}

// saveLocked rewrites the session file with the live sessions only. Callers
// must hold st.mu.
func (st *sessionStore) saveLocked() {
	if st.file == "" {
		return
	}
	now := time.Now()
	stored := make([]storedSession, 0, len(st.data))
	for token, sess := range st.data {
		if now.Before(sess.expires) {
			stored = append(stored, storedSession{Token: token, User: sess.user, Expires: sess.expires})
		}
	}
	raw, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		if st.log != nil {
			st.log.Printf("sessions: %v", err)
		}
		return
	}
	if err := os.WriteFile(st.file, raw, 0o600); err != nil && st.log != nil {
		st.log.Printf("sessions: cannot write %s: %v", st.file, err)
	}
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
	st.saveLocked()
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
		st.saveLocked()
		return session{}, false
	}
	return sess, true
}

func (st *sessionStore) delete(token string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.data[token]; !ok {
		return
	}
	delete(st.data, token)
	st.saveLocked()
}

// loginThrottle slows down online password guessing. After enough failures
// from one address, further attempts are bounced for a window that doubles
// with every additional failure. It is in-memory on purpose: a restart clears
// it, and the addresses are the remote host of the connection (behind a
// reverse proxy every client shares the proxy's address, so one attacker can
// briefly lock out everyone behind it — the trade-off is documented).
type loginThrottle struct {
	mu    sync.Mutex
	fails map[string]*loginFailures
}

type loginFailures struct {
	count       int
	lockedUntil time.Time
}

const (
	// loginFailLimit is how many failures are free before the lockout starts.
	loginFailLimit = 5
	// loginLockoutBase is the first lockout window; it doubles per extra
	// failure up to loginLockoutMax.
	loginLockoutBase = 5 * time.Second
	loginLockoutMax  = 5 * time.Minute
	// loginThrottleMaxAddrs bounds the map so a spoofed flood of source
	// addresses cannot grow it without end.
	loginThrottleMaxAddrs = 10_000
)

func newLoginThrottle() *loginThrottle {
	return &loginThrottle{fails: map[string]*loginFailures{}}
}

// check reports whether an attempt from addr may be verified at all.
func (t *loginThrottle) check(addr string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	f := t.fails[addr]
	if f == nil {
		return true
	}
	return time.Now().After(f.lockedUntil)
}

func (t *loginThrottle) fail(addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.fails) >= loginThrottleMaxAddrs {
		t.fails = map[string]*loginFailures{}
	}
	f := t.fails[addr]
	if f == nil {
		f = &loginFailures{}
		t.fails[addr] = f
	}
	f.count++
	if f.count < loginFailLimit {
		return
	}
	wait := loginLockoutBase
	if shift := uint(f.count - loginFailLimit); shift < 12 {
		wait = loginLockoutBase << shift
	} else {
		wait = loginLockoutMax
	}
	if wait > loginLockoutMax {
		wait = loginLockoutMax
	}
	f.lockedUntil = time.Now().Add(wait)
}

func (t *loginThrottle) reset(addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.fails, addr)
}

// addrHost reduces RemoteAddr to its host part, so a client cannot get a
// fresh throttle bucket per connection just by getting a new source port.
func addrHost(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// verifyPassword checks the password against the configured verifier: the
// PBKDF2 hash when PASSWORD_HASH is set (and parsed), the plaintext
// comparison otherwise.
func (s *Server) verifyPassword(password string) bool {
	if s.passwordHash != nil {
		return s.passwordHash.verify(password)
	}
	return equal(password, s.cfg.Password)
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
		return equal(user, s.cfg.Admin) && s.verifyPassword(pass)
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

	host := addrHost(r.RemoteAddr)
	// A throttled attempt is answered exactly like a wrong password, so the
	// lockout itself leaks nothing.
	if !s.logins.check(host) {
		s.logger.Printf("login throttled for %s", host)
		http.Redirect(w, r, loginPath+"?error=1&next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}

	user := r.PostFormValue("username")
	pass := r.PostFormValue("password")
	if !equal(user, s.cfg.Admin) || !s.verifyPassword(pass) {
		s.logins.fail(host)
		s.logger.Printf("failed login for %q from %s", user, r.RemoteAddr)
		http.Redirect(w, r, loginPath+"?error=1&next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}
	s.logins.reset(host)

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
