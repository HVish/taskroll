package taskroll

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// SettingsFile sits in the tracker directory and defines the tracker: what
// its records mean and how they render. It travels with the data, so a
// tracker directory moved or copied elsewhere still describes itself.
// (ConfigFile, at the repository root, only says where the directory is.)
const SettingsFile = "taskroll.json"

// Settings is a tracker's definition. Everything a project decides about its
// tracker lives here rather than in code: the sizes and what they are worth,
// the fields a task line shows, the kinds of entry kept beside the epics and
// the vocabulary their fields take.
//
// The workflow (Statuses) is deliberately not configurable. Merging treats a
// status and its stamps as one unit, velocity and burndown count done items,
// and every view reads open and closed from it; a project-defined status
// would mean something to none of them.
type Settings struct {
	// Format is the tracker format the directory is written in; see
	// FormatVersion. Zero, a settings file without it, means 1.
	Format int `json:"format,omitempty"`
	// Banner is the line under a generated file's heading that says it is
	// generated. %s becomes the data file's name.
	Banner string `json:"banner,omitempty"`
	// BrowseURL is where this tracker directory can be read in a repository
	// browser, such as https://github.com/org/repo/blob/main/docs/tasks.
	// When set, every item has a link that lands on it, for docs to cite.
	BrowseURL string     `json:"browse_url,omitempty"`
	Sizes     []SizeSpec `json:"sizes,omitempty"`
	Epic      EpicSpec   `json:"epic"`
	// Collections are the kinds of entry kept one file per entry beside the
	// epics, such as debt.
	Collections []CollectionSpec `json:"collections,omitempty"`
}

// SizeSpec is one size an item may take and the points velocity counts it at.
type SizeSpec struct {
	Name   string  `json:"name"`
	Points float64 `json:"points"`
}

// EpicSpec is how an epic reads.
type EpicSpec struct {
	// Legend is the line a new epic carries under its heading.
	Legend  string       `json:"legend,omitempty"`
	Fields  []FieldSpec  `json:"fields,omitempty"`
	Markers []MarkerSpec `json:"markers,omitempty"`
	// CloseTitles ends every new task title with a full stop unless it
	// already ends in closing punctuation, so the task lines read as
	// sentences.
	CloseTitles bool `json:"close_titles,omitempty"`
}

// CollectionSpec is one kind of entry: the record type, the directory its
// files render into (and its records file, data/<dir>.jsonl), the series it
// is numbered in (empty for entries named by a slug), its frontmatter keys in
// file order, the fields every entry needs, and the values a field may take.
type CollectionSpec struct {
	Dir      string              `json:"dir"`
	Type     string              `json:"type"`
	Series   string              `json:"series,omitempty"`
	Keys     []string            `json:"keys"`
	Required []string            `json:"required,omitempty"`
	Values   map[string][]string `json:"values,omitempty"`
}

// DefaultBanner is the banner of a tracker that sets none.
const DefaultBanner = "<!-- Generated from ../data/%s. Change it with the tracker CLI, never by hand. -->"

// DefaultSettings is the definition of a tracker that has no settings file:
// S, M and L sizes, plain task lines, no collections.
func DefaultSettings() Settings {
	return Settings{
		Format: FormatVersion,
		Banner: DefaultBanner,
		Sizes:  []SizeSpec{{"S", 1}, {"M", 2.5}, {"L", 4.5}},
	}
}

var (
	sizeName   = regexp.MustCompile(`^[A-Z0-9]{1,4}$`)
	seriesName = regexp.MustCompile(`^[A-Z]+$`)
	// The keys a collection file always carries, in the fields of their own.
	collectionFixed = []string{"id", "title", "type", "status", "effort", "closed"}
)

// LoadSettings reads trackerDir's settings file, or returns the defaults
// when there is none. Unknown keys are refused, as in the records.
func LoadSettings(trackerDir string) (Settings, error) {
	path := filepath.Join(trackerDir, SettingsFile)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	// The format is read on its own first: a newer format may carry keys
	// this build would refuse, and the useful answer is to upgrade.
	var peek struct {
		Format int `json:"format"`
	}
	if json.Unmarshal(raw, &peek) == nil && peek.Format > FormatVersion {
		return Settings{}, &NewerError{Path: path, What: "format", Have: peek.Format, Supported: FormatVersion}
	}
	var s Settings
	if err := decodeStrict(raw, &s); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", path, err)
	}
	def := DefaultSettings()
	if s.Format == 0 {
		s.Format = 1
	}
	if s.Banner == "" {
		s.Banner = def.Banner
	}
	if len(s.Sizes) == 0 {
		s.Sizes = def.Sizes
	}
	if err := s.Validate(); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// Validate checks the settings are coherent: names are well-formed and
