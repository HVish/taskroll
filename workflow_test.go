package taskroll

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func storeFixture(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	f := &File{
		Path:  filepath.Join(DataDir(dir), "epic-1-demo.jsonl"),
		Epic:  Item{ID: "epic-1-demo", Type: TypeEpic, Schema: SchemaVersion, Title: "Demo"},
		Items: []Item{{ID: "D-001", Type: TypeTask, Status: StatusTodo, Title: "a"}},
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	return &Store{Dir: dir, Actor: "vs", Now: func() time.Time { return time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC) }}
}

func get(t *testing.T, s *Store, id string) Item {
	t.Helper()
	files, err := Load(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	f, i, ok := Find(files, id)
	if !ok {
		t.Fatalf("no %s", id)
	}
	return f.Items[i]
}

// Before the lock, six parallel comments kept two.
func TestConcurrentWritesAreNotLost(t *testing.T) {
	s := storeFixture(t)
	const n = 24
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// A separate Store per writer, as separate processes would have.
			w := *s
			_, err := w.Comment("D-001", fmt.Sprintf("comment %d", i))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := len(get(t, s, "D-001").Comments); got != n {
		t.Fatalf("%d of %d comments survived", got, n)
	}
}

func TestSetStatusStamps(t *testing.T) {
	s := storeFixture(t)
	it, err := s.SetStatus("d-001", StatusDone, "2026-09-20")
	if err != nil {
		t.Fatal(err)
	}
	if it.Done != "2026-09-20" || it.ClosedBy != "vs" || it.ClosedAt != "2026-09-28T10:00:00Z" {
		t.Fatalf("%+v", it)
	}
	if it, _ = s.SetStatus("D-001", StatusInProgress, ""); it.Done != "" || it.ClosedAt != "" {
		t.Fatalf("reopening keeps stamps: %+v", it)
	}
	if _, err := s.SetStatus("D-001", "blocked", ""); err == nil {
		t.Fatal("an unknown status must be refused")
	}
	if _, err := s.SetStatus("epic-1-demo", StatusDone, ""); err == nil {
		t.Fatal("an epic has no status of its own")
	}
}

func TestMutateChecksAcrossFilesBeforeSaving(t *testing.T) {
	s := storeFixture(t)
	dup := &File{Path: filepath.Join(DataDir(s.Dir), "debt.jsonl"), Items: []Item{{ID: "D-001", Type: TypeDebt, Status: StatusTodo, Title: "dup"}}}
	err := s.Mutate(func([]*File) ([]*File, error) { return []*File{dup}, nil })
	if err == nil {
		t.Fatal("a duplicate id across files must be refused")
	}
	if _, err := os.Stat(dup.Path); !os.IsNotExist(err) {
		t.Fatal("nothing may be written when the check fails")
	}
}

func TestRenderRunsInsideTheLock(t *testing.T) {
	s := storeFixture(t)
	s.Render = func() error {
		// The lock is held, so taking it again from here must wait; a
		// non-blocking probe shows it is taken.
		path, _ := lockPath(s.Dir)
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		if err := flockNB(f); err == nil {
			return fmt.Errorf("render ran without the lock")
		}
		return nil
	}
	if _, err := s.Comment("D-001", "x"); err != nil {
		t.Fatal(err)
	}
}

// WriteFileAtomic is general - it writes epic files as well as the archive -
// so the rename must not change the destination's mode. CreateTemp opens at
// 0600, and a fixed 0644 in its place would silently widen a file somebody had
// deliberately narrowed.
func TestAtomicWritePreservesDestinationMode(t *testing.T) {
	dir := t.TempDir()

	narrow := filepath.Join(dir, "narrow.md")
	if err := os.WriteFile(narrow, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(narrow, []byte("after\n")); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	fi, err := os.Stat(narrow)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("mode widened from 0600 to %04o", got)
	}

	// The 0600 regression this replaced: a new file must still be readable.
	fresh := filepath.Join(dir, "fresh.md")
	if err := WriteFileAtomic(fresh, []byte("new\n")); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	fi, err = os.Stat(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o644 {
		t.Errorf("a new file should be 0644, got %04o", got)
	}
}
