// Package web is the tracker's local web UI: a board and a list over the
// same records the CLI writes, for one person on their own machine.
//
// It is a server on the loopback interface that can change files in a
// repository, so every web page the person visits can try to reach it. The
// defences are layered, and each one alone stops a class of attack:
//
//   - It listens on 127.0.0.1 only, and refuses a request whose Host is not
//     that address and port, which defeats DNS rebinding (a hostile name
//     resolving to 127.0.0.1 still sends its own Host).
//   - A random token, printed once in the URL, becomes an HttpOnly,
//     SameSite=Strict cookie; every request needs it, so another local user
//     or a page that guesses the port reads nothing.
//   - A write is a POST carrying the token in a header and the server's own
//     Origin; a cross-site form cannot set the header, and a cross-site
//     script cannot read the token or pass CORS, which is never answered.
//   - The pages load no inline script under a strict Content-Security-Policy,
//     and the client puts every string into the DOM as text, so a title
//     holding markup renders as the characters it is.
//
// Every write goes through the core's Store, under the tracker lock, and
// regenerates the views exactly as the CLI does.
package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/hvish/taskroll"
)

//go:embed static
var static embed.FS

// cookieName carries the session token after the first visit.
const cookieName = "taskroll_session"

// maxBody bounds a request body; the largest legitimate one is a comment.
const maxBody = 64 << 10

// Server serves one tracker to one person.
type Server struct {
	// Open returns the project fresh for each request, so a change made by
	// the CLI or another agent shows on the next load.
	Open  func() (*taskroll.Project, error)
	Actor string
	token string
	host  string // the Host every request must name, e.g. 127.0.0.1:7788
}

// New makes a server with a fresh session token.
func New(open func() (*taskroll.Project, error), actor string) (*Server, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return &Server{Open: open, Actor: actor, token: hex.EncodeToString(b)}, nil
}

// Listen binds the loopback interface; port 0 picks a free port.
func (s *Server) Listen(port int) (net.Listener, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}
	s.host = ln.Addr().String()
	return ln, nil
}

// URL is the address to open, carrying the session token.
func (s *Server) URL() string { return "http://" + s.host + "/?token=" + s.token }

// Serve runs until the listener closes.
func (s *Server) Serve(ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	return srv.Serve(ln)
}

// Handler is the whole application, behind the guards.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	assets, _ := fs.Sub(static, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(assets)))
	mux.HandleFunc("GET /{$}", s.page)
	mux.HandleFunc("GET /api/meta", s.meta)
	mux.HandleFunc("GET /api/items", s.items)
	mux.HandleFunc("GET /api/items/{id}", s.item)
	mux.HandleFunc("POST /api/items/{id}/status", s.setStatus)
	mux.HandleFunc("POST /api/items/{id}/comments", s.comment)
	mux.HandleFunc("POST /api/tasks", s.addTask)
	return s.guard(mux)
}

