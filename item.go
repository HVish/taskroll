// Package taskroll is a work tracker stored as JSONL records in git.
//
// It knows nothing about the project it tracks. What a project decides (its
// sizes, the fields a task line shows, the collections kept beside its epics
// and their vocabularies) is read from taskroll.json in the tracker
// directory, and what a project renders beyond the records plugs in through
// Project's hooks.
package taskroll

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// SchemaVersion is written into every epic record. A reader that meets a
// higher version refuses the file rather than guessing at fields it does not
// know, because a silently dropped field is lost on the next write.
const SchemaVersion = 1

// Item types.
const (
	TypeEpic        = "epic"
	TypeTask        = "task"
	TypeBug         = "bug"
	TypeDebt        = "debt"
	TypeOpportunity = "opportunity"
	TypeWatch       = "watch"
)

// Statuses, in workflow order.
const (
	StatusTodo       = "todo"
	StatusInProgress = "in_progress"
	StatusInReview   = "in_review"
	StatusDone       = "done"
	StatusDropped    = "dropped"
)

var (
	Types    = []string{TypeEpic, TypeTask, TypeBug, TypeDebt, TypeOpportunity, TypeWatch}
	Statuses = []string{StatusTodo, StatusInProgress, StatusInReview, StatusDone, StatusDropped}
)

// Item is one tracker record. Field order here is the serialized key order,
// so it is also the order a reviewer reads a diff in: identity, then what the
// work is, then how it is scheduled, then prose.
type Item struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Schema int    `json:"schema,omitempty"` // epic records only
	Status string `json:"status,omitempty"`
	Title  string `json:"title"`

	Size     string `json:"size,omitempty"`
	SizeNote string `json:"size_note,omitempty"` // text between the size and the first field, e.g. "per card"
	Blocker  bool   `json:"blocker,omitempty"`

	// Depends and Closes hold one entry per comma-separated reference. An
	// entry is usually a bare ID; it may also be an ID with a parenthetical or
	// a prose condition no graph can evaluate, which is kept verbatim so it
	// keeps the task out of the ready queue.
	Depends []string `json:"depends,omitempty"`
	Closes  []string `json:"closes,omitempty"`

	// Notes are short free-text fields shown on the task line.
	Notes  []string          `json:"notes,omitempty"`
	Fields map[string]string `json:"fields,omitempty"` // project-defined, e.g. phase, track
	Labels []string          `json:"labels,omitempty"`
	Done   string            `json:"done,omitempty"` // YYYY-MM-DD the work shipped (the PR merge date)

	// Audit trail. Done is the date the work shipped and may be backdated to
	// a merge; ClosedAt is when the record was closed. Velocity reads the
	// first, cycle time the pair CreatedAt to ClosedAt. Records migrated from
	// markdown have neither creation field: that history was never kept.
	CreatedAt string `json:"created_at,omitempty"` // RFC 3339, UTC
	CreatedBy string `json:"created_by,omitempty"`
	ClosedAt  string `json:"closed_at,omitempty"` // RFC 3339, UTC; set on done or dropped
	ClosedBy  string `json:"closed_by,omitempty"`

	// Epic records: the markdown before and after the task list, verbatim.
	Intro string `json:"intro,omitempty"`
	Outro string `json:"outro,omitempty"`
	// Archived is the release an epic was archived at; its tasks then live
	// only in the quarterly archive and the rendered file is a tombstone.
	Archived     string `json:"archived,omitempty"`
	ArchivedFile string `json:"archived_file,omitempty"`

	Description string    `json:"description,omitempty"` // markdown
	Comments    []Comment `json:"comments,omitempty"`
}

// Comment is one dated remark on an item.
type Comment struct {
	Date   string `json:"date"` // YYYY-MM-DD
	Author string `json:"author,omitempty"`
	Body   string `json:"body"`
}

