package taskroll

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func burndownProject(t *testing.T) *Project {
	t.Helper()
	dir := t.TempDir()
	writeData(t, dir, "epic-1-x.jsonl", epicRec+
		`{"id":"A-001","type":"task","status":"done","title":"a","done":"2026-06-30"}`+"\n"+
		`{"id":"A-002","type":"task","status":"done","title":"b","done":"2026-07-01"}`+"\n"+
		`{"id":"A-003","type":"task","status":"done","title":"c","done":"2026-07-05"}`+"\n"+
		`{"id":"A-004","type":"task","status":"done","title":"d","done":"2026-07-21"}`+"\n"+
		`{"id":"A-005","type":"task","status":"todo","title":"e"}`+"\n"+
		`{"id":"A-006","type":"task","status":"todo","title":"f"}`+"\n")
	return &Project{Dir: dir, Settings: DefaultSettings()}
}

type bRow struct {
	Date                        string
	Done, Cumulative, Remaining int
}

func rowsOf(rep *BurndownReport) []bRow {
	var out []bRow
	for _, r := range rep.Rows {
		out = append(out, bRow{r.Date, r.Completed, r.Cumulative, r.Remaining})
	}
	return out
}

func TestBurndownGroups(t *testing.T) {
	p := burndownProject(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		o    BurndownOptions
		want []bRow
	}{
		{BurndownOptions{}, []bRow{
			{"2026-06-30", 1, 1, 5}, {"2026-07-01", 1, 2, 4}, {"2026-07-05", 1, 3, 3}, {"2026-07-21", 1, 4, 2}}},
		// 2026-06-30 and 2026-07-05 share the week of Monday 2026-06-29; the
		// weeks with nothing done still get a row, up to the week of now.
		{BurndownOptions{GroupBy: GroupWeek}, []bRow{
			{"2026-06-29", 3, 3, 3}, {"2026-07-06", 0, 3, 3}, {"2026-07-13", 0, 3, 3},
			{"2026-07-20", 1, 4, 2}, {"2026-07-27", 0, 4, 2}, {"2026-08-03", 0, 4, 2}, {"2026-08-10", 0, 4, 2}}},
		{BurndownOptions{GroupBy: GroupMonth}, []bRow{
			{"2026-06", 1, 1, 5}, {"2026-07", 3, 4, 2}, {"2026-08", 0, 4, 2}}},
		// A window counts from its start and leaves out what came after it.
		{BurndownOptions{GroupBy: GroupMonth, From: "2026-07-01", To: "2026-07-31"}, []bRow{
			{"2026-07", 3, 3, 2}}},
		{BurndownOptions{GroupBy: GroupWeek, From: "2026-07-01", To: "2026-07-13"}, []bRow{
			{"2026-06-29", 2, 2, 3}, {"2026-07-06", 0, 2, 3}, {"2026-07-13", 0, 2, 3}}},
		{BurndownOptions{From: "2026-07-02", To: "2026-07-20"}, []bRow{
			{"2026-07-05", 1, 1, 3}}},
	} {
		c.o.Now = now
		rep, err := p.Burndown(c.o)
		if err != nil {
			t.Fatal(err)
		}
		if got := rowsOf(rep); !slices.Equal(got, c.want) {
			t.Errorf("%+v:\nhave %v\nwant %v", c.o, got, c.want)
		}
	}
}

func TestBurndownRefuses(t *testing.T) {
	p := burndownProject(t)
	for _, o := range []BurndownOptions{
		{GroupBy: "year"}, {From: "2026-02-30"}, {To: "July"}, {From: "2026-08-01", To: "2026-07-01"},
	} {
		if _, err := p.Burndown(o); err == nil {
			t.Errorf("%+v: want an error", o)
		}
	}
}

func TestRenderBurndownWindow(t *testing.T) {
	p := burndownProject(t)
	s, err := p.RenderBurndown(BurndownOptions{GroupBy: GroupMonth, From: "2026-07-01", To: "2026-07-31"})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"MONTH", "3 completed from 2026-07-01 to 2026-07-31, of 5 open when it began (60%)", "4 of 6 complete (66%)"} {
		if !strings.Contains(s, w) {
			t.Errorf("want %q in\n%s", w, s)
		}
	}
	s, err = p.RenderBurndown(BurndownOptions{GroupBy: GroupWeek, From: "2026-09-01", To: "2026-09-30"})
	if err != nil || s != "no task completed from 2026-09-01 to 2026-09-30\n" {
		t.Errorf("an empty window: %q, %v", s, err)
	}
}

func TestPeriod(t *testing.T) {
	now := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name     string
		fy       time.Month
		from, to string
	}{
		{"month", 1, "2026-02-01", "2026-02-28"},
		{"last-month", 1, "2026-01-01", "2026-01-31"},
		{"quarter", 1, "2026-01-01", "2026-03-31"},
		{"last-quarter", 1, "2025-10-01", "2025-12-31"},
		{"fy", 1, "2026-01-01", "2026-12-31"},
		{"last-fy", 1, "2025-01-01", "2025-12-31"},
		// A year opening in April: February is in its fourth quarter.
		{"quarter", 4, "2026-01-01", "2026-03-31"},
		{"fy", 4, "2025-04-01", "2026-03-31"},
		{"last-fy", 4, "2024-04-01", "2025-03-31"},
		// One opening in February, so quarters are not calendar ones.
		{"quarter", 2, "2026-02-01", "2026-04-30"},
		{"last-quarter", 2, "2025-11-01", "2026-01-31"},
		{"fy", 10, "2025-10-01", "2026-09-30"},
	} {
		from, to, err := Period(c.name, now, c.fy)
		if err != nil || from != c.from || to != c.to {
			t.Errorf("%s from month %d: have %s..%s %v, want %s..%s", c.name, c.fy, from, to, err, c.from, c.to)
		}
	}
	for _, bad := range []string{"week", "next-month", ""} {
		if _, _, err := Period(bad, now, 1); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
	if _, _, err := Period("fy", now, 13); err == nil {
		t.Error("month 13: want an error")
	}
}
