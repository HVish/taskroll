package taskroll

import (
	"fmt"
	"sort"
	"strings"
)

// Completion is one finished task.
type Completion struct {
	ID   string
	Date string // YYYY-MM-DD, the date it shipped
	Epic string
}

// MissingStamp names a completed task with no date on it.
type MissingStamp struct {
	ID   string
	Epic string
}

// ScanCompletions reads every finished task in a live epic, and reports any
// that carry no ship date so the gate can refuse them: a finished task with
// no date is invisible to the burndown, and the gap only shows up as a wrong
// chart months later.
//
// The date is stamped rather than derived from git because git records when
// the record changed, and a tracker-wide restructure rewrites every record
// at once.
func (p *Project) ScanCompletions() ([]Completion, []MissingStamp, error) {
	s, err := p.Load()
	if err != nil {
		return nil, nil, err
	}
	var done []Completion
	var missing []MissingStamp
	for _, f := range s.LiveEpics() {
		for _, it := range f.Items {
			if it.Status != StatusDone {
				continue
			}
			if it.Done == "" {
				missing = append(missing, MissingStamp{ID: it.ID, Epic: EpicFileName(f)})
				continue
			}
			done = append(done, Completion{ID: it.ID, Date: it.Done, Epic: EpicFileName(f)})
		}
	}
	return done, missing, nil
}

// BurndownRow is one date on which work completed.
type BurndownRow struct {
	Date       string
	Completed  int // finished on this date
	Cumulative int // finished on or before it
	Remaining  int // open tasks left after it
	IDs        []string
}

// Burndown collapses the completion stamps into a cumulative series.
//
// Remaining counts against the work known today, so it is a burn-down of the
// current backlog rather than of the backlog as it stood at each date — tasks
// added later were not visible then. That makes the trend honest and the
// absolute early values approximate; a tracker that only ever grows cannot
// reconstruct its own past scope.
func (p *Project) Burndown() ([]BurndownRow, error) {
	done, _, err := p.ScanCompletions()
	if err != nil {
		return nil, err
	}
	s, err := p.Load()
	if err != nil {
		return nil, err
	}
	open := 0
	for _, f := range s.LiveEpics() {
		for _, it := range f.Items {
			if it.Open() {
				open++
			}
		}
	}

	byDate := map[string][]string{}
	for _, c := range done {
		byDate[c.Date] = append(byDate[c.Date], c.ID)
	}
	dates := make([]string, 0, len(byDate))
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Strings(dates) // ISO dates sort chronologically as text

	total := len(done) + open
	rows := make([]BurndownRow, 0, len(dates))
	cum := 0
	for _, d := range dates {
		ids := byDate[d]
		sort.Strings(ids)
		cum += len(ids)
		rows = append(rows, BurndownRow{
			Date: d, Completed: len(ids), Cumulative: cum,
			Remaining: total - cum, IDs: ids,
		})
	}
	return rows, nil
}

// RenderBurndown formats the series for a terminal.
func (p *Project) RenderBurndown() (string, error) {
	rows, err := p.Burndown()
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "no completed tasks carry a date yet\n", nil
	}

	var b strings.Builder
	total := rows[len(rows)-1].Cumulative + rows[len(rows)-1].Remaining
	fmt.Fprintf(&b, "%-12s %5s %6s %6s  %s\n", "DATE", "DONE", "CUM", "LEFT", "")
	for _, r := range rows {
		// Bar width is scaled to the total so the shape is comparable between
		// runs as the backlog grows.
		w := r.Cumulative * 40 / max(total, 1)
		fmt.Fprintf(&b, "%-12s %5d %6d %6d  %s\n",
			r.Date, r.Completed, r.Cumulative, r.Remaining, strings.Repeat("█", w))
	}
	fmt.Fprintf(&b, "\n%d of %d complete (%d%%)\n",
		rows[len(rows)-1].Cumulative, total, rows[len(rows)-1].Cumulative*100/max(total, 1))
	return b.String(), nil
}
