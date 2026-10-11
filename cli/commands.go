package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/hvish/taskroll"
)

func printJSON(o out, v any) error {
	enc := json.NewEncoder(o.w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func fmtCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "fmt",
		Short: "Rewrite the JSONL records in canonical form",
		Long: "One record per line, keys in schema order. Run after a hand edit or a merge; with\n" +
			"--check it only reports, for CI.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := project()
			if err != nil {
				return err
			}
			bad, err := p.Format(check)
			if err != nil {
				return err
			}
			o := out{cmd.OutOrStdout()}
			if check && len(bad) > 0 {
				return fmt.Errorf("not in canonical form - run `%s`:\n    %s", inv("fmt"), strings.Join(bad, "\n    "))
			}
			for _, p := range bad {
				o.printf("formatted %s\n", p)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "exit non-zero if a file is not canonical instead of rewriting it")
	return cmd
}

func listCmd() *cobra.Command {
	var f taskroll.Filter
	var fields []string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List items, filtered",
		Long: "Shows open items unless --all or --status says otherwise. Filters combine with AND;\n" +
			"a repeated or comma-separated filter matches any of its values, except --label,\n" +
			"which requires every label given.",
		Example: "  TRACKER list --epic epic-3 --ready\n" +
			"  TRACKER list --label pilot --blocked --json\n" +
			"  TRACKER list --field phase=2 --size L --text import",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := project()
			if err != nil {
				return err
			}
			x, err := p.Index()
			if err != nil {
				return err
			}
			if len(fields) > 0 {
				f.Fields = map[string]string{}
				for _, kv := range fields {
					k, v, ok := strings.Cut(kv, "=")
					if !ok {
						return fmt.Errorf("--field %q is not key=value", kv)
					}
					f.Fields[k] = v
				}
			}
			for _, s := range f.Statuses {
				if !slices.Contains(taskroll.Statuses, s) {
					return fmt.Errorf("status %q is not one of %s", s, strings.Join(taskroll.Statuses, ", "))
				}
			}
			items := x.List(f)
			o := out{cmd.OutOrStdout()}
			if asJSON {
				rows := make([]listRow, 0, len(items))
				for _, it := range items {
					rows = append(rows, rowFor(p, x, it))
				}
				return printJSON(o, rows)
			}
			tw := tabwriter.NewWriter(o.w, 0, 0, 2, ' ', 0)
			for _, it := range items {
				where := shortEpic(x.EpicOf(it.ID))
				if where == "" {
					where = it.Type // an entry belongs to no epic
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", it.ID, it.Status, dash(it.Size), where, clip(it.Title, 90))
			}
			_ = tw.Flush()
			o.printf("%d item(s)\n", len(items))
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringSliceVar(&f.Types, "type", nil, "task, bug, ...")
	fl.StringSliceVar(&f.Statuses, "status", nil, strings.Join(taskroll.Statuses, ", "))
	fl.StringSliceVar(&f.Epics, "epic", nil, "epic id or its number prefix, e.g. epic-3")
	fl.StringSliceVar(&f.Labels, "label", nil, "label the item must carry, e.g. pilot")
	fl.StringSliceVar(&f.Sizes, "size", nil, "S, M or L")
	fl.StringArrayVar(&fields, "field", nil, "custom field match, key=value (e.g. phase=2, track=B)")
	fl.BoolVar(&f.Ready, "ready", false, "todo with every dependency done")
	fl.BoolVar(&f.Blocked, "blocked", false, "open with an unmet dependency")
	fl.StringVar(&f.DependsOn, "depends-on", "", "items that depend on this id")
	fl.StringVar(&f.Text, "text", "", "case-insensitive text in id, title, notes or description")
	fl.BoolVar(&f.All, "all", false, "include done and dropped items")
	fl.BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

// listRow is an item plus the joins a reader of one record cannot compute.
type listRow struct {
	taskroll.Item
	Epic       string   `json:"epic"`
	Ready      bool     `json:"ready"`
	BlockedBy  []string `json:"blocked_by,omitempty"`
	Conditions []string `json:"conditions,omitempty"`
	Dependents []string `json:"dependents,omitempty"`
	URL        string   `json:"url,omitempty"`
}

func rowFor(p *taskroll.Project, x *taskroll.Index, it taskroll.Item) listRow {
	ids, prose := x.Blockers(it)
	r := listRow{Item: it, Epic: x.EpicOf(it.ID), Ready: x.Ready(it), Dependents: x.Dependents(it.ID), URL: p.URL(x, it)}
	if it.Open() {
		r.BlockedBy, r.Conditions = ids, prose
	}
	return r
}

func showCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show one item with its dependencies and dependents",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := project()
			if err != nil {
				return err
			}
			x, err := p.Index()
			if err != nil {
				return err
			}
			it, ok := x.Get(args[0])
			if !ok {
				return fmt.Errorf("no item %s", args[0])
			}
			r := rowFor(p, x, it)
			o := out{cmd.OutOrStdout()}
			if asJSON {
				return printJSON(o, r)
			}
			o.printf("%s  %s\n%s\n\n", it.ID, it.Title, strings.Repeat("-", min(len(it.ID)+2+len(it.Title), 100)))
			kv := func(k, v string) {
				if v != "" {
					o.printf("%-11s %s\n", k+":", v)
				}
			}
			kv("epic", r.Epic)
			kv("type", it.Type)
			kv("status", it.Status)
			kv("done", it.Done)
			kv("size", strings.TrimSpace(it.Size+it.SizeNote))
			if it.Blocker {
				kv("blocker", "yes")
			}
			kv("labels", strings.Join(it.Labels, ", "))
			for _, k := range sortedKeys(it.Fields) {
				kv(k, it.Fields[k])
			}
			kv("depends", strings.Join(it.Depends, ", "))
			kv("blocked by", strings.Join(r.BlockedBy, ", "))
			kv("conditions", strings.Join(r.Conditions, "; "))
			kv("dependents", strings.Join(r.Dependents, ", "))
			kv("closes", strings.Join(it.Closes, ", "))
			for _, n := range it.Notes {
				kv("note", n)
			}
			kv("link", r.URL)
			if it.Description != "" {
				o.printf("\n%s\n", it.Description)
			}
			for _, c := range it.Comments {
				who := c.Author
				if who == "" {
					who = "-"
				}
				o.printf("\n[%s %s]\n%s\n", c.Date, who, c.Body)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func statusCmd() *cobra.Command {
	var on string
	cmd := &cobra.Command{
		Use:   "status <id> <status>",
		Short: "Move an item through the workflow",
		Long: "Statuses: " + strings.Join(taskroll.Statuses, ", ") + ". Moving to done stamps the date\n" +
			"(--on, default today); moving out of done clears it. A dropped item keeps its\n" +
			"record, so its id stays spent, but renders no line in the epic file.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetStatus(out{cmd.OutOrStdout()}, args[0], args[1], on)
		},
	}
	cmd.Flags().StringVar(&on, "on", "", "done date, YYYY-MM-DD; default today")
	return cmd
}

func doneCmd() *cobra.Command {
	var on string
	cmd := &cobra.Command{
		Use:   "done <id>",
		Short: "Mark an item done and stamp the date it shipped",
		Long:  "Shorthand for `status <id> done`. Use the PR merge date, not the date the work was written.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetStatus(out{cmd.OutOrStdout()}, args[0], taskroll.StatusDone, on)
		},
	}
	cmd.Flags().StringVar(&on, "on", "", "date it shipped, YYYY-MM-DD; default today")
	return cmd
}

func runSetStatus(o out, id, status, on string) error {
	p, err := writable()
	if err != nil {
		return err
	}
	it, err := p.Store(actor(p.Dir)).SetStatus(id, status, on)
	if err != nil {
		return err
	}
	o.printf("%s is %s\n", it.ID, it.Status)
	regenerated(o)
	return nil
}

func editCmd() *cobra.Command {
	var (
		title, size, sizeNote, description     string
		blocker                                bool
		addLabels, rmLabels, addDeps, rmDeps   []string
		addCloses, rmCloses, addNotes, rmNotes []string
		setFields                              []string
		ff                                     fieldFlags
	)
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Change an item's fields",
		Long: "Only the flags given change anything. List fields take --add-X and --remove-X;\n" +
			"removing a value the item does not have is an error, so a typo is not a no-op.",
		Example: "  TRACKER edit PAY-005 --size L --add-depends API-004 --add-label pilot\n" +
			"  TRACKER edit PAY-031 --field quarter=Q3 --no-blocker",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := writable()
			if err != nil {
				return err
			}
			ch := cmd.Flags().Changed
			it, err := p.Update(actor(p.Dir), args[0], func(it *taskroll.Item) error {
				if ch("title") {
					if err := p.CheckLineText("--title", title); err != nil {
						return err
					}
					it.Title = strings.TrimSpace(title)
					if it.Type == taskroll.TypeTask || it.Type == taskroll.TypeBug {
						it.Title = p.TaskTitle(title)
					}
				}
				for _, group := range [][]string{addDeps, addCloses, addNotes, {sizeNote}} {
					for _, v := range group {
						if err := p.CheckLineText("value", v); err != nil {
							return err
						}
					}
				}
				if ch("size") {
					it.Size = strings.ToUpper(size)
				}
				if ch("size-note") {
					it.SizeNote = sizeNote
				}
				if ch("blocker") {
					it.Blocker = blocker
				}
				if ch("no-blocker") {
					it.Blocker = false
				}
				if ch("description") {
					it.Description = description
				}
				for k, v := range ff.fields {
					if ch(strings.ReplaceAll(k, "_", "-")) {
						setField(it, k, *v)
					}
				}
				for _, kv := range setFields {
					k, v, ok := strings.Cut(kv, "=")
					if !ok {
						return fmt.Errorf("--field %q is not key=value; an empty value removes the field", kv)
					}
					setField(it, k, v)
				}
				var err error
				for _, e := range []struct {
					name     string
					list     *[]string
					add, del []string
				}{
					{"label", &it.Labels, addLabels, rmLabels},
					{"depends", &it.Depends, upper(addDeps), upper(rmDeps)},
					{"closes", &it.Closes, upper(addCloses), upper(rmCloses)},
					{"note", &it.Notes, addNotes, rmNotes},
				} {
					if *e.list, err = editList(e.name, *e.list, e.add, e.del); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			o := out{cmd.OutOrStdout()}
			o.printf("updated %s\n", it.ID)
			if it.Type == taskroll.TypeTask || it.Type == taskroll.TypeBug {
				o.printf("  %s\n", p.Markdown().RenderLine(it))
			}
			regenerated(o)
			return nil
		},
	}
	ff = addFieldFlags(cmd, settingsHere(), "; empty clears it", false)
	fl := cmd.Flags()
	fl.StringVar(&title, "title", "", "new title")
	fl.StringVar(&size, "size", "", "one of the tracker's sizes (S, M, L by default); empty clears it")
	fl.StringVar(&sizeNote, "size-note", "", "text after the size, e.g. ' per card'")
	fl.StringArrayVar(&setFields, "field", nil, "set a custom field, key=value; key= removes it")
	fl.BoolVar(&blocker, "blocker", false, "mark as [BLOCKER]")
	fl.Bool("no-blocker", false, "clear [BLOCKER]")
	fl.StringVar(&description, "description", "", "markdown description")
	fl.StringSliceVar(&addLabels, "add-label", nil, "")
	fl.StringSliceVar(&rmLabels, "remove-label", nil, "")
	fl.StringSliceVar(&addDeps, "add-depends", nil, "")
	fl.StringSliceVar(&rmDeps, "remove-depends", nil, "")
	fl.StringSliceVar(&addCloses, "add-closes", nil, "")
	fl.StringSliceVar(&rmCloses, "remove-closes", nil, "")
	fl.StringArrayVar(&addNotes, "add-note", nil, "a short note shown on the task line")
	fl.StringArrayVar(&rmNotes, "remove-note", nil, "")
	return cmd
}

func commentCmd() *cobra.Command {
	var body, author string
	cmd := &cobra.Command{
		Use:   "comment <id>",
		Short: "Add a dated comment to an item",
		Long:  "Comments live in the JSONL only; the generated epic files do not show them. Read them with show.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := writable()
			if err != nil {
				return err
			}
			if author == "" {
				author = actor(p.Dir)
			}
			it, err := p.Store(author).Comment(args[0], body)
			if err != nil {
				return err
			}
			out{cmd.OutOrStdout()}.printf("%s has %d comment(s)\n", it.ID, len(it.Comments))
			return nil
		},
	}
	cmd.Flags().StringVar(&body, "body", "", "the comment, markdown (required)")
	cmd.Flags().StringVar(&author, "author", "", "who wrote it; default the tracker user (see whoami)")
	_ = cmd.MarkFlagRequired("body")
	return cmd
}

func epicListCmd() *cobra.Command {
	var asJSON, all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List epics with open, done, ready and blocked counts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := project()
			if err != nil {
				return err
			}
			x, err := p.Index()
			if err != nil {
				return err
			}
			var rows []taskroll.EpicSummary
			for _, e := range x.Epics() {
				if all || (e.Archived == "" && (e.Open > 0 || e.Done == 0)) {
					rows = append(rows, e)
				}
			}
			o := out{cmd.OutOrStdout()}
			if asJSON {
				return printJSON(o, rows)
			}
			tw := tabwriter.NewWriter(o.w, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "EPIC\tOPEN\tREADY\tBLOCKED\tDONE\tTITLE")
			for _, e := range rows {
				_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%s\n", e.ID, e.Open, e.Ready, e.Blocked, e.Done, clip(e.Title, 60))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&all, "all", false, "include finished and archived epics")
	return cmd
}

func setField(it *taskroll.Item, k, v string) {
	if v == "" {
		delete(it.Fields, k)
		return
	}
	if it.Fields == nil {
		it.Fields = map[string]string{}
	}
	it.Fields[k] = v
}

func editList(name string, list, add, del []string) ([]string, error) {
	for _, d := range del {
		i := slices.Index(list, d)
		if i < 0 {
			return nil, fmt.Errorf("--remove-%s %q: the item has no such value (has: %s)", name, d, strings.Join(list, ", "))
		}
		list = slices.Delete(slices.Clone(list), i, i+1)
	}
	for _, a := range add {
		if slices.Contains(list, a) {
			return nil, fmt.Errorf("--add-%s %q: already there", name, a)
		}
		list = append(list, a)
	}
	return list, nil
}

func upper(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ToUpper(strings.TrimSpace(s))
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func shortEpic(id string) string {
	if i := strings.Index(id[min(len(id), 5):], "-"); i >= 0 {
		return id[:5+i]
	}
	return id
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func initCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up a tracker in this repository",
		Long: "Writes " + taskroll.ConfigFile + " at the current directory (run it at the repository root),\n" +
			"naming the tracker directory, and creates that directory with data/ and a README.\n" +
			"Every other command finds the tracker by walking up to the config.",
		Example: "  TRACKER init\n  TRACKER init --dir planning/tasks",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			if root, _, err := taskroll.FindConfig(wd); err == nil {
				return fmt.Errorf("already initialised: %s", filepath.Join(root, taskroll.ConfigFile))
			}
			path, err := taskroll.Init(wd, taskroll.Config{Dir: dir})
			if err != nil {
				return err
			}
			o := out{cmd.OutOrStdout()}
			o.printf("wrote %s; tracker in %s\nyou are %q (change with: %s)\n", taskroll.ConfigFile, path, actor(wd), inv("config user NAME"))
			rel, err := filepath.Rel(wd, path)
			if err != nil {
				return err
			}
			// Outside a repository there is nothing to merge; init still
			// succeeds, and config merge-driver can run once there is one.
			if _, err := setUpMerge(wd, rel, ""); err != nil {
				o.printf("merge driver not set up (%v); run %s inside the repository\n", err, inv("config merge-driver"))
				return nil
			}
			o.line("records merge by id: merge drivers in .git/config, routing in .gitattributes")
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", taskroll.DefaultDir, "tracker directory, relative to the repository root")
	return cmd
}

func whoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Print the name recorded as created_by, closed_by and comment author",
		Long: "Resolved from TASKROLL_USER, then git config " + taskroll.UserKey + ", then git config user.name,\n" +
			"then the login name.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := trackerDir()
			if err != nil {
				return err
			}
			out{cmd.OutOrStdout()}.line(actor(dir))
			return nil
		},
	}
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show the tracker's settings and set up this clone",
		Args:  cobra.NoArgs,
		RunE:  groupRunE,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "user <name>",
		Short: "Set the name this clone records, in the local git config (never committed)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := trackerDir()
			if err != nil {
				return err
			}
			if err := taskroll.SetUser(dir, strings.TrimSpace(args[0])); err != nil {
				return err
			}
			out{cmd.OutOrStdout()}.printf("you are %q\n", actor(dir))
			return nil
		},
	}, configMergeDriverCmd(), configShowCmd())
	return cmd
}

func configShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show",
		Short: "List every setting in " + taskroll.SettingsFile + ", its value, and whether it is set or the default",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := trackerDir()
			if err != nil {
				return err
			}
			if _, err := openAt(dir); err != nil { // warns about a newer format
				return err
			}
			entries, format, err := taskroll.SettingEntries(dir)
			if err != nil {
				return err
			}
			o := out{cmd.OutOrStdout()}
			if asJSON {
				return printJSON(o, struct {
					Format   string                  `json:"format"`
					Settings []taskroll.SettingEntry `json:"settings"`
				}{format.String(), entries})
			}
			o.printf("%s, format %s\n\n", filepath.Join(dir, taskroll.SettingsFile), format)
			for _, e := range entries {
				// What a project changed is what differs from the default,
				// whether or not the file spells the default out.
				note, needs := "default", ""
				if !e.Default {
					note = "custom"
				}
				if e.Needs != nil {
					needs = fmt.Sprintf("  (setting it needs format %s: %s)", e.Needs, inv("upgrade"))
				}
				o.printf("%-18s %-7s %s%s\n", e.Key, note, e.Value, needs)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func upgradeCmd() *cobra.Command {
	var to string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Move the tracker to a newer format",
		Long: "Changes the format in " + taskroll.SettingsFile + " and nothing else, so the diff is the one line\n" +
			"that decides which taskroll releases can read the tracker. Settings the new format\n" +
			"adds are listed, not written: set the ones you want by hand. A tracker without the\n" +
			"file gets one with every setting at its default.\n\n" +
			"Upgrading taskroll never does this on its own. Commit the change by itself, once\n" +
			"everyone who works on the tracker runs a release that reads the new format.",
		Example: "  TRACKER upgrade --dry-run\n  TRACKER upgrade\n  TRACKER upgrade --to 1.1",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := trackerDir()
			if err != nil {
				return err
			}
			target := taskroll.CurrentFormat
			if to != "" {
				if err := json.Unmarshal([]byte(strconv.Quote(to)), &target); err != nil {
					return err
				}
			}
			res, err := taskroll.UpgradeFormat(dir, target, dryRun)
			if err != nil {
				return err
			}
			o := out{cmd.OutOrStdout()}
			rel := res.Path
			if wd, err := os.Getwd(); err == nil {
				if r, err := filepath.Rel(wd, res.Path); err == nil {
					rel = r
				}
			}
			would := ""
			if dryRun {
				would = "would be "
			}
			switch {
			case res.Created:
				o.printf("%s: %swritten at format %s with every setting at its default\n", rel, would, res.To)
			case res.From == res.To:
				o.printf("%s is already format %s\n", rel, res.To)
				return nil
			default:
				o.printf("%s: %sformat %s → %s\n", rel, would, res.From, res.To)
			}
			if len(res.New) > 0 {
				o.printf("\nNew since %s, not set, so the defaults apply:\n", res.From)
				for _, n := range res.New {
					o.printf("  %-18s %s\n", n.Key, n.Help)
				}
			}
			o.printf("\n%s\n", taskroll.ReadersOf(res.To))
			if !dryRun {
				o.printf("\nCommit it on its own:\n  git commit -m \"chore: move tracker to format %s\" %s\n", res.To, rel)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "format to move to, such as 1.1; default the newest this build writes")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "say what would change and write nothing")
	return cmd
}

