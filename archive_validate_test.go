package taskroll

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archiveWith(t *testing.T, dir, name string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "archive", name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const goodRec = `{"id":"TD-039","kind":"debt","title":"t","archived_at":"v0.1.5","archived_on":"2026-08-23","source":"debt/TD-039.md","meta":{},"body":"b"}`

func problemsFor(t *testing.T, lines ...string) []ArchiveProblem {
	t.Helper()
	dir := t.TempDir()
	archiveWith(t, dir, "2026-Q3.jsonl", lines...)
	got, err := ValidateArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestValidateArchiveAcceptsGoodRecord(t *testing.T) {
	if got := problemsFor(t, goodRec); len(got) != 0 {
		t.Fatalf("valid record rejected: %v", got)
	}
}

func TestValidateArchiveRejectsMalformedJSON(t *testing.T) {
	got := problemsFor(t, goodRec, `{"id":"TD-040",`)
	if len(got) != 1 || !strings.Contains(got[0].Msg, "not valid JSON") {
		t.Fatalf("want a JSON error on line 2, got %v", got)
	}
	if got[0].Line != 2 {
		t.Errorf("line = %d, want 2 - the location is the point", got[0].Line)
	}
}

func TestValidateArchiveRequiresFields(t *testing.T) {
	got := problemsFor(t, `{"id":"TD-041","kind":"debt","archived_on":"2026-08-23"}`)
	var missing []string
	for _, p := range got {
		if strings.Contains(p.Msg, "missing required field") {
			missing = append(missing, p.Msg)
		}
	}
	if len(missing) != 2 { // archived_at and source
		t.Fatalf("want 2 missing-field problems, got %v", got)
	}
}

func TestValidateArchiveRejectsUnknownKindAndBadDate(t *testing.T) {
	got := problemsFor(t,
		`{"id":"A","kind":"nonsense","archived_at":"v1","archived_on":"2026-08-23","source":"s"}`,
		`{"id":"B","kind":"debt","archived_at":"v1","archived_on":"23-08-2026","source":"s"}`)
	var kinds, dates int
	for _, p := range got {
		if strings.Contains(p.Msg, "unknown kind") {
			kinds++
		}
		if strings.Contains(p.Msg, "not YYYY-MM-DD") {
			dates++
		}
	}
	if kinds != 1 || dates != 1 {
		t.Fatalf("want one kind and one date problem, got %v", got)
	}
}

// A record in the wrong quarter file is invisible to a lookup by quarter.
func TestValidateArchiveRejectsWrongQuarterFile(t *testing.T) {
	got := problemsFor(t,
		`{"id":"A","kind":"debt","archived_at":"v1","archived_on":"2026-02-14","source":"s"}`)
	if len(got) != 1 || !strings.Contains(got[0].Msg, "belongs in 2026-Q1.jsonl") {
		t.Fatalf("want a quarter mismatch, got %v", got)
	}
}

// The same ID archived twice means a sweep ran against an already-swept source.
func TestValidateArchiveRejectsDuplicateID(t *testing.T) {
	got := problemsFor(t, goodRec, goodRec)
	if len(got) != 1 || !strings.Contains(got[0].Msg, "duplicate record for TD-039") {
		t.Fatalf("want a duplicate report, got %v", got)
	}
	if !strings.Contains(got[0].Msg, "2026-Q3.jsonl:1") {
		t.Errorf("duplicate should name where the first one is: %v", got[0])
	}
}

func TestValidateArchiveRejectsBadFilename(t *testing.T) {
	dir := t.TempDir()
	archiveWith(t, dir, "notes.jsonl", goodRec)
	got, err := ValidateArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || !strings.Contains(got[0].Msg, "not YYYY-QN.jsonl") {
		t.Fatalf("want a filename problem, got %v", got)
	}
}
