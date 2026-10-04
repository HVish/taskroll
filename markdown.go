package taskroll

import (
	"fmt"
	"strconv"
	"strings"
)

// Markdown says how records render as an epic file. The grammar is the
// project's, so the caller supplies it.
type Markdown struct {
	// Fields render after the notes as `· Label: value`, in this order.
	Fields []FieldSpec
	// Markers render last, as `· Marker`, for items carrying Label.
	Markers []MarkerSpec
	// ArchivedMarker is the comment that flags a tombstoned epic file.
	ArchivedMarker string
	// Banner, when set, is written as the line after the heading of every
	// rendered epic, telling a reader the file is generated. %s becomes the
	// data file's name.
	Banner string
}

// FieldSpec maps a custom field to its task-line label.
type FieldSpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Pattern, when set, is a regular expression every value must match.
	Pattern string `json:"pattern,omitempty"`
}

// MarkerSpec maps a label to the bare marker that shows it on a task line.
type MarkerSpec struct {
	Label  string `json:"label"`
	Marker string `json:"marker"`
}

const (
	sep           = " · "
	blockerMarker = "**[BLOCKER]**"
)

// RenderEpic renders a file as the epic markdown.
func (md Markdown) RenderEpic(f *File) string {
	heading := fmt.Sprintf("# EPIC %d: %s\n", f.Number(), f.Epic.Title)
	if f.Epic.Archived != "" {
		return fmt.Sprintf("%s\n\n%s\n\nComplete. Archived at %s - the full task list and its\nimplementation notes are in [%s](../archive/%s).\n",
			strings.TrimSuffix(heading, "\n"), md.ArchivedMarker, f.Epic.Archived, f.Epic.ArchivedFile, f.Epic.ArchivedFile)
	}
	var b strings.Builder
	b.WriteString(heading)
	if md.Banner != "" {
		b.WriteString(md.banner(f.Epic.ID))
		b.WriteString("\n")
	}
	b.WriteString(f.Epic.Intro)
	for _, it := range f.Items {
		if it.Status == StatusDropped {
			// A dropped item has no line: a checked box would read as done
			// and unblock its dependents, an open one as owed work. The
			// record stays in the JSONL, which keeps its ID spent.
			continue
		}
		b.WriteString(md.RenderLine(it))
		b.WriteString("\n")
	}
	b.WriteString(f.Epic.Outro)
	return b.String()
}

// RenderLine renders one item as its task line.
func (md Markdown) RenderLine(it Item) string {
	box := " "
	if it.Status == StatusDone {
		box = "x"
	}
	tag := ""
	if it.Type == TypeBug {
		tag = " `bug`"
	}
	return fmt.Sprintf("- [%s] **%s**%s - %s", box, it.ID, tag, md.LineBody(it))
}

// LineBody is the task line after its checkbox, id and tag: the title and
// every field, which is what a summary is cut from.
func (md Markdown) LineBody(it Item) string {
	var b strings.Builder
	title := it.Title
	if it.Blocker && !blockerInText(it) {
		if t, ok := strings.CutSuffix(title, "."); ok {
			title = t + sep + blockerMarker + "."
		} else {
			title += sep + blockerMarker
		}
	}
	b.WriteString(title)
	if it.Size != "" {
		fmt.Fprintf(&b, " `%s`%s", it.Size, it.SizeNote)
	}
	var segs []string
	if len(it.Depends) > 0 {
		segs = append(segs, "Depends: "+strings.Join(it.Depends, ", "))
	}
	if len(it.Closes) > 0 {
		segs = append(segs, "Closes: "+strings.Join(it.Closes, ", "))
	}
	segs = append(segs, it.Notes...)
	for _, f := range md.Fields {
		if v := it.Fields[f.Key]; v != "" {
			segs = append(segs, f.Label+": "+v)
		}
	}
	if it.Done != "" {
		segs = append(segs, "Done: "+it.Done)
	}
	for _, m := range md.Markers {
		if it.HasLabel(m.Label) {
			segs = append(segs, m.Marker)
		}
	}
	for _, sg := range segs {
		b.WriteString(sep)
		b.WriteString(sg)
	}
	return b.String()
}

func (md Markdown) banner(epicID string) string {
	return strings.ReplaceAll(md.Banner, "%s", epicID+".jsonl")
}

// blockerInText reports whether the title or a note already spells the
// blocker marker, as older lines do ("· **[BLOCKER]** for all DB work"), so
// rendering must not add a second one.
func blockerInText(it Item) bool {
	if strings.Contains(it.Title, blockerMarker) {
		return true
	}
	for _, n := range it.Notes {
		if strings.HasPrefix(n, blockerMarker) {
			return true
		}
	}
	return false
}

// FieldInt reads a numeric custom field, 0 when absent or not a number.
func (it Item) FieldInt(key string) int {
	n, _ := strconv.Atoi(it.Fields[key])
	return n
}
