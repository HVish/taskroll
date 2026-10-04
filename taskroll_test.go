package taskroll

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

var md = Markdown{
	Fields:         []FieldSpec{{Key: "phase", Label: "Phase"}, {Key: "track", Label: "Track"}},
	Markers:        []MarkerSpec{{Label: "pilot", Marker: "Pilot"}},
	ArchivedMarker: "<!-- archived -->",
}

// The shapes the epic files carry, rendered from records.
func TestRenderLine(t *testing.T) {
	task := func(id, title string) Item { return Item{ID: id, Type: TypeTask, Status: StatusTodo, Title: title} }
	gate := task("A-003", "Gate.")
	gate.Blocker, gate.Size = true, "L"
	gate.Depends, gate.Closes = []string{"A-001", "a human decision"}, []string{"B-009"}
	gate.Fields, gate.Labels = map[string]string{"phase": "2", "track": "C"}, []string{"pilot"}
	bug := task("A-002", "Broken thing.")
	bug.Type, bug.Status, bug.Size, bug.Depends, bug.Done = TypeBug, StatusDone, "M", []string{"A-001"}, "2026-01-02"
	inText := task("A-004", "Gate · **[BLOCKER]** for all DB work.")
	inText.Blocker, inText.Size, inText.Fields = true, "M", map[string]string{"phase": "1"}
	split := task("A-006", "Split size.")
	split.Size, split.SizeNote, split.Notes = "S", " (alert) / `M` (throttle)", []string{"Was TD-011"}
	noStop := task("A-008", "Blocker without a stop")
	noStop.Blocker, noStop.Size = true, "S"
	for _, c := range []struct {
		it   Item
		want string
	}{
		{task("A-001", "Plain."), "- [ ] **A-001** - Plain."},
		{bug, "- [x] **A-002** `bug` - Broken thing. `M` · Depends: A-001 · Done: 2026-01-02"},
		{gate, "- [ ] **A-003** - Gate · **[BLOCKER]**. `L` · Depends: A-001, a human decision · Closes: B-009 · Phase: 2 · Track: C · Pilot"},
		{inText, "- [ ] **A-004** - Gate · **[BLOCKER]** for all DB work. `M` · Phase: 1"},
		{split, "- [ ] **A-006** - Split size. `S` (alert) / `M` (throttle) · Was TD-011"},
		{noStop, "- [ ] **A-008** - Blocker without a stop · **[BLOCKER]** `S`"},
	} {
		if got := md.RenderLine(c.it); got != c.want {
			t.Errorf("\n have %s\n want %s", got, c.want)
		}
	}
}

func TestRenderEpic(t *testing.T) {
	f := &File{
		Epic: Item{ID: "epic-3-reporting", Type: TypeEpic, Schema: SchemaVersion, Title: "Reporting", Intro: "\n> Sizing legend\n\nIntro.\n\n", Outro: "\n## Not in this epic\n"},
		Items: []Item{
			{ID: "RP-001", Type: TypeTask, Status: StatusTodo, Title: "One.", Size: "S"},
			{ID: "RP-002", Type: TypeTask, Status: StatusDropped, Title: "Gone."},
			{ID: "RP-003", Type: TypeTask, Status: StatusDone, Title: "Two.", Size: "M", Done: "2026-01-02"},
		},
	}
	want := "# EPIC 3: Reporting\n\n> Sizing legend\n\nIntro.\n\n- [ ] **RP-001** - One. `S`\n- [x] **RP-003** - Two. `M` · Done: 2026-01-02\n\n## Not in this epic\n"
	if got := md.RenderEpic(f); got != want {
		t.Fatalf("have %q\nwant %q", got, want)
	}
	f.Epic.Archived, f.Epic.ArchivedFile = "v0.2.0", "2026-Q3.jsonl"
	if got := md.RenderEpic(f); !strings.Contains(got, "<!-- archived -->") || strings.Contains(got, "RP-001") {
		t.Fatalf("tombstone: %q", got)
	}
}