func velocityCmd() *cobra.Command {
	var weeks int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "velocity",
		Short: "Items created, done and dropped per week, with size points and cycle time",
		Long: "Done counts by the ship date; points count sizes at the legend's midpoint (S 1,\n" +
			"M 2.5, L 4.5 days). Created and dropped read the audit stamps, which records\n" +
			"migrated from markdown do not carry, so weeks before 2026-09-28 undercount them.\n" +
			"Cycle time is creation to close, for done items that carry both stamps.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if weeks < 1 {
				return fmt.Errorf("--weeks must be at least 1")
			}
			p, err := project()
			if err != nil {
				return err
			}
			x, err := p.Index()
			if err != nil {
				return err
			}
			rows := taskroll.Velocity(x.Files, weeks, time.Now(), p.Settings.Points())
			cycle := taskroll.CycleTimes(x.Files)
			o := out{cmd.OutOrStdout()}
			if asJSON {
				return printJSON(o, map[string]any{"weeks": rows, "cycle_days": cycle})
			}
			tw := tabwriter.NewWriter(o.w, 0, 0, 2, ' ', tabwriter.AlignRight)
			_, _ = fmt.Fprintln(tw, "WEEK OF\tCREATED\tDONE\tPOINTS\tDROPPED\t")
			for _, w := range rows {
				_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%.1f\t%d\t\n", w.Start, w.Created, w.Done, w.Points, w.Dropped)
			}
			_ = tw.Flush()
			if len(cycle) > 0 {
				o.printf("cycle time: median %.1f days over %d item(s)\n", cycle[len(cycle)/2], len(cycle))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&weeks, "weeks", 8, "how many weeks back, including this one")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}
