package taskroll

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// File is one epic: its record and its items, in the order they render.
// A collection file (debt, opportunities, watch) has no epic record: Epic is
// the zero Item and every record is in Items.
//
// Items keep file order rather than being sorted by ID. The order of an
// epic's task list is authored (dependencies first, then what builds on
// them), and sorting would scramble every rendered epic.
type File struct {
	Path  string
	Epic  Item
	Items []Item
}

// IsEpic reports whether the file is an epic rather than a collection.
func (f *File) IsEpic() bool { return f.Epic.ID != "" }

// Records returns the file's records in file order, the epic first.
func (f *File) Records() []Item {
	if !f.IsEpic() {
		return f.Items
	}
	return append([]Item{f.Epic}, f.Items...)
}

// epicFile names an epic's record file; any other name is a collection.
var epicFile = regexp.MustCompile(`^epic-.*\.jsonl$`)

// DataDir is where the JSONL files live under a tracker directory.
func DataDir(trackerDir string) string { return filepath.Join(trackerDir, "data") }

var epicNumber = regexp.MustCompile(`^epic-([0-9]+)-`)

// Number returns the epic's number, or -1 when the id has none.
func (f *File) Number() int {
	if m := epicNumber.FindStringSubmatch(f.Epic.ID); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return -1
}

// Load reads every record file under trackerDir/data: the epics ordered by
// number, then the collections by name. A record that fails to decode or
// validate is reported with its file and line, since the fix is always an
// edit at that line.
func Load(trackerDir string) ([]*File, error) {
	paths, err := filepath.Glob(filepath.Join(DataDir(trackerDir), "*.jsonl"))
	if err != nil {
		return nil, err
	}
	var files []*File
	for _, p := range paths {
		f, err := LoadFile(p)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	sort.SliceStable(files, func(i, j int) bool {
		a, b := files[i], files[j]
		if a.IsEpic() != b.IsEpic() {
			return a.IsEpic()
		}
		if a.IsEpic() {
			return a.Number() < b.Number()
		}
		return filepath.Base(a.Path) < filepath.Base(b.Path)
	})
	return files, nil
}

// LoadFile reads one record file. A file named epic-* holds an epic; any
// other name holds a collection.
func LoadFile(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := Decode(path, raw)
	if err != nil {
		return nil, err
	}
	if !epicFile.MatchString(filepath.Base(path)) {
		if f.IsEpic() {
			return nil, fmt.Errorf("%s: holds epic %s, so the file must be named %s.jsonl", path, f.Epic.ID, f.Epic.ID)
		}
		return f, nil
	}
	if !f.IsEpic() {
		return nil, fmt.Errorf("%s: an epic file whose first record is not the epic", path)
	}
	if want := f.Epic.ID + ".jsonl"; filepath.Base(path) != want {
		return nil, fmt.Errorf("%s: holds epic %s, so the file must be named %s", path, f.Epic.ID, want)
	}
	return f, nil
}

// conflictMarker is the start of a line git writes into a file it could not
// merge. Reported by name, because the JSON error it would otherwise cause
// ("invalid character '<'") does not say what happened.
var conflictMarker = regexp.MustCompile(`^(<{7}|={7}|>{7}|\|{7})( |$)`)

// Decode parses the records of one file; name is used in errors only. A
// first record of type epic makes an epic file; otherwise it is a collection.
// Empty input decodes to an empty collection, which is what git hands a merge
// driver for a file one side added.
func Decode(name string, raw []byte) (*File, error) {
	f := &File{Path: name}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Bytes()
		if len(bytes.TrimSpace(text)) == 0 {
			continue
		}
		if conflictMarker.Match(text) {
			return nil, fmt.Errorf("%s:%d: unresolved merge conflict; resolve it, or set up the record merge with `taskroll config merge-driver` and merge again", name, line)
		}
		var it Item
		dec := json.NewDecoder(bytes.NewReader(text))
		// An unknown key is refused rather than dropped: the next write
		// would otherwise delete it without anyone noticing.
		dec.DisallowUnknownFields()
		if err := dec.Decode(&it); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", name, line, err)
		}
		if err := it.Validate(); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", name, line, err)
		}
		if it.Type == TypeEpic {
			if f.IsEpic() || len(f.Items) > 0 {
				return nil, fmt.Errorf("%s:%d: an epic record that is not the first line; one epic per file, first", name, line)
			}
			f.Epic = it
			continue
		}
		f.Items = append(f.Items, it)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return f, nil
}

