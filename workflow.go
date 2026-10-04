package taskroll

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Store writes a tracker directory. Every change runs the same way, under
// the directory's lock: load every file, apply the change, check the rules
// that span files, save what changed, then Render. Nothing reads a record
// outside the lock and writes it back inside, which is how two processes
// used to lose one another's writes.
type Store struct {
	Dir   string
	Actor string // who is acting; stamped as created_by, closed_by, comment author
	// Now is the clock every stamp reads; nil means time.Now.
	Now func() time.Time
	// Render regenerates whatever the project derives from the records. It
	// runs inside the lock, so a view never reflects half a change.
	Render func() error
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Stamp is the current time as the audit fields store it.
func (s *Store) Stamp() string { return s.now().UTC().Format(time.RFC3339) }

// Today is the local calendar date, as done dates and comments store it.
func (s *Store) Today() string { return s.now().Format("2006-01-02") }

// Mutate runs fn over every file under the lock. fn returns the files it
// changed or created; they are checked together with the rest and saved.
func (s *Store) Mutate(fn func(files []*File) ([]*File, error)) error {
	unlock, err := Lock(s.Dir)
	if err != nil {
		return err
	}
	defer unlock()
	files, err := Load(s.Dir)
	if err != nil {
		return err
	}
	changed, err := fn(files)
	if err != nil {
		return err
	}
	all := files
	for _, c := range changed {
		if !slices.ContainsFunc(files, func(f *File) bool { return filepath.Clean(f.Path) == filepath.Clean(c.Path) }) {
			all = append(all, c)
		}
	}
	if err := Check(all); err != nil {
		return err
	}
	for _, f := range changed {
		if err := f.Save(); err != nil {
			return err
		}
	}
	if s.Render != nil {
		return s.Render()
	}
	return nil
}

// Update applies change to the record with the given id. change returns an
// error to abort without writing. An archived epic's records do not change.
func (s *Store) Update(id string, change func(it *Item) error) (Item, error) {
	var out Item
	err := s.Mutate(func(files []*File) ([]*File, error) {
		f, i, ok := Find(files, id)
		if !ok {
			return nil, fmt.Errorf("no item %s", id)
		}
		if f.Epic.Archived != "" {
			return nil, fmt.Errorf("%s is archived at %s and no longer changes", f.Epic.ID, f.Epic.Archived)
		}
		it := &f.Epic
		if i >= 0 {
			it = &f.Items[i]
		}
		if err := change(it); err != nil {
			return nil, err
		}
		if err := it.Validate(); err != nil {
			return nil, err
		}
		out = *it
		return []*File{f}, nil
	})
	return out, err
}

// SetStatus moves an item through the workflow. Reaching done stamps the
// ship date (day, today when empty); reaching done or dropped stamps who
// closed it and when. Reopening clears all three, since a reopened item has
// neither shipped nor closed.
func (s *Store) SetStatus(id, status, day string) (Item, error) {
	if !slices.Contains(Statuses, status) {
		return Item{}, fmt.Errorf("status %q is not one of %s", status, strings.Join(Statuses, ", "))
	}
	if day == "" {
		day = s.Today()
	}
	if !dayShape.MatchString(day) {
		return Item{}, fmt.Errorf("date %q is not YYYY-MM-DD", day)
	}
	return s.Update(id, func(it *Item) error {
		if it.Type == TypeEpic {
			return fmt.Errorf("%s is an epic; its status is its items'", it.ID)
		}
		wasOpen := it.Open()
		it.Status = status
		it.Done = ""
		if status == StatusDone {
			it.Done = day
		}
		switch {
		case !it.Open() && wasOpen:
			it.ClosedAt, it.ClosedBy = s.Stamp(), s.Actor
		case it.Open():
			it.ClosedAt, it.ClosedBy = "", ""
		}
		return nil
	})
}

// Comment adds a dated remark by the acting user.
func (s *Store) Comment(id, body string) (Item, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Item{}, fmt.Errorf("a comment needs a body")
	}
	return s.Update(id, func(it *Item) error {
		it.Comments = append(it.Comments, Comment{Date: s.Today(), Author: s.Actor, Body: body})
		return nil
	})
}

// Created stamps a new record with when and by whom.
func (s *Store) Created(it *Item) {
	it.CreatedAt, it.CreatedBy = s.Stamp(), s.Actor
}
