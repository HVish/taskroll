package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hvish/taskroll"
)

const host = "127.0.0.1:7788"

func fixture(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	f := &taskroll.File{
		Path: filepath.Join(taskroll.DataDir(dir), "epic-1-demo.jsonl"),
		Epic: taskroll.Item{ID: "epic-1-demo", Type: taskroll.TypeEpic, Schema: taskroll.SchemaVersion, Title: "Demo"},
		Items: []taskroll.Item{
			{ID: "D-001", Type: taskroll.TypeTask, Status: taskroll.StatusTodo, Title: `<img src=x onerror=alert(1)> escape me`, Size: "S"},
			{ID: "D-002", Type: taskroll.TypeTask, Status: taskroll.StatusTodo, Title: "second", Depends: []string{"D-001"}},
		},
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	s, err := New(func() (*taskroll.Project, error) { return taskroll.OpenProject(dir) }, "tester")
	if err != nil {
		t.Fatal(err)
	}
	s.host = host
	return s, dir
}

type req struct {
	method, path, body string
	host, origin       string
	cookie, header     bool
	contentType        string
}

func (s *Server) do(t *testing.T, r req) *httptest.ResponseRecorder {
	t.Helper()
	hr := httptest.NewRequest(r.method, r.path, strings.NewReader(r.body))
	hr.Host = host
	if r.host != "" {
		hr.Host = r.host
	}
	if r.cookie {
		hr.AddCookie(&http.Cookie{Name: cookieName, Value: s.token})
	}
	if r.header {
		hr.Header.Set("X-Taskroll-Token", s.token)
	}
	if r.origin != "" {
		hr.Header.Set("Origin", r.origin)
	}
	if r.contentType != "" {
		hr.Header.Set("Content-Type", r.contentType)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, hr)
	return w
}

func write(path, body string) req {
	return req{method: "POST", path: path, body: body, cookie: true, header: true, origin: "http://" + host, contentType: "application/json"}
}

func TestGuards(t *testing.T) {
	s, _ := fixture(t)
	for name, c := range map[string]struct {
		r    req
		want int
	}{
		"no session":                        {req{method: "GET", path: "/api/items"}, http.StatusForbidden},
		"wrong session":                     {req{method: "GET", path: "/?token=nope"}, http.StatusForbidden},
		"a rebinding name":                  {req{method: "GET", path: "/api/items", cookie: true, host: "evil.example:7788"}, http.StatusMisdirectedRequest},
		"write without the header":          {req{method: "POST", path: "/api/items/D-001/status", body: `{"status":"done"}`, cookie: true, origin: "http://" + host, contentType: "application/json"}, http.StatusForbidden},
		"write from another origin":         {req{method: "POST", path: "/api/items/D-001/status", body: `{"status":"done"}`, cookie: true, header: true, origin: "http://evil.example", contentType: "application/json"}, http.StatusForbidden},
		"write as a form, not json":         {req{method: "POST", path: "/api/items/D-001/status", body: `status=done`, cookie: true, header: true, origin: "http://" + host, contentType: "application/x-www-form-urlencoded"}, http.StatusUnsupportedMediaType},
		"a read with the session":           {req{method: "GET", path: "/api/items", cookie: true}, http.StatusOK},
		"the page with the session":         {req{method: "GET", path: "/", cookie: true}, http.StatusOK},
		"an unknown field in a write":       {write("/api/items/D-001/status", `{"status":"done","sudo":true}`), http.StatusBadRequest},
		"a status the workflow has not got": {write("/api/items/D-001/status", `{"status":"blocked"}`), http.StatusUnprocessableEntity},
	} {
		if got := s.do(t, c.r).Code; got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}

	// The landing URL sets a strict, script-proof cookie and leaves the
	// token out of the address bar.
	w := s.do(t, req{method: "GET", path: "/?token=" + s.token})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("landing: %d %s", w.Code, w.Header().Get("Location"))
	}
	ck := w.Result().Cookies()
	if len(ck) != 1 || !ck[0].HttpOnly || ck[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie: %+v", ck)
	}
	page := s.do(t, req{method: "GET", path: "/", cookie: true}).Body.String()
	if !strings.Contains(page, "/static/app.js?v="+assetVersion) || !strings.Contains(page, s.token) {
		t.Fatalf("page does not version its assets or carry the token:\n%s", page)
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("csp: %s", csp)
	}
}

func TestReadsAndWrites(t *testing.T) {
	s, dir := fixture(t)

	w := s.do(t, req{method: "GET", path: "/api/items?ready=1", cookie: true})
	var rows []Row
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "D-001" {
		t.Fatalf("ready: %+v", rows)
	}
	// Markup in a title leaves the server escaped in the JSON; the client
	// only ever sets it as text.
	if strings.Contains(w.Body.String(), "<img") {
		t.Fatalf("markup not escaped: %s", w.Body.String())
	}

	if w := s.do(t, write("/api/items/D-001/status", `{"status":"done","on":"2026-09-30"}`)); w.Code != http.StatusOK {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	if w := s.do(t, write("/api/items/D-002/comments", `{"body":"unblocked now"}`)); w.Code != http.StatusOK {
		t.Fatalf("comment: %d %s", w.Code, w.Body.String())
	}
	w = s.do(t, write("/api/tasks", `{"epic":"epic-1-demo","series":"D","title":"third","size":"M"}`))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"D-003"`) {
		t.Fatalf("add: %d %s", w.Code, w.Body.String())
	}

	// The writes went through the store: the record changed, the stamps
	// name the server's user, and the epic file was regenerated.
	files, err := taskroll.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	f, i, _ := taskroll.Find(files, "D-001")
	if it := f.Items[i]; it.Status != taskroll.StatusDone || it.Done != "2026-09-30" || it.ClosedBy != "tester" {
		t.Fatalf("D-001: %+v", it)
	}
	_, i, _ = taskroll.Find(files, "D-002")
	if c := f.Items[i].Comments; len(c) != 1 || c[0].Author != "tester" {
		t.Fatalf("D-002 comments: %+v", c)
	}
	md, err := os.ReadFile(filepath.Join(dir, "epics", "epic-1-demo.md"))
	if err != nil || !strings.Contains(string(md), "- [x] **D-001**") || !strings.Contains(string(md), "**D-003** - third") {
		t.Fatalf("epic file not regenerated: %v\n%s", err, md)
	}

	w = s.do(t, req{method: "GET", path: "/api/meta", cookie: true})
	var m Meta
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil || m.Actor != "tester" || len(m.Epics) != 1 || m.Series["epic-1-demo"][0] != "D" {
		t.Fatalf("meta: %v %+v", err, m)
	}
}

func TestBodyIsBounded(t *testing.T) {
	s, _ := fixture(t)
	big := `{"body":"` + strings.Repeat("x", maxBody) + `"}`
	if w := s.do(t, write("/api/items/D-001/comments", big)); w.Code != http.StatusBadRequest {
		t.Fatalf("an oversized body: %d", w.Code)
	}
}

func TestListenIsLoopbackOnly(t *testing.T) {
	s, _ := fixture(t)
	ln, err := s.Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	if !strings.HasPrefix(ln.Addr().String(), "127.0.0.1:") || !strings.HasPrefix(s.URL(), "http://127.0.0.1:") || !strings.Contains(s.URL(), "token=") {
		t.Fatalf("listening on %s, url %s", ln.Addr(), s.URL())
	}
}
