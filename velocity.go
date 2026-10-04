package taskroll

import (
	"sort"
	"time"
)

// Week is one ISO week (Monday to Sunday) of throughput.
type Week struct {
	Start   string  `json:"start"` // the Monday, YYYY-MM-DD
	Created int     `json:"created"`
	Done    int     `json:"done"`
	Points  float64 `json:"points"` // size points of the items done
	Dropped int     `json:"dropped"`
}

// Velocity reports the last n weeks up to and including the week of now.
//
// Done counts by the ship date (Done), which may be backdated to a merge;
// created and dropped count by their audit stamps, which records migrated
// from markdown do not have, so weeks before the migration undercount them.
func Velocity(files []*File, n int, now time.Time, points map[string]float64) []Week {
	monday := func(t time.Time) time.Time {
		t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		return t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7))
	}
	first := monday(now).AddDate(0, 0, -7*(n-1))
	weeks := make([]Week, n)
	for i := range weeks {
		weeks[i].Start = first.AddDate(0, 0, 7*i).Format("2006-01-02")
	}
	slot := func(t time.Time) int {
		d := int(monday(t).Sub(first).Hours() / (24 * 7))
		if d < 0 || d >= n {
			return -1
		}
		return d
	}
	for _, f := range files {
		for _, it := range f.Items {
			if t, err := time.Parse(time.RFC3339, it.CreatedAt); err == nil {
				if i := slot(t); i >= 0 {
					weeks[i].Created++
				}
			}
			if t, err := time.Parse("2006-01-02", it.Done); err == nil && it.Status == StatusDone {
				if i := slot(t); i >= 0 {
					weeks[i].Done++
					weeks[i].Points += points[it.Size]
				}
			}
			if t, err := time.Parse(time.RFC3339, it.ClosedAt); err == nil && it.Status == StatusDropped {
				if i := slot(t); i >= 0 {
					weeks[i].Dropped++
				}
			}
		}
	}
	return weeks
}

// CycleTimes returns, in days and ascending, the time from creation to close
// of every done item that carries both stamps.
func CycleTimes(files []*File) []float64 {
	var out []float64
	for _, f := range files {
		for _, it := range f.Items {
			if it.Status != StatusDone {
				continue
			}
			c, err1 := time.Parse(time.RFC3339, it.CreatedAt)
			d, err2 := time.Parse(time.RFC3339, it.ClosedAt)
			if err1 == nil && err2 == nil {
				out = append(out, d.Sub(c).Hours()/24)
			}
		}
	}
	sort.Float64s(out)
	return out
}
