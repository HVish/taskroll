package taskroll

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// lockTimeout is how long a writer waits for another to finish. Every write
// is milliseconds; anything this long is a hung process, and failing loudly
// beats waiting forever.
const lockTimeout = 30 * time.Second

// lockPath is outside the repository, so it never shows up in git status or
// needs ignoring. Inside git it is keyed by the repository's common directory
// and the tracker's path within the worktree, so every worktree of one clone
// shares it: allocation reads the other worktrees' uncommitted records, and
// two worktrees allocating at once would otherwise both see the same highest
// id. Outside git it is keyed by the tracker directory's absolute path.
func lockPath(trackerDir string) (string, error) {
	abs, err := filepath.Abs(trackerDir)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	key := abs
	if common, rel, ok := repoKey(abs); ok {
		key = common + "\x00" + rel
	}
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(os.TempDir(), "taskroll-"+hex.EncodeToString(sum[:8])+".lock"), nil
}

// repoKey returns the git common directory, which every worktree of a clone
// shares, and dir's path relative to its worktree's top.
func repoKey(dir string) (common, rel string, ok bool) {
	out, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir", "--show-toplevel")
	if err != nil {
		return "", "", false
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		return "", "", false
	}
	common, top := lines[0], lines[1]
	for _, p := range []*string{&common, &top} {
		if real, err := filepath.EvalSymlinks(*p); err == nil {
			*p = real
		}
	}
	rel, err = filepath.Rel(top, dir)
	if err != nil {
		return "", "", false
	}
	return common, filepath.ToSlash(rel), true
}

// Lock takes the tracker directory's exclusive write lock and returns the
// function that releases it. Two processes that each read the records,
// change one and write them back would otherwise lose one change silently.
//
// It is an flock on a file, so the kernel releases it when the holder exits,
// however it exits: there is no stale lock to clean up after a crash.
func Lock(trackerDir string) (func(), error) {
	path, err := lockPath(trackerDir)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("taskroll lock: %w", err)
	}
	deadline := time.Now().Add(lockTimeout)
	for {
		err := flockNB(f)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("taskroll lock %s: another write has held it for %s: %w", path, lockTimeout, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

func flockNB(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }
