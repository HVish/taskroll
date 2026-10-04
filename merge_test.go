package taskroll

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func encodeItems(t *testing.T, items ...Item) []byte {
	t.Helper()
	f := &File{Epic: Item{ID: "epic-1-demo", Type: TypeEpic, Schema: SchemaVersion, Title: "Demo"}, Items: items}
	raw, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func task(id, title string) Item {
	return Item{ID: id, Type: TypeTask, Status: StatusTodo, Title: title}
}

func mergedItems(t *testing.T, res MergeResult) []Item {
	t.Helper()
	f, err := Decode("merged", res.Data)
	if err != nil {
		t.Fatal(err)
	}
	return f.Items
}

func ids(items []Item) string {
	var s []string
	for _, it := range items {
		s = append(s, it.ID)
	}
	return strings.Join(s, ",")
}

func TestMergeAppendsFromBothSides(t *testing.T) {
	a := task("D-001", "a")
	base := encodeItems(t, a)
	ours := encodeItems(t, a, task("D-002", "ours"))
	theirs := encodeItems(t, a, task("D-003", "theirs"))
	res, err := Merge(base, ours, theirs, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) > 0 {
		t.Fatalf("conflicts: %v", res.Conflicts)
	}
	if got := ids(mergedItems(t, res)); got != "D-001,D-002,D-003" {
		t.Fatalf("got %s", got)
	}
}

func TestMergeRenumbersAnIDBothSidesAdded(t *testing.T) {
	a := task("D-001", "a")
	base := encodeItems(t, a)
	ours := encodeItems(t, a, task("D-002", "ours"))
	dep := task("D-003", "follow-up")
	dep.Depends = []string{"D-002", "D-001"}
	theirs := encodeItems(t, a, task("D-002", "theirs"), dep)

	res, err := Merge(base, ours, theirs, MergeOptions{Reserved: map[string]bool{"D-007": true}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Renamed["D-002"] != "D-008" {
		t.Fatalf("renamed = %v, want D-002 -> D-008 (past the reserved D-007)", res.Renamed)
	}
	if len(res.Conflicts) != 1 || !strings.Contains(res.Conflicts[0], "D-008") {
		t.Fatalf("conflicts = %v", res.Conflicts)
	}
	got := mergedItems(t, res)
	if ids(got) != "D-001,D-002,D-008,D-003" {
		t.Fatalf("got %s", ids(got))
	}
	if got[1].Title != "ours" || got[2].Title != "theirs" {
		t.Fatalf("ours must keep the id: %+v", got)
	}
	if len(got[2].Comments) != 1 || !strings.Contains(got[2].Comments[0].Body, "Renumbered from D-002") {
		t.Fatalf("the renumbered record says so: %+v", got[2].Comments)
	}
	if strings.Join(got[3].Depends, ",") != "D-008,D-001" {
		t.Fatalf("theirs' reference not rewritten: %v", got[3].Depends)
	}
}

func TestMergeRenumbersOursWhenTheirsIsPublished(t *testing.T) {
	a := task("D-001", "a")
	base := encodeItems(t, a)
	published := encodeItems(t, a, task("D-002", "on main"))
	dep := task("D-003", "local follow-up")
	dep.Depends = []string{"D-002"}
	ours := encodeItems(t, a, task("D-002", "local"), dep)

	res, err := Merge(base, ours, published, MergeOptions{Mainline: published})
	if err != nil {
		t.Fatal(err)
	}
	got := mergedItems(t, res)
	if ids(got) != "D-001,D-004,D-003,D-002" {
		t.Fatalf("got %s", ids(got))
	}
	if got[0+1].Title != "local" || got[3].Title != "on main" || got[2].Depends[0] != "D-004" {
		t.Fatalf("the published record keeps its id and ours follows the rename: %+v", got)
	}
	if len(res.Conflicts) != 1 || !strings.Contains(res.Conflicts[0], "ours is now D-004") {
		t.Fatalf("conflicts = %v", res.Conflicts)
	}
}

func TestMergeSameAdditionOnBothSidesIsOne(t *testing.T) {
	base := encodeItems(t)
	both := encodeItems(t, task("D-001", "same"))
	res, err := Merge(base, both, both, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) > 0 || ids(mergedItems(t, res)) != "D-001" {
		t.Fatalf("got %s %v", ids(mergedItems(t, res)), res.Conflicts)
	}
}

func TestMergeFieldByField(t *testing.T) {
	b := task("D-001", "a")
	b.Labels = []string{"x", "y"}
	b.Comments = []Comment{{Date: "2026-09-01", Body: "first"}}
	b.Fields = map[string]string{"phase": "1"}

	o := b
	o.Status, o.Done, o.ClosedAt, o.ClosedBy = StatusDone, "2026-09-28", "2026-09-28T10:00:00Z", "ours"
	o.Labels = []string{"x"} // removed y
	o.Comments = append(append([]Comment{}, b.Comments...), Comment{Date: "2026-09-28", Body: "ours"})

	th := b
	th.Title = "renamed"
	th.Labels = []string{"x", "y", "z"}
	th.Comments = append(append([]Comment{}, b.Comments...), Comment{Date: "2026-09-28", Body: "theirs"})
	th.Fields = map[string]string{"phase": "1", "track": "B"}

	res, err := Merge(encodeItems(t, b), encodeItems(t, o), encodeItems(t, th), MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) > 0 {
		t.Fatalf("conflicts: %v", res.Conflicts)
	}
	got := mergedItems(t, res)[0]
	if got.Status != StatusDone || got.ClosedBy != "ours" || got.Title != "renamed" {
		t.Fatalf("scalars: %+v", got)
	}
	if strings.Join(got.Labels, ",") != "x,z" {
		t.Fatalf("labels = %v", got.Labels)
	}
	if len(got.Comments) != 3 || got.Comments[1].Body != "ours" || got.Comments[2].Body != "theirs" {
		t.Fatalf("comments = %+v", got.Comments)
	}
	if got.Fields["track"] != "B" || got.Fields["phase"] != "1" {
		t.Fatalf("fields = %v", got.Fields)
	}
}

func TestMergeConflictKeepsOurs(t *testing.T) {
	b := task("D-001", "a")
	o := b
	o.Status = StatusInProgress
	o.Title = "ours"
	th := b
	th.Status, th.Done, th.ClosedAt, th.ClosedBy = StatusDone, "2026-09-28", "2026-09-28T10:00:00Z", "them"
	th.Title = "theirs"
	res, err := Merge(encodeItems(t, b), encodeItems(t, o), encodeItems(t, th), MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) != 2 {
		t.Fatalf("want a status and a title conflict, got %v", res.Conflicts)
	}
	got := mergedItems(t, res)[0]
	if got.Status != StatusInProgress || got.ClosedAt != "" || got.Title != "ours" {
		t.Fatalf("got %+v", got)
	}
	// Their side survives as a comment, so a blind `git add` loses nothing.
	if len(got.Comments) != 1 || got.Comments[0].Author != mergeAuthor ||
		!strings.Contains(got.Comments[0].Body, `"title":"theirs"`) || !strings.Contains(got.Comments[0].Body, `"status":"done"`) {
		t.Fatalf("comments = %+v", got.Comments)
	}
}

func TestMergeDeletions(t *testing.T) {
	a, b := task("D-001", "a"), task("D-002", "b")
	changed := b
	changed.Title = "b2"
	base := encodeItems(t, a, b)

	res, err := Merge(base, encodeItems(t, a, b), encodeItems(t, a), MergeOptions{})
	if err != nil || len(res.Conflicts) > 0 || ids(mergedItems(t, res)) != "D-001" {
		t.Fatalf("an unchanged record deleted by theirs goes: %v %v", err, res.Conflicts)
	}
	res, err = Merge(base, encodeItems(t, a, changed), encodeItems(t, a), MergeOptions{})
	if err != nil || len(res.Conflicts) != 1 || ids(mergedItems(t, res)) != "D-001,D-002" {
		t.Fatalf("a changed record deleted by theirs stays, flagged: %v %v", err, res.Conflicts)
	}
}

func TestMergeRefusesConflictMarkers(t *testing.T) {
	raw := string(encodeItems(t, task("D-001", "a"))) + "<<<<<<< ours\n"
	if _, err := Merge(nil, []byte(raw), nil, MergeOptions{}); err == nil || !strings.Contains(err.Error(), "unresolved merge conflict") {
		t.Fatalf("err = %v", err)
	}
}

func TestElsewhereIDs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	data := filepath.Join(repo, "docs", "tasks", "data")
	write := func(dir string, items ...Item) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "epic-1-demo.jsonl"), encodeItems(t, items...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run(repo, "init", "-q", "-b", "main")
	write(data, task("D-001", "a"))
	run(repo, "add", ".")
	run(repo, "commit", "-q", "-m", "one")
	run(repo, "checkout", "-q", "-b", "side")
	write(data, task("D-001", "a"), task("D-002", "side"))
	run(repo, "commit", "-q", "-am", "two")
	run(repo, "checkout", "-q", "main")
	wt := filepath.Join(t.TempDir(), "wt")
	run(repo, "worktree", "add", "-q", "-b", "agent", wt)
	write(filepath.Join(wt, "docs", "tasks", "data"), task("D-001", "a"), task("D-003", "uncommitted"))

	got, err := ElsewhereIDs(filepath.Join(repo, "docs", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"D-001", "D-002", "D-003"} {
		if !got[id] {
			t.Errorf("%s not seen; got %v", id, got)
		}
	}

	if got, err := ElsewhereIDs(t.TempDir()); err != nil || len(got) != 0 {
		t.Fatalf("outside a repository: %v %v", got, err)
	}

	// Allocation reads the other worktree's records, so both must take one lock.
	mainLock, err := lockPath(filepath.Join(repo, "docs", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	wtLock, err := lockPath(filepath.Join(wt, "docs", "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	if mainLock != wtLock {
		t.Fatalf("worktrees of one clone take different locks: %s and %s", mainLock, wtLock)
	}
	if other, _ := lockPath(filepath.Join(repo, "other")); other == mainLock {
		t.Fatal("a different tracker directory shares the lock")
	}
}

func TestEnsureAttributes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("*.go text"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := Attributes("docs/tasks")
	if wrote, err := EnsureAttributes(root, lines); err != nil || !wrote {
		t.Fatalf("first: %v %v", wrote, err)
	}
	if wrote, err := EnsureAttributes(root, lines); err != nil || wrote {
		t.Fatalf("second must be a no-op: %v %v", wrote, err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, ".gitattributes"))
	want := "*.go text\ndocs/tasks/data/*.jsonl merge=taskroll\ndocs/tasks/epics/*.md merge=taskroll-view\n"
	if string(raw) != want {
		t.Fatalf("got %q", raw)
	}
}

func TestMergeFlagsItemsLeftInAnArchivedEpic(t *testing.T) {
	a := task("D-001", "a")
	base := encodeItems(t, a)
	archived, err := Encode(&File{Epic: Item{ID: "epic-1-demo", Type: TypeEpic, Schema: SchemaVersion, Title: "Demo", Archived: "v0.2.0", ArchivedFile: "2026-Q3.jsonl"}})
	if err != nil {
		t.Fatal(err)
	}
	theirs := encodeItems(t, a, task("D-002", "late"))
	res, err := Merge(base, archived, theirs, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ids(mergedItems(t, res)) != "D-002" || len(res.Conflicts) != 1 || !strings.Contains(res.Conflicts[0], "archived") {
		t.Fatalf("got %s %v", ids(mergedItems(t, res)), res.Conflicts)
	}
	f, _ := Decode("epic-1-demo.jsonl", res.Data)
	if err := Check([]*File{f}); err == nil {
		t.Fatal("Check must refuse an archived epic that holds items")
	}
}

func encodeCollection(t *testing.T, items ...Item) []byte {
	t.Helper()
	raw, err := Encode(&File{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func debt(id, title string) Item {
	return Item{ID: id, Type: TypeDebt, Status: StatusTodo, Title: title}
}

func TestCollectionFiles(t *testing.T) {
	dir := t.TempDir()
	data := DataDir(dir)
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, raw []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(data, name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("epic-1-demo.jsonl", encodeItems(t, task("D-001", "a")))
	write("debt.jsonl", encodeCollection(t, debt("TD-001", "x")))
	files, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || !files[0].IsEpic() || files[1].IsEpic() || files[1].Items[0].ID != "TD-001" {
		t.Fatalf("epics first, then collections: %+v", files)
	}
	if _, _, ok := Find(files, "TD-001"); !ok {
		t.Fatal("Find must search collections")
	}
	if n := len(NewIndex(files, nil).Epics()); n != 1 {
		t.Fatalf("a collection is not an epic: %d epics", n)
	}

	write("watch.jsonl", encodeItems(t))
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "must be named") {
		t.Fatalf("an epic record in a collection file: %v", err)
	}
	_ = os.Remove(filepath.Join(data, "watch.jsonl"))
	write("epic-2-x.jsonl", encodeCollection(t, debt("TD-002", "y")))
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "not the epic") {
		t.Fatalf("an epic file without its epic: %v", err)
	}
}

func TestMergeCollection(t *testing.T) {
	a := debt("TD-001", "a")
	res, err := Merge(encodeCollection(t, a), encodeCollection(t, a, debt("TD-002", "ours")), encodeCollection(t, a, debt("TD-003", "theirs")), MergeOptions{})
	if err != nil || len(res.Conflicts) > 0 {
		t.Fatalf("%v %v", err, res.Conflicts)
	}
	f, err := Decode("debt.jsonl", res.Data)
	if err != nil || f.IsEpic() || ids(f.Items) != "TD-001,TD-002,TD-003" {
		t.Fatalf("%v %+v", err, f)
	}
}

func TestMergeRenamesASlugBothSidesAdded(t *testing.T) {
	w := func(id, title string) Item { return Item{ID: id, Type: TypeWatch, Status: StatusTodo, Title: title} }
	res, err := Merge(encodeCollection(t), encodeCollection(t, w("valkey-10", "ours")), encodeCollection(t, w("valkey-10", "theirs")), MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Renamed["valkey-10"] != "valkey-10-2" || len(res.Conflicts) != 1 {
		t.Fatalf("%+v", res)
	}
}

// Midway through a merge another records file can carry line-merge conflict
// markers; the driver must still reserve the ids in it.
func TestMergeReservedReadsAConflictedTree(t *testing.T) {
	dir := storeFixture(t).Dir
	conflicted := "<<<<<<< ours\n" + string(encodeItems(t, task("X-007", "ours"))) +
		"=======\n" + string(encodeItems(t, task("X-009", "theirs"))) + ">>>>>>> theirs\n"
	if err := os.WriteFile(filepath.Join(DataDir(dir), "debt.jsonl"), []byte(conflicted), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UsedIDs(); err == nil {
		t.Fatal("the fixture should not load")
	}
	got, err := p.MergeReserved()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"D-001", "X-007", "X-009"} {
		if !got[id] {
			t.Errorf("%s not reserved; got %v", id, got)
		}
	}
}
