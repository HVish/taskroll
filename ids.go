package taskroll

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	seriesShape = regexp.MustCompile(`^[A-Za-z]+$`)
	idParts     = regexp.MustCompile(`^([A-Za-z]+)-([0-9]{3}(?:\.[0-9]+)?)$`)
	boldID      = regexp.MustCompile(`\*\*([A-Za-z]+-[0-9.]+)\*\*`)
	jsonID      = regexp.MustCompile(`"?id"?:\s*"?([A-Za-z]+-[0-9.]+)"?`)
)

// UsedIDs collects every id the tracker has ever issued: every record
// (dropped ones included, since their ids stay spent), the quarterly archive,
// which holds archived epics' tasks and closed entries, and whatever the
// project's Reserved hook adds. Reusing an id would silently join two pieces
// of work in every view that joins on it.
func (p *Project) UsedIDs() (map[string]bool, error) {
	used := map[string]bool{}
	files, err := Load(p.Dir)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		for _, it := range f.Records() {
			used[it.ID] = true
		}
	}
	archives, err := filepath.Glob(filepath.Join(p.Dir, "archive", "*"))
	if err != nil {
		return nil, err
	}
	for _, path := range archives {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		for _, re := range []*regexp.Regexp{boldID, jsonID} {
			for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
				used[m[1]] = true
			}
		}
	}
	if p.Reserved != nil {
		more, err := p.Reserved()
		if err != nil {
			return nil, err
		}
		maps.Copy(used, more)
	}
	return used, nil
}

// NextID returns one past the highest whole number a series has ever issued.
// It does not fill gaps: an id that has been used is spent. It also counts
// past every id another branch or worktree holds, so two branches filing
// into one series at once get different numbers.
func (p *Project) NextID(series string) (string, error) {
	if !seriesShape.MatchString(series) {
		return "", fmt.Errorf("series %q is not a bare prefix such as R or FE", series)
	}
	series = strings.ToUpper(series)
	used, err := p.UsedIDs()
	if err != nil {
		return "", err
	}
	if !hasSeries(used, series) {
		return "", fmt.Errorf("series %q has no existing item to number from; file the first one with --id %s-001", series, series)
	}
	elsewhere, err := ElsewhereIDs(p.Dir)
	if err != nil {
		return "", err
	}
	maps.Copy(used, elsewhere)
	width, highest := 3, 0
	for id := range used {
		m := idParts.FindStringSubmatch(id)
		if m == nil || !strings.EqualFold(m[1], series) {
			continue
		}
		whole := strings.SplitN(m[2], ".", 2)[0]
		if n, err := strconv.Atoi(whole); err == nil && n > highest {
			highest, width = n, len(whole)
		}
	}
	return fmt.Sprintf("%s-%0*d", series, width, highest+1), nil
}

// nextOrFirst is NextID for a series a collection owns, which starts at 001.
func (p *Project) nextOrFirst(series string) (string, error) {
	used, err := p.UsedIDs()
	if err != nil {
		return "", err
	}
	if !hasSeries(used, series) {
		return series + "-001", nil
	}
	return p.NextID(series)
}

func hasSeries(ids map[string]bool, series string) bool {
	for id := range ids {
		if m := idParts.FindStringSubmatch(id); m != nil && strings.EqualFold(m[1], series) {
			return true
		}
	}
	return false
}

// MergeReserved is what an id the merge driver renumbers must not collide
// with: every id the working tree has issued and every id other branches and
// worktrees hold. Midway through a merge another records file can hold git's
// conflict markers, which Load refuses; the working tree is then read as
// text, which over-reserves at worst.
func (p *Project) MergeReserved() (map[string]bool, error) {
	used, err := p.UsedIDs()
	if err != nil {
		used = map[string]bool{}
		if err := scanIDs(p.Dir, used); err != nil {
			return nil, err
		}
		if p.Reserved != nil {
			more, err := p.Reserved()
			if err != nil {
				return nil, err
			}
			maps.Copy(used, more)
		}
	}
	elsewhere, err := ElsewhereIDs(p.Dir)
	if err != nil {
		return nil, err
	}
	maps.Copy(used, elsewhere)
	return used, nil
}
