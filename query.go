package taskroll

import (
	"slices"
	"strings"
)

// Index joins the records of every file for queries.
type Index struct {
	Files []*File
	byID  map[string]Item
	epic  map[string]string
	known map[string]bool
	// external holds items the store does not own yet (debt, watch and
	// opportunity entries still in markdown), as id → closed.
	external map[string]bool
	// dependents is the reverse of Depends, resolved to IDs.
	dependents map[string][]string
}

// NewIndex builds the index. external may be nil.
func NewIndex(files []*File, external map[string]bool) *Index {
	x := &Index{Files: files, byID: map[string]Item{}, epic: map[string]string{}, known: map[string]bool{}, external: external, dependents: map[string][]string{}}
	for id := range external {
		if p := Prefix(id); p != "" {
			x.known[p] = true
		}
	}
	for _, f := range files {
		for _, it := range f.Items {
			x.byID[it.ID] = it
			x.epic[it.ID] = f.Epic.ID
			if p := Prefix(it.ID); p != "" {
				x.known[p] = true
			}
		}
	}
	for _, f := range files {
		for _, it := range f.Items {
			ids, _ := RefIDs(it.Depends, x.known)
			for _, d := range ids {
				x.dependents[d] = append(x.dependents[d], it.ID)
			}
		}
	}
	return x
}

// Get returns an item by id.
func (x *Index) Get(id string) (Item, bool) {
	if it, ok := x.byID[id]; ok {
		return it, true
	}
	it, ok := x.byID[strings.ToUpper(id)]
	return it, ok
}

// EpicOf returns the epic id an item belongs to.
func (x *Index) EpicOf(id string) string { return x.epic[id] }

// Dependents returns the items that depend on id.
func (x *Index) Dependents(id string) []string { return x.dependents[id] }

// Blockers returns an item's unmet dependencies: IDs not yet closed (an ID
// nobody can find counts, since treating it as met would hide a tracker bug)
// and prose conditions, which only a person can clear.
func (x *Index) Blockers(it Item) (ids, prose []string) {
	refs, prose := RefIDs(it.Depends, x.known)
	for _, id := range refs {
		if dep, ok := x.byID[id]; ok {
			if dep.Open() {
				ids = append(ids, id)
			}
			continue
		}
		if closed, ok := x.external[id]; ok && closed {
			continue
		}
		ids = append(ids, id)
	}
	return ids, prose
}

// Ready reports an open, not-yet-started item with nothing blocking it.
func (x *Index) Ready(it Item) bool {
	if it.Status != StatusTodo {
		return false
	}
	ids, prose := x.Blockers(it)
	return len(ids) == 0 && len(prose) == 0
}

// Filter selects items. Empty fields match everything; a list matches when
// the item has any of its values.
type Filter struct {
	Types     []string
	Statuses  []string
	Epics     []string // epic id, or a prefix such as "epic-3" or "epic-3-attendance"
	Labels    []string // the item must carry every one
	Sizes     []string
	Fields    map[string]string
	Ready     bool
	Blocked   bool
	DependsOn string
	Text      string // case-insensitive substring of id, title, notes or description
	All       bool   // include done and dropped; otherwise open items only
}

// List returns the matching items in file order.
func (x *Index) List(f Filter) []Item {
	var out []Item
	for _, file := range x.Files {
		for _, it := range file.Items {
			if x.match(it, file.Epic.ID, f) {
				out = append(out, it)
			}
		}
	}
	return out
}

func (x *Index) match(it Item, epic string, f Filter) bool {
	if !f.All && len(f.Statuses) == 0 && !it.Open() {
		return false
	}
	if len(f.Types) > 0 && !slices.Contains(f.Types, it.Type) {
		return false
	}
	if len(f.Statuses) > 0 && !slices.Contains(f.Statuses, it.Status) {
		return false
	}
	if len(f.Sizes) > 0 && !slices.Contains(f.Sizes, it.Size) {
		return false
	}
	if len(f.Epics) > 0 && !slices.ContainsFunc(f.Epics, func(e string) bool {
		return epic == e || strings.HasPrefix(epic, e+"-")
	}) {
		return false
	}
	for _, l := range f.Labels {
		if !it.HasLabel(l) {
			return false
		}
	}
	for k, v := range f.Fields {
		if !strings.EqualFold(it.Fields[k], v) {
			return false
		}
	}
	if f.Ready && !x.Ready(it) {
		return false
	}
	if f.Blocked {
		ids, prose := x.Blockers(it)
		if !it.Open() || len(ids)+len(prose) == 0 {
			return false
		}
	}
	if f.DependsOn != "" {
		ids, _ := RefIDs(it.Depends, x.known)
		if !slices.Contains(ids, strings.ToUpper(f.DependsOn)) {
			return false
		}
	}
	if f.Text != "" {
		hay := strings.ToLower(strings.Join(append([]string{it.ID, it.Title, it.Description}, it.Notes...), " "))
		if !strings.Contains(hay, strings.ToLower(f.Text)) {
			return false
		}
	}
	return true
}

// EpicSummary is one row of the epic list.
type EpicSummary struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Archived string `json:"archived,omitempty"`
	Open     int    `json:"open"`
	Done     int    `json:"done"`
	Ready    int    `json:"ready"`
	Blocked  int    `json:"blocked"`
}

// Epics summarises every epic.
func (x *Index) Epics() []EpicSummary {
	var out []EpicSummary
	for _, f := range x.Files {
		if !f.IsEpic() {
			continue
		}
		s := EpicSummary{ID: f.Epic.ID, Title: f.Epic.Title, Archived: f.Epic.Archived}
		for _, it := range f.Items {
			switch it.Status {
			case StatusDone:
				s.Done++
			case StatusDropped:
			default:
				s.Open++
				if x.Ready(it) {
					s.Ready++
				} else if ids, prose := x.Blockers(it); len(ids)+len(prose) > 0 {
					s.Blocked++
				}
			}
		}
		out = append(out, s)
	}
	return out
}
