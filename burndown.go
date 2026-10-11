package taskroll

import (
	"fmt"
	"sort"
	"strings"
	"time"
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
	done, missing := scanCompletions(s)
	return done, missing, nil
}

func scanCompletions(s *Snapshot) ([]Completion, []MissingStamp) {
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
	return done, missing
}

// Burndown groupings.
const (
	GroupDay   = "day"
	GroupWeek  = "week" // ISO weeks, labelled by their Monday
	GroupMonth = "month"
)

// BurndownOptions narrows and buckets the series.
type BurndownOptions struct {
	GroupBy string // GroupDay (the default), GroupWeek or GroupMonth
	// From and To bound the window, YYYY-MM-DD and inclusive; empty is open.
	From, To string
	Now      time.Time // ends a week or month series; zero means time.Now
}

// BurndownRow is one period in which work completed.
type BurndownRow struct {
	Date       string // the day, the week's Monday, or YYYY-MM
	Completed  int    // finished in this period
	Cumulative int    // finished in the window up to the end of it
	Remaining  int    // open tasks left after it
	IDs        []string
}

// BurndownReport is the series and the totals it is measured against.
type BurndownReport struct {
	Rows   []BurndownRow
	Before int // finished before the window opens
	Total  int // finished plus open, the backlog as it stands today
}

// Burndown collapses the completion stamps into a cumulative series.
//
// Remaining counts against the work known today, so it is a burn-down of the
// current backlog rather than of the backlog as it stood at each date — tasks
// added later were not visible then. That makes the trend honest and the
// absolute early values approximate; a tracker that only ever grows cannot
// reconstruct its own past scope.
//
// Day rows list only the days something shipped. Week and month rows list
// every period from the window's start (or the first completion) to its end
// (or today), so a stall shows as empty rows rather than a missing one.
func (p *Project) Burndown(o BurndownOptions) (*BurndownReport, error) {
	bucket, err := bucketer(o.GroupBy)
	if err != nil {
		return nil, err
	}
	for _, d := range []string{o.From, o.To} {
		if _, err := time.Parse(time.DateOnly, d); d != "" && err != nil {
			return nil, fmt.Errorf("date %q is not a YYYY-MM-DD day", d)
		}
	}
	if o.From != "" && o.To != "" && o.From > o.To {
		return nil, fmt.Errorf("the window starts on %s, after it ends on %s", o.From, o.To)
	}
	s, err := p.Load()
	if err != nil {
		return nil, err
	}
	done, _ := scanCompletions(s)
	// Records check a ship date's shape, not that the day exists. One that
	// does not, 2026-02-30, would fall in no week, so it is refused here
	// rather than charted somewhere wrong.
	for _, c := range done {
		if _, err := time.Parse(time.DateOnly, c.Date); err != nil {
			return nil, fmt.Errorf("%s in %s: done %q is not a calendar day", c.ID, c.Epic, c.Date)
		}
	}
	rep := &BurndownReport{Total: len(done)}
	for _, f := range s.LiveEpics() {
		for _, it := range f.Items {
			if it.Open() {
				rep.Total++
			}
		}
	}

	// ISO dates and YYYY-MM labels sort chronologically as text.
	byKey := map[string][]string{}
	for _, c := range done {
		switch {
		case o.From != "" && c.Date < o.From:
			rep.Before++
		case o.To != "" && c.Date > o.To:
		default:
			k := bucket(c.Date)
			byKey[k] = append(byKey[k], c.ID)
		}
	}
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if o.GroupBy == GroupWeek || o.GroupBy == GroupMonth {
		keys = fillPeriods(keys, o, bucket)
	}

	cum := 0
	for _, k := range keys {
		ids := byKey[k]
		sort.Strings(ids)
		cum += len(ids)
		rep.Rows = append(rep.Rows, BurndownRow{
			Date: k, Completed: len(ids), Cumulative: cum,
			Remaining: rep.Total - rep.Before - cum, IDs: ids,
		})
	}
	return rep, nil
}

// bucketer returns the function that names the period a ship date falls in.
func bucketer(groupBy string) (func(day string) string, error) {
	switch groupBy {
	case "", GroupDay:
		return func(d string) string { return d }, nil
	case GroupWeek:
		return func(d string) string {
			t, _ := time.Parse(time.DateOnly, d)
			return t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7)).Format(time.DateOnly)
		}, nil
	case GroupMonth:
		return func(d string) string { return d[:7] }, nil
	}
	return nil, fmt.Errorf("group %q is not day, week or month", groupBy)
}

