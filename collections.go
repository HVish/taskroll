package taskroll

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// A collection keeps one markdown file per entry (debt, say) beside the
// epics, generated from one records file, data/<dir>.jsonl. The file's
// frontmatter carries the keys its CollectionSpec lists, in that order; the
// body is the record's description.

// CollectionPath is a collection's records file.
func CollectionPath(trackerDir, dir string) string {
	return filepath.Join(DataDir(trackerDir), dir+".jsonl")
}

// EntryPath is where an entry renders: its id, which for a collection named
// by slug is the slug.
func EntryPath(trackerDir string, c CollectionSpec, it Item) string {
	return filepath.Join(trackerDir, c.Dir, it.ID+".md")
}

// EntryRel is EntryPath relative to the tracker directory, as links use it.
func EntryRel(c CollectionSpec, it Item) string { return c.Dir + "/" + it.ID + ".md" }

// isFieldKey reports a frontmatter key held in Item.Fields rather than in a
// field of its own.
func isFieldKey(key string) bool { return !slices.Contains(collectionFixed, key) }

// yamlNeedsQuotes matches a scalar YAML would misread unquoted: one starting
// with an indicator character, or holding ": " (a nested mapping) or " #" (a
// comment), or ending in a colon. The files are read by tools other than
// this one (editors, Obsidian), so the frontmatter has to be real YAML.
var yamlNeedsQuotes = regexp.MustCompile("^[`\\[\\]{}&*!|>'\"%@#,?:-]|: | #|:$")

