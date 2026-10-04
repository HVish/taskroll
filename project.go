package taskroll

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// Project is one tracker directory with its settings: everything the tracker
// does is a method on it. A project adds what is its own through the hooks.
type Project struct {
	Dir      string
	Settings Settings
	// Views regenerates the project's own files after the records' (a
	// project's index or status page). It runs inside the write lock.
	Views func() error
	// Reserved returns ids issued somewhere the records and the archive do
	// not show, such as a ledger of retired ids, so allocation skips them.
	Reserved func() (map[string]bool, error)
	// Now is the clock every stamp reads; nil means time.Now.
	Now func() time.Time
}

// ErrNotInitialised is returned by writes to a directory with no records.
var ErrNotInitialised = errors.New("no tracker records here; run init")

// OpenProject loads a tracker directory's settings.
func OpenProject(dir string) (*Project, error) {
	s, err := LoadSettings(dir)
	if err != nil {
		return nil, err
	}
	return &Project{Dir: dir, Settings: s}, nil
}

// Initialised reports whether the directory holds records: a data directory
// exists, made by init.
func (p *Project) Initialised() bool {
	st, err := os.Stat(DataDir(p.Dir))
	return err == nil && st.IsDir()
}

func (p *Project) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Markdown is the project's epic grammar.
func (p *Project) Markdown() Markdown { return p.Settings.Markdown() }

// Store returns the writer for the project, acting as actor. Every change
// it makes regenerates every view before the lock is released.
func (p *Project) Store(actor string) *Store {
	return &Store{Dir: p.Dir, Actor: actor, Now: p.now, Render: p.RenderAll}
}

// Snapshot is every record, loaded once for a render or a query.
type Snapshot struct {
	Dir      string
	Settings Settings
	Files    []*File
}

// Load reads and cross-checks every record.
func (p *Project) Load() (*Snapshot, error) {
	files, err := Load(p.Dir)
	if err != nil {
		return nil, err
	}
	if err := Check(files); err != nil {
		return nil, err
	}
	return &Snapshot{Dir: p.Dir, Settings: p.Settings, Files: files}, nil
}

// EpicFileName is the name an epic renders under in epics/.
func EpicFileName(f *File) string { return f.Epic.ID + ".md" }

// LiveEpics are the epics that are not tombstones, in file-name order,
// which is the order views list them in.
func (s *Snapshot) LiveEpics() []*File {
	var out []*File
	for _, f := range s.Files {
		if f.IsEpic() && f.Epic.Archived == "" {
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return EpicFileName(out[i]) < EpicFileName(out[j]) })
	return out
}

// Entries returns one collection's records that have files (every status
// but dropped), in file-name order.
func (s *Snapshot) Entries(c CollectionSpec) []Item {
	var out []Item
	for _, f := range s.Files {
		if f.IsEpic() {
			continue
		}
		for _, it := range f.Items {
			if it.Type == c.Type && it.Status != StatusDropped {
				out = append(out, it)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// RenderAll regenerates every file derived from the records: the epic and
// collection files, then the project's own views. The caller holds the lock
// (Store does), or is the only writer.
func (p *Project) RenderAll() error {
	if _, err := p.RenderRecords(false); err != nil {
		return err
	}
	if p.Views != nil {
		return p.Views()
	}
	return nil
}

// Refresh is RenderAll under the lock, for when nothing but the views changes.
func (p *Project) Refresh() error {
	unlock, err := Lock(p.Dir)
	if err != nil {
		return err
	}
	defer unlock()
	return p.RenderAll()
}

// RenderRecords renders the epic and collection files. With check it writes
// nothing and returns what is stale.
func (p *Project) RenderRecords(check bool) ([]string, error) {
	s, err := p.Load()
	if err != nil {
		return nil, err
	}
	stale, err := p.renderEpics(s, check)
	if err != nil {
		return nil, err
	}
	more, err := p.renderCollections(s, check)
	return append(stale, more...), err
}

func (p *Project) renderEpics(s *Snapshot, check bool) ([]string, error) {
	md := p.Markdown()
	var stale []string
	have := map[string]bool{}
	for _, f := range s.Files {
		if !f.IsEpic() {
			continue
		}
		have[f.Epic.ID] = true
		path := filepath.Join(p.Dir, "epics", EpicFileName(f))
		changed, err := WriteIfChanged(path, []byte(md.RenderEpic(f)), check)
		if err != nil {
			return nil, err
		}
		if changed {
			stale = append(stale, path)
		}
	}
	// An epic file with no record behind it is a hand-written file the
	// generator would never refresh.
	mds, err := filepath.Glob(filepath.Join(p.Dir, "epics", "*.md"))
	if err != nil {
		return nil, err
	}
	for _, path := range mds {
		if !have[strings.TrimSuffix(filepath.Base(path), ".md")] {
			return nil, fmt.Errorf("%s has no record in data/; create epics with the CLI", path)
		}
	}
	return stale, nil
}

// WriteIfChanged writes want to path unless it already holds it, and reports
// whether it differed. With check it only reports.
func WriteIfChanged(path string, want []byte, check bool) (bool, error) {
	have, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err == nil && bytes.Equal(have, want) {
		return false, nil
	}
	if check {
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, WriteFileAtomic(path, want)
}

// Format rewrites every record file in canonical form, or with check only
// reports the ones that are not.
func (p *Project) Format(check bool) ([]string, error) {
	files, err := Load(p.Dir)
	if err != nil {
		return nil, err
	}
	bad, err := Unformatted(files)
	if err != nil || check || len(bad) == 0 {
		return bad, err
	}
	err = p.Store("").Mutate(func(files []*File) ([]*File, error) {
		var changed []*File
		for _, f := range files {
			if slices.Contains(bad, f.Path) {
				changed = append(changed, f)
			}
		}
		return changed, nil
	})
	return bad, err
}

// Index loads the records for querying.
func (p *Project) Index() (*Index, error) {
	s, err := p.Load()
	if err != nil {
		return nil, err
	}
	return NewIndex(s.Files, nil), nil
}
