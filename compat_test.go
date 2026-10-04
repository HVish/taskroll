package taskroll

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The promise FormatVersion makes: a tracker written in format 1 renders
// byte for byte as format 1 always has, so upgrading taskroll leaves it
// untouched. testdata/format-1 is frozen. Never regenerate it: a change that
// would alter it is a new format, with its own fixture beside this one.
func TestFormat1RendersUnchanged(t *testing.T) {
	src := filepath.Join("testdata", "format-1")
	dir := t.TempDir()
	want := map[string][]byte{}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		want[rel] = raw
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, rel), raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Settings.Format != 1 {
		t.Fatalf("a settings file without a format is format 1, got %d", p.Settings.Format)
	}
	// Every write re-encodes the whole file, so the records must encode
	// back to the bytes they were read from, or the next write to a file
	// rewrites every line of it.
	for rel, raw := range want {
		if !strings.HasSuffix(rel, ".jsonl") {
			continue
		}
		f, err := Decode(rel, raw)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Encode(f)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(raw) {
			t.Errorf("%s: format 1 records encode differently\ngot:\n%s\nwant:\n%s", rel, got, raw)
		}
	}
	// Delete the generated files first, so a view that renders differently
	// and one that is not rendered at all both show up. README.md is written
	// once by init and never rendered, so it stays.
	for rel := range want {
		if strings.HasSuffix(rel, ".md") && rel != "README.md" {
			if err := os.Remove(filepath.Join(dir, rel)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := p.RenderAll(); err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		got, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if w, ok := want[rel]; !ok {
			t.Errorf("%s: rendered, but format 1 has no such file", rel)
		} else if string(got) != string(w) {
			t.Errorf("%s: format 1 output changed\ngot:\n%s\nwant:\n%s", rel, got, w)
		}
		delete(want, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel := range want {
		t.Errorf("%s: format 1 renders it, this build did not", rel)
	}
}

// The format gates the whole tracker: only epic records carry a schema, so
// an old binary sees a newer record schema in a collection only if the format
// in taskroll.json moved with it. Each format names the record schema it
// writes; bumping SchemaVersion without a new format fails here.
func TestEachRecordSchemaHasItsOwnFormat(t *testing.T) {
	schemaOf := map[int]int{1: 1}
	if schemaOf[FormatVersion] != SchemaVersion {
		t.Fatalf("format %d writes record schema %d, but SchemaVersion is %d: a new record schema is a new format", FormatVersion, schemaOf[FormatVersion], SchemaVersion)
	}
	for f := 2; f <= FormatVersion; f++ {
		if schemaOf[f] < schemaOf[f-1] {
			t.Fatalf("format %d writes record schema %d, older than format %d's", f, schemaOf[f], f-1)
		}
	}
	for f := 1; f <= FormatVersion; f++ {
		if _, err := os.Stat(filepath.Join("testdata", fmt.Sprintf("format-%d", f), SettingsFile)); err != nil {
			t.Errorf("format %d has no frozen fixture: %v", f, err)
		}
	}
}

func TestNewerFormatAsksForAnUpgrade(t *testing.T) {
	dir := t.TempDir()
	// The newer format also adds a key: the format is what gets reported.
	if err := os.WriteFile(filepath.Join(dir, SettingsFile), []byte(`{"format": 2, "colour": "teal"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSettings(dir)
	var newer *NewerError
	if !errors.As(err, &newer) || newer.Have != 2 || newer.What != "format" {
		t.Fatalf("want a NewerError for format 2, got %v", err)
	}
	if !strings.Contains(err.Error(), UpgradeCommand) {
		t.Fatalf("the error must say how to upgrade: %v", err)
	}
}

func TestNewerRecordSchemaAsksForAnUpgrade(t *testing.T) {
	_, err := Decode("epic-0-x.jsonl", []byte(`{"id":"epic-0-x","type":"epic","schema":2,"title":"X","owner":"someone"}`+"\n"))
	var newer *NewerError
	if !errors.As(err, &newer) || newer.Have != 2 || newer.What != "record schema" {
		t.Fatalf("want a NewerError for schema 2, got %v", err)
	}
}

func TestUnknownKeySuggestsAnUpgrade(t *testing.T) {
	_, err := Decode("epic-0-x.jsonl", []byte(`{"id":"epic-0-x","type":"epic","schema":1,"title":"X","owner":"someone"}`+"\n"))
	if err == nil || !strings.Contains(err.Error(), `unknown field "owner"`) || !strings.Contains(err.Error(), UpgradeCommand) {
		t.Fatalf("got %v", err)
	}
}
