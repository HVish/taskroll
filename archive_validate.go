package taskroll

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ArchiveProblem is one malformed archive record, located for a human.
type ArchiveProblem struct {
	File string
	Line int // 1-indexed
	Msg  string
}

func (p ArchiveProblem) String() string {
	return fmt.Sprintf("%s:%d: %s", p.File, p.Line, p.Msg)
}

var (
	isoDay       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	quarterFile  = regexp.MustCompile(`^(\d{4})-Q([1-4])\.jsonl$`)
	knownKinds   = map[string]bool{"debt": true, "opportunity": true, "watch": true, "epic": true}
	requiredJSON = []string{"id", "kind", "archived_at", "archived_on", "source"}
)

// ValidateArchive parses every archive record and reports what is wrong.
//
// The archive is append-only and never rewritten, so a malformed line is
// permanent — and it would otherwise surface only when RETIRED.md next
// regenerates, which is loud but late. Validating at gate time catches it in
// the change that wrote it, while the fix is still cheap.
func ValidateArchive(tasksDir string) ([]ArchiveProblem, error) {
	paths, err := filepath.Glob(filepath.Join(tasksDir, "archive", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	var problems []ArchiveProblem
	seen := map[string]string{} // id -> "file:line" it was first archived at

	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		base := filepath.Base(p)
		add := func(line int, format string, a ...any) {
			problems = append(problems, ArchiveProblem{File: base, Line: line, Msg: fmt.Sprintf(format, a...)})
		}

		fileYear, fileQuarter := "", 0
		if m := quarterFile.FindStringSubmatch(base); m != nil {
			fileYear = m[1]
			fileQuarter, _ = strconv.Atoi(m[2])
		} else {
			add(0, "filename is not YYYY-QN.jsonl, so its records have no quarter to belong to")
		}

		for i, line := range strings.Split(string(raw), "\n") {
			n := i + 1
			if strings.TrimSpace(line) == "" {
				// A trailing newline is normal; a blank line in the middle is
				// not, but it costs nothing and breaks nothing, so allow both.
				continue
			}

			var rec ArchiveRecord
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				add(n, "not valid JSON: %v", err)
				continue
			}

			// Required fields, checked generically so the message names the key.
			var generic map[string]json.RawMessage
			_ = json.Unmarshal([]byte(line), &generic)
			for _, k := range requiredJSON {
				if v, ok := generic[k]; !ok || string(v) == `""` || string(v) == "null" {
					add(n, "missing required field %q", k)
				}
			}

			if rec.Kind != "" && !knownKinds[rec.Kind] {
				add(n, "unknown kind %q", rec.Kind)
			}
			if rec.ArchivedOn != "" && !isoDay.MatchString(rec.ArchivedOn) {
				add(n, "archived_on %q is not YYYY-MM-DD", rec.ArchivedOn)
			}

			// A record must sit in the quarter file its date belongs to,
			// otherwise a lookup by quarter silently misses it.
			if fileQuarter > 0 && isoDay.MatchString(rec.ArchivedOn) {
				y := rec.ArchivedOn[:4]
				mon, _ := strconv.Atoi(rec.ArchivedOn[5:7])
				if q := (mon-1)/3 + 1; y != fileYear || q != fileQuarter {
					add(n, "archived_on %s belongs in %s-Q%d.jsonl", rec.ArchivedOn, y, q)
				}
			}

			// The same ID archived twice means a sweep ran against a source
			// that was already swept — the bug the tombstone guard prevents.
			if rec.ID != "" {
				where := fmt.Sprintf("%s:%d", base, n)
				if first, dup := seen[rec.ID]; dup {
					add(n, "duplicate record for %s (first archived at %s)", rec.ID, first)
					continue
				}
				seen[rec.ID] = where
			}
		}
	}
	return problems, nil
}
