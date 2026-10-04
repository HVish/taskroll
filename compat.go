package taskroll

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// FormatVersion is the newest tracker format this build reads and writes:
// the shape of taskroll.json and how the views render. A tracker names its
// format in taskroll.json, and a missing one means 1. A build renders every
// format it supports exactly as that format always rendered, so upgrading
// taskroll leaves a tracker's files as they were; moving a tracker to a newer
// format is a deliberate step, never a side effect of a write.
const FormatVersion = 1

// UpgradeCommand installs the latest release.
const UpgradeCommand = "go install github.com/hvish/taskroll/cmd/taskroll@latest"

// NewerError is a file written in a format or record schema newer than this
// build supports. It is reported before the file is parsed, so an older
// binary on another machine says what to do instead of failing on whatever
// key the newer one added.
type NewerError struct {
	Path      string
	What      string // "format" or "record schema"
	Have      int
	Supported int
}

func (e *NewerError) Error() string {
	return fmt.Sprintf("%s: written in taskroll %s %d; this build supports up to %d. Upgrade with: %s", e.Path, e.What, e.Have, e.Supported, UpgradeCommand)
}

// decodeStrict decodes one JSON value and refuses unknown keys, because the
// next write would otherwise drop them. An unknown key is most often one a
// newer taskroll added, so the error says how to upgrade.
func decodeStrict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err != nil && strings.HasPrefix(err.Error(), "json: unknown field") {
		return fmt.Errorf("%w; if a newer taskroll wrote it, upgrade with: %s", err, UpgradeCommand)
	}
	return err
}
