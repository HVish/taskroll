package taskroll

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// FormatMajor and FormatMinor are the newest tracker format this build
// reads and writes: the shape of taskroll.json and how the views render. A
// tracker names its format in taskroll.json, and a missing one means 1.0.
//
// The major covers anything an older build could get wrong: how the views
// render, what an existing key means, what the records may hold. A build
// refuses a tracker with a newer major. A build renders every major it
// supports exactly as that major always rendered, so upgrading taskroll
// leaves a tracker's files as they were; moving a tracker to a newer major is
// a deliberate step, never a side effect of a write.
//
// The minor covers optional settings keys that change no generated file and
// whose absence keeps the behaviour before them (settingsKeySince). A build
// reads a tracker with a newer minor of its own major, ignores the keys it
// does not know, and warns: its output is the same, it only lacks the
// feature. That is safe because no command rewrites taskroll.json, which
// TestNewerMinorSettingsSurviveEveryWrite holds every command to.
const (
	FormatMajor = 1
	FormatMinor = 0
)

// FormatVersion is the newest major format.
//
// Deprecated: use FormatMajor, or CurrentFormat for the minor as well.
const FormatVersion = FormatMajor

// CurrentFormat is the newest format this build reads and writes.
var CurrentFormat = Format{FormatMajor, FormatMinor}

// settingsKeySince names the minor of FormatMajor that introduced each
// optional settings key, as a dotted path such as "epic.close_titles". A
// file that sets one must declare at least that minor, so a build that
// predates the key reads the file and warns instead of refusing it.
var settingsKeySince = map[string]int{}

// Format is a tracker format, MAJOR.MINOR. In taskroll.json it is a number
// while the minor is 0, which every build before minors existed reads, and a
// "MAJOR.MINOR" string otherwise.
type Format struct {
	Major, Minor int
}

func (f Format) String() string { return fmt.Sprintf("%d.%d", f.Major, f.Minor) }

// Less reports whether f is older than g.
func (f Format) Less(g Format) bool {
	return f.Major < g.Major || f.Major == g.Major && f.Minor < g.Minor
}

// NewerMinor reports whether f is this build's major with a minor it does
// not know: readable, but with settings this build ignores.
func (f Format) NewerMinor() bool { return f.Major == FormatMajor && f.Minor > FormatMinor }

// formatShape is MAJOR or MAJOR.MINOR, without leading zeros, so each
// format has one spelling.
var formatShape = regexp.MustCompile(`^([1-9][0-9]*)(?:\.(0|[1-9][0-9]*))?$`)

func (f *Format) UnmarshalJSON(raw []byte) error {
	// A bare number with a fraction is refused: 1.10 and 1.1 are one JSON
	// number but two formats, so a minor is always quoted.
	text := string(raw)
	if s, err := strconv.Unquote(text); err == nil && raw[0] == '"' {
		text = s
	} else if strings.Contains(text, ".") {
		return fmt.Errorf("format %s needs quotes, \"%s\", or 1.10 would read as 1.1", raw, raw)
	}
	m := formatShape.FindStringSubmatch(text)
	if m == nil {
		return fmt.Errorf("format %s is not MAJOR or \"MAJOR.MINOR\", such as 1 or \"1.2\"", raw)
	}
	f.Major, _ = strconv.Atoi(m[1])
	f.Minor = 0 // an absent minor
	if m[2] != "" {
		f.Minor, _ = strconv.Atoi(m[2])
	}
	return nil
}

// UpgradeCommand installs the latest release.
const UpgradeCommand = "go install github.com/hvish/taskroll/cmd/taskroll@latest"

// NewerError is a file written in a format or record schema newer than this
// build supports. It is reported before the file is parsed, so an older
// binary on another machine says what to do instead of failing on whatever
// key the newer one added.
type NewerError struct {
	Path      string
	What      string // "format" or "record schema"
	Have      int    // a format's major, or a record schema
	Supported int
}

func (e *NewerError) Error() string {
	return fmt.Sprintf("%s: written in taskroll %s %d; this build supports up to %d. Upgrade with: %s", e.Path, e.What, e.Have, e.Supported, UpgradeCommand)
}

// decodeStrict decodes one JSON value and refuses unknown keys, because the
// next write would otherwise drop them. An unknown key is most often one a
// newer taskroll added, so the error says how to upgrade. The match is on
// encoding/json's message, which TestUnknownKeySuggestsAnUpgrade pins.
func decodeStrict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err != nil && strings.HasPrefix(err.Error(), "json: unknown field") {
		return fmt.Errorf("%w; if a newer taskroll wrote it, upgrade with: %s", err, UpgradeCommand)
	}
	return err
}