// YAMLScalar quotes v when YAML would misread it bare.
func YAMLScalar(v string) string {
	if !yamlNeedsQuotes.MatchString(v) {
		return v
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

// EntryStatus is the three-state lifecycle an entry file shows.
func EntryStatus(s string) string {
	switch s {
	case StatusDone, StatusDropped:
		return s
	default:
		return "open"
	}
}

// EntryMeta is an entry's frontmatter as values, the way a reader of the
// file sees it.
func EntryMeta(c CollectionSpec, it Item) map[string]string {
	meta := map[string]string{}
	for _, key := range c.Keys {
		switch key {
		case "id":
			meta[key] = it.ID
		case "title":
			meta[key] = it.Title
		case "type":
			meta[key] = it.Type
		case "status":
			meta[key] = EntryStatus(it.Status)
		case "effort":
			if it.Size != "" {
				meta[key] = it.Size
			}
		case "closed":
			meta[key] = it.Done
		default:
			if v, ok := it.Fields[key]; ok {
				meta[key] = v
			}
		}
	}
	return meta
}

// RenderEntry builds an entry's file, with banner (when set) under the
// heading.
func RenderEntry(c CollectionSpec, it Item, banner string) string {
	meta := EntryMeta(c, it)
	var b strings.Builder
	b.WriteString("---\n")
	for _, key := range c.Keys {
		v, ok := meta[key]
		if !ok {
			continue
		}
		switch {
		case v == "":
			b.WriteString(key + ":\n")
		case key == "title" || isFieldKey(key):
			b.WriteString(key + ": " + YAMLScalar(v) + "\n")
		default:
			b.WriteString(key + ": " + v + "\n")
		}
	}
	b.WriteString("---\n")
	body := it.Description
	if banner != "" {
		head, rest, found := strings.Cut(body, "\n")
		if found && strings.HasPrefix(head, "# ") {
			body = head + "\n" + banner + "\n" + rest
		} else {
			body = banner + "\n" + body
		}
	}
	b.WriteString(body)
	return b.String()
}

func (p *Project) entryBanner(c CollectionSpec) string {
	return strings.ReplaceAll(p.Settings.Banner, "%s", c.Dir+".jsonl")
}

// renderCollections writes every entry file from its record, removes the
// file of a dropped entry, and refuses an entry file with no record.
func (p *Project) renderCollections(s *Snapshot, check bool) ([]string, error) {
	var stale []string
	owned := map[string]bool{}
	for _, f := range s.Files {
		if f.IsEpic() {
			continue
		}
		for _, it := range f.Items {
			c, ok := p.Settings.Collection(it.Type)
			if !ok {
				return nil, fmt.Errorf("%s: %s is a %s, and the settings define no collection for it", f.Path, it.ID, it.Type)
			}
			if want := CollectionPath(p.Dir, c.Dir); filepath.Base(f.Path) != filepath.Base(want) {
				return nil, fmt.Errorf("%s: %s is a %s entry and belongs in %s", f.Path, it.ID, it.Type, filepath.Base(want))
			}
			path := EntryPath(p.Dir, c, it)
			owned[path] = true
			if it.Status == StatusDropped {
				// A dropped entry was never real work; its record keeps the
				// id spent, and it has no file.
				if _, err := os.Stat(path); err == nil {
					stale = append(stale, path)
					if !check {
						if err := os.Remove(path); err != nil {
							return nil, err
						}
					}
				}
				continue
			}
			changed, err := WriteIfChanged(path, []byte(RenderEntry(c, it, p.entryBanner(c))), check)
			if err != nil {
				return nil, err
			}
			if changed {
				stale = append(stale, path)
			}
		}
	}
	for _, c := range p.Settings.Collections {
		mds, err := filepath.Glob(filepath.Join(p.Dir, c.Dir, "*.md"))
		if err != nil {
			return nil, err
		}
		for _, path := range mds {
			if !owned[path] {
				return nil, fmt.Errorf("%s has no record in data/%s.jsonl; file entries with the CLI", path, c.Dir)
			}
		}
	}
	return stale, nil
}

// NewEntry is a collection entry to file.
type NewEntry struct {
	Type   string            // the collection's record type
	Title  string            // one line
	Slug   string            // unnumbered collections only: the id and file name; default from the title
	Size   string            // the entry's effort, when the collection has one
	Fields map[string]string // frontmatter fields
	Body   string            // markdown under the heading
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns a title into an unnumbered entry's id.
func Slugify(title string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
}

func (p *Project) validateEntry(c CollectionSpec, e NewEntry) (map[string]string, error) {
	title := strings.TrimSpace(e.Title)
	if title == "" || strings.ContainsAny(title, "\n\r") {
		return nil, fmt.Errorf("an entry needs a one-line title")
	}
	if strings.TrimSpace(e.Body) == "" {
		return nil, fmt.Errorf("an entry needs a body: why it is here and what would change that")
	}
	hasSize := slices.Contains(c.Keys, "effort")
	if hasSize && e.Size == "" {
		return nil, fmt.Errorf("a %s entry needs --size, its effort", e.Type)
	}
	if !hasSize && e.Size != "" {
		return nil, fmt.Errorf("a %s entry has no size", e.Type)
	}
	if e.Size != "" {
		if _, ok := p.Settings.Size(e.Size); !ok {
			return nil, fmt.Errorf("size %q is not one of %s", e.Size, p.sizeNames())
		}
	}
	if c.Series != "" && e.Slug != "" {
		return nil, fmt.Errorf("only an unnumbered entry takes a slug; a %s entry is numbered", e.Type)
	}
	fields := map[string]string{}
	for key, v := range e.Fields {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !slices.Contains(c.Keys, key) || !isFieldKey(key) || key == "opened" {
			return nil, fmt.Errorf("a %s entry has no %s field", e.Type, key)
		}
		if strings.ContainsAny(v, "\n\r") {
			return nil, fmt.Errorf("%s is one line", key)
		}
		if allowed, ok := c.Values[key]; ok && !slices.Contains(allowed, v) {
			return nil, fmt.Errorf("%s %q is not one of %s", strings.ReplaceAll(key, "_", " "), v, strings.Join(allowed, ", "))
		}
		fields[key] = v
	}
	for _, key := range c.Required {
		if fields[key] == "" {
			return nil, fmt.Errorf("a %s entry needs --%s", e.Type, strings.ReplaceAll(key, "_", "-"))
		}
	}
	return fields, nil
}

// AddEntry files a new entry under the tracker lock: it allocates the next
// id in the collection's series (or takes the slug), stamps opened and who
// filed it, appends the record and renders its file.
func (p *Project) AddEntry(actor string, e NewEntry) (Item, error) {
	c, ok := p.Settings.Collection(e.Type)
	if !ok {
		return Item{}, fmt.Errorf("no collection for type %q in the settings", e.Type)
	}
	fields, err := p.validateEntry(c, e)
	if err != nil {
		return Item{}, err
	}
	if !p.Initialised() {
		return Item{}, ErrNotInitialised
	}
	title := strings.TrimSpace(e.Title)
	s := p.Store(actor)
	if slices.Contains(c.Keys, "opened") {
		fields["opened"] = s.Today()
	}

	var it Item
	err = s.Mutate(func(files []*File) ([]*File, error) {
		var id string
		if c.Series != "" {
			var err error
			if id, err = p.nextOrFirst(c.Series); err != nil {
				return nil, err
			}
		} else {
			id = e.Slug
			if id == "" {
				id = Slugify(title)
			}
			if id != Slugify(id) || Numbered(id) {
				return nil, fmt.Errorf("slug %q is not lower-case words joined by hyphens", id)
			}
		}
		// A dropped entry has no file but keeps its record, so its id is spent.
		if _, _, taken := Find(files, id); taken {
			return nil, fmt.Errorf("%s is already used; ids are never reused (give an unnumbered entry a different --slug)", id)
		}
		it = Item{
			ID: id, Type: e.Type, Status: StatusTodo, Title: title, Size: e.Size, Fields: fields,
			Description: "# " + title + "\n\n" + strings.TrimSpace(e.Body) + "\n",
		}
		s.Created(&it)
		if err := it.Validate(); err != nil {
			return nil, err
		}
		path := CollectionPath(p.Dir, c.Dir)
		f := &File{Path: path}
		for _, have := range files {
			if filepath.Clean(have.Path) == filepath.Clean(path) {
				f = have
			}
		}
		f.Items = append(f.Items, it)
		return []*File{f}, nil
	})
	return it, err
}

func (p *Project) sizeNames() string {
	var names []string
	for _, z := range p.Settings.Sizes {
		names = append(names, z.Name)
	}
	return strings.Join(names, ", ")
}