func TestValidate(t *testing.T) {
	ok := Item{ID: "A-001", Type: TypeTask, Status: StatusTodo, Title: "x"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Item){
		"type":        func(it *Item) { it.Type = "story" },
		"status":      func(it *Item) { it.Status = "blocked" },
		"id":          func(it *Item) { it.ID = "A-1" },
		"size":        func(it *Item) { it.Size = "extra large" },
		"done date":   func(it *Item) { it.Done = "2026-01-02" },
		"label":       func(it *Item) { it.Labels = []string{"Pilot"} },
		"field":       func(it *Item) { it.Fields = map[string]string{"Phase": "1"} },
		"separator":   func(it *Item) { it.Notes = []string{"a · b"} },
		"title lines": func(it *Item) { it.Title = "a\nb" },
		"comment":     func(it *Item) { it.Comments = []Comment{{Date: "today", Body: "x"}} },
	} {
		it := ok
		change(&it)
		if it.Validate() == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func writeData(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(DataDir(dir), name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const epicRec = `{"id":"epic-1-x","type":"epic","schema":1,"title":"X"}` + "\n"

func TestLoadNamesFileAndLine(t *testing.T) {
	for name, body := range map[string]string{
		"unknown key":    epicRec + `{"id":"A-001","type":"task","status":"todo","title":"x","owner":"me"}` + "\n",
		"invalid":        epicRec + `{"id":"A-001","type":"task","status":"nope","title":"x"}` + "\n",
		"not json":       epicRec + "{\n",
		"epic not first": `{"id":"A-001","type":"task","status":"todo","title":"x"}` + "\n",
		"newer schema":   `{"id":"epic-1-x","type":"epic","schema":2,"title":"X"}` + "\n",
	} {
		dir := t.TempDir()
		writeData(t, dir, "epic-1-x.jsonl", body)
		_, err := Load(dir)
		if err == nil || !strings.Contains(err.Error(), "epic-1-x.jsonl:") {
			t.Errorf("%s: want a file:line error, got %v", name, err)
		}
	}
}

func TestEncodeIsCanonical(t *testing.T) {
	dir := t.TempDir()
	// Keys out of order, HTML-escaped and an empty list: all normalised.
	p := writeData(t, dir, "epic-1-x.jsonl", epicRec+`{"title":"a <b> & c","status":"todo","type":"task","id":"A-001","labels":[]}`+"\n")
	files, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	bad, _ := Unformatted(files)
	if len(bad) != 1 {
		t.Fatalf("want the file reported, got %v", bad)
	}
	if err := files[0].Save(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	want := epicRec + `{"id":"A-001","type":"task","status":"todo","title":"a <b> & c"}` + "\n"
	if string(raw) != want {
		t.Fatalf("have %s\nwant %s", raw, want)
	}
}

func TestCheckFindsDuplicatesAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	task := `{"id":"A-001","type":"task","status":"todo","title":"x"}` + "\n"
	writeData(t, dir, "epic-1-x.jsonl", epicRec+task)
	writeData(t, dir, "epic-2-y.jsonl", `{"id":"epic-2-y","type":"epic","schema":1,"title":"Y"}`+"\n"+task)
	files, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(files); err == nil || !strings.Contains(err.Error(), "A-001") {
		t.Fatalf("want a duplicate error, got %v", err)
	}
}

func TestQuery(t *testing.T) {
	f := &File{Epic: Item{ID: "epic-3-reporting", Type: TypeEpic, Schema: 1, Title: "A"}, Items: []Item{
		{ID: "A-001", Type: TypeTask, Status: StatusDone, Title: "done", Done: "2026-01-01"},
		{ID: "A-002", Type: TypeTask, Status: StatusTodo, Title: "ready", Depends: []string{"A-001", "TD-001"}, Labels: []string{"pilot"}},
		{ID: "A-003", Type: TypeBug, Status: StatusTodo, Title: "blocked", Depends: []string{"A-002"}, Fields: map[string]string{"phase": "2"}},
		{ID: "A-004", Type: TypeTask, Status: StatusTodo, Title: "waits on a person", Depends: []string{"a decision"}},
		{ID: "A-005", Type: TypeTask, Status: StatusTodo, Title: "unknown dep", Depends: []string{"A-999"}},
		{ID: "A-006", Type: TypeTask, Status: StatusInProgress, Title: "started"},
	}}
	x := NewIndex([]*File{f}, map[string]bool{"TD-001": true})
	ids := func(items []Item) []string {
		var out []string
		for _, it := range items {
			out = append(out, it.ID)
		}
		return out
	}
	for name, c := range map[string]struct {
		f    Filter
		want []string
	}{
		"open by default": {Filter{}, []string{"A-002", "A-003", "A-004", "A-005", "A-006"}},
		"all":             {Filter{All: true}, []string{"A-001", "A-002", "A-003", "A-004", "A-005", "A-006"}},
		"ready":           {Filter{Ready: true}, []string{"A-002"}},
		"blocked":         {Filter{Blocked: true}, []string{"A-003", "A-004", "A-005"}},
		"label":           {Filter{Labels: []string{"pilot"}}, []string{"A-002"}},
		"type":            {Filter{Types: []string{TypeBug}}, []string{"A-003"}},
		"field":           {Filter{Fields: map[string]string{"phase": "2"}}, []string{"A-003"}},
		"epic prefix":     {Filter{Epics: []string{"epic-3"}, Statuses: []string{StatusDone}}, []string{"A-001"}},
		"other epic":      {Filter{Epics: []string{"epic-30"}}, nil},
		"depends on":      {Filter{DependsOn: "a-002"}, []string{"A-003"}},
		"text":            {Filter{Text: "PERSON"}, []string{"A-004"}},
	} {
		if got := ids(x.List(c.f)); !slices.Equal(got, c.want) {
			t.Errorf("%s: have %v want %v", name, got, c.want)
		}
	}
	if d := x.Dependents("A-002"); !slices.Equal(d, []string{"A-003"}) {
		t.Errorf("dependents %v", d)
	}
	e := x.Epics()[0]
	if e.Open != 5 || e.Done != 1 || e.Ready != 1 || e.Blocked != 3 {
		t.Errorf("summary %+v", e)
	}
}

// The published schema and the Go struct describe one record; a field added
// to one and not the other is how a consumer silently drops data.
func TestSchemaMatchesStruct(t *testing.T) {
	raw, err := os.ReadFile("item.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	var tags []string
	rt := reflect.TypeFor[Item]()
	for i := range rt.NumField() {
		tags = append(tags, strings.Split(rt.Field(i).Tag.Get("json"), ",")[0])
	}
	var props []string
	for k := range schema.Properties {
		props = append(props, k)
	}
	slices.Sort(tags)
	slices.Sort(props)
	if !slices.Equal(tags, props) {
		t.Fatalf("struct fields %v\nschema properties %v", tags, props)
	}
}

func TestVelocity(t *testing.T) {
	f := &File{Epic: Item{ID: "epic-1-x", Type: TypeEpic, Schema: 1, Title: "X"}, Items: []Item{
		{ID: "A-001", Status: StatusDone, Size: "M", Done: "2026-09-21", CreatedAt: "2026-09-14T10:00:00Z", ClosedAt: "2026-09-21T10:00:00Z"},
		{ID: "A-002", Status: StatusDone, Size: "S", Done: "2026-09-27"},
		{ID: "A-003", Status: StatusTodo, CreatedAt: "2026-09-28T01:00:00Z"},
		{ID: "A-004", Status: StatusDropped, ClosedAt: "2026-09-28T02:00:00Z"},
		{ID: "A-005", Status: StatusDone, Size: "L", Done: "2026-01-01"},
	}}
	w := Velocity([]*File{f}, 3, time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC), map[string]float64{"S": 1, "M": 2.5, "L": 4.5})
	want := []Week{
		{Start: "2026-09-14", Created: 1},
		{Start: "2026-09-21", Done: 2, Points: 3.5},
		{Start: "2026-09-28", Created: 1, Dropped: 1},
	}
	if !slices.Equal(w, want) {
		t.Fatalf("have %+v\nwant %+v", w, want)
	}
	if c := CycleTimes([]*File{f}); !slices.Equal(c, []float64{7}) {
		t.Fatalf("cycle times %v", c)
	}
}