// unique, and every collection is a type the records know.
func (s Settings) Validate() error {
	if s.Format < 0 || s.Format > FormatVersion {
		return fmt.Errorf("format %d is not one this build supports (1 to %d)", s.Format, FormatVersion)
	}
	if strings.Count(s.Banner, "%s") != 1 || strings.ContainsAny(s.Banner, "\n\r") {
		return fmt.Errorf("banner must be one line holding %%s once, for the data file's name")
	}
	if s.BrowseURL != "" {
		u, err := url.Parse(s.BrowseURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("browse_url %q is not an http(s) address of the tracker directory", s.BrowseURL)
		}
	}
	seen := map[string]bool{}
	for _, z := range s.Sizes {
		if !sizeName.MatchString(z.Name) || seen[z.Name] {
			return fmt.Errorf("size %q is not a short upper-case name used once", z.Name)
		}
		seen[z.Name] = true
	}
	for _, f := range s.Epic.Fields {
		if !fieldKey.MatchString(f.Key) || strings.TrimSpace(f.Label) == "" {
			return fmt.Errorf("epic field %q needs a lower_snake_case key and a label", f.Key)
		}
		if _, err := regexp.Compile(f.Pattern); err != nil {
			return fmt.Errorf("epic field %q: pattern: %w", f.Key, err)
		}
	}
	for _, m := range s.Epic.Markers {
		if !labelShape.MatchString(m.Label) || strings.TrimSpace(m.Marker) == "" {
			return fmt.Errorf("epic marker %q needs a label and a marker", m.Label)
		}
	}
	dirs := map[string]bool{}
	for _, c := range s.Collections {
		if !slices.Contains([]string{TypeDebt, TypeOpportunity, TypeWatch}, c.Type) {
			return fmt.Errorf("collection %q: type %q is not debt, opportunity or watch", c.Dir, c.Type)
		}
		if !labelShape.MatchString(c.Dir) || c.Dir == "epics" || c.Dir == "data" || c.Dir == "archive" || dirs[c.Dir] {
			return fmt.Errorf("collection dir %q must be a new lower-case directory name", c.Dir)
		}
		dirs[c.Dir] = true
		if c.Series != "" && !seriesName.MatchString(c.Series) {
			return fmt.Errorf("collection %q: series %q is not upper-case letters", c.Dir, c.Series)
		}
		if c.Series != "" && !slices.Contains(c.Keys, "id") {
			return fmt.Errorf("collection %q is numbered, so its keys need id", c.Dir)
		}
		for _, k := range []string{"title", "type", "status"} {
			if !slices.Contains(c.Keys, k) {
				return fmt.Errorf("collection %q: keys must include %s", c.Dir, k)
			}
		}
		for _, k := range c.Keys {
			if !fieldKey.MatchString(k) {
				return fmt.Errorf("collection %q: key %q is not lower_snake_case", c.Dir, k)
			}
		}
		for _, k := range c.Required {
			if !slices.Contains(c.Keys, k) || slices.Contains(collectionFixed, k) {
				return fmt.Errorf("collection %q: required %q must be one of its field keys", c.Dir, k)
			}
		}
		for k := range c.Values {
			if !slices.Contains(c.Keys, k) || slices.Contains(collectionFixed, k) {
				return fmt.Errorf("collection %q: values given for %q, which is not one of its field keys", c.Dir, k)
			}
		}
	}
	return nil
}

// Size returns the spec of a size name.
func (s Settings) Size(name string) (SizeSpec, bool) {
	for _, z := range s.Sizes {
		if z.Name == name {
			return z, true
		}
	}
	return SizeSpec{}, false
}

// Points maps each size to the points velocity counts it at.
func (s Settings) Points() map[string]float64 {
	out := map[string]float64{}
	for _, z := range s.Sizes {
		out[z.Name] = z.Points
	}
	return out
}

// Collection returns the spec of an entry type.
func (s Settings) Collection(typ string) (CollectionSpec, bool) {
	for _, c := range s.Collections {
		if c.Type == typ {
			return c, true
		}
	}
	return CollectionSpec{}, false
}

// CollectionDir returns the spec of a collection directory.
func (s Settings) CollectionDir(dir string) (CollectionSpec, bool) {
	for _, c := range s.Collections {
		if c.Dir == dir {
			return c, true
		}
	}
	return CollectionSpec{}, false
}

// Markdown is the epic grammar these settings describe.
func (s Settings) Markdown() Markdown {
	return Markdown{Fields: s.Epic.Fields, Markers: s.Epic.Markers, ArchivedMarker: ArchivedMarker, Banner: s.Banner}
}

// ArchivedMarker flags an epic file whose content has moved to the archive.
// The file stays in place as a tombstone, so links to it keep working.
const ArchivedMarker = "<!-- archived -->"
