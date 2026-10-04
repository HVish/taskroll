package taskroll

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A tracker with its own settings and none of the defaults: other sizes,
// a field with a pattern, a marker, and one unnumbered collection.
const ideasSettings = `{
  "sizes": [{"name": "XS", "points": 0.5}, {"name": "XL", "points": 8}],
  "epic": {
    "legend": "Sizes: XS or XL.",
    "fields": [{"key": "quarter", "label": "Quarter", "pattern": "^Q[1-4]$"}],
    "markers": [{"label": "urgent", "marker": "Urgent"}]
  },
  "collections": [
    {"dir": "ideas", "type": "opportunity", "keys": ["title", "type", "status", "source"], "required": ["source"],
     "values": {"source": ["customer", "team"]}}
  ]
}`

func projectFixture(t *testing.T, settings string) *Project {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(DataDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if settings != "" {
		if err := os.WriteFile(filepath.Join(dir, SettingsFile), []byte(settings), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := OpenProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	p.Now = func() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC) }
	return p
}

func TestProjectDrivenBySettings(t *testing.T) {
	p := projectFixture(t, ideasSettings)
	id, err := p.CreateEpic("ana", NewEpic{Slug: "growth", Title: "Growth"})
	if err != nil {
		t.Fatal(err)
	}
	it, err := p.AddTask("ana", NewTask{Epic: id, ID: "G-001", Title: "Referral link", Size: "XL",
		Fields: map[string]string{"quarter": "Q4"}, Labels: []string{"urgent"}})
	if err != nil {
		t.Fatal(err)
	}
	// No close_titles: the title is kept as given.
	if it.Title != "Referral link" || it.CreatedBy != "ana" {
		t.Fatalf("%+v", it)
	}
	raw, err := os.ReadFile(filepath.Join(p.Dir, "epics", id+".md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "# EPIC 0: Growth\n" + strings.ReplaceAll(DefaultBanner, "%s", id+".jsonl") + "\n\nSizes: XS or XL.\n\n- [ ] <a id=\"g-001\"></a>**G-001** - Referral link `XL` · Quarter: Q4 · Urgent\n"
	if string(raw) != want {
		t.Fatalf("got:\n%q\nwant:\n%q", raw, want)
	}

	for name, bad := range map[string]NewTask{
		"size not in the settings":  {Epic: id, Series: "G", Title: "x", Size: "M"},
		"field against its pattern": {Epic: id, Series: "G", Title: "x", Fields: map[string]string{"quarter": "Q9"}},
		"a size in the title":       {Epic: id, Series: "G", Title: "x `XS` y"},
	} {
		if _, err := p.AddTask("ana", bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := p.Update("ana", "G-001", func(it *Item) error { it.Size = "S"; return nil }); err == nil {
		t.Error("an edit to a size the settings do not define must be refused")
	}

	e, err := p.AddEntry("ana", NewEntry{Type: TypeOpportunity, Title: "Dark mode", Body: "Asked twice.", Fields: map[string]string{"source": "customer"}})
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "dark-mode" {
		t.Fatalf("an unnumbered collection names entries by slug: %s", e.ID)
	}
	entry, err := os.ReadFile(filepath.Join(p.Dir, "ideas", "dark-mode.md"))
	if err != nil || !strings.HasPrefix(string(entry), "---\ntitle: Dark mode\ntype: opportunity\nstatus: open\nsource: customer\n---\n# Dark mode\n") {
		t.Fatalf("entry file: %v\n%s", err, entry)
	}
	if _, err := p.AddEntry("ana", NewEntry{Type: TypeOpportunity, Title: "Other", Body: "b", Fields: map[string]string{"source": "rumour"}}); err == nil {
		t.Error("a value outside the collection's vocabulary must be refused")
	}
	if _, err := p.AddEntry("ana", NewEntry{Type: TypeDebt, Title: "Debt", Body: "b"}); err == nil {
		t.Error("a type with no collection must be refused")
	}
	if pts := p.Settings.Points(); pts["XL"] != 8 {
		t.Fatalf("points %v", pts)
	}
}

func TestDefaultSettings(t *testing.T) {
	p := projectFixture(t, "")
	if len(p.Settings.Sizes) != 3 || p.Settings.Banner != DefaultBanner || len(p.Settings.Collections) != 0 {
		t.Fatalf("%+v", p.Settings)
	}
}

func TestSettingsAreValidated(t *testing.T) {
	for name, bad := range map[string]string{
		"unknown key":          `{"epic": {}, "colour": "blue"}`,
		"banner without %s":    `{"banner": "generated", "epic": {}}`,
		"duplicate size":       `{"sizes": [{"name": "S"}, {"name": "S"}], "epic": {}}`,
		"bad pattern":          `{"epic": {"fields": [{"key": "q", "label": "Q", "pattern": "("}]}}`,
		"collection of tasks":  `{"epic": {}, "collections": [{"dir": "x", "type": "task", "keys": ["title", "type", "status"]}]}`,
		"numbered without id":  `{"epic": {}, "collections": [{"dir": "x", "type": "debt", "series": "X", "keys": ["title", "type", "status"]}]}`,
		"required not a field": `{"epic": {}, "collections": [{"dir": "x", "type": "debt", "keys": ["title", "type", "status"], "required": ["title"]}]}`,
		"reserved dir":         `{"epic": {}, "collections": [{"dir": "epics", "type": "debt", "keys": ["title", "type", "status"]}]}`,
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, SettingsFile), []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSettings(dir); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestBrowseURL(t *testing.T) {
	for _, bad := range []string{"ftp://example.com/tasks", "docs/tasks", "https://example.com/tasks?ref=main", "https:///tasks"} {
		if err := (Settings{Banner: DefaultBanner, BrowseURL: bad}).Validate(); err == nil {
			t.Errorf("browse_url %q accepted", bad)
		}
	}
	if err := (Settings{Banner: DefaultBanner, BrowseURL: "https://github.com/o/r/blob/main/docs/tasks/"}).Validate(); err != nil {
		t.Error(err)
	}
	p := &Project{Settings: Settings{BrowseURL: "https://example.com/r/docs/tasks/"}}
	x := NewIndex([]*File{{Epic: Item{ID: "epic-0-pay", Type: TypeEpic}, Items: []Item{{ID: "PAY-012.1", Type: TypeTask}}}}, nil)
	it, _ := x.Get("PAY-012.1")
	if got := p.URL(x, it); got != "https://example.com/r/docs/tasks/epics/epic-0-pay.md#pay-012.1" {
		t.Errorf("url: %s", got)
	}
	if got := (&Project{}).URL(x, it); got != "" {
		t.Errorf("no browse_url, no link: %s", got)
	}
}
