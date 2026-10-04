package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hvish/taskroll"
)

// The merge driver is only as good as git's use of it, so this runs real
// merges through a real repository with the real binary.
func TestMergeDriverThroughGit(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the tracker and runs git")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	bin := filepath.Join(t.TempDir(), "taskroll")
	build := exec.Command("go", "build", "-o", bin, "github.com/hvish/taskroll/cmd/taskroll")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	repo := t.TempDir()
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "TASKROLL_USER=tester",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	run := func(ok bool, name string, args ...string) string {
		t.Helper()
		c := exec.Command(name, args...)
		c.Dir, c.Env = repo, env
		out, err := c.CombinedOutput()
		if (err == nil) != ok {
			t.Fatalf("%s %v: err=%v\n%s", name, args, err, out)
		}
		return string(out)
	}
	git := func(args ...string) { t.Helper(); run(true, "git", args...) }
	dev := func(args ...string) { t.Helper(); run(true, bin, args...) }

	git("init", "-q", "-b", "main")
	dev("init")
	dev("epic", "new", "--slug", "pay", "--title", "Payments")
	dev("add", "--epic", "epic-0-pay", "--id", "PAY-001", "--title", "Card checkout", "--size", "M")
	git("add", "-A")
	git("commit", "-qm", "base")

	// Two branches append to one epic and edit one record in different fields.
	git("checkout", "-qb", "side")
	dev("add", "--epic", "epic-0-pay", "--series", "PAY", "--title", "Refunds")
	dev("comment", "PAY-001", "--body", "needs a PSP")
	git("commit", "-qam", "side")
	git("checkout", "-q", "main")
	dev("add", "--epic", "epic-0-pay", "--series", "PAY", "--title", "Receipts")
	dev("status", "PAY-001", "in_progress")
	git("commit", "-qam", "main")
	git("merge", "-q", "--no-edit", "side")

	x := index(t, repo)
	for _, id := range []string{"PAY-001", "PAY-002", "PAY-003"} {
		if _, ok := x.Get(id); !ok {
			t.Fatalf("%s lost in the merge", id)
		}
	}
	if it, _ := x.Get("PAY-001"); it.Status != taskroll.StatusInProgress || len(it.Comments) != 1 {
		t.Fatalf("PAY-001 did not merge field by field: %+v", it)
	}
	// The views kept one side; the gate says so until they are regenerated.
	run(false, bin, "index", "--check")
	dev("index")
	dev("index", "--check")
	git("commit", "-qam", "views")

	// A conflict stops the merge, keeps ours, and leaves theirs on the record.
	git("checkout", "-qb", "other")
	dev("edit", "PAY-002", "--title", "Refunds via the PSP")
	git("commit", "-qam", "other")
	git("checkout", "-q", "main")
	dev("edit", "PAY-002", "--title", "Refunds by hand")
	git("commit", "-qam", "ours")
	out := run(false, "git", "merge", "--no-edit", "other")
	if !strings.Contains(out, "title changed on both sides") {
		t.Fatalf("the driver did not report the conflict:\n%s", out)
	}
	it, _ := index(t, repo).Get("PAY-002")
	if it.Title != "Refunds by hand" || len(it.Comments) != 1 || !strings.Contains(it.Comments[0].Body, "Refunds via the PSP") {
		t.Fatalf("ours kept with theirs as a comment: %+v", it)
	}

	// With the binary gone, the merge falls back to conflict markers rather
	// than leaving ours unmarked, where a blind `git add` would drop theirs.
	git("merge", "--abort")
	git("checkout", "-qb", "late")
	dev("add", "--epic", "epic-0-pay", "--series", "PAY", "--title", "Late side")
	git("commit", "-qam", "late")
	git("checkout", "-q", "main")
	dev("add", "--epic", "epic-0-pay", "--series", "PAY", "--title", "Late main")
	git("commit", "-qam", "late main")
	if err := os.Rename(bin, bin+".gone"); err != nil {
		t.Fatal(err)
	}
	run(false, "git", "merge", "--no-edit", "late")
	raw, err := os.ReadFile(filepath.Join(repo, "docs", "tasks", "data", "epic-0-pay.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "<<<<<<< ours") || !strings.Contains(string(raw), "Late side") {
		t.Fatalf("no conflict markers without the driver binary:\n%s", raw)
	}
}

func TestDriverCommand(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "tools", "bin", "mytool")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd, err := driverCommand(root, exe, "tasks")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cmd, "' ./tools/bin/mytool") || !strings.Contains(cmd, `exec "$0" tasks merge-driver`) {
		t.Fatalf("a binary inside the repository is named relative to it: %s", cmd)
	}
	outside := filepath.Join(t.TempDir(), "taskroll")
	if cmd, _ = driverCommand(root, outside, ""); !strings.HasSuffix(cmd, " "+outside) {
		t.Fatalf("a binary outside keeps its absolute path: %s", cmd)
	}
	if _, err := driverCommand(root, outside, "tasks;rm"); err == nil {
		t.Fatal("an unsafe invocation word must be refused")
	}
}

func index(t *testing.T, repo string) *taskroll.Index {
	t.Helper()
	files, err := taskroll.Load(filepath.Join(repo, "docs", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	return taskroll.NewIndex(files, nil)
}
