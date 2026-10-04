package taskroll

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// NewTask is a task to append to an epic.
type NewTask struct {
	Epic    string            `json:"epic"`             // epic id, with or without the .md suffix
	ID      string            `json:"id,omitempty"`     // when empty, allocated from Series
	Series  string            `json:"series,omitempty"` // id prefix to allocate from, e.g. "R"
	Title   string            `json:"title"`
	Size    string            `json:"size,omitempty"`
	Depends []string          `json:"depends,omitempty"`
	Closes  []string          `json:"closes,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"` // the epic grammar's fields, e.g. phase
	Labels  []string          `json:"labels,omitempty"`
	Bug     bool              `json:"bug,omitempty"`
	Blocker bool              `json:"blocker,omitempty"`
}

var endsClosed = regexp.MustCompile(`[.?!)\x60]$`)

// TaskTitle is a task's title as its line carries it: when the settings
// close titles, with a full stop unless it already ends in punctuation that
// closes it.
func (p *Project) TaskTitle(title string) string {
	title = strings.TrimSpace(title)
	if p.Settings.Epic.CloseTitles && !endsClosed.MatchString(title) {
		title += "."
	}
	return title
}

// CheckLineText refuses text that would read as a field once written into a
// task line: a '·' starts a new field and a backticked size is the size, so
// a reader of the epic file would see fields nobody set.
func (p *Project) CheckLineText(what, s string) error {
	if strings.ContainsAny(s, "\n\r") {
		return fmt.Errorf("%s %q has a newline in it; a task line is one line", what, s)
	}
	if strings.Contains(s, "·") {
		return fmt.Errorf("%s %q cannot contain '·'; it separates the task line's fields", what, s)
	}
	for _, z := range p.Settings.Sizes {
		if strings.Contains(s, "`"+z.Name+"`") {
			return fmt.Errorf("%s %q cannot contain `%s`; that is the size field", what, s, z.Name)
		}
	}
	return nil
}

// CheckField refuses a value an epic field's pattern does not allow.
func (p *Project) CheckField(key, v string) error {
	for _, f := range p.Settings.Epic.Fields {
		if f.Key == key && f.Pattern != "" && !regexp.MustCompile(f.Pattern).MatchString(v) {
			return fmt.Errorf("%s %q does not match %s", f.Label, v, f.Pattern)
		}
	}
	return p.CheckLineText(key, v)
}

func (p *Project) validateTask(t NewTask) error {
	if strings.TrimSpace(t.Title) == "" {
		return fmt.Errorf("a task needs a title")
	}
	if err := p.CheckLineText("title", t.Title); err != nil {
		return err
	}
	for _, group := range []struct {
		what    string
		entries []string
	}{{"depends entry", t.Depends}, {"closes entry", t.Closes}} {
		for _, e := range group.entries {
			if err := p.CheckLineText(group.what, e); err != nil {
				return err
			}
		}
	}
	if t.Size != "" {
		if _, ok := p.Settings.Size(t.Size); !ok {
			return fmt.Errorf("size %q is not one of %s", t.Size, p.sizeNames())
		}
	}
	for k, v := range t.Fields {
		if err := p.CheckField(k, v); err != nil {
			return err
		}
	}
	return nil
}

// findEpic returns an epic's file by id, refusing a tombstoned one.
func findEpic(files []*File, epic string) (*File, error) {
	id := strings.TrimSuffix(epic, ".md")
	var names []string
	for _, f := range files {
		if !f.IsEpic() {
			continue
		}
		if f.Epic.ID == id {
			if f.Epic.Archived != "" {
				return nil, fmt.Errorf("epic %q is archived; it takes no new tasks", epic)
			}
			return f, nil
		}
		names = append(names, f.Epic.ID)
	}
	sort.Strings(names)
	return nil, fmt.Errorf("no epic %q; have: %s", epic, strings.Join(names, ", "))
}

// AddTask validates a task, allocates its id and appends its record to the
// epic, all under the lock, and returns the record. Allocation happens inside
// the lock: two parallel adds would otherwise read the same highest number
// and issue it twice.
func (p *Project) AddTask(actor string, t NewTask) (Item, error) {
	if err := p.validateTask(t); err != nil {
		return Item{}, err
	}
	if !p.Initialised() {
		return Item{}, ErrNotInitialised
	}
	// Ids are compared as strings everywhere downstream, all spelled
	// upper-case; a lower-case one would slip past the duplicate check here
	// and then fail to join there.
	t.ID = strings.ToUpper(t.ID)
	t.Series = strings.ToUpper(t.Series)
	if t.ID == "" && t.Series == "" {
		return Item{}, fmt.Errorf("give either an id or a series to allocate from")
	}

	var it Item
	s := p.Store(actor)
	err := s.Mutate(func(files []*File) ([]*File, error) {
		f, err := findEpic(files, t.Epic)
		if err != nil {
			return nil, err
		}
		allocated := t.ID == ""
		if allocated {
			if t.ID, err = p.NextID(t.Series); err != nil {
				return nil, err
			}
		}
		if !idShape.MatchString(t.ID) {
			return nil, fmt.Errorf("id %q is not of the form PAY-031 or PAY-012.1", t.ID)
		}
		used, err := p.UsedIDs()
		if err != nil {
			return nil, err
		}
		if used[t.ID] {
			return nil, fmt.Errorf("id %s is already in use; ids are never reused", t.ID)
		}
		// NextID already counted past other branches; a hand-picked id has not.
		if !allocated {
			elsewhere, err := ElsewhereIDs(p.Dir)
			if err != nil {
				return nil, err
			}
			if elsewhere[t.ID] {
				return nil, fmt.Errorf("id %s is already used on another branch or worktree; leave --id off to allocate the next free one", t.ID)
			}
		}
		it = p.taskItem(t)
		s.Created(&it)
		if err := it.Validate(); err != nil {
			return nil, err
		}
		f.Items = append(f.Items, it)
		return []*File{f}, nil
	})
	return it, err
}

