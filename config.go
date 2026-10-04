package taskroll

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigFile sits at the repository root and marks where the tracker lives.
const ConfigFile = ".taskroll.json"

// DefaultDir is where init puts the tracker unless told otherwise.
const DefaultDir = "docs/tasks"

// Config is the committed, per-repository tracker configuration.
type Config struct {
	// Dir is the tracker directory, relative to the repository root.
	Dir string `json:"dir"`
}

// ErrNoConfig means no ConfigFile was found above the start directory.
var ErrNoConfig = errors.New("no " + ConfigFile + " in this directory or any parent; run init")

// FindConfig walks up from start to the nearest ConfigFile and returns the
// directory holding it with the parsed config.
func FindConfig(start string) (root string, cfg Config, err error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", cfg, err
	}
	for {
		raw, err := os.ReadFile(filepath.Join(dir, ConfigFile))
		if err == nil {
			if err := decodeStrict(raw, &cfg); err != nil {
				return "", cfg, fmt.Errorf("%s: %w", filepath.Join(dir, ConfigFile), err)
			}
			if err := cfg.validate(); err != nil {
				return "", cfg, fmt.Errorf("%s: %w", filepath.Join(dir, ConfigFile), err)
			}
			return dir, cfg, nil
		}
		if !os.IsNotExist(err) {
			return "", cfg, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", cfg, ErrNoConfig
		}
		dir = parent
	}
}

func (c Config) validate() error {
	d := filepath.ToSlash(filepath.Clean(c.Dir))
	if c.Dir == "" || filepath.IsAbs(c.Dir) || d == "." || strings.HasPrefix(d, "../") || d == ".." {
		return fmt.Errorf("dir %q must be a path inside the repository, e.g. %s", c.Dir, DefaultDir)
	}
	return nil
}

// Init writes the config at root and creates the tracker directory with its
// data directory and a README for the people and agents who will use it.
// It refuses to overwrite an existing config.
func Init(root string, cfg Config) (string, error) {
	if cfg.Dir == "" {
		cfg.Dir = DefaultDir
	}
	cfg.Dir = filepath.ToSlash(filepath.Clean(cfg.Dir))
	if err := cfg.validate(); err != nil {
		return "", err
	}
	path := filepath.Join(root, ConfigFile)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists", path)
	}
	dir := filepath.Join(root, filepath.FromSlash(cfg.Dir))
	if err := os.MkdirAll(DataDir(dir), 0o755); err != nil {
		return "", err
	}
	readme := filepath.Join(dir, "README.md")
	if _, err := os.Stat(readme); os.IsNotExist(err) {
		if err := os.WriteFile(readme, []byte(readmeTemplate), 0o644); err != nil {
			return "", err
		}
	}
	// The settings are written out in full, so the knobs a project can turn
	// are in front of it rather than in the documentation.
	settings := filepath.Join(dir, SettingsFile)
	if _, err := os.Stat(settings); os.IsNotExist(err) {
		raw, err := marshalSettings(DefaultSettings())
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(settings, append(raw, '\n'), 0o644); err != nil {
			return "", err
		}
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err := WriteFileAtomic(path, append(raw, '\n')); err != nil {
		return "", err
	}
	return dir, nil
}

const readmeTemplate = `# Tasks

This directory is a work tracker kept in git. The records in ` + "`data/*.jsonl`" + ` are the source of truth: one JSON record per line, the epic record first in each file. Markdown under ` + "`epics/`" + ` is generated from them.

**Change it only through the CLI**, never by editing the files: every write validates the record, stamps who did it and when, and regenerates the views.

    taskroll epic new --slug payments --title 'Payments'
    taskroll add --epic epic-0-payments --id PAY-001 --title 'Card checkout' --size M --label checkout
    taskroll status PAY-001 in_progress
    taskroll done PAY-001
    taskroll list --ready --json
    taskroll show PAY-001
    taskroll whoami

What the tracker holds is defined in ` + "`taskroll.json`" + ` beside this file: the sizes and the points velocity counts them at, the fields and markers a task line shows, the legend a new epic starts with, and any collections of entries kept one file each beside the epics (debt, opportunities, a watch list), with the fields they need and the values those fields take.

Who you are comes from ` + "`git config taskroll.user`" + `, then ` + "`git config user.name`" + `; set it with ` + "`taskroll config user 'Your Name'`" + `.

Run ` + "`taskroll config merge-driver`" + ` once per clone: git then merges the records by ID instead of by line, so branches that each add items merge cleanly. After a merge, regenerate the views with ` + "`taskroll index`" + `.
`

// marshalSettings writes a settings file people read and edit: indented, and
// without the HTML escaping that turns the banner's <!-- into \u003c!--.
func marshalSettings(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}
