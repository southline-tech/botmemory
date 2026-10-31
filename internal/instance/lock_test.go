package instance

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSingleInstance(t *testing.T) {
	p := filepath.Join(t.TempDir(), "botmemory.lock")
	l, err := Acquire(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(p); !errors.Is(err, ErrRunning) {
		t.Fatalf("second instance allowed: %v", err)
	}
	l.Release()
	l2, err := Acquire(p)
	if err != nil {
		t.Fatal("not reacquirable after release:", err)
	}
	l2.Release()
}

// M3: a symlink at the lock path is refused and its target is untouched.
func TestLockRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "victim")
	if err := os.WriteFile(target, []byte("precious\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "botmemory.lock")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if l, err := Acquire(link); err == nil {
		l.Release()
		t.Fatal("Acquire followed a symlink")
	}
	if b, _ := os.ReadFile(target); string(b) != "precious\n" {
		t.Fatalf("symlink target modified: %q", b)
	}
}

// R3-1: an existing non-empty non-lock file is refused and never truncated;
// an empty file or a stale PID-line lock is reused.
func TestLockRefusesNonLockFile(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(victim, []byte("precious data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if l, err := Acquire(victim); !errors.Is(err, ErrNotLockFile) {
		if l != nil {
			l.Release()
		}
		t.Fatalf("non-lock file accepted: %v", err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "precious data\n" {
		t.Fatalf("target modified: %q", b)
	}
	for name, content := range map[string]string{"empty": "", "stale": "12345\n"} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(content), 0o600)
		l, err := Acquire(p)
		if err != nil {
			t.Fatalf("%s lock refused: %v", name, err)
		}
		l.Release()
	}
	if l, err := Acquire(dir); err == nil {
		l.Release()
		t.Fatal("directory accepted as lock")
	}
}

// R3-2: SecureDir creates 0700 dirs and refuses loose modes and symlinks.
func TestSecureDir(t *testing.T) {
	base := t.TempDir()
	d := filepath.Join(base, "botmemory")
	if err := SecureDir(d); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(d); fi.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	loose := filepath.Join(base, "loose")
	os.Mkdir(loose, 0o700)
	os.Chmod(loose, 0o777)
	if err := SecureDir(loose); !errors.Is(err, ErrUnsafeDir) {
		t.Fatalf("world-writable dir accepted: %v", err)
	}
	link := filepath.Join(base, "link")
	os.Symlink(d, link)
	if err := SecureDir(link); !errors.Is(err, ErrUnsafeDir) {
		t.Fatalf("symlinked dir accepted: %v", err)
	}
	if err := SecureDir("relative/botmemory"); !errors.Is(err, ErrUnsafeDir) {
		t.Fatalf("relative dir accepted: %v", err)
	}
	if _, err := userDirIn("home/x"); !errors.Is(err, ErrUnsafeDir) {
		t.Fatalf("relative home accepted: %v", err)
	}
	if u, err := userDirIn(base); err != nil || u != filepath.Join(base, ".cache", "botmemory") {
		t.Fatalf("userDirIn = %q, %v", u, err)
	}
}

// R4-1: UserDir ignores XDG_RUNTIME_DIR / XDG_CACHE_HOME / HOME.
func TestUserDirStable(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	os.Unsetenv("XDG_RUNTIME_DIR")
	a, err := UserDir()
	if err != nil || !filepath.IsAbs(a) {
		t.Fatalf("UserDir unset: %q %v", a, err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	b, err := UserDir()
	if err != nil || a != b {
		t.Fatalf("UserDir changed with env: %q vs %q (%v)", a, b, err)
	}
}
