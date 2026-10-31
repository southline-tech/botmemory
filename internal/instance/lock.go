// Package instance enforces the spec's single-instance rule with an
// exclusive, non-blocking flock on a lock file. The kernel releases it when
// the process exits, so a crash never leaves a stale lock.
package instance

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
)

var ErrRunning = errors.New("another botmemory instance holds the lock")

// ErrNotLockFile: the lock path names an existing, non-empty file whose
// contents are not a botmemory lock (a PID line). It is never truncated.
var ErrNotLockFile = errors.New("refusing to use an existing non-lock file as a lock")

// ErrUnsafeDir: a lock directory is not a real directory owned by us with
// no group/other permissions.
var ErrUnsafeDir = errors.New("unsafe lock directory")

var pidLine = regexp.MustCompile(`^[0-9]{1,10}\n?$`)

// SecureDir creates dir (mode 0700) if missing and checks that it is a real
// directory (not a symlink), owned by the current user, with no group or
// other permission bits.
func SecureDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("%w: %q is not absolute", ErrUnsafeDir, dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("lock dir %s: %w", dir, err)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("lock dir %s: %w", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrUnsafeDir, dir)
	}
	if !dirOwnedByUs(fi) {
		return fmt.Errorf("%w: %s is not owned by the current user", ErrUnsafeDir, dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: %s has mode %v (want 0700)", ErrUnsafeDir, dir, fi.Mode().Perm())
	}
	return nil
}

// UserDir is the per-user botmemory lock directory, <home>/.cache/botmemory,
// where <home> is the passwd home directory of the current uid. It does NOT
// consult $HOME, $XDG_RUNTIME_DIR or $XDG_CACHE_HOME, so cron, ssh -c and
// agent shells of the same user all get the same path. Created 0700 and
// checked by SecureDir.
func UserDir() (string, error) {
	u, err := user.LookupId(strconv.Itoa(os.Getuid()))
	if err != nil {
		return "", fmt.Errorf("no per-user lock dir: %w", err)
	}
	return userDirIn(u.HomeDir)
}

func userDirIn(home string) (string, error) {
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("%w: home %q is not absolute", ErrUnsafeDir, home)
	}
	d := filepath.Join(home, ".cache", "botmemory")
	if err := SecureDir(d); err != nil {
		return "", err
	}
	return d, nil
}

type Lock struct{ f *os.File }

func Acquire(path string) (*Lock, error) {
	// O_NOFOLLOW: a planted symlink must not redirect our write to another
	// file. No O_TRUNC: an existing file is only rewritten once it is known
	// to be a regular file that is empty or holds a PID line.
	// Lstat first and O_NONBLOCK: a FIFO or device is never opened in a way
	// that can block, and is refused before any write.
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrNotLockFile, path)
	}
	f, err := openLockFile(path)
	if err != nil {
		return nil, fmt.Errorf("lock file %s: %w", path, err)
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrNotLockFile, path)
	}
	if fi.Size() > 0 {
		buf, err := io.ReadAll(io.LimitReader(f, 64))
		if err != nil || fi.Size() > 64 || !pidLine.Match(buf) {
			f.Close()
			return nil, fmt.Errorf("%w: %s", ErrNotLockFile, path)
		}
	}
	if err := fileLock(f, true); err != nil {
		f.Close()
		return nil, err
	}
	f.Truncate(0)
	f.WriteAt([]byte(fmt.Sprintf("%d\n", os.Getpid())), 0)
	return &Lock{f}, nil
}

func (l *Lock) Release() error {
	_ = fileUnlock(l.f)
	return l.f.Close()
}