var (
	idShape    = regexp.MustCompile(`^[A-Za-z]+-[0-9]{3}(?:\.[0-9]+)?$`)
	slugID     = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	epicID     = regexp.MustCompile(`^epic-[0-9]+-[a-z0-9]+(?:-[a-z0-9]+)*$`)
	dayShape   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	labelShape = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	fieldKey   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// Validate checks one record on its own. Cross-record rules (duplicate IDs,
// references) are Check's job.
func (it Item) Validate() error {
	fail := func(format string, a ...any) error {
		return fmt.Errorf("%s: %s", it.ID, fmt.Sprintf(format, a...))
	}
	if !slices.Contains(Types, it.Type) {
		return fail("type %q is not one of %s", it.Type, strings.Join(Types, ", "))
	}
	if strings.TrimSpace(it.Title) == "" {
		return fail("empty title")
	}
	for _, s := range []string{it.Title, it.SizeNote} {
		if strings.ContainsAny(s, "\n\r") {
			return fail("title and size note are one line")
		}
	}
	if it.Type == TypeEpic {
		if !epicID.MatchString(it.ID) {
			return fail("an epic id is epic-N-slug")
		}
		if it.Schema == 0 || it.Schema > SchemaVersion {
			return fail("schema %d; this build reads up to %d", it.Schema, SchemaVersion)
		}
		return nil
	}
	// A slug id is for items that deliberately have no number, such as
	// a watch list: a number implies an owner and a due date.
	if !idShape.MatchString(it.ID) && !slugID.MatchString(it.ID) {
		return fail("id is not PREFIX-NNN or a lower-case slug")
	}
	if !slices.Contains(Statuses, it.Status) {
		return fail("status %q is not one of %s", it.Status, strings.Join(Statuses, ", "))
	}
	// Which sizes exist is the tracker's setting; a record only has to
	// carry one in the shape every setting uses.
	if it.Size != "" && !sizeName.MatchString(it.Size) {
		return fail("size %q is not a short upper-case name such as M", it.Size)
	}
	if it.Done != "" && !dayShape.MatchString(it.Done) {
		return fail("done %q is not YYYY-MM-DD", it.Done)
	}
	if it.Done != "" && it.Status != StatusDone {
		return fail("has a done date but status %s", it.Status)
	}
	for _, ts := range []string{it.CreatedAt, it.ClosedAt} {
		if ts != "" {
			if _, err := time.Parse(time.RFC3339, ts); err != nil {
				return fail("timestamp %q is not RFC 3339", ts)
			}
		}
	}
	if (it.ClosedAt != "" || it.ClosedBy != "") && it.Open() {
		return fail("is %s but carries a close stamp", it.Status)
	}
	for _, l := range it.Labels {
		if !labelShape.MatchString(l) {
			return fail("label %q is not lower-case words joined by hyphens", l)
		}
	}
	for k, v := range it.Fields {
		if !fieldKey.MatchString(k) {
			return fail("field name %q is not lower_snake_case", k)
		}
		if strings.ContainsAny(v, "\n\r") {
			return fail("field %s is one line", k)
		}
	}
	for _, list := range [][]string{it.Depends, it.Closes, it.Notes} {
		for _, s := range list {
			if strings.TrimSpace(s) == "" || strings.ContainsAny(s, "\n\r") || strings.Contains(s, " · ") {
				return fail("a reference or note is one non-empty line without ' · ': %q", s)
			}
		}
	}
	for _, c := range it.Comments {
		if !dayShape.MatchString(c.Date) || strings.TrimSpace(c.Body) == "" {
			return fail("a comment needs a YYYY-MM-DD date and a body")
		}
	}
	return nil
}

// HasLabel reports whether the item carries label l.
func (it Item) HasLabel(l string) bool { return slices.Contains(it.Labels, l) }

// Open reports whether the item still owes work.
func (it Item) Open() bool { return it.Status != StatusDone && it.Status != StatusDropped }

var leadingRef = regexp.MustCompile(`^([A-Za-z]+)-([0-9.]+)\b`)

// RefIDs splits references into those naming an item and prose conditions.
// known is the set of ID prefixes in use, so `epic-3` or `NFR-001` in a
// dependency reads as prose rather than as an item nobody can find.
func RefIDs(refs []string, known map[string]bool) (ids, prose []string) {
	for _, r := range refs {
		r = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(r), "."))
		if r == "" || r == "-" {
			continue
		}
		if m := leadingRef.FindStringSubmatch(r); m != nil && known[m[1]] {
			ids = append(ids, m[1]+"-"+strings.TrimSuffix(m[2], "."))
			continue
		}
		prose = append(prose, r)
	}
	return ids, prose
}

// Numbered reports whether id is in a numbered series rather than a slug.
func Numbered(id string) bool { return idShape.MatchString(id) }

// Prefix returns the ID series of an item ID: "SEC" for "SEC-011", and ""
// for a slug.
func Prefix(id string) string {
	if !Numbered(id) {
		return ""
	}
	if m := leadingRef.FindStringSubmatch(id); m != nil {
		return m[1]
	}
	return ""
}
