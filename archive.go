package taskroll

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ArchiveResult reports what a sweep did.
type ArchiveResult struct {
	Archived int // closed entries appended to the quarterly archive
	Epics    int // completed epics moved wholesale
	File     string
}

// ArchiveRecord is one archived entry or epic, as stored in the quarterly
// JSONL, archive/YYYY-QN.jsonl.
//
// One JSON object per line, rather than concatenated markdown, because
// markdown gave `---` three jobs at once (entry separator, frontmatter open,
// frontmatter close), so nothing could split it reliably. A line is
// append-native, unambiguous whatever the body holds, and greppable by id.
type ArchiveRecord struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"` // an entry type, or epic
	Title      string            `json:"title"`
	ArchivedAt string            `json:"archived_at"` // release version
	ArchivedOn string            `json:"archived_on"` // YYYY-MM-DD
	Source     string            `json:"source"`      // tracker-relative path it came from
	Meta       map[string]string `json:"meta"`        // full frontmatter
	Body       string            `json:"body"`
}

type closedEntry struct {
	spec CollectionSpec
	item Item
}

// Archive sweeps closed work out of the live records, under the lock.
//
// Done entries are appended to archive/YYYY-QN.jsonl and their records and
// files removed. (A dropped entry never reaches here: it lost its file when
// it was dropped and keeps its record, so its id stays spent.) An epic whose
// tasks are all complete moves wholesale and its record becomes a
// tombstone: a completed task carries the notes that make its epic readable
// as a narrative of what shipped, so tasks are never swept one by one out of
// a live epic.
//
// The archive file is per quarter, so the directory grows four files a year
// rather than one per release, and it is created on first write: a sweep with
// nothing to archive leaves no trace.
//
// Ordering is deliberate. The archive is written and synced first; then the
// records, so a crash leaves at worst an entry file with no record, which the
// gate reports, rather than a record that would render its file back and be
// archived twice; then the entry files.
func (p *Project) Archive(version string, at time.Time) (ArchiveResult, error) {
	var res ArchiveResult
	if !p.Initialised() {
		return res, ErrNotInitialised
	}
	quarter := (int(at.Month())-1)/3 + 1
	archivePath := filepath.Join(p.Dir, "archive", fmt.Sprintf("%d-Q%d.jsonl", at.Year(), quarter))

	err := p.Store("").Mutate(func(files []*File) ([]*File, error) {
		pending := p.closedEntries(files)
		epics := completedEpics(files)
		if len(pending) == 0 && len(epics) == 0 {
			return nil, nil
		}
		body, err := p.buildArchive(archivePath, pending, epics, version, at)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
			return nil, err
		}
		if err := WriteFileAtomic(archivePath, []byte(body)); err != nil {
			return nil, err
		}
		gone := map[string]bool{}
		for _, e := range pending {
			gone[e.item.ID] = true
		}
		var changed []*File
		for _, f := range files {
			if f.IsEpic() {
				continue
			}
			kept := f.Items[:0:0]
			for _, it := range f.Items {
				if !gone[it.ID] {
					kept = append(kept, it)
				}
			}
			if len(kept) != len(f.Items) {
				f.Items = kept
				changed = append(changed, f)
			}
		}
		for _, f := range epics {
			f.Epic.Archived = version
			f.Epic.ArchivedFile = filepath.Base(archivePath)
			f.Epic.Intro, f.Epic.Outro, f.Items = "", "", nil
			changed = append(changed, f)
		}
		for _, f := range changed {
			if err := f.Save(); err != nil {
				return nil, err
			}
		}
		for _, e := range pending {
			if err := os.Remove(EntryPath(p.Dir, e.spec, e.item)); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}
		res = ArchiveResult{Archived: len(pending), Epics: len(epics), File: archivePath}
		// Saved above, in the order the sweep needs; Mutate checks and renders.
		return nil, nil
	})
	return res, err
}

func (p *Project) closedEntries(files []*File) []closedEntry {
	var out []closedEntry
	for _, f := range files {
		if f.IsEpic() {
			continue
		}
		for _, it := range f.Items {
			if c, ok := p.Settings.Collection(it.Type); ok && it.Status == StatusDone {
				out = append(out, closedEntry{c, it})
			}
		}
	}
	return out
}

// completedEpics are the live epics with at least one done task and none
// open. An epic with no tasks at all is unwritten, not complete, and stays.
func completedEpics(files []*File) []*File {
	var out []*File
	for _, f := range files {
		if !f.IsEpic() || f.Epic.Archived != "" {
			continue
		}
		open, done := 0, 0
		for _, it := range f.Items {
			switch {
			case it.Status == StatusDone:
				done++
			case it.Open():
				open++
			}
		}
		if open == 0 && done > 0 {
			out = append(out, f)
		}
	}
	return out
}

// buildArchive returns the quarter file with the new records appended. An
// epic's body is its markdown as it read before the sweep, without the
// generated banner: the archive keeps the work as it was.
func (p *Project) buildArchive(archivePath string, pending []closedEntry, epics []*File, version string, at time.Time) (string, error) {
	existing, err := os.ReadFile(archivePath)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	var b strings.Builder
	if len(existing) > 0 {
		b.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			b.WriteString("\n")
		}
	}
	day := at.Format("2006-01-02")
	add := func(rec ArchiveRecord) error {
		line, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteString("\n")
		return nil
	}
	for _, e := range pending {
		if err := add(ArchiveRecord{
			ID: e.item.ID, Kind: e.item.Type, Title: e.item.Title,
			ArchivedAt: version, ArchivedOn: day, Source: EntryRel(e.spec, e.item),
			Meta: EntryMeta(e.spec, e.item), Body: e.item.Description,
		}); err != nil {
			return "", err
		}
	}
	plain := p.Markdown()
	plain.Banner = ""
	for _, f := range epics {
		if err := add(ArchiveRecord{
			ID: f.Epic.ID, Kind: TypeEpic, Title: fmt.Sprintf("EPIC %d: %s", f.Number(), f.Epic.Title),
			ArchivedAt: version, ArchivedOn: day, Source: "epics/" + EpicFileName(f),
			Meta: map[string]string{}, Body: plain.RenderEpic(f),
		}); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}