// guard applies the checks every request passes before any handler runs.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")

		if r.Host != s.host {
			http.Error(w, "wrong host", http.StatusMisdirectedRequest)
			return
		}
		// The landing URL carries the token once; it becomes a cookie and
		// the browser is sent to a clean URL, so the token leaves the
		// address bar and the history.
		if t := r.URL.Query().Get("token"); t != "" && r.Method == http.MethodGet && r.URL.Path == "/" {
			if !s.valid(t) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		c, err := r.Cookie(cookieName)
		if err != nil || !s.valid(c.Value) {
			http.Error(w, "forbidden: open the URL the serve command printed", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !s.valid(r.Header.Get("X-Taskroll-Token")) || r.Header.Get("Origin") != "http://"+s.host {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				http.Error(w, "json only", http.StatusUnsupportedMediaType)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) valid(t string) bool {
	return subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) == 1
}

// page is the application shell. The token reaches the script through a
// meta tag the script reads, never through inline script.
func (s *Server) page(w http.ResponseWriter, _ *http.Request) {
	raw, err := static.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	page := strings.Replace(string(raw), "{{TOKEN}}", s.token, 1)
	// The asset URLs carry a hash of the build's assets, so a browser never
	// pairs a new page with a script or stylesheet it cached from an old one.
	page = strings.NewReplacer("/static/app.css", "/static/app.css?v="+assetVersion, "/static/app.js", "/static/app.js?v="+assetVersion).Replace(page)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(page))
}

// assetVersion is a hash of the embedded assets.
var assetVersion = func() string {
	h := sha256.New()
	_ = fs.WalkDir(static, "static", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			raw, _ := static.ReadFile(path)
			h.Write(raw)
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:12]
}()

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func fail(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// Meta is what the forms and filters need to know about the tracker.
type Meta struct {
	Actor    string                 `json:"actor"`
	Statuses []string               `json:"statuses"`
	Sizes    []string               `json:"sizes"`
	Types    []string               `json:"types"`
	Labels   []string               `json:"labels"`
	Epics    []taskroll.EpicSummary `json:"epics"`
	Fields   []taskroll.FieldSpec   `json:"fields"`
	Markers  []taskroll.MarkerSpec  `json:"markers"`
	Series   map[string][]string    `json:"series"` // epic id -> id prefixes its tasks use
}

func (s *Server) meta(w http.ResponseWriter, _ *http.Request) {
	p, x, err := s.index()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	m := Meta{Actor: s.Actor, Statuses: taskroll.Statuses, Fields: p.Settings.Epic.Fields, Markers: p.Settings.Epic.Markers, Series: map[string][]string{}}
	for _, z := range p.Settings.Sizes {
		m.Sizes = append(m.Sizes, z.Name)
	}
	labels := map[string]bool{}
	types := map[string]bool{}
	for _, f := range x.Files {
		for _, it := range f.Items {
			types[it.Type] = true
			for _, l := range it.Labels {
				labels[l] = true
			}
			if f.IsEpic() {
				if pre := taskroll.Prefix(it.ID); pre != "" && !slices.Contains(m.Series[f.Epic.ID], pre) {
					m.Series[f.Epic.ID] = append(m.Series[f.Epic.ID], pre)
				}
			}
		}
	}
	for l := range labels {
		m.Labels = append(m.Labels, l)
	}
	for t := range types {
		m.Types = append(m.Types, t)
	}
	slices.Sort(m.Labels)
	slices.Sort(m.Types)
	for _, e := range x.Epics() {
		if e.Archived == "" {
			m.Epics = append(m.Epics, e)
		}
	}
	writeJSON(w, http.StatusOK, m)
}

// Row is an item with the joins a reader of one record cannot compute.
type Row struct {
	taskroll.Item
	Epic       string   `json:"epic"`
	Ready      bool     `json:"ready"`
	BlockedBy  []string `json:"blocked_by,omitempty"`
	Conditions []string `json:"conditions,omitempty"`
	Dependents []string `json:"dependents,omitempty"`
}

func rowFor(x *taskroll.Index, it taskroll.Item) Row {
	ids, prose := x.Blockers(it)
	r := Row{Item: it, Epic: x.EpicOf(it.ID), Ready: x.Ready(it), Dependents: x.Dependents(it.ID)}
	if it.Open() {
		r.BlockedBy, r.Conditions = ids, prose
	}
	return r
}

func (s *Server) index() (*taskroll.Project, *taskroll.Index, error) {
	p, err := s.Open()
	if err != nil {
		return nil, nil, err
	}
	x, err := p.Index()
	return p, x, err
}

// items lists with the CLI's filters: type, status, epic, label, size and
// text repeat or take commas; ready, blocked and all are flags.
func (s *Server) items(w http.ResponseWriter, r *http.Request) {
	_, x, err := s.index()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	q := r.URL.Query()
	list := func(k string) []string {
		var out []string
		for _, v := range q[k] {
			for part := range strings.SplitSeq(v, ",") {
				if part = strings.TrimSpace(part); part != "" {
					out = append(out, part)
				}
			}
		}
		return out
	}
	f := taskroll.Filter{
		Types: list("type"), Statuses: list("status"), Epics: list("epic"), Labels: list("label"), Sizes: list("size"),
		Text: q.Get("text"), Ready: q.Get("ready") == "1", Blocked: q.Get("blocked") == "1", All: q.Get("all") == "1",
	}
	for _, st := range f.Statuses {
		if !slices.Contains(taskroll.Statuses, st) {
			fail(w, http.StatusBadRequest, fmt.Errorf("status %q is not one of %s", st, strings.Join(taskroll.Statuses, ", ")))
			return
		}
	}
	rows := []Row{}
	for _, it := range x.List(f) {
		rows = append(rows, rowFor(x, it))
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) item(w http.ResponseWriter, r *http.Request) {
	_, x, err := s.index()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	it, ok := x.Get(r.PathValue("id"))
	if !ok {
		fail(w, http.StatusNotFound, fmt.Errorf("no item %s", r.PathValue("id")))
		return
	}
	writeJSON(w, http.StatusOK, rowFor(x, it))
}

// writeResult answers a write: the record as it now reads, or why not.
// Validation failures are the client's to fix; anything else is ours.
func (s *Server) writeResult(w http.ResponseWriter, it taskroll.Item, err error) {
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, taskroll.ErrNotInitialised) {
			status = http.StatusConflict
		}
		fail(w, status, err)
		return
	}
	_, x, err := s.index()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rowFor(x, it))
}

func (s *Server) setStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
		On     string `json:"on,omitempty"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.Open()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	it, err := p.Store(s.Actor).SetStatus(r.PathValue("id"), req.Status, req.On)
	s.writeResult(w, it, err)
}

func (s *Server) comment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.Open()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	it, err := p.Store(s.Actor).Comment(r.PathValue("id"), req.Body)
	s.writeResult(w, it, err)
}

func (s *Server) addTask(w http.ResponseWriter, r *http.Request) {
	var req taskroll.NewTask
	if err := decode(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.Open()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	it, err := p.AddTask(s.Actor, req)
	s.writeResult(w, it, err)
}
