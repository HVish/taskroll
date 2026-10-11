package taskroll

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The edit changes the format's bytes and no others, however the file is
// laid out.
func TestSetFormatChangesOnlyTheFormat(t *testing.T) {
	to := Format{1, 1}
	for _, c := range []struct{ in, want string }{
		{"{\n  \"format\": 1,\n  \"banner\": \"x\"\n}\n", "{\n  \"format\": \"1.1\",\n  \"banner\": \"x\"\n}\n"},
		{"{\n  \"banner\": \"x\",\n  \"format\" :  1\n}", "{\n  \"banner\": \"x\",\n  \"format\" :  \"1.1\"\n}"},
		{"{\n    \"banner\": \"x\"\n}\n", "{\n    \"format\": \"1.1\",\n    \"banner\": \"x\"\n}\n"},
		{"{\"banner\":\"x\",\"z\":{\"format\":9}}", "{\"format\": \"1.1\",\"banner\":\"x\",\"z\":{\"format\":9}}"},
		{"{}", "{\"format\": \"1.1\"}"},
		{"{ }\n", "{\"format\": \"1.1\" }\n"},
		{"{\"format\":\"1.0\"}", "{\"format\":\"1.1\"}"},
		{"{\t\"a\\u0062\": 1}", "{\t\"format\": \"1.1\",\t\"a\\u0062\": 1}"},
	} {
		got, err := setFormat([]byte(c.in), to)
		if err != nil || string(got) != c.want {
			t.Errorf("%q:\nhave %q %v\nwant %q", c.in, got, err, c.want)
		}
	}
	// Go reads the last of two keys; changing the first would change nothing.
	if _, err := setFormat([]byte(`{"format": 1, "format": 1}`), to); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Errorf("a duplicated format: %v", err)
	}
	for _, bad := range []string{`[]`, `"x"`, `{"format":`} {
		if _, err := setFormat([]byte(bad), to); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestUpgradeFormat(t *testing.T) {
	const before = "{\n  \"browse_url\": \"https://example.com/r/docs/tasks\",\n  \"epic\": {\"legend\": \"> L\"}\n}\n"
	dir := writeSettings(t, before)
	path := filepath.Join(dir, SettingsFile)

	res, err := UpgradeFormat(dir, CurrentFormat, true)
	if err != nil || res.From != (Format{1, 0}) || res.To != CurrentFormat {
		t.Fatalf("dry run: %+v %v", res, err)
	}
	if readFile(t, path) != before {
		t.Fatal("a dry run wrote the file")
	}

	res, err = UpgradeFormat(dir, Format{1, 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []SettingHelp{{"fiscal_year_start", minorSettings[0].Help}}; !slices.Equal(res.New, want) {
		t.Errorf("new settings: %v", res.New)
	}
	after := readFile(t, path)
	if after != strings.Replace(before, "{\n", "{\n  \"format\": \"1.1\",\n", 1) {
		t.Fatalf("the upgrade changed more than the format:\n%s", after)
	}
	s, err := LoadSettings(dir)
	if err != nil || s.Version() != (Format{1, 1}) {
		t.Fatalf("upgraded settings: %+v %v", s, err)
	}

	// Again, it has nothing to do; back, it refuses.
	if res, err := UpgradeFormat(dir, Format{1, 1}, false); err != nil || res.From != res.To || readFile(t, path) != after {
		t.Errorf("a second upgrade: %+v %v", res, err)
	}
	for _, to := range []Format{{1, 0}, {2, 0}, {1, FormatMinor + 1}} {
		if _, err := UpgradeFormat(dir, to, false); err == nil || readFile(t, path) != after {
			t.Errorf("to %s: want a refusal and the file untouched, got %v", to, err)
		}
	}
}

// A symlinked settings file is upgraded where it lives, and stays a link.
func TestUpgradeFollowsASymlink(t *testing.T) {
	real := filepath.Join(t.TempDir(), "shared-settings.json")
	if err := os.WriteFile(real, []byte("{\n  \"banner\": \""+strings.ReplaceAll(DefaultBanner, `"`, `\"`)+"\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	link := filepath.Join(dir, SettingsFile)
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := UpgradeFormat(dir, Format{1, 1}, false); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v", err)
	}
	if !strings.Contains(readFile(t, real), `"format": "1.1"`) {
		t.Fatalf("the target was not upgraded:\n%s", readFile(t, real))
	}
}

func TestReadersOf(t *testing.T) {
	for f, want := range map[Format]string{{1, 0}: "v0.1.1 and later; v0.1.0 cannot", {1, 1}: "v0.1.2 and later"} {
		if got := ReadersOf(f); !strings.Contains(got, want) {
			t.Errorf("%s: %q", f, got)
		}
	}
}

// A tracker on a newer minor than this build is not this build's to move.
func TestUpgradeRefusesANewerTracker(t *testing.T) {
	newer := Format{FormatMajor, FormatMinor + 1}.String()
	body := `{"format": "` + newer + `", "colour": "teal"}`
	dir := writeSettings(t, body)
	_, err := UpgradeFormat(dir, CurrentFormat, false)
	if err == nil || !strings.Contains(err.Error(), "newer than this build") {
		t.Fatalf("want a refusal, got %v", err)
	}
	if readFile(t, filepath.Join(dir, SettingsFile)) != body {
		t.Fatal("the refused upgrade wrote the file")
	}
}

// A tracker with no settings file gets one with every key at its default.
func TestUpgradeWritesAMissingSettingsFile(t *testing.T) {
	dir := t.TempDir()
	res, err := UpgradeFormat(dir, CurrentFormat, false)
	if err != nil || !res.Created {
		t.Fatalf("%+v %v", res, err)
	}
	want, err := DefaultSettingsFile(CurrentFormat)
	if err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(dir, SettingsFile)) != string(want)+"\n" {
		t.Fatal("the written file is not the default one")
	}
	if s, err := LoadSettings(dir); err != nil || s.Version() != CurrentFormat {
		t.Fatalf("%+v %v", s, err)
	}
}

// A minor changes no generated file, so the frozen format-1 tracker renders
// byte for byte the same once upgraded.
func TestUpgradedFixtureRendersUnchanged(t *testing.T) {
	src := filepath.Join("testdata", "format-1")
	dir := t.TempDir()
	want := map[string]string{}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		want[rel] = string(raw)
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, rel), raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpgradeFormat(dir, CurrentFormat, false); err != nil {
		t.Fatal(err)
	}
	p, err := OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.RenderAll(); err != nil {
		t.Fatal(err)
	}
	for rel, w := range want {
		if rel == SettingsFile {
			continue
		}
		if got := readFile(t, filepath.Join(dir, rel)); got != w {
			t.Errorf("%s: rendered differently after the upgrade", rel)
		}
	}
}

// init writes every key, so a key added to Settings must be added to the
// file init writes too, or it would be the one knob nobody sees.
func TestSettingsFileHasEveryKey(t *testing.T) {
	raw, err := DefaultSettingsFile(CurrentFormat)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	keys := func(v any) []string {
		var out []string
		for f := range reflect.TypeOf(v).Fields() {
			if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); name != "-" && name != "" {
				out = append(out, name)
			}
		}
		return out
	}
	for _, k := range keys(Settings{}) {
		if !hasPath(doc, []string{k}) {
			t.Errorf("init does not write %s", k)
		}
	}
	for _, k := range keys(EpicSpec{}) {
		if !hasPath(doc, []string{"epic", k}) {
			t.Errorf("init does not write epic.%s", k)
		}
	}

	// It reads back as the defaults, and an older format leaves out what it
	// cannot carry.
	dir := writeSettings(t, string(raw))
	s, err := LoadSettings(dir)
	def := DefaultSettings()
	def.FormatMinor, def.FiscalYearStart, def.Collections = FormatMinor, 1, []CollectionSpec{}
	def.Epic.Fields, def.Epic.Markers = []FieldSpec{}, []MarkerSpec{}
	if err != nil || !reflect.DeepEqual(s, def) {
		t.Errorf("the default file reads as\n%+v %v\nwant\n%+v", s, err, def)
	}
	old, err := DefaultSettingsFile(Format{1, 0})
	if err != nil || strings.Contains(string(old), "fiscal_year_start") || !strings.Contains(string(old), `"format": 1,`) {
		t.Errorf("format 1.0 defaults: %s %v", old, err)
	}
	if _, err := LoadSettings(writeSettings(t, string(old))); err != nil {
		t.Error(err)
	}
}

func TestSettingEntries(t *testing.T) {
	dir := writeSettings(t, `{"browse_url": "https://example.com/r/docs/tasks", "epic": {"close_titles": true}}`)
	entries, f, err := SettingEntries(dir)
	if err != nil || f != (Format{1, 0}) {
		t.Fatal(f, err)
	}
	got := map[string]SettingEntry{}
	var order []string
	for _, e := range entries {
		got[e.Key] = e
		order = append(order, e.Key)
	}
	want := []string{"banner", "browse_url", "sizes", "fiscal_year_start", "epic.legend", "epic.fields", "epic.markers", "epic.close_titles", "collections"}
	if !slices.Equal(order, want) {
		t.Fatalf("keys %v", order)
	}
	if !got["browse_url"].Set || !got["epic.close_titles"].Set || got["banner"].Set || string(got["epic.close_titles"].Value) != "true" {
		t.Errorf("set flags or values: %+v", entries)
	}
	if got["browse_url"].Default || got["epic.close_titles"].Default || !got["banner"].Default || !got["sizes"].Default {
		t.Errorf("default flags: %+v", entries)
	}
	if fy := got["fiscal_year_start"]; fy.Set || fy.Needs == nil || *fy.Needs != (Format{1, 1}) || string(fy.Value) != "1" {
		t.Errorf("fiscal_year_start: %+v", fy)
	}
	if !strings.Contains(string(got["banner"].Value), "<!--") {
		t.Errorf("banner is HTML-escaped: %s", got["banner"].Value)
	}
}

// init's file sets every key to its default, and config show still reports
// each as the default, not as a project's choice.
func TestSettingEntriesOfInitsFile(t *testing.T) {
	raw, err := DefaultSettingsFile(CurrentFormat)
	if err != nil {
		t.Fatal(err)
	}
	entries, _, err := SettingEntries(writeSettings(t, string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.Set || !e.Default || e.Needs != nil {
			t.Errorf("%+v", e)
		}
	}
}