// fillPeriods returns every week or month from the window's start, or the
// first one with a completion, to its end, or today, or the last one with a
// completion if that is later.
func fillPeriods(keys []string, o BurndownOptions, bucket func(string) string) []string {
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	first, last := o.From, now.Format(time.DateOnly)
	if o.To != "" && o.To < last {
		last = o.To
	}
	if first == "" {
		if len(keys) == 0 {
			return nil
		}
		first = keys[0]
	}
	if first > last {
		return keys
	}
	// Step from the start of the first period, so none is skipped at the end.
	first = bucket(first)
	if o.GroupBy == GroupMonth {
		first += "-01"
	}
	var out []string
	for t, _ := time.Parse(time.DateOnly, first); t.Format(time.DateOnly) <= last; {
		k := bucket(t.Format(time.DateOnly))
		out = append(out, k)
		if o.GroupBy == GroupWeek {
			t = t.AddDate(0, 0, 7)
		} else {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		}
	}
	// A completion past today (a date stamped ahead) still gets its row.
	for _, k := range keys {
		if k > out[len(out)-1] {
			out = append(out, k)
		}
	}
	return out
}

// Periods are the reporting windows Period resolves.
var Periods = []string{"month", "quarter", "fy", "last-month", "last-quarter", "last-fy"}

// Period resolves a named reporting window around now into the first and
// last day it covers. Quarters are fiscal: they start from fyStart, the month
// the financial year opens in, so with January they are calendar quarters.
func Period(name string, now time.Time, fyStart time.Month) (from, to string, err error) {
	if fyStart < time.January || fyStart > time.December {
		return "", "", fmt.Errorf("financial year start %d is not a month, 1 to 12", fyStart)
	}
	unit, back := name, 0
	if rest, ok := strings.CutPrefix(name, "last-"); ok {
		unit, back = rest, 1
	}
	months := map[string]int{"month": 1, "quarter": 3, "fy": 12}[unit]
	if months == 0 {
		return "", "", fmt.Errorf("period %q is not one of %s", name, strings.Join(Periods, ", "))
	}
	// Months since the financial year opened, counted from a fixed origin so
	// the arithmetic needs no special case at the year boundary.
	since := (now.Year()*12 + int(now.Month()-1)) - int(fyStart-1)
	start := since - since%months - back*months + int(fyStart-1)
	first := time.Date(start/12, time.Month(start%12+1), 1, 0, 0, 0, 0, time.UTC)
	return first.Format(time.DateOnly), first.AddDate(0, months, -1).Format(time.DateOnly), nil
}

// RenderBurndown formats the series for a terminal.
func (p *Project) RenderBurndown(o BurndownOptions) (string, error) {
	rep, err := p.Burndown(o)
	if err != nil {
		return "", err
	}
	windowed := o.From != "" || o.To != ""
	if len(rep.Rows) == 0 || rep.Rows[len(rep.Rows)-1].Cumulative == 0 {
		if windowed {
			return fmt.Sprintf("no task completed %s\n", window(o)), nil
		}
		return "no completed tasks carry a date yet\n", nil
	}

	var b strings.Builder
	head := map[string]string{GroupWeek: "WEEK OF", GroupMonth: "MONTH"}[o.GroupBy]
	if head == "" {
		head = "DATE"
	}
	// Bars are scaled to the scope the window opened with, so the shape is
	// comparable between runs as the backlog grows.
	scope := max(rep.Total-rep.Before, 1)
	fmt.Fprintf(&b, "%-12s %5s %6s %6s  %s\n", head, "DONE", "CUM", "LEFT", "")
	for _, r := range rep.Rows {
		w := r.Cumulative * 40 / scope
		fmt.Fprintf(&b, "%-12s %5d %6d %6d  %s\n",
			r.Date, r.Completed, r.Cumulative, r.Remaining, strings.Repeat("█", w))
	}
	in := rep.Rows[len(rep.Rows)-1].Cumulative
	if windowed {
		fmt.Fprintf(&b, "\n%d completed %s, of %d open when it began (%d%%)",
			in, window(o), rep.Total-rep.Before, in*100/scope)
	}
	// Overall, everything finished up to the window's end.
	fmt.Fprintf(&b, "\n%d of %d complete (%d%%)\n",
		rep.Before+in, rep.Total, (rep.Before+in)*100/max(rep.Total, 1))
	return b.String(), nil
}

// window describes the bounds of a windowed series in words.
func window(o BurndownOptions) string {
	switch {
	case o.From != "" && o.To != "":
		return "from " + o.From + " to " + o.To
	case o.From != "":
		return "since " + o.From
	}
	return "up to " + o.To
}
