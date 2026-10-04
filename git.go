package taskroll

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// idToken is any series ID in text. It over-matches on purpose (a prose
// mention of an ID reserves it too): allocation only needs an upper bound per
// series, and skipping a number is harmless where reusing one is not.
var idToken = regexp.MustCompile(`\b[A-Za-z]+-[0-9]{3}(?:\.[0-9]+)?\b`)

// ElsewhereIDs returns the IDs in the tracker directory as every other branch
// and worktree of the repository sees it: local and remote-tracking branches,
// and the uncommitted files of other worktrees, which is where a parallel
// agent's new records sit before it commits. Allocating past them is what
// keeps two branches from issuing one number for different work.
//
// Outside a git repository it returns nothing: there are no other branches.
func ElsewhereIDs(trackerDir string) (map[string]bool, error) {
	ids := map[string]bool{}
	abs, err := filepath.Abs(trackerDir)
	if err != nil {
		return nil, err
	}
	top, err := git(abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return ids, nil //nolint:nilerr // not a repository: nothing elsewhere
	}
	top = strings.TrimSpace(top)
	if real, err := filepath.EvalSymlinks(top); err == nil {
		top = real
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	rel, err := filepath.Rel(top, abs)
	if err != nil {
		return nil, err
	}

	refs, err := git(abs, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, r := range strings.Fields(refs) {
		if !strings.HasSuffix(r, "/HEAD") {
			names = append(names, r)
		}
	}
	if len(names) > 0 {
		args := append([]string{"grep", "-h", "-o", "-I", "-E", `[A-Za-z]+-[0-9]{3}(\.[0-9]+)?`}, names...)
		args = append(args, "--", filepath.ToSlash(rel))
		found, err := gitGrep(top, args...)
		if err != nil {
			return nil, err
		}
		for _, id := range idToken.FindAllString(found, -1) {
			ids[id] = true
		}
	}

	wts, err := git(abs, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(strings.NewReader(wts))
	for sc.Scan() {
		path, ok := strings.CutPrefix(sc.Text(), "worktree ")
		if !ok {
			continue
		}
		if real, err := filepath.EvalSymlinks(path); err == nil {
			path = real
		}
		if path == top {
			continue
		}
		if err := scanIDs(filepath.Join(path, rel), ids); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// scanIDs adds the IDs in a tracker directory's JSONL and markdown files. A
// worktree that does not have the directory (an older branch) adds nothing.
func scanIDs(dir string, ids map[string]bool) error {
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || (filepath.Ext(p) != ".jsonl" && filepath.Ext(p) != ".md") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, id := range idToken.FindAllString(string(raw), -1) {
			ids[id] = true
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// gitGrep treats exit status 1, no match, as an empty result.
func gitGrep(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) && ee.ExitCode() == 1 && stderr.Len() == 0 {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("git grep: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// Merge driver names, as .gitattributes refers to them.
const (
	// MergeDriver merges the JSONL records with Merge.
	MergeDriver = "taskroll"
	// ViewDriver keeps our side of a generated view; the gate then fails
	// until the views are regenerated from the merged records, which is
	// right, where conflict markers in generated text are not.
	ViewDriver = "taskroll-view"
)

// Attributes are the .gitattributes lines that route a tracker directory's
// files to the drivers. Without the drivers configured, git ignores the
// names and falls back to its line merge, so the lines are safe to commit.
func Attributes(dir string) []string {
	dir = filepath.ToSlash(filepath.Clean(dir))
	return []string{
		dir + "/data/*.jsonl merge=" + MergeDriver,
		dir + "/epics/*.md merge=" + ViewDriver,
	}
}

// InstallMergeDriver defines the drivers in the clone's local git config.
// Drivers run commands, so git never takes them from a committed file; each
// clone opts in. command is the tracker binary and its subcommand, which is
// given %O %A %B %P.
func InstallMergeDriver(repoDir, command string) error {
	sets := [][2]string{
		{"merge." + MergeDriver + ".name", "tracker records, merged by id"},
		{"merge." + MergeDriver + ".driver", command + " %O %A %B %P"},
		{"merge." + ViewDriver + ".name", "generated tracker view: keep ours, then regenerate"},
		{"merge." + ViewDriver + ".driver", "true"},
	}
	for _, kv := range sets {
		if _, err := git(repoDir, "config", "--local", kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}

// EnsureAttributes appends to root/.gitattributes whichever of lines it does
// not already hold, and reports whether it wrote.
func EnsureAttributes(root string, lines []string) (bool, error) {
	path := filepath.Join(root, ".gitattributes")
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	have := map[string]bool{}
	for l := range strings.SplitSeq(string(raw), "\n") {
		have[strings.TrimSpace(l)] = true
	}
	var add []string
	for _, l := range lines {
		if !have[l] {
			add = append(add, l)
		}
	}
	if len(add) == 0 {
		return false, nil
	}
	if len(raw) > 0 && !bytes.HasSuffix(raw, []byte("\n")) {
		raw = append(raw, '\n')
	}
	raw = append(raw, []byte(strings.Join(add, "\n")+"\n")...)
	return true, WriteFileAtomic(path, raw)
}

// MainlineFile returns path as the default branch has it: the remote's HEAD
// branch when there is one, since that is what others have seen, else a
// local main or master. It returns nil when none has the file.
func MainlineFile(repoDir, path string) []byte {
	var refs []string
	if head, err := git(repoDir, "symbolic-ref", "-q", "refs/remotes/origin/HEAD"); err == nil {
		refs = append(refs, strings.TrimSpace(head))
	}
	refs = append(refs, "refs/remotes/origin/main", "refs/heads/main", "refs/heads/master")
	for _, ref := range refs {
		if raw, err := git(repoDir, "show", ref+":"+filepath.ToSlash(path)); err == nil {
			return []byte(raw)
		}
	}
	return nil
}
