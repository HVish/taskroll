package taskroll

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// UpgradeResult is what UpgradeFormat changed, or would change.
type UpgradeResult struct {
	Path     string
	From, To Format
	// Created is set when the tracker had no settings file: one is written
	// with every key at its default.
	Created bool
	// New are the settings the formats after From add that the file does
	// not set, so their defaults apply.
	New []SettingHelp
}

// SettingHelp names a setting and says what it does.
type SettingHelp struct {
	Key, Help string
}

// UpgradeFormat moves the tracker in trackerDir to format to. It changes
// the format in taskroll.json and nothing else: every other byte of the
// file, unknown keys included, stays as it was, and a key a new format adds
// is listed rather than written, so the diff is the one line that decides
// which builds can read the tracker. With dryRun it writes nothing.
//
// Within a major nothing else needs to move: a minor only adds optional
// settings that change no generated file. A step to a new major, which may
// re-encode records and views, belongs here when there is one.
func UpgradeFormat(trackerDir string, to Format, dryRun bool) (*UpgradeResult, error) {
	switch {
	case to.Major != FormatMajor:
		return nil, fmt.Errorf("this build upgrades within format %d only, not to %s", FormatMajor, to)
	case CurrentFormat.Less(to):
		return nil, fmt.Errorf("this build writes up to format %s, not %s; upgrade taskroll first: %s", CurrentFormat, to, UpgradeCommand)
	}
	if !dryRun {
		unlock, err := Lock(trackerDir)
		if err != nil {
			return nil, err
		}
		defer unlock()
	}
	path := filepath.Join(trackerDir, SettingsFile)
	res := &UpgradeResult{Path: path, From: Format{1, 0}, To: to}
	// A symlinked settings file is upgraded where it lives; renaming over
	// the link would replace it with a copy and leave the target behind.
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		res.Created = true
		if dryRun {
			return res, nil
		}
		out, err := DefaultSettingsFile(to)
		if err != nil {
			return nil, err
		}
		return res, WriteFileAtomic(path, append(out, '\n'))
	}
	if err != nil {
		return nil, err
	}
	before, err := parseSettings(path, raw)
	if err != nil {
		return nil, err
	}
	res.From = before.Version()
	switch {
	case CurrentFormat.Less(res.From):
		return nil, fmt.Errorf("%s is format %s, newer than this build's %s; upgrade taskroll instead: %s", path, res.From, CurrentFormat, UpgradeCommand)
	case to.Less(res.From):
		return nil, fmt.Errorf("%s is already format %s; upgrade does not move a tracker back to %s", path, res.From, to)
	case res.From == to:
		return res, nil
	}

	out, err := setFormat(raw, to)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// The edit must change the format and only the format.
	after, err := parseSettings(path, out)
	if err != nil {
		return nil, fmt.Errorf("%s: the upgraded file does not read back: %w", path, err)
	}
	moved := after.Version() == to
	before.Format, before.FormatMinor, after.Format, after.FormatMinor = 0, 0, 0, 0
	if !moved || !reflect.DeepEqual(before, after) {
		return nil, fmt.Errorf("%s: the upgrade would change more than the format", path)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	for _, k := range minorSettings {
		if k.Minor > res.From.Minor && k.Minor <= to.Minor && !hasPath(doc, strings.Split(k.Path, ".")) {
			res.New = append(res.New, SettingHelp{k.Path, k.Help})
		}
	}
	if dryRun {
		return res, nil
	}
	return res, WriteFileAtomic(path, out)
}

// setFormat returns raw, a JSON object, with its top-level "format" set to
// f. Only the value's bytes change; a file without the key gets it as its
// first key, indented like the key it then precedes.
func setFormat(raw []byte, f Format) ([]byte, error) {
	val, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("settings are not a JSON object")
	}
	open := int(dec.InputOffset())
	empty := !dec.More()
	at := -1 // where the format value starts and ends, once found
	var end int
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, err
		}
		keyEnd := int(dec.InputOffset())
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, err
		}
		if k != "format" {
			continue
		}
		if at >= 0 {
			return nil, errors.New(`"format" is given twice; keep one`)
		}
		// The value starts after the colon and any space around it, and
		// ends where a decoder reading only it stops.
		start := skipSpace(raw, keyEnd)
		if raw[start] != ':' {
			return nil, errors.New("format key is not followed by a value")
		}
		start = skipSpace(raw, start+1)
		one := json.NewDecoder(bytes.NewReader(raw[start:]))
		var v json.RawMessage
		if err := one.Decode(&v); err != nil {
			return nil, err
		}
		at, end = start, start+int(one.InputOffset())
	}
	if at >= 0 {
		return concat(raw[:at], val, raw[end:]), nil
	}
	if empty {
		return concat(raw[:open], []byte(`"format": `), val, raw[open:]), nil
	}
	first := skipSpace(raw, open)
	gap := raw[open:first] // the space between { and the first key, e.g. "\n  "
	return concat(raw[:first], []byte(`"format": `), val, []byte(","), gap, raw[first:]), nil
}

// skipSpace returns the offset of the first byte at or after i that is not
// JSON whitespace.
func skipSpace(raw []byte, i int) int {
	return i + len(raw[i:]) - len(bytes.TrimLeft(raw[i:], " \t\r\n"))
}

func concat(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}
