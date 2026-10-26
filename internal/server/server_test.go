package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

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
