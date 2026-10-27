package server

import (
	"archive/zip"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"go_file_manager/internal/config"
	"go_file_manager/internal/files"
	"go_file_manager/internal/version"
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

func TestStatReportsEntryMetadata(t *testing.T) {
	s := newTestServer(t)
	if err := os.WriteFile(filepath.Join(s.fm.Root(), "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// /api/metadata is the primary route; /api/stat stays as an alias.
	for _, endpoint := range []string{"/api/metadata", "/api/stat"} {
		t.Run(endpoint, func(t *testing.T) {
			req := httptest.NewRequest("GET", endpoint+"?path=/note.txt", nil)
			req.SetBasicAuth("admin", "secret")
			rec := s.do(t, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
			}

			var entry files.Entry
			if err := json.Unmarshal(rec.Body.Bytes(), &entry); err != nil {
				t.Fatalf("decode entry: %v", err)
			}
			if entry.Name != "note.txt" || entry.Path != "/note.txt" {
				t.Fatalf("name/path = %q/%q, want note.txt//note.txt", entry.Name, entry.Path)
			}
			if entry.IsDir {
				t.Fatal("file reported as a directory")
			}
			if entry.Size != 5 {
				t.Fatalf("size = %d, want 5", entry.Size)
			}
			if entry.Mode == "" || entry.ModTime.IsZero() {
				t.Fatalf("mode/modTime missing: %+v", entry)
			}
		})
	}
}

func TestListHidesDotEntriesUnlessRequested(t *testing.T) {
	s := newTestServer(t)
	root := s.fm.Root()
	for _, name := range []string{"visible.txt", ".hidden"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	list := func(query string) []string {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/list?path=/"+query, nil)
		req.SetBasicAuth("admin", "secret")
		rec := s.do(t, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Entries []files.Entry `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		names := make([]string, len(body.Entries))
		for i, e := range body.Entries {
			names[i] = e.Name
		}
		return names
	}

	if got := list(""); len(got) != 1 || got[0] != "visible.txt" {
		t.Fatalf("default list = %v, want [visible.txt]", got)
	}
	if got := list("&hidden=1"); len(got) != 2 {
		t.Fatalf("hidden list = %v, want both entries", got)
	}
}

func TestInfoReportsVersionAndRoot(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/info", nil)
	req.SetBasicAuth("admin", "secret")

	rec := s.do(t, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var info struct {
		Version string         `json:"version"`
		Root    string         `json:"root"`
		Disk    map[string]any `json:"disk"`
		Host    map[string]any `json:"host"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode info: %v", err)
	}
	if info.Version != version.Version {
		t.Fatalf("version = %q, want %q", info.Version, version.Version)
	}
	if info.Root == "" {
		t.Fatal("root is empty")
	}
	if info.Disk == nil || info.Host == nil {
		t.Fatalf("disk/host missing: %+v", info)
	}
}

func TestMoveEndpoint(t *testing.T) {
	s := newTestServer(t)
	root := s.fm.Root()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dest", "taken.txt"), []byte("t"), 0o644); err != nil {
		t.Fatal(err)
	}

	move := func(from, to string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(
			"POST",
			"/api/move?from="+url.QueryEscape(from)+"&to="+url.QueryEscape(to),
			nil,
		)
		req.SetBasicAuth("admin", "secret")
		return s.do(t, req)
	}

	rec := move("/a.txt", "/dest/a.txt")
	if rec.Code != http.StatusOK {
		t.Fatalf("move status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode move response: %v", err)
	}
	if body.Path != "/dest/a.txt" {
		t.Fatalf("returned path = %q, want /dest/a.txt", body.Path)
	}
	if _, err := os.Stat(filepath.Join(root, "dest", "a.txt")); err != nil {
		t.Fatalf("moved file missing: %v", err)
	}

	if rec := move("/dest", "/dest/a.txt"); rec.Code != http.StatusBadRequest {
		t.Fatalf("move into itself status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if rec := move("/dest/a.txt", "/dest/taken.txt"); rec.Code != http.StatusConflict {
		t.Fatalf("move onto existing status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if rec := move("/missing.txt", "/dest/missing.txt"); rec.Code != http.StatusNotFound {
		t.Fatalf("move missing status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if rec := move("/dest/a.txt", "/nope/a.txt"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing destination status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if rec := move("/", "/dest"); rec.Code != http.StatusBadRequest {
		t.Fatalf("move root status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestCopyEndpoint(t *testing.T) {
	s := newTestServer(t)
	root := s.fm.Root()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "nested", "note.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dest", "taken.txt"), []byte("t"), 0o644); err != nil {
		t.Fatal(err)
	}

	copyPath := func(from, to string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(
			"POST",
			"/api/copy?from="+url.QueryEscape(from)+"&to="+url.QueryEscape(to),
			nil,
		)
		req.SetBasicAuth("admin", "secret")
		return s.do(t, req)
	}

	rec := copyPath("/a.txt", "/dest/a.txt")
	if rec.Code != http.StatusOK {
		t.Fatalf("copy status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode copy response: %v", err)
	}
	if body.Path != "/dest/a.txt" {
		t.Fatalf("returned path = %q, want /dest/a.txt", body.Path)
	}
	if data, err := os.ReadFile(filepath.Join(root, "dest", "a.txt")); err != nil || string(data) != "a" {
		t.Fatalf("copied file = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("source lost: %v", err)
	}

	if rec := copyPath("/docs", "/dest/docs"); rec.Code != http.StatusOK {
		t.Fatalf("copy dir status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "dest", "docs", "nested", "note.txt")); err != nil {
		t.Fatalf("nested copy missing: %v", err)
	}

	if rec := copyPath("/docs", "/docs/nested/docs"); rec.Code != http.StatusBadRequest {
		t.Fatalf("copy into itself status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if rec := copyPath("/a.txt", "/dest/taken.txt"); rec.Code != http.StatusConflict {
		t.Fatalf("copy onto existing status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if rec := copyPath("/missing.txt", "/dest/missing.txt"); rec.Code != http.StatusNotFound {
		t.Fatalf("copy missing status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if rec := copyPath("/a.txt", "/nope/a.txt"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing destination status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if rec := copyPath("/", "/dest/root"); rec.Code != http.StatusBadRequest {
		t.Fatalf("copy root status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestTrashEndpoint(t *testing.T) {
	s := newTestServer(t)
	root := s.fm.Root()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	trash := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST", "/api/trash?path="+url.QueryEscape(path), nil)
		req.SetBasicAuth("admin", "secret")
		return s.do(t, req)
	}

	rec := trash("/a.txt")
	if rec.Code != http.StatusOK {
		t.Fatalf("trash status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Trashed string `json:"trashed"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode trash response: %v", err)
	}
	if body.Trashed != "/a.txt" || body.Path != "/.trash/a.txt" {
		t.Fatalf("trash response = %+v, want trashed /a.txt at /.trash/a.txt", body)
	}
	if data, err := os.ReadFile(filepath.Join(root, ".trash", "a.txt")); err != nil || string(data) != "a" {
		t.Fatalf("trashed file = %q, %v", data, err)
	}

	if rec := trash("/missing.txt"); rec.Code != http.StatusNotFound {
		t.Fatalf("trash missing status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if rec := trash("/"); rec.Code != http.StatusBadRequest {
		t.Fatalf("trash root status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if rec := trash("/.trash"); rec.Code != http.StatusBadRequest {
		t.Fatalf("trash trash folder status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if rec := trash("/.trash/a.txt"); rec.Code != http.StatusBadRequest {
		t.Fatalf("trash entry inside trash status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestSearchEndpoint(t *testing.T) {
	s := newTestServer(t)
	root := s.fm.Root()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "report-final.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", ".hidden-report.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unrelated.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	get := func(query string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/search?"+query, nil)
		req.SetBasicAuth("admin", "secret")
		return s.do(t, req)
	}

	rec := get("q=report")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Path      string        `json:"path"`
		Entries   []files.Entry `json:"entries"`
		Truncated bool          `json:"truncated"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Path != "/" || body.Truncated {
		t.Fatalf("body = %+v, want path / and no truncation", body)
	}
	// The dotfile is skipped like /api/list skips it; the file inside the
	// subfolder is found (the folder "docs" itself does not match "report").
	if len(body.Entries) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(body.Entries), body.Entries)
	}
	if body.Entries[0].Path != "/docs/report-final.txt" || body.Entries[0].IsDir {
		t.Fatalf("entry = %+v, want /docs/report-final.txt", body.Entries[0])
	}

	// hidden=1 reveals dot entries, again like /api/list.
	rec = get("q=report&hidden=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("hidden status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode hidden response: %v", err)
	}
	if len(body.Entries) != 2 {
		t.Fatalf("hidden got %d entries, want 2: %+v", len(body.Entries), body.Entries)
	}

	if rec := get("q="); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty query status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if rec := get("q=report"); rec.Code != http.StatusOK {
		t.Fatalf("basic auth search status = %d, want 200", rec.Code)
	}

	// Unauthenticated callers get the same JSON 401 as the other API routes.
	req := httptest.NewRequest("GET", "/api/search?q=report", nil)
	if rec := s.do(t, req); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", rec.Code)
	}
}

func TestTrashEmptyEndpoint(t *testing.T) {
	s := newTestServer(t)
	root := s.fm.Root()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/api/trash?path="+url.QueryEscape("/"+name), nil)
		req.SetBasicAuth("admin", "secret")
		if rec := s.do(t, req); rec.Code != http.StatusOK {
			t.Fatalf("trash %s status = %d: %s", name, rec.Code, rec.Body.String())
		}
	}

	empty := func() *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST", "/api/trash/empty", nil)
		req.SetBasicAuth("admin", "secret")
		return s.do(t, req)
	}

	rec := empty()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Removed int `json:"removed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Removed != 2 {
		t.Fatalf("removed = %d, want 2", body.Removed)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".trash"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("trash not emptied: %v, %d entries", err, len(entries))
	}

	// A second call removes nothing but still succeeds.
	if rec := empty(); rec.Code != http.StatusOK {
		t.Fatalf("second empty status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestDownloadZipEndpoint(t *testing.T) {
	s := newTestServer(t)
	root := s.fm.Root()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/download-zip?path="+url.QueryEscape(path), nil)
		req.SetBasicAuth("admin", "secret")
		return s.do(t, req)
	}

	rec := get("/docs")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("Content-Type = %q, want application/zip", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="docs.zip"` {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("response is not a valid zip: %v", err)
	}
	found := false
	for _, file := range zr.File {
		if file.Name == "docs/note.txt" {
			found = true
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("open docs/note.txt: %v", err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil || string(data) != "hello" {
				t.Fatalf("docs/note.txt = %q, %v", data, err)
			}
		}
	}
	if !found {
		t.Fatalf("zip is missing docs/note.txt (%d entries)", len(zr.File))
	}

	// A file is not a folder, a missing path is 404, and the unauthenticated
	// request gets the usual JSON 401.
	if rec := get("/docs/note.txt"); rec.Code != http.StatusBadRequest {
		t.Fatalf("zip of a file status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if rec := get("/missing"); rec.Code != http.StatusNotFound {
		t.Fatalf("zip of missing status = %d, want 404", rec.Code)
	}
	req := httptest.NewRequest("GET", "/api/download-zip?path=/docs", nil)
	if rec := s.do(t, req); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", rec.Code)
	}
}

func TestPBKDF2SHA256KnownVectors(t *testing.T) {
	// Published PBKDF2-HMAC-SHA256 vectors for P="password", S="salt".
	vectors := []struct {
		iterations int
		keyLen     int
		want       string
	}{
		{1, 32, "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"},
		{2, 32, "ae4d0c95af6b46d32d0adff928f06dd02a303f8ef3c251dfd6e2d85a95474c43"},
		{4096, 32, "c5e478d59288c841aa530db6845c4c8d962893a001ce4e11a4963873aa98134a"},
	}
	for _, v := range vectors {
		got := pbkdf2SHA256([]byte("password"), []byte("salt"), v.iterations, v.keyLen)
		if hex.EncodeToString(got) != v.want {
			t.Errorf("pbkdf2(c=%d) = %x, want %s", v.iterations, got, v.want)
		}
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	encoded, err := NewPasswordHash("hunter2", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, passwordHashPrefix) {
		t.Fatalf("hash = %q, want the %s... format", encoded, passwordHashPrefix)
	}
	verifier, err := parsePasswordHash(encoded)
	if err != nil {
		t.Fatalf("parse own hash: %v", err)
	}
	if !verifier.verify("hunter2") {
		t.Error("correct password rejected")
	}
	if verifier.verify("hunter3") {
		t.Error("wrong password accepted")
	}

	for _, bad := range []string{
		"",
		"sha256$1000$abc$def",
		"pbkdf2-sha256$",
		"pbkdf2-sha256$0$YWJjZGVmZ2hpamtsbW5vcA==$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"pbkdf2-sha256$1000$short$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"pbkdf2-sha256$1000$YWJjZGVmZ2hpamtsbW5vcA==$AAAA",
	} {
		if _, err := parsePasswordHash(bad); err == nil {
			t.Errorf("parsePasswordHash(%q) accepted a malformed hash", bad)
		}
	}
}

func TestAuthenticateWithPasswordHash(t *testing.T) {
	encoded, err := NewPasswordHash("secret", 1000)
	if err != nil {
		t.Fatal(err)
	}
	web := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>app</title>")}}
	cfg := &config.Config{
		HomeDir:      t.TempDir(),
		Admin:        "admin",
		Password:     "plaintext-should-be-ignored",
		PasswordHash: encoded,
		Port:         "0",
	}
	s := New(cfg, files.New(cfg.HomeDir), web, log.New(io.Discard, "", 0))

	req := httptest.NewRequest("GET", "/api/info", nil)
	req.SetBasicAuth("admin", "secret")
	if rec := s.do(t, req); rec.Code != http.StatusOK {
		t.Fatalf("hash login status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest("GET", "/api/info", nil)
	req.SetBasicAuth("admin", "plaintext-should-be-ignored")
	if rec := s.do(t, req); rec.Code != http.StatusUnauthorized {
		t.Fatalf("plaintext password status = %d, want 401 when a hash is configured", rec.Code)
	}

	// A malformed hash fails closed: even the right plaintext is refused.
	cfg = &config.Config{HomeDir: t.TempDir(), Admin: "admin", Password: "x", PasswordHash: "garbage", Port: "0"}
	s = New(cfg, files.New(cfg.HomeDir), web, log.New(io.Discard, "", 0))
	req = httptest.NewRequest("GET", "/api/info", nil)
	req.SetBasicAuth("admin", "x")
	if rec := s.do(t, req); rec.Code != http.StatusUnauthorized {
		t.Fatalf("malformed hash status = %d, want 401 (fail closed)", rec.Code)
	}
}

func TestLoginThrottle(t *testing.T) {
	s := newTestServer(t)

	attempt := func(password string) *httptest.ResponseRecorder {
		t.Helper()
		req := form(url.Values{"username": {"admin"}, "password": {password}, "next": {"/"}})
		return s.do(t, req)
	}

	// Five failures are free; from the sixth, even the correct password is
	// bounced for a while.
	for i := 0; i < loginFailLimit; i++ {
		if rec := attempt("wrong"); rec.Code != http.StatusSeeOther {
			t.Fatalf("failure #%d status = %d, want 303", i+1, rec.Code)
		}
	}
	if rec := attempt("secret"); rec.Code != http.StatusSeeOther {
		t.Fatalf("throttled correct password status = %d, want 303 redirect to the error", rec.Code)
	}
	if loc := attempt("secret").Header().Get("Location"); !strings.Contains(loc, "error=1") {
		t.Fatalf("throttled login Location = %q, want the error redirect", loc)
	}

	// A different source address is not affected.
	req := form(url.Values{"username": {"admin"}, "password": {"secret"}, "next": {"/"}})
	req.RemoteAddr = "10.9.9.9:5555"
	if rec := s.do(t, req); rec.Code != http.StatusSeeOther {
		t.Fatalf("other address status = %d, want 303", rec.Code)
	}
	if loc := s.do(t, req).Header().Get("Location"); strings.Contains(loc, "error=1") {
		t.Fatalf("other address Location = %q, want a successful login", loc)
	}
}

func TestSessionStorePersistsToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	logger := log.New(io.Discard, "", 0)

	first := newSessionStoreFile(path, logger)
	token, err := first.create("admin")
	if err != nil {
		t.Fatal(err)
	}

	// A fresh store over the same file keeps the live session (a restart).
	second := newSessionStoreFile(path, logger)
	if _, ok := second.get(token); !ok {
		t.Fatal("session did not survive the store restart")
	}
	second.delete(token)

	// Deletion is persisted too.
	third := newSessionStoreFile(path, logger)
	if _, ok := third.get(token); ok {
		t.Fatal("deleted session came back after the store restart")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("sessions file mode = %v, %v; want 0600", info.Mode(), err)
	}
}

func TestParseShareTTL(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Duration
		bad  bool
	}{
		{raw: "", want: 0},
		{raw: "7d", want: 7 * 24 * time.Hour},
		{raw: "12h", want: 12 * time.Hour},
		{raw: "30m", want: 30 * time.Minute},
		{raw: "0", bad: true},
		{raw: "-1h", bad: true},
		{raw: "bogus", bad: true},
		{raw: "7x", bad: true},
	}
	for _, c := range cases {
		got, err := parseShareTTL(c.raw)
		if c.bad {
			if err == nil {
				t.Errorf("parseShareTTL(%q) = %v, want an error", c.raw, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("parseShareTTL(%q) = (%v, %v), want (%v, nil)", c.raw, got, err, c.want)
		}
	}
}

func TestShareEndpoints(t *testing.T) {
	s := newTestServer(t)
	root := s.fm.Root()
	if err := os.WriteFile(filepath.Join(root, "report.pdf"), []byte("%PDF-report"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "note.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	create := func(path, ttl string) *httptest.ResponseRecorder {
		t.Helper()
		endpoint := "/api/share?path=" + url.QueryEscape(path)
		if ttl != "" {
			endpoint += "&ttl=" + url.QueryEscape(ttl)
		}
		req := httptest.NewRequest("POST", endpoint, nil)
		req.SetBasicAuth("admin", "secret")
		return s.do(t, req)
	}

	// Share a file without a ttl.
	rec := create("/report.pdf", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Token  string `json:"token"`
		URL    string `json:"url"`
		Path   string `json:"path"`
		IsDir  bool   `json:"isDir"`
		Expiry string `json:"expires"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(created.Token) != 32 || created.URL != "/s/"+created.Token || created.IsDir {
		t.Fatalf("share = %+v", created)
	}
	if created.Expiry != "0001-01-01T00:00:00Z" {
		// A share without a ttl never expires: the zero time.
		t.Fatalf("no-ttl share has expiry %q", created.Expiry)
	}

	// The public route serves the file inline (its type is mapped), without
	// any authentication.
	public := httptest.NewRequest("GET", "/s/"+created.Token, nil)
	rec = s.do(t, public)
	if rec.Code != http.StatusOK {
		t.Fatalf("public status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("public Content-Type = %q, want application/pdf", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "inline") {
		t.Fatalf("public Content-Disposition = %q, want inline", cd)
	}
	if rec.Body.String() != "%PDF-report" {
		t.Fatalf("public body = %q", rec.Body.String())
	}

	// An unknown or malformed token is 404, never a path operation.
	for _, tok := range []string{"", "nope", created.Token + "/x"} {
		req := httptest.NewRequest("GET", "/s/"+tok, nil)
		if rec := s.do(t, req); rec.Code != http.StatusNotFound {
			t.Fatalf("GET /s/%q status = %d, want 404", tok, rec.Code)
		}
	}

	// Folders are served as zips.
	rec = create("/docs", "7d")
	if rec.Code != http.StatusOK {
		t.Fatalf("folder share status = %d: %s", rec.Code, rec.Body.String())
	}
	var folder struct {
		Token string `json:"token"`
		URL   string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &folder); err != nil {
		t.Fatal(err)
	}
	rec = s.do(t, httptest.NewRequest("GET", folder.URL, nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("folder share status = %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if _, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len())); err != nil {
		t.Fatalf("folder share is not a zip: %v", err)
	}

	// The list shows both shares; revoking removes the link.
	req := httptest.NewRequest("GET", "/api/shares", nil)
	req.SetBasicAuth("admin", "secret")
	rec = s.do(t, req)
	var list struct {
		Shares []struct {
			Token string `json:"token"`
		} `json:"shares"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Shares) != 2 {
		t.Fatalf("shares listed = %d, want 2", len(list.Shares))
	}

	req = httptest.NewRequest("DELETE", "/api/share?token="+url.QueryEscape(created.Token), nil)
	req.SetBasicAuth("admin", "secret")
	if rec := s.do(t, req); rec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := s.do(t, httptest.NewRequest("GET", created.URL, nil)); rec.Code != http.StatusNotFound {
		t.Fatalf("revoked link status = %d, want 404", rec.Code)
	}
	req = httptest.NewRequest("DELETE", "/api/share?token=nope", nil)
	req.SetBasicAuth("admin", "secret")
	if rec := s.do(t, req); rec.Code != http.StatusNotFound {
		t.Fatalf("revoke unknown status = %d, want 404", rec.Code)
	}

	// Creating requires authentication like every other mutation.
	if rec := s.do(t, httptest.NewRequest("POST", "/api/share?path=/docs", nil)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create status = %d, want 401", rec.Code)
	}
	// Sharing a missing path is 404.
	if rec := create("/missing", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("missing path status = %d, want 404", rec.Code)
	}
	// A bad ttl is 400.
	if rec := create("/docs", "never"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad ttl status = %d, want 400", rec.Code)
	}
}

func TestExtractEndpoint(t *testing.T) {
	s := newTestServer(t)
	root := s.fm.Root()
	// The folder must not share the archive's name: extracting docs.zip aims
	// at /docs, so the source folder is called something else.
	if err := os.Mkdir(filepath.Join(root, "archive-me"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "archive-me", "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Build a real archive of the folder through the app's own zip code.
	var buf bytes.Buffer
	if err := s.fm.Zip("/archive-me", &buf); err != nil {
		t.Fatal(err)
	}
	// The archive is stored under a different name than the source folder:
	// extracting archive-me.zip aims at /archive-me, which exists — a real
	// deployment's downloaded-then-uploaded archive lands somewhere fresh.
	if err := os.WriteFile(filepath.Join(root, "bundle.zip"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	extract := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST", "/api/extract?path="+url.QueryEscape(path), nil)
		req.SetBasicAuth("admin", "secret")
		return s.do(t, req)
	}

	rec := extract("/bundle.zip")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Extracted string `json:"extracted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Extracted != "/bundle" {
		t.Fatalf("extracted = %q, want /bundle", body.Extracted)
	}
	if data, err := os.ReadFile(filepath.Join(root, "bundle", "note.txt")); err != nil || string(data) != "hello" {
		t.Fatalf("extracted file = %q, %v", data, err)
	}

	// Extracting again refuses to overwrite (409), a missing archive is 404,
	// a plain folder is 500-class "is a directory" — and auth is required.
	if rec := extract("/bundle.zip"); rec.Code != http.StatusConflict {
		t.Fatalf("re-extract status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if rec := extract("/missing.zip"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want 404", rec.Code)
	}
	if rec := s.do(t, httptest.NewRequest("POST", "/api/extract?path=/bundle.zip", nil)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", rec.Code)
	}
}

func TestUploadEndpointNestedPaths(t *testing.T) {
	s := newTestServer(t)

	// Folder uploads send one "path" field per file, aligned by order; Go's
	// multipart parser strips directories from the part filename itself.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, item := range []struct{ path, content string }{
		{"docs/img/a.png", "a"},
		{"docs/b.txt", "b"},
	} {
		if err := mw.WriteField("path", item.path); err != nil {
			t.Fatal(err)
		}
		fw, err := mw.CreateFormFile("file", "whatever-the-client-sent")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(fw, item.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/upload?path=/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.SetBasicAuth("admin", "secret")
	rec := s.do(t, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Uploaded []string `json:"uploaded"`
		Failed   []any    `json:"failed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Uploaded) != 2 || len(body.Failed) != 0 {
		t.Fatalf("uploaded = %v, failed = %v", body.Uploaded, body.Failed)
	}
	root := s.fm.Root()
	if data, err := os.ReadFile(filepath.Join(root, "docs", "img", "a.png")); err != nil || string(data) != "a" {
		t.Fatalf("docs/img/a.png = %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "docs", "b.txt")); err != nil || string(data) != "b" {
		t.Fatalf("docs/b.txt = %q, %v", data, err)
	}

	// A ".." path is clamped into the destination directory, never outside.
	var buf2 bytes.Buffer
	mw2 := multipart.NewWriter(&buf2)
	if err := mw2.WriteField("path", "../../evil.txt"); err != nil {
		t.Fatal(err)
	}
	fw2, err := mw2.CreateFormFile("file", "x")
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(fw2, "evil")
	mw2.Close()
	req = httptest.NewRequest("POST", "/api/upload?path=/", &buf2)
	req.Header.Set("Content-Type", mw2.FormDataContentType())
	req.SetBasicAuth("admin", "secret")
	if rec := s.do(t, req); rec.Code != http.StatusOK {
		t.Fatalf("clamped upload status = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "evil.txt")); err != nil {
		t.Fatalf("clamped upload should land at the root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("the upload escaped the root")
	}
}