// Encode renders a file canonically: one record per line, keys in struct
// order, no HTML escaping, newline-terminated. Every write goes through here,
// so a change to one item is a one-line diff.
func Encode(f *File) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for _, it := range f.Records() {
		if err := enc.Encode(canonical(it)); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}

// canonical drops empty containers so `[]` and absent never both appear.
func canonical(it Item) Item {
	if len(it.Depends) == 0 {
		it.Depends = nil
	}
	if len(it.Closes) == 0 {
		it.Closes = nil
	}
	if len(it.Notes) == 0 {
		it.Notes = nil
	}
	if len(it.Labels) == 0 {
		it.Labels = nil
	}
	if len(it.Fields) == 0 {
		it.Fields = nil
	}
	if len(it.Comments) == 0 {
		it.Comments = nil
	}
	return it
}

// Save validates and writes the file atomically.
func (f *File) Save() error {
	for _, it := range f.Records() {
		if err := it.Validate(); err != nil {
			return err
		}
	}
	data, err := Encode(f)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	return WriteFileAtomic(f.Path, data)
}

// Unformatted returns the files whose bytes differ from their canonical
// encoding, e.g. after a hand edit or a merge.
func Unformatted(files []*File) ([]string, error) {
	var out []string
	for _, f := range files {
		raw, err := os.ReadFile(f.Path)
		if err != nil {
			return nil, err
		}
		want, err := Encode(f)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(raw, want) {
			out = append(out, f.Path)
		}
	}
	return out, nil
}

// Check applies the rules that span records: an ID appears once across all
// files, and an archived epic holds no items (its tasks live in the quarterly
// archive; one left behind, e.g. by a merge with a branch that added to the
// epic, would render nowhere).
func Check(files []*File) error {
	seen := map[string]string{}
	var errs []error
	for _, f := range files {
		if f.Epic.Archived != "" && len(f.Items) > 0 {
			errs = append(errs, fmt.Errorf("%s: archived at %s but still holds %d item(s), starting with %s; move them to an open epic", filepath.Base(f.Path), f.Epic.Archived, len(f.Items), f.Items[0].ID))
		}
		for _, it := range f.Records() {
			if prev, dup := seen[it.ID]; dup {
				errs = append(errs, fmt.Errorf("%s: %s is also in %s", filepath.Base(f.Path), it.ID, prev))
				continue
			}
			seen[it.ID] = filepath.Base(f.Path)
		}
	}
	return errors.Join(errs...)
}

// Find returns the file holding id and the item's index in it; index -1
// means the id is the epic record itself.
func Find(files []*File, id string) (*File, int, bool) {
	for _, f := range files {
		if strings.EqualFold(f.Epic.ID, id) {
			return f, -1, true
		}
		for i, it := range f.Items {
			if strings.EqualFold(it.ID, id) {
				return f, i, true
			}
		}
	}
	return nil, 0, false
}

// WriteFileAtomic writes via a temp file in the same directory, syncs it,
// renames it over path and syncs the directory, so a crash leaves either the
// previous content or the new, never a truncated file, and a rename that
// later work relies on (the archive sweep deletes sources on the strength of
// it) is durable. The destination keeps its mode; a new file gets 0644.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".taskroll-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }() // a no-op once the rename succeeds
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}
