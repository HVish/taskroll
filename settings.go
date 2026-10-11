package taskroll

import (
	"bytes"
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
	// Format is the major of the tracker format the directory is written
	// in, and FormatMinor its minor; see FormatMajor. A settings file
	// without a format is 1.0. Version returns the two together.
	Format      int `json:"format,omitempty"`
	FormatMinor int `json:"-"`
	// Banner is the line under a generated file's heading that says it is
	// generated. %s becomes the data file's name.
	Banner string `json:"banner,omitempty"`
	// BrowseURL is where this tracker directory can be read in a repository
	// browser, such as https://github.com/org/repo/blob/main/docs/tasks.
	// When set, every item has a link that lands on it, for docs to cite.
	BrowseURL string     `json:"browse_url,omitempty"`
	Sizes     []SizeSpec `json:"sizes,omitempty"`
	// FiscalYearStart is the month, 1 to 12, the financial year opens in,
	// which burndown's quarter and year periods count from. Zero is
	// January. Format 1.1.
	FiscalYearStart int      `json:"fiscal_year_start,omitempty"`
	Epic            EpicSpec `json:"epic"`
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
		Format: FormatMajor,
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
	return parseSettings(path, raw)
}

// parseSettings reads the settings file at path from raw.
func parseSettings(path string, raw []byte) (Settings, error) {
	// The format is read on its own first: a newer major may carry keys
	// this build would misread, and the useful answer is to upgrade. A newer
	// minor only adds optional keys, so it is read with them ignored.
	var peek struct {
		Format Format `json:"format"`
	}
	if json.Unmarshal(raw, &peek) == nil && peek.Format.Major > FormatMajor {
		return Settings{}, &NewerError{Path: path, What: "format", Have: peek.Format.Major, Supported: FormatMajor}
	}
	// The format is decoded whole here, where it shadows Settings.Format.
	var doc struct {
		Settings
		Format *Format `json:"format"`
	}
	decode := decodeStrict
	if peek.Format.NewerMinor() {
		decode = func(raw []byte, v any) error {
			if err := noDuplicateKeys(raw); err != nil {
				return err
			}
			return json.Unmarshal(raw, v)
		}
	}
	if err := decode(raw, &doc); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", path, err)
	}
	s := doc.Settings
	s.Format = 1
	if doc.Format != nil {
		s.Format, s.FormatMinor = doc.Format.Major, doc.Format.Minor
	}
	def := DefaultSettings()
	if err := keysNeedTheirFormat(raw, s.Version()); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", path, err)
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
	if s.Format < 0 || s.Format > FormatMajor {
		return fmt.Errorf("format %d is not one this build supports (1 to %d)", s.Format, FormatMajor)
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
	if s.FiscalYearStart < 0 || s.FiscalYearStart > 12 {
		return fmt.Errorf("fiscal_year_start %d is not a month, 1 to 12", s.FiscalYearStart)
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

// Version is the tracker format, major and minor. Zero, as in settings built
// in code, is 1.0.
func (s Settings) Version() Format {
	if s.Format == 0 {
		return Format{1, 0}
	}
	return Format{s.Format, s.FormatMinor}
}

// keysNeedTheirFormat refuses a settings key newer than the format the file
// declares. Declaring it is what lets a build that predates the key read the
// file and warn, where it would otherwise refuse the key as unknown.
func keysNeedTheirFormat(raw []byte, f Format) error {
	if len(minorSettings) == 0 {
		return nil
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	for _, k := range minorSettings {
		need := Format{FormatMajor, k.Minor}
		if hasPath(doc, strings.Split(k.Path, ".")) && f.Less(need) {
			return fmt.Errorf("%s needs \"format\": %q or later, so that older builds know to ignore it", k.Path, need.String())
		}
	}
	return nil
}

func hasPath(doc map[string]any, path []string) bool {
	v, ok := doc[path[0]]
	if !ok || len(path) == 1 {
		return ok
	}
	sub, isObj := v.(map[string]any)
	return isObj && hasPath(sub, path[1:])
}

// settingsFile is the settings file with every key present, in file order:
// what init writes, so the knobs a project can turn are in front of it, and
// what config show lists. TestSettingsFileHasEveryKey keeps it in step with
// Settings.
type settingsFile struct {
	Format          Format           `json:"format"`
	Banner          string           `json:"banner"`
	BrowseURL       string           `json:"browse_url"`
	Sizes           []SizeSpec       `json:"sizes"`
	FiscalYearStart *int             `json:"fiscal_year_start,omitempty"`
	Epic            epicSettingsFile `json:"epic"`
	Collections     []CollectionSpec `json:"collections"`
}

type epicSettingsFile struct {
	Legend      string       `json:"legend"`
	Fields      []FieldSpec  `json:"fields"`
	Markers     []MarkerSpec `json:"markers"`
	CloseTitles bool         `json:"close_titles"`
}

// fullSettings spells out s at format f. A key newer than f is left out,
// since a file at f may not set it; zero values become their defaults.
func fullSettings(s Settings, f Format) settingsFile {
	out := settingsFile{
		Format: f, Banner: s.Banner, BrowseURL: s.BrowseURL, Sizes: s.Sizes,
		Epic:        epicSettingsFile{Legend: s.Epic.Legend, Fields: s.Epic.Fields, Markers: s.Epic.Markers, CloseTitles: s.Epic.CloseTitles},
		Collections: s.Collections,
	}
	if f.Minor >= 1 {
		fy := max(s.FiscalYearStart, 1)
		out.FiscalYearStart = &fy
	}
	// Empty lists are written as [] rather than null, as a person would.
	if out.Sizes == nil {
		out.Sizes = []SizeSpec{}
	}
	if out.Epic.Fields == nil {
		out.Epic.Fields = []FieldSpec{}
	}
	if out.Epic.Markers == nil {
		out.Epic.Markers = []MarkerSpec{}
	}
	if out.Collections == nil {
		out.Collections = []CollectionSpec{}
	}
	return out
}

// DefaultSettingsFile is a settings file at format f with every key this
// build knows at that format, each at its default.
func DefaultSettingsFile(f Format) ([]byte, error) {
	return marshalSettings(fullSettings(DefaultSettings(), f))
}

// SettingEntry is one setting as it applies to a tracker.
type SettingEntry struct {
	Key   string          `json:"key"` // dotted for a nested key, such as "epic.legend"
	Value json.RawMessage `json:"value"`
	// Set is whether the settings file gives the value, and Default
	// whether the value is the default, so init's file, which sets every
	// key to its default, still shows what a project changed.
	Set     bool `json:"set"`
	Default bool `json:"default"`
	// Needs is the format a file must declare to set the key, when the
	// tracker's is older.
	Needs *Format `json:"needs,omitempty"`
}

// SettingEntries lists every setting this build knows, in file order, with
// the value in effect for the tracker in trackerDir, and its format.
func SettingEntries(trackerDir string) ([]SettingEntry, Format, error) {
	s, err := LoadSettings(trackerDir)
	if err != nil {
		return nil, Format{}, err
	}
	var doc map[string]any
	raw, err := os.ReadFile(filepath.Join(trackerDir, SettingsFile))
	if err == nil {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, Format{}, err
		}
	} else if !os.IsNotExist(err) {
		return nil, Format{}, err
	}
	have := s.Version()
	top, err := flatSettings(fullSettings(s, CurrentFormat))
	if err != nil {
		return nil, Format{}, err
	}
	def, err := flatSettings(fullSettings(DefaultSettings(), CurrentFormat))
	if err != nil {
		return nil, Format{}, err
	}
	defaults := map[string]string{}
	for _, kv := range def {
		defaults[kv.Key] = string(kv.Value)
	}
	var out []SettingEntry
	for _, kv := range top {
		e := SettingEntry{Key: kv.Key, Value: kv.Value, Set: hasPath(doc, strings.Split(kv.Key, ".")), Default: defaults[kv.Key] == string(kv.Value)}
		for _, k := range minorSettings {
			if k.Path == kv.Key && have.Less(Format{FormatMajor, k.Minor}) {
				e.Needs = &Format{FormatMajor, k.Minor}
			}
		}
		out = append(out, e)
	}
	return out, have, nil
}

// flatSettings lists a settings file's keys in file order, the epic's as
// "epic.<key>", leaving out the format.
func flatSettings(f settingsFile) (orderedObject, error) {
	// Not escaped for HTML: a banner is an HTML comment, shown as written.
	var full bytes.Buffer
	enc := json.NewEncoder(&full)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	var top, out orderedObject
	if err := json.Unmarshal(full.Bytes(), &top); err != nil {
		return nil, err
	}
	for _, kv := range top {
		switch kv.Key {
		case "format":
		case "epic":
			var epic orderedObject
			if err := json.Unmarshal(kv.Value, &epic); err != nil {
				return nil, err
			}
			for _, e := range epic {
				e.Key = "epic." + e.Key
				out = append(out, e)
			}
		default:
			out = append(out, kv)
		}
	}
	return out, nil
}

// orderedObject is a JSON object's members in the order they are written.
type orderedObject []struct {
	Key   string
	Value json.RawMessage
}

func (o *orderedObject) UnmarshalJSON(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return fmt.Errorf("not a JSON object")
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return err
		}
		*o = append(*o, struct {
			Key   string
			Value json.RawMessage
		}{k.(string), v})
	}
	return nil
}
