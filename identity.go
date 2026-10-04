package taskroll

import (
	"os"
	"os/exec"
	"strings"
)

// UserKey is the git config key that names the tracker user, when the git
// identity is not the name a team wants on its records.
const UserKey = "taskroll.user"

// Actor returns who is acting: TASKROLL_USER, then git config taskroll.user,
// then git config user.name, then the login name. It never fails; a record
// with an approximate author beats a command that refuses to run.
func Actor(repoDir string) string {
	if u := strings.TrimSpace(os.Getenv("TASKROLL_USER")); u != "" {
		return u
	}
	for _, key := range []string{UserKey, "user.name"} {
		if v := gitConfig(repoDir, key); v != "" {
			return v
		}
	}
	return os.Getenv("USER")
}

// SetUser stores the tracker user in the repository's local git config, which
// is per clone and never committed.
func SetUser(repoDir, name string) error {
	cmd := exec.Command("git", "config", "--local", UserKey, name)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &gitError{strings.TrimSpace(string(out)), err}
	}
	return nil
}

func gitConfig(dir, key string) string {
	cmd := exec.Command("git", "config", "--get", key)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

type gitError struct {
	out string
	err error
}

func (e *gitError) Error() string { return "git config: " + e.err.Error() + ": " + e.out }
func (e *gitError) Unwrap() error { return e.err }
