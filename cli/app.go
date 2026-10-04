// Package cli is the tracker's command line: the same commands whether they
// run as the standalone taskroll binary or mounted under a group in a host
// program (`mytool tasks`). A host adds what is its own through Hooks.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hvish/taskroll"
)

// Hooks is what a host program adds to the tracker's commands.
type Hooks struct {
	// Invocation is how people type the commands, e.g. "taskroll" or
	// "mytool tasks"; help text, messages and the merge driver use it.
	Invocation string
	// Open opens the project at a tracker directory, with the host's views
	// and reserved ids plugged in. Nil means taskroll.OpenProject.
	Open func(dir string) (*taskroll.Project, error)
	// Views are the files the host generates beside the records' own,
	// relative to the tracker directory; a merge keeps one side of each.
	Views []string
	// CheckViews reports a stale host view; nil means the host has none.
	CheckViews func(dir string) error
}

// hooks is set once by Commands: a process runs one command tree.
var hooks = Hooks{Invocation: "taskroll"}

// Commands returns the tracker's commands for a host to mount, at the root
// of the standalone binary or under a group such as `mytool tasks`.
func Commands(h Hooks) []*cobra.Command {
	if h.Invocation == "" {
		h.Invocation = "taskroll"
	}
	hooks = h
	cmds := []*cobra.Command{
		initCmd(), whoamiCmd(), configCmd(), fmtCmd(), listCmd(), showCmd(), statusCmd(), doneCmd(),
		editCmd(), commentCmd(), velocityCmd(), mergeDriverCmd(), newEntryCmd(),
		addCmd(), epicCmd(), indexCmd(), archiveCmd(), burndownCmd(), serveCmd(),
	}
	for _, c := range cmds {
		invocationIn(c)
	}
	return cmds
}

// invocationIn writes the host's invocation into a command's help, which is
// written once, with the placeholder, for every host.
func invocationIn(c *cobra.Command) {
	r := strings.NewReplacer("TRACKER ", hooks.Invocation+" ")
	c.Short, c.Long, c.Example = r.Replace(c.Short), r.Replace(c.Long), r.Replace(c.Example)
	for _, sub := range c.Commands() {
		invocationIn(sub)
	}
}

// inv is the invocation, for messages built at run time.
func inv(rest string) string { return hooks.Invocation + " " + rest }

// out is a write-and-forget printer for command output. A failed write to the
// command's own stdout is not actionable, so the error is dropped once here.
type out struct{ w io.Writer }

func (o out) printf(format string, a ...any) { _, _ = fmt.Fprintf(o.w, format, a...) }
func (o out) line(s string)                  { _, _ = fmt.Fprintln(o.w, s) }

// groupRunE gives a command group explicit behaviour for a bad subcommand.
// Cobra returns help for a non-runnable parent before it validates args, so
// a typo'd subcommand would otherwise print help and exit 0, and look like a
// passing gate in CI.
func groupRunE(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unknown subcommand %q for %q", args[0], cmd.CommandPath())
	}
	return cmd.Help()
}

// trackerDir is the tracker directory: the one .taskroll.json names, or
// docs/tasks in the nearest directory above that has one, for a checkout
// that predates the config.
func trackerDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, cfg, err := taskroll.FindConfig(wd)
	if err == nil {
		return filepath.Join(root, filepath.FromSlash(cfg.Dir)), nil
	}
	if !errors.Is(err, taskroll.ErrNoConfig) {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(taskroll.DefaultDir))); err == nil && st.IsDir() {
			return filepath.Join(dir, filepath.FromSlash(taskroll.DefaultDir)), nil
		}
		if filepath.Dir(dir) == dir {
			return "", fmt.Errorf("%w (and no %s above here)", taskroll.ErrNoConfig, taskroll.DefaultDir)
		}
	}
}

// project opens the tracker the working directory belongs to.
func project() (*taskroll.Project, error) {
	dir, err := trackerDir()
	if err != nil {
		return nil, err
	}
	return openAt(dir)
}

func openAt(dir string) (*taskroll.Project, error) {
	if hooks.Open != nil {
		return hooks.Open(dir)
	}
	return taskroll.OpenProject(dir)
}

// writable opens the project and refuses one with no records.
func writable() (*taskroll.Project, error) {
	p, err := project()
	if err != nil {
		return nil, err
	}
	if !p.Initialised() {
		return nil, taskroll.ErrNotInitialised
	}
	return p, nil
}

// settingsHere reads the settings of the tracker the working directory
// belongs to, for flags built from them; the defaults when there is none.
func settingsHere() taskroll.Settings {
	if p, err := project(); err == nil {
		return p.Settings
	}
	return taskroll.DefaultSettings()
}

