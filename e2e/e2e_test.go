// Package e2e drives the built taskroll binary the way a person or an agent
// does: in a real git repository, through the commands, git's merges and the
// web server. The fixture is examples/demo, the same project the README
// offers as a demo, so the demo cannot rot without this failing.
package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

var bin string

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Println("skipping e2e: no git")
		os.Exit(0)
	}
	dir, err := os.MkdirTemp("", "taskroll-e2e")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "taskroll")
	if out, err := exec.Command("go", "build", "-o", bin, "github.com/hvish/taskroll/cmd/taskroll").CombinedOutput(); err != nil {
		fmt.Printf("build: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// repo is a git repository the binary runs in, isolated from the machine's
// git configuration.
type repo struct {
	t   *testing.T
	dir string
	env []string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	if testing.Short() {
		t.Skip("runs the binary and git")
	}
	r := &repo{t: t, dir: t.TempDir(), env: append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "TASKROLL_USER=Tester",
		"GIT_AUTHOR_NAME=Tester", "GIT_AUTHOR_EMAIL=tester@example.com",
		"GIT_COMMITTER_NAME=Tester", "GIT_COMMITTER_EMAIL=tester@example.com")}
	r.git("init", "-q", "-b", "main")
	return r
}

// demoRepo is a repository holding a copy of examples/demo, committed.
func demoRepo(t *testing.T) *repo {
	t.Helper()
	r := newRepo(t)
	src := filepath.Join("..", "examples", "demo")
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		dst := filepath.Join(r.dir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	r.git("add", "-A")
	r.git("commit", "-qm", "demo")
	return r
}

func (r *repo) cmd(name string, args ...string) (string, error) {
	r.t.Helper()
	c := exec.Command(name, args...)
	c.Dir, c.Env = r.dir, r.env
	out, err := c.CombinedOutput()
	return string(out), err
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	out, err := r.cmd("git", args...)
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

// tr runs a taskroll command that must succeed.
func (r *repo) tr(args ...string) string {
	r.t.Helper()
	out, err := r.cmd(bin, args...)
	if err != nil {
		r.t.Fatalf("taskroll %v: %v\n%s", args, err, out)
	}
	return out
}

// fails runs a taskroll command that must fail, and returns its output.
func (r *repo) fails(args ...string) string {
	r.t.Helper()
	out, err := r.cmd(bin, args...)
	if err == nil {
		r.t.Fatalf("taskroll %v succeeded, want an error:\n%s", args, out)
	}
	return out
}

type row struct {
	ID        string   `json:"id"`
	Status    string   `json:"status"`
	Title     string   `json:"title"`
	Epic      string   `json:"epic"`
	Ready     bool     `json:"ready"`
	BlockedBy []string `json:"blocked_by"`
	Comments  []struct {
		Author string `json:"author"`
		Body   string `json:"body"`
	} `json:"comments"`
}

func (r *repo) list(args ...string) []row {
	r.t.Helper()
	var rows []row
	out := r.tr(append([]string{"list", "--json"}, args...)...)
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		r.t.Fatalf("list --json: %v\n%s", err, out)
	}
	return rows
}

func ids(rows []row) []string {
	var out []string
	for _, x := range rows {
		out = append(out, x.ID)
	}
	slices.Sort(out)
	return out
}

func want(t *testing.T, out string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
}

// The committed demo is canonical and its generated files are current. This
// reads it in place, as `cd examples/demo && taskroll serve` would.
func TestDemoIsCurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the binary")
	}
	c := exec.Command(bin, "index", "--check")
	c.Dir = filepath.Join("..", "examples", "demo")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("examples/demo is stale; regenerate with `taskroll index` there: %v\n%s", err, out)
	}
}

