package taskroll

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The promise FormatMajor makes: a tracker written in format 1 renders
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
	if p.Settings.Version() != (Format{1, 0}) {
		t.Fatalf("a settings file without a format is format 1.0, got %s", p.Settings.Version())
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
// in taskroll.json moved with it. Each major names the record schema it
// writes; bumping SchemaVersion without a new major fails here (a minor only
// adds settings, so it cannot carry a record change).
func TestEachRecordSchemaHasItsOwnFormat(t *testing.T) {
	schemaOf := map[int]int{1: 1}
	if schemaOf[FormatMajor] != SchemaVersion {
		t.Fatalf("format %d writes record schema %d, but SchemaVersion is %d: a new record schema is a new format", FormatMajor, schemaOf[FormatMajor], SchemaVersion)
	}
	for f := 2; f <= FormatMajor; f++ {
		if schemaOf[f] < schemaOf[f-1] {
			t.Fatalf("format %d writes record schema %d, older than format %d's", f, schemaOf[f], f-1)
		}
	}
	for f := 1; f <= FormatMajor; f++ {
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

func TestFormatJSON(t *testing.T) {
	for in, want := range map[string]Format{`1`: {1, 0}, `"1"`: {1, 0}, `"1.0"`: {1, 0}, `"1.2"`: {1, 2}, `"2.10"`: {2, 10}} {
		var f Format
		if err := json.Unmarshal([]byte(in), &f); err != nil || f != want {
			t.Errorf("%s: have %v %v, want %v", in, f, err, want)
		}
	}
	// A bare 1.10 is the number 1.1, so a minor must be quoted.
	for _, in := range []string{`1.2`, `0`, `"0.1"`, `"1.02"`, `"+1"`, `"1.2.3"`, `"1."`, `true`, `-1`} {
		var f Format
		if err := json.Unmarshal([]byte(in), &f); err == nil {
			t.Errorf("%s: want an error, got %v", in, f)
		}
	}
}

func writeSettings(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, SettingsFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A newer minor of this build's major only adds optional settings, so the
// file is read with the keys this build does not know ignored, wherever
// they sit. Its own minor and older ones stay strict, so a typo is caught.
func TestNewerMinorIsReadWithoutItsKeys(t *testing.T) {
	newer := Format{FormatMajor, FormatMinor + 1}
	dir := writeSettings(t, `{"format": "`+newer.String()+`", "colour": "teal", "epic": {"legend": "> L", "badge": true}}`)
	s, err := LoadSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Version() != newer || !s.Version().NewerMinor() || s.Epic.Legend != "> L" {
		t.Fatalf("have %+v", s)
	}
	for _, f := range []string{`1`, `"` + CurrentFormat.String() + `"`} {
		_, err := LoadSettings(writeSettings(t, `{"format": `+f+`, "colour": "teal"}`))
		if err == nil || !strings.Contains(err.Error(), `unknown field "colour"`) {
			t.Errorf("format %s: want the unknown key refused, got %v", f, err)
		}
	}
}

// A key added in a minor must come with that minor declared, so a build
// that predates it reads the file and warns rather than refusing the key.
func TestSettingsKeyNeedsItsFormat(t *testing.T) {
	for i, k := range minorSettings {
		if k.Minor < 1 || k.Minor > FormatMinor || i > 0 && k.Minor < minorSettings[i-1].Minor || k.Help == "" {
			t.Errorf("%s: added in minor %d, out of order, beyond this build, or without help", k.Path, k.Minor)
		}
	}
	for _, f := range []string{``, `"format": 1, `, `"format": "1.0", `} {
		_, err := LoadSettings(writeSettings(t, `{`+f+`"fiscal_year_start": 4}`))
		if err == nil || !strings.Contains(err.Error(), `fiscal_year_start needs "format": "1.1"`) {
			t.Errorf("%q: want the key to ask for its format, got %v", f, err)
		}
	}
	s, err := LoadSettings(writeSettings(t, `{"format": "1.1", "fiscal_year_start": 4}`))
	if err != nil || s.FiscalYearStart != 4 || s.Version() != (Format{1, 1}) {
		t.Fatalf("have %+v %v", s, err)
	}
	if _, err := LoadSettings(writeSettings(t, `{"format": "1.1", "fiscal_year_start": 13}`)); err == nil {
		t.Error("month 13: want an error")
	}
	// A file without the key needs no particular format.
	if _, err := LoadSettings(writeSettings(t, `{"epic": {"legend": "> L"}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestNoDuplicateKeys(t *testing.T) {
	for _, ok := range []string{
		`{"a":1,"b":{"a":2},"c":[{"a":1},{"a":2}]}`,
		`{"comments":[{"date":"2026-01-01","body":"x"},{"date":"2026-01-02","body":"y"}]}`,
		`{"a":"\"a\":1, {\"a\":2}","b":"}{,"}`,
		`{"a":["a","a"],"b":"a"}`,
		`{}`, `[]`, `{"a":`,
	} {
		if err := noDuplicateKeys([]byte(ok)); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{
		`{"a":1,"a":2}`,
		`{"x":{"q":"Q3","q":"Q4"}}`,
		`{"c":[{"date":"d","date":"e"}]}`,
		`{"a":"}","b":{"z":1},"a":3}`,
		`{"a\"b":1,"a\"b":2}`,
	} {
		if err := noDuplicateKeys([]byte(bad)); err == nil || !strings.Contains(err.Error(), "appears twice") {
			t.Errorf("%s: want a refusal, got %v", bad, err)
		}
	}
}

// A doubled key in a record or the settings is refused where it is read,
// naming the line, rather than resolved to whichever came last.
func TestDuplicateKeysAreRefused(t *testing.T) {
	_, err := Decode("epic-0-x.jsonl", []byte(epicRec+`{"id":"A-001","type":"task","status":"todo","status":"done","title":"x","done":"2026-01-01"}`+"\n"))
	if err == nil || !strings.Contains(err.Error(), `epic-0-x.jsonl:2: key "status" appears twice`) {
		t.Errorf("record: %v", err)
	}
	_, err = LoadSettings(writeSettings(t, `{"banner": "<!-- %s -->", "banner": "<!-- %s, again -->"}`))
	if err == nil || !strings.Contains(err.Error(), `key "banner" appears twice`) {
		t.Errorf("settings: %v", err)
	}
}