// actor is who the current command acts as. The tracker directory sits in
// the repository, so git config resolves against it.
func actor(dir string) string { return taskroll.Actor(dir) }

// regenerated is what a write prints: the store rendered every view before
// it released the lock.
func regenerated(o out) { o.line("regenerated the generated files") }

// splitCSV turns a comma-separated flag into trimmed entries, dropping empties
// so `--depends ""` means "none" rather than one blank dependency.
func splitCSV(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// fieldFlags registers a string flag per epic field and a bool flag per
// marker in the settings, so a project's own vocabulary (phase, pilot) is
// typed as flags without any code knowing it.
type fieldFlags struct {
	fields  map[string]*string
	markers map[string]*bool
}

func addFieldFlags(cmd *cobra.Command, s taskroll.Settings, what string, markers bool) fieldFlags {
	ff := fieldFlags{fields: map[string]*string{}, markers: map[string]*bool{}}
	fl := cmd.Flags()
	for _, f := range s.Epic.Fields {
		name := strings.ReplaceAll(f.Key, "_", "-")
		if fl.Lookup(name) != nil {
			continue
		}
		v := new(string)
		ff.fields[f.Key] = v
		fl.StringVar(v, name, "", fmt.Sprintf("%s (the %q field)%s", f.Label, f.Key, what))
	}
	for _, m := range s.Epic.Markers {
		if !markers || fl.Lookup(m.Label) != nil {
			continue
		}
		v := new(bool)
		ff.markers[m.Label] = v
		fl.BoolVar(v, m.Label, false, fmt.Sprintf("label it %s (shows %q on the task line)", m.Label, m.Marker))
	}
	return ff
}

func addCmd() *cobra.Command {
	var t taskroll.NewTask
	var depends, closes string
	var setFields []string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a task to an epic",
		Long: "Validates the task, allocates its id under the tracker lock and appends its record;\n" +
			"the epic file and every other view are regenerated before the lock is released.\n\n" +
			"Ids are never reused: allocation reads the records (dropped ones too), the archive\n" +
			"and every other branch and worktree. The first task of a new series takes --id.",
		Example: "  TRACKER add --epic epic-2-payments --series PAY --title 'Refunds' --size M --depends PAY-001",
		Args:    cobra.NoArgs,
	}
	ff := addFieldFlags(cmd, settingsHere(), "", true)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := writable()
		if err != nil {
			return err
		}
		t.Depends, t.Closes = splitCSV(depends), splitCSV(closes)
		t.Fields = map[string]string{}
		for k, v := range ff.fields {
			if *v != "" {
				t.Fields[k] = *v
			}
		}
		for _, kv := range setFields {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				return fmt.Errorf("--field %q is not key=value", kv)
			}
			t.Fields[k] = v
		}
		for label, on := range ff.markers {
			if *on {
				t.Labels = append(t.Labels, label)
			}
		}
		it, err := p.AddTask(actor(p.Dir), t)
		if err != nil {
			return err
		}
		o := out{cmd.OutOrStdout()}
		o.printf("added to %s:\n  %s\n", t.Epic, p.Markdown().RenderLine(it))
		regenerated(o)
		return nil
	}
	fl := cmd.Flags()
	fl.StringVar(&t.Epic, "epic", "", "the epic to add to, e.g. epic-2-payments (required)")
	fl.StringVar(&t.Title, "title", "", "what the task is (required)")
	fl.StringVar(&t.ID, "id", "", "explicit id; omit to allocate from --series")
	fl.StringVar(&t.Series, "series", "", "id prefix to allocate the next number from, e.g. PAY or API")
	fl.StringVar(&t.Size, "size", "", "one of the tracker's sizes")
	fl.StringVar(&depends, "depends", "", "comma-separated dependencies; ids or prose conditions")
	fl.StringVar(&closes, "closes", "", "comma-separated ids this one closes")
	fl.StringArrayVar(&setFields, "field", nil, "any field, key=value")
	fl.BoolVar(&t.Blocker, "blocker", false, "mark as [BLOCKER]")
	fl.BoolVar(&t.Bug, "bug", false, "a bug in shipped behaviour")
	fl.StringSliceVar(&t.Labels, "label", nil, "labels to filter by later, e.g. frontend,import")
	_ = cmd.MarkFlagRequired("epic")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func epicCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "epic",
		Short: "Create and list epics",
		Args:  cobra.NoArgs,
		RunE:  groupRunE,
	}
	var e taskroll.NewEpic
	newCmd := &cobra.Command{
		Use:   "new",
		Short: "Create an epic under the next free number",
		Long: "Writes the epic record with the settings' legend and the intro paragraphs, one line\n" +
			"each; add tasks with add. A new series needs its first task filed with --id.",
		Example: "  TRACKER epic new --slug payments --title 'Payments' --intro 'Checkout and refunds.'",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := writable()
			if err != nil {
				return err
			}
			id, err := p.CreateEpic(actor(p.Dir), e)
			if err != nil {
				return err
			}
			o := out{cmd.OutOrStdout()}
			o.printf("created %s\n", id)
			regenerated(o)
			return nil
		},
	}
	newCmd.Flags().StringVar(&e.Slug, "slug", "", "id part after the number, e.g. auth-provisioning (required)")
	newCmd.Flags().StringVar(&e.Title, "title", "", "heading after 'EPIC N:' (required)")
	newCmd.Flags().StringArrayVar(&e.Intro, "intro", nil, "an intro paragraph; repeat for more")
	_ = newCmd.MarkFlagRequired("slug")
	_ = newCmd.MarkFlagRequired("title")
	cmd.AddCommand(newCmd, epicListCmd())
	return cmd
}

func indexCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Regenerate every generated file, or with --check fail if one is stale",
		Long: "The generated files are views of the records and are never hand-edited. --check is\n" +
			"the gate: records in canonical form, every generated file fresh and backed by a\n" +
			"record, archive records valid, every finished task carrying a ship date.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndex(out{cmd.OutOrStdout()}, check)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "exit non-zero if a generated file is stale instead of rewriting it")
	return cmd
}

func runIndex(o out, check bool) error {
	p, err := project()
	if err != nil {
		return err
	}
	if !check {
		if err := p.Refresh(); err != nil {
			return err
		}
		o.line("wrote every generated file")
		return nil
	}
	if bad, err := p.Format(true); err != nil {
		return err
	} else if len(bad) > 0 {
		return fmt.Errorf("not in canonical form - run `%s`:\n    %s", inv("fmt"), strings.Join(bad, "\n    "))
	}
	// The epic and entry files are the first generated views; a stale one
	// is a hand edit the next write would overwrite.
	stale, err := p.RenderRecords(true)
	if err != nil {
		return err
	}
	if len(stale) > 0 {
		return fmt.Errorf("generated files differ from the records - run `%s` and commit; never edit them by hand:\n    %s", inv("index"), strings.Join(stale, "\n    "))
	}
	// The archive is append-only and never rewritten, so a malformed record
	// is permanent. Catch it in the change that wrote it.
	problems, err := taskroll.ValidateArchive(p.Dir)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "%d malformed archive record(s):", len(problems))
		for _, pr := range problems {
			fmt.Fprintf(&b, "\n    %s", pr)
		}
		return errors.New(b.String())
	}
	if hooks.CheckViews != nil {
		if err := hooks.CheckViews(p.Dir); err != nil {
			return err
		}
	}
	// A finished task with no date is invisible to the burndown, and the gap
	// only shows up as a wrong chart months later.
	_, missing, err := p.ScanCompletions()
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "%d completed task(s) carry no ship date:", len(missing))
		for _, m := range missing {
			fmt.Fprintf(&b, "\n    %s (%s)", m.ID, m.Epic)
		}
		fmt.Fprintf(&b, "\n  stamp each with %s ID --on YYYY-MM-DD", inv("done"))
		return errors.New(b.String())
	}
	o.line("the generated files are up to date; records canonical; archive records valid")
	return nil
}

func archiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive [version]",
		Short: "Sweep closed entries and completed epics into the quarterly archive",
		Long: "Done entries are appended to archive/YYYY-QN.jsonl and their records and files\n" +
			"removed; an epic with every task complete moves whole and leaves a tombstone at its\n" +
			"path. Run at a release tag; the archive is one file per quarter.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			version := "unreleased"
			if len(args) == 1 && args[0] != "" {
				version = args[0]
			}
			p, err := writable()
			if err != nil {
				return err
			}
			res, err := p.Archive(version, time.Now().UTC())
			if err != nil {
				return err
			}
			o := out{cmd.OutOrStdout()}
			if res.Archived == 0 && res.Epics == 0 {
				o.line("nothing to sweep")
				return nil
			}
			if res.Archived > 0 {
				o.printf("archived %d closed entr(ies) into %s\n", res.Archived, filepath.Base(res.File))
			}
			if res.Epics > 0 {
				o.printf("archived %d completed epic(s), leaving a tombstone at each path\n", res.Epics)
			}
			return nil
		},
	}
}

func burndownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "burndown",
		Short: "Show completed tasks over time",
		Long: "Reads each finished task's ship date and prints the cumulative series. Remaining\n" +
			"counts against the backlog as it stands today, so the trend is honest but early\n" +
			"absolute values are approximate.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := project()
			if err != nil {
				return err
			}
			s, err := p.RenderBurndown()
			if err != nil {
				return err
			}
			out{cmd.OutOrStdout()}.printf("%s", s)
			return nil
		},
	}
}