func (p *Project) taskItem(t NewTask) Item {
	title := p.TaskTitle(t.Title)
	if t.Blocker && p.Settings.Epic.CloseTitles {
		// The blocker marker sits before the stop, so the stop is always
		// the one the renderer moves after it.
		title = strings.TrimSuffix(strings.TrimSpace(t.Title), ".") + "."
	}
	it := Item{
		ID: t.ID, Type: TypeTask, Status: StatusTodo, Title: title,
		Size: t.Size, Blocker: t.Blocker, Depends: t.Depends, Closes: t.Closes,
	}
	if t.Bug {
		it.Type = TypeBug
	}
	for k, v := range t.Fields {
		if v == "" {
			continue
		}
		if it.Fields == nil {
			it.Fields = map[string]string{}
		}
		it.Fields[k] = v
	}
	for _, l := range t.Labels {
		if !slices.Contains(it.Labels, l) {
			it.Labels = append(it.Labels, l)
		}
	}
	return it
}

// NewEpic is an epic to create with no tasks yet.
type NewEpic struct {
	Slug  string   // id part after the number, e.g. "auth-provisioning"
	Title string   // heading after "EPIC N: "
	Intro []string // paragraphs under the legend, one line each
}

var taskShaped = regexp.MustCompile(`^\s*[-*]\s*\[[ xX]\]`)

// nextEpicNumber is one past the highest epic number in the records.
// Tombstoned epics keep their records, so their numbers stay spent.
func nextEpicNumber(files []*File) int {
	highest := -1
	for _, f := range files {
		if f.IsEpic() && f.Number() > highest {
			highest = f.Number()
		}
	}
	return highest + 1
}

// CreateEpic writes a new epic record under the next free number, under the
// lock, and returns its id.
//
// Intro paragraphs are kept one per line: a hard-wrapped paragraph renders
// as abrupt line breaks in a markdown preview.
func (p *Project) CreateEpic(actor string, e NewEpic) (string, error) {
	if !labelShape.MatchString(e.Slug) {
		return "", fmt.Errorf("slug %q is not lower-case words joined by hyphens, e.g. auth-provisioning", e.Slug)
	}
	title := strings.TrimSpace(e.Title)
	if title == "" || strings.ContainsAny(title, "\n\r") {
		return "", fmt.Errorf("an epic needs a one-line title")
	}
	var b strings.Builder
	if p.Settings.Epic.Legend != "" {
		b.WriteString("\n" + p.Settings.Epic.Legend + "\n")
	}
	for _, para := range e.Intro {
		if strings.ContainsAny(para, "\n\r") {
			return "", fmt.Errorf("an intro paragraph is one line; pass --intro again for the next paragraph")
		}
		if taskShaped.MatchString(para) {
			return "", fmt.Errorf("intro paragraph reads as a task line; add tasks with the CLI: %s", para)
		}
		if para = strings.TrimSpace(para); para != "" {
			b.WriteString("\n" + para + "\n")
		}
	}
	// The blank line the first task line will follow.
	b.WriteString("\n")
	if !p.Initialised() {
		return "", ErrNotInitialised
	}
	var id string
	err := p.Store(actor).Mutate(func(files []*File) ([]*File, error) {
		for _, f := range files {
			if f.IsEpic() && f.Epic.ID == fmt.Sprintf("epic-%d-%s", f.Number(), e.Slug) {
				return nil, fmt.Errorf("an epic with slug %q already exists: %s", e.Slug, f.Epic.ID)
			}
		}
		id = fmt.Sprintf("epic-%d-%s", nextEpicNumber(files), e.Slug)
		return []*File{{
			Path: filepath.Join(DataDir(p.Dir), id+".jsonl"),
			Epic: Item{ID: id, Type: TypeEpic, Schema: SchemaVersion, Title: title, Intro: b.String()},
		}}, nil
	})
	return id, err
}

// Update applies change to one record under the lock, then holds the result
// to the settings: its size is one they define, its epic fields match their
// patterns, and an entry's fields take their collection's values.
func (p *Project) Update(actor, id string, change func(it *Item) error) (Item, error) {
	return p.Store(actor).Update(id, func(it *Item) error {
		if err := change(it); err != nil {
			return err
		}
		return p.CheckItem(*it)
	})
}

// CheckItem holds one record to the settings.
func (p *Project) CheckItem(it Item) error {
	if it.Size != "" {
		if _, ok := p.Settings.Size(it.Size); !ok {
			return fmt.Errorf("%s: size %q is not one of %s", it.ID, it.Size, p.sizeNames())
		}
	}
	if c, ok := p.Settings.Collection(it.Type); ok {
		for k, v := range it.Fields {
			if allowed, ok := c.Values[k]; ok && !slices.Contains(allowed, v) {
				return fmt.Errorf("%s: %s %q is not one of %s", it.ID, strings.ReplaceAll(k, "_", " "), v, strings.Join(allowed, ", "))
			}
		}
		return nil
	}
	if it.Type == TypeTask || it.Type == TypeBug {
		for k, v := range it.Fields {
			if err := p.CheckField(k, v); err != nil {
				return fmt.Errorf("%s: %w", it.ID, err)
			}
		}
	}
	return nil
}
