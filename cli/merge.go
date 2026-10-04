package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hvish/taskroll"
)

func mergeDriverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "merge-driver <base> <ours> <theirs> [path]",
		Short: "Git merge driver for the JSONL records (git runs it; see `config merge-driver`)",
		Long: "Merges the three versions of one records file by id and writes the result over\n" +
			"<ours>. Two branches appending to one epic merge cleanly; changes to one record\n" +
			"merge field by field. An id both sides added for different work is renumbered on\n" +
			"their side, and a change both sides made differently keeps ours; either way the\n" +
			"merge stops so a person checks it. A file that is not valid records falls back\n" +
			"to git's line merge.",
		Args:   cobra.RangeArgs(3, 4),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			basePath, oursPath, theirsPath := args[0], args[1], args[2]
			name := oursPath
			if len(args) == 4 {
				name = args[3]
			}
			base, err := os.ReadFile(basePath)
			if err != nil {
				return err
			}
			ours, err := os.ReadFile(oursPath)
			if err != nil {
				return err
			}
			theirs, err := os.ReadFile(theirsPath)
			if err != nil {
				return err
			}
			errw := cmd.ErrOrStderr()
			// Without the reserved set a renumbered ID could collide with
			// another file's, which the gate then reports; failing here
			// would leave ours in place and drop theirs on a careless add.
			reserved, err := reservedIDs()
			if err != nil {
				_, _ = fmt.Fprintf(errw, "%s: could not read the IDs issued elsewhere (%v); a renumbered ID may still collide\n", name, err)
			}
			opt := taskroll.MergeOptions{Reserved: reserved}
			if len(args) == 4 {
				if wd, err := os.Getwd(); err == nil {
					opt.Mainline = taskroll.MainlineFile(wd, args[3])
				}
			}
			res, err := taskroll.Merge(base, ours, theirs, opt)
			if err != nil {
				_, _ = fmt.Fprintf(errw, "%s: not merged by record (%v); falling back to a line merge\n", name, err)
				return lineMerge(oursPath, basePath, theirsPath)
			}
			if err := taskroll.WriteFileAtomic(oursPath, res.Data); err != nil {
				return err
			}
			if len(res.Conflicts) == 0 {
				return nil
			}
			_, _ = fmt.Fprintf(errw, "%s: merged by record, but check:\n", name)
			for _, c := range res.Conflicts {
				_, _ = fmt.Fprintf(errw, "    %s\n", c)
			}
			_, _ = fmt.Fprintf(errw, "  fix it with %s or %s, then git add the file; regenerate the views with %s\n", inv("edit"), inv("status"), inv("index"))
			return fmt.Errorf("%s: %d record conflict(s)", name, len(res.Conflicts))
		},
	}
}

// reservedIDs is what a renumbered record must not collide with.
func reservedIDs() (map[string]bool, error) {
	p, err := project()
	if err != nil {
		return nil, err
	}
	return p.MergeReserved()
}

// lineMerge leaves git's conflict markers in ours, which the loader then
// reports by line.
func lineMerge(ours, base, theirs string) error {
	c := exec.Command("git", "merge-file", "-L", "ours", "-L", "base", "-L", "theirs", ours, base, theirs)
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return fmt.Errorf("%s: %d line conflict(s)", ours, ee.ExitCode())
		}
		return err
	}
	return nil
}

func configMergeDriverCmd() *cobra.Command {
	var command string
	cmd := &cobra.Command{
		Use:   "merge-driver",
		Short: "Merge the records by id instead of by line, in this clone",
		Long: "Defines the tracker merge drivers in the local git config and adds the lines that\n" +
			"route the tracker's files to them to .gitattributes (commit that file). Git takes\n" +
			"drivers only from local config, since they run commands, so every clone runs this\n" +
			"once; a clone that has not falls back to git's line merge.\n\n" +
			"Records merge by id. Generated views keep our side, then fail the gate until\n" +
			"`TRACKER index` regenerates them from the merged records.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, rel, err := trackerLocation()
			if err != nil {
				return err
			}
			o := out{cmd.OutOrStdout()}
			wrote, err := setUpMerge(root, rel, command)
			if err != nil {
				return err
			}
			if wrote {
				o.line("added the tracker merge attributes to .gitattributes; commit it")
			}
			o.line("merge drivers defined in .git/config")
			return nil
		},
	}
	cmd.Flags().StringVar(&command, "command", "", "the driver command git runs (default: this binary's merge-driver command)")
	return cmd
}

// setUpMerge installs the drivers and the attributes that route to them.
func setUpMerge(root, rel, command string) (bool, error) {
	if command == "" {
		exe, err := os.Executable()
		if err != nil {
			return false, err
		}
		if exe, err = filepath.EvalSymlinks(exe); err != nil {
			return false, err
		}
		// The program is this binary; what follows its name in the
		// invocation ("tasks" in "mytool tasks") leads to merge-driver.
		_, sub, _ := strings.Cut(hooks.Invocation, " ")
		if command, err = driverCommand(root, exe, sub); err != nil {
			return false, err
		}
	}
	if err := taskroll.InstallMergeDriver(root, command); err != nil {
		return false, err
	}
	lines := taskroll.Attributes(rel)
	for _, v := range hooks.Views {
		lines = append(lines, filepath.ToSlash(filepath.Join(rel, v))+" merge="+taskroll.ViewDriver)
	}
	if p, err := openAt(filepath.Join(root, rel)); err == nil {
		for _, c := range p.Settings.Collections {
			lines = append(lines, filepath.ToSlash(filepath.Join(rel, c.Dir))+"/*.md merge="+taskroll.ViewDriver)
		}
	}
	return taskroll.EnsureAttributes(root, lines)
}

// trackerLocation returns the repository root and the tracker directory
// relative to it.
func trackerLocation() (root, rel string, err error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	root, cfg, err := taskroll.FindConfig(wd)
	if err == nil {
		return root, cfg.Dir, nil
	}
	if !errors.Is(err, taskroll.ErrNoConfig) {
		return "", "", err
	}
	dir, err := trackerDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Dir(filepath.Dir(dir)), taskroll.DefaultDir, nil
}

// driverCommand is the command git runs for the record merge, before git
// appends %O %A %B %P.
//
// Git keeps one config for every worktree of a clone, and runs a driver from
// the top of the worktree that is merging. A binary inside the repository is
// therefore named relative to it, so each worktree runs its own build and
// removing a worktree does not break merges in the others. When the binary
// is missing (not built yet, or cleaned), the command falls back to git's
// line merge: a driver that cannot start leaves ours without conflict
// markers, and a careless `git add` would then drop their side silently.
func driverCommand(root, exe, sub string) (string, error) {
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if rel, err := filepath.Rel(root, exe); err == nil && filepath.IsLocal(rel) {
		exe = "./" + filepath.ToSlash(rel)
	}
	words := strings.Fields(sub + " merge-driver")
	for _, w := range words {
		if !safeWord.MatchString(w) {
			return "", fmt.Errorf("invocation word %q cannot go in the driver command; pass --command", w)
		}
	}
	script := `test -x "$0" || exec git merge-file -L ours -L base -L theirs "$2" "$1" "$3"; ` +
		`exec "$0" ` + strings.Join(words, " ") + ` "$1" "$2" "$3" "$4"`
	return "sh -c '" + script + "' " + shellQuote(exe), nil
}

var safeWord = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func shellQuote(s string) string {
	if !strings.ContainsAny(s, " '\"$\\`") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
