package server

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"go_file_manager/internal/config"
	"go_file_manager/internal/files"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	web := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>app</title>")},
		"login.html":    {Data: []byte("<!doctype html><title>login</title>")},
		"static/app.js": {Data: []byte("// app")},
	}
	cfg := &config.Config{HomeDir: t.TempDir(), Admin: "admin", Password: "secret", Port: "0"}
	return New(cfg, files.New(cfg.HomeDir), web, log.New(io.Discard, "", 0))
}

func (s *Server) do(t *testing.T, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func form(values url.Values) *http.Request {
	req := httptest.NewRequest("POST", "/login", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// sessionCookieFrom pulls the session cookie out of a response, or returns nil.
func sessionCookieFrom(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			return c
		}
	}
	return nil
}

func TestLoginPageIsPublic(t *testing.T) {
	rec := newTestServer(t).do(t, httptest.NewRequest("GET", "/login", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "login") {
		t.Fatalf("body does not look like the login page: %q", rec.Body.String())
	}
}

func TestStaticAssetsArePublic(t *testing.T) {
	rec := newTestServer(t).do(t, httptest.NewRequest("GET", "/static/app.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the login page needs its CSS/JS)", rec.Code)
	}
}

func TestPageRequestRedirectsToLogin(t *testing.T) {
	rec := newTestServer(t).do(t, httptest.NewRequest("GET", "/some/dir?x=1", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login?next=%2Fsome%2Fdir%3Fx%3D1" {
		t.Fatalf("Location = %q", loc)
	}
	// A challenge header would make browsers show their native credential prompt.
	if rec.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("WWW-Authenticate must not be sent")
	}
}

func TestAPIUnauthorizedIsJSON401(t *testing.T) {
	rec := newTestServer(t).do(t, httptest.NewRequest("GET", "/api/list?path=/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", ct)
	}
}

func TestLoginSetsSessionCookieThatAuthenticates(t *testing.T) {
	s := newTestServer(t)

	rec := s.do(t, form(url.Values{
		"username": {"admin"},
		"password": {"secret"},
		"next":     {"/sub?x=1"},
	}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/sub?x=1" {
		t.Fatalf("Location = %q, want the requested page", loc)
	}
	cookie := sessionCookieFrom(rec)
	if cookie == nil {
		t.Fatal("no session cookie set")
	}
	if !cookie.HttpOnly {
		t.Error("session cookie should be HttpOnly")
	}

	req := httptest.NewRequest("GET", "/api/info", nil)
	req.AddCookie(cookie)
	if rec := s.do(t, req); rec.Code != http.StatusOK {
		t.Fatalf("authenticated request status = %d, want 200", rec.Code)
	}
}

func TestLoginRejectsBadPassword(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(t, form(url.Values{"username": {"admin"}, "password": {"nope"}}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/login?error=1") {
		t.Fatalf("Location = %q, want /login?error=1...", loc)
	}
	if c := sessionCookieFrom(rec); c != nil {
		t.Fatal("a failed login must not set a session cookie")
	}
}

func TestBasicAuthStillWorks(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/info", nil)
	req.SetBasicAuth("admin", "secret")
	if rec := newTestServer(t).do(t, req); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	s := newTestServer(t)
	cookie := sessionCookieFrom(s.do(t, form(url.Values{
		"username": {"admin"},
		"password": {"secret"},
	})))

	req := httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(cookie)
	if rec := s.do(t, req); rec.Code != http.StatusSeeOther {
		t.Fatalf("logout status = %d, want 303", rec.Code)
	}

	req = httptest.NewRequest("GET", "/api/info", nil)
	req.AddCookie(cookie)
	if rec := s.do(t, req); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status after logout = %d, want 401", rec.Code)
	}
}

func TestSafeNextBlocksOpenRedirects(t *testing.T) {
	cases := map[string]string{
		"":                    "/",
		"/sub":                "/sub",
		"/a?b=c":              "/a?b=c",
		"//evil.example":      "/",
		"http://evil.example": "/",
		"evil.example":        "/",
	}
	for in, want := range cases {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}
