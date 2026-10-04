package cli

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hvish/taskroll"
)

// newEntryCmd files an entry in one of the settings' collections. Its type
// argument and its field flags come from the settings, so a project's own
// collections (debt, a watch list) need no code.
func newEntryCmd() *cobra.Command {
	var e taskroll.NewEntry
	var body, bodyFile string
	s := settingsHere()
	var types []string
	for _, c := range s.Collections {
		types = append(types, c.Type)
	}
	cmd := &cobra.Command{
		Use:   "new <" + strings.Join(types, "|") + ">",
		Short: "File an entry in one of the tracker's collections",
		Long: "Allocates the next id in the collection's series (or names an unnumbered entry by a\n" +
			"slug), stamps opened and who filed it when the collection keeps those, and renders\n" +
			"its file. Epic tasks are filed with add instead. The collections, their fields and\n" +
			"the values those take are defined in the tracker's taskroll.json.",
		Example:   "  TRACKER new opportunity --title 'Saved cards' --source customer --body-file /tmp/why.md",
		Args:      cobra.ExactArgs(1),
		ValidArgs: types,
	}
	if len(types) == 0 {
		cmd.Use = "new <type>"
	}
	fields := map[string]*string{}
	fl := cmd.Flags()
	fl.StringVar(&e.Title, "title", "", "one-line title (required)")
	fl.StringVar(&e.Size, "size", "", "effort, for collections that keep one")
	fl.StringVar(&e.Slug, "slug", "", "unnumbered collections: the id and file name (default: from the title)")
	fl.StringVar(&body, "body", "", "markdown under the heading")
	fl.StringVar(&bodyFile, "body-file", "", "read the body from a file")
	for _, key := range entryFieldKeys(s) {
		v := new(string)
		fields[key] = v
		fl.StringVar(v, strings.ReplaceAll(key, "_", "-"), "", entryFieldHelp(s, key))
	}
	_ = cmd.MarkFlagRequired("title")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		p, err := writable()
		if err != nil {
			return err
		}
		e.Type = args[0]
		switch {
		case body != "" && bodyFile != "":
			return fmt.Errorf("give --body or --body-file, not both")
		case bodyFile != "":
			raw, err := os.ReadFile(bodyFile)
			if err != nil {
				return err
			}
			e.Body = string(raw)
		default:
			e.Body = body
		}
		e.Fields = map[string]string{}
		for k, v := range fields {
			e.Fields[k] = *v
		}
		it, err := p.AddEntry(actor(p.Dir), e)
		if err != nil {
			return err
		}
		o := out{cmd.OutOrStdout()}
		o.printf("filed %s: %s\n", it.ID, it.Title)
		regenerated(o)
		return nil
	}
	return cmd
}

// entryFieldKeys are the field keys any collection takes from the command
// line: its frontmatter keys other than those held in a field of their own
// and opened, which is stamped.
func entryFieldKeys(s taskroll.Settings) []string {
	seen := map[string]bool{}
	for _, c := range s.Collections {
		for _, k := range c.Keys {
			switch k {
			case "id", "title", "type", "status", "effort", "closed", "opened":
			default:
				seen[k] = true
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// entryFieldHelp names the collections a field belongs to and its values.
func entryFieldHelp(s taskroll.Settings, key string) string {
	var in []string
	var values []string
	for _, c := range s.Collections {
		if slices.Contains(c.Keys, key) {
			in = append(in, c.Type)
			if v, ok := c.Values[key]; ok {
				values = v
			}
		}
	}
	help := strings.Join(in, ", ")
	if len(values) > 0 {
		help += ": one of " + strings.Join(values, ", ")
	}
	return help
}