func TestFromScratch(t *testing.T) {
	r := newRepo(t)
	want(t, r.tr("init"), "wrote .taskroll.json")
	want(t, r.tr("whoami"), "Tester")
	r.tr("epic", "new", "--slug", "payments", "--title", "Payments", "--intro", "Checkout and refunds.")
	want(t, r.tr("epic", "list"), "epic-0-payments")

	r.fails("add", "--epic", "epic-0-payments", "--series", "PAY", "--title", "No first id")
	r.tr("add", "--epic", "epic-0-payments", "--id", "PAY-001", "--title", "Card checkout", "--size", "M")
	want(t, r.tr("add", "--epic", "epic-0-payments", "--series", "PAY", "--title", "Refunds", "--size", "S", "--depends", "PAY-001"), "PAY-002")
	r.fails("status", "PAY-001", "blocked")

	if got := ids(r.list("--ready")); !slices.Equal(got, []string{"PAY-001"}) {
		t.Fatalf("ready before PAY-001 ships: %v", got)
	}
	r.tr("status", "PAY-001", "in_progress")
	r.tr("comment", "PAY-002", "--body", "Waiting on the provider contract.")
	r.tr("done", "PAY-001", "--on", "2026-09-01")
	if got := ids(r.list("--ready")); !slices.Equal(got, []string{"PAY-002"}) {
		t.Fatalf("ready after PAY-001 ships: %v", got)
	}
	r.tr("edit", "PAY-002", "--title", "Refunds and voids", "--add-label", "money")

	show := r.tr("show", "PAY-002")
	want(t, show, "Refunds and voids", "Waiting on the provider contract.", "money")
	want(t, r.tr("velocity", "--weeks", "2"), "cycle time")
	want(t, r.tr("burndown"), "1 of 2 complete")
	r.tr("fmt")
	want(t, r.tr("index", "--check"), "up to date")

	md, err := os.ReadFile(filepath.Join(r.dir, "docs", "tasks", "epics", "epic-0-payments.md"))
	if err != nil {
		t.Fatal(err)
	}
	want(t, string(md), "- [x] **PAY-001** - Card checkout `M` · Done: 2026-09-01", "- [ ] **PAY-002** - Refunds and voids `S`")

	// A hand edit to a generated file is caught by the gate.
	if err := os.WriteFile(filepath.Join(r.dir, "docs", "tasks", "epics", "epic-0-payments.md"), append(md, "edited by hand\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	r.fails("index", "--check")
}

func TestDemoReads(t *testing.T) {
	r := demoRepo(t)
	epics := r.tr("epic", "list")
	want(t, epics, "epic-0-checkout", "epic-1-catalog", "epic-2-platform")

	got := ids(r.list("--ready", "--type", "task,bug"))
	if !slices.Equal(got, []string{"API-003", "API-004", "CAT-004", "CO-005"}) {
		t.Errorf("ready tasks: %v", got)
	}
	blocked := r.list("--blocked")
	if !slices.Contains(ids(blocked), "CO-006") {
		t.Errorf("CO-006 waits on CO-003 and a sign-off, so it is blocked: %v", ids(blocked))
	}
	want(t, r.tr("show", "CO-006"), "a finance sign-off on partial refunds", "second approver")
	want(t, r.tr("show", "CO-003"), "blocker:    yes", "3-D Secure")
	want(t, r.tr("list", "--type", "debt"), "TD-001", "TD-002")
	want(t, r.tr("list", "--type", "opportunity"), "gift-cards")
	want(t, r.tr("velocity", "--weeks", "8"), "cycle time: median")
	want(t, r.tr("burndown"), "8 of 18 complete")
}

func TestDemoWritesAndMerges(t *testing.T) {
	r := demoRepo(t)
	r.tr("config", "merge-driver")
	r.git("add", "-A")
	r.git("commit", "-qm", "merge driver")

	r.git("switch", "-qc", "a")
	want(t, r.tr("add", "--epic", "epic-0-checkout", "--series", "CO", "--title", "Apple Pay", "--size", "M"), "CO-009")
	r.tr("status", "CO-005", "in_progress")
	r.git("commit", "-qam", "a")

	r.git("switch", "-q", "main")
	r.git("switch", "-qc", "b")
	// Allocation sees branch a, so the two branches never mint the same id.
	want(t, r.tr("add", "--epic", "epic-0-checkout", "--series", "CO", "--title", "Google Pay", "--size", "M"), "CO-010")
	r.tr("comment", "CO-005", "--body", "Design is ready.")
	r.tr("new", "debt", "--title", "Cart totals computed twice", "--area", "checkout", "--size", "S", "--body", "Once in the API and once in the page.")
	r.git("commit", "-qam", "b")

	r.git("switch", "-q", "main")
	r.git("merge", "-q", "--no-edit", "a")
	if out, err := r.cmd("git", "merge", "--no-edit", "b"); err != nil {
		t.Fatalf("merging two branches that touched one epic and one item conflicted: %v\n%s", err, out)
	}
	r.tr("index")
	want(t, r.tr("index", "--check"), "up to date")

	got := r.list("--epic", "epic-0-checkout")
	if !slices.Contains(ids(got), "CO-009") || !slices.Contains(ids(got), "CO-010") {
		t.Fatalf("both branches' tasks survive the merge: %v", ids(got))
	}
	co5 := r.tr("show", "CO-005")
	want(t, co5, "in_progress", "Design is ready.")
	want(t, r.tr("list", "--type", "debt"), "TD-003")
}

func TestDemoServe(t *testing.T) {
	r := demoRepo(t)
	c := exec.Command(bin, "serve")
	c.Dir, c.Env = r.dir, r.env
	stdout, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })

	line := make(chan string, 1)
	go func() {
		buf := make([]byte, 512)
		n, _ := stdout.Read(buf)
		line <- string(buf[:n])
	}()
	var printed string
	select {
	case printed = <-line:
	case <-time.After(10 * time.Second):
		t.Fatal("serve printed no URL")
	}
	m := regexp.MustCompile(`(http://127\.0\.0\.1:\d+)/\?token=([0-9a-f]+)`).FindStringSubmatch(printed)
	if m == nil {
		t.Fatalf("no URL in %q", printed)
	}
	base, token := m[1], m[2]

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	get := func(path string) (int, string, http.Header) {
		t.Helper()
		res, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(body), res.Header
	}
	post := func(path, body string, withToken bool, origin string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, base+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		if withToken {
			req.Header.Set("X-Taskroll-Token", token)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res.StatusCode
	}

	if code, _, _ := get("/api/items"); code != http.StatusForbidden {
		t.Fatalf("no session: %d", code)
	}
	// The token link to an item lands on that item, now with a session.
	code, page, hdr := get("/items/CO-003?token=" + token)
	if code != http.StatusOK || !strings.Contains(page, `<div id="root">`) {
		t.Fatalf("deep link: %d\n%s", code, page)
	}
	if csp := hdr.Get("Content-Security-Policy"); strings.Contains(csp, "unsafe-inline") || !strings.Contains(csp, "'nonce-") {
		t.Fatalf("page csp: %s", csp)
	}
	asset := regexp.MustCompile(`/assets/[^"]+\.js`).FindString(page)
	if code, body, _ := get(asset); asset == "" || code != http.StatusOK || len(body) < 1000 {
		t.Fatalf("the page's script %q: %d", asset, code)
	}

	var rows []row
	_, body, _ := get("/api/items?ready=1")
	if err := json.Unmarshal([]byte(body), &rows); err != nil || !slices.Contains(ids(rows), "CO-005") {
		t.Fatalf("ready items: %v %s", err, body)
	}

	if code := post("/api/items/CO-005/status", `{"status":"in_progress"}`, false, base); code != http.StatusForbidden {
		t.Fatalf("a write without the token header: %d", code)
	}
	if code := post("/api/items/CO-005/status", `{"status":"in_progress"}`, true, "http://evil.example"); code != http.StatusForbidden {
		t.Fatalf("a write from another origin: %d", code)
	}
	if code := post("/api/items/CO-005/status", `{"status":"in_progress"}`, true, base); code != http.StatusOK {
		t.Fatalf("a write from the page: %d", code)
	}
	if code := post("/api/items/CO-005/comments", `{"body":"From the board."}`, true, base); code != http.StatusOK {
		t.Fatalf("a comment from the page: %d", code)
	}

	// The web's writes are the CLI's writes: same records, views regenerated.
	want(t, r.tr("show", "CO-005"), "in_progress", "From the board.")
	want(t, r.tr("index", "--check"), "up to date")
}
